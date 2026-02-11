package agent

import (
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

type IdempotencyCache struct {
	maxEntries int
	ttl        time.Duration
	now        func() time.Time

	entries map[string]cacheEntry
	order   []string
}

type cacheEntry struct {
	Result    contracts.CommandResult
	ExpiresAt time.Time
}

func NewIdempotencyCache(maxEntries int, ttl time.Duration, nowFn func() time.Time) *IdempotencyCache {
	if maxEntries <= 0 {
		maxEntries = 1
	}
	return &IdempotencyCache{
		maxEntries: maxEntries,
		ttl:        ttl,
		now:        nowFn,
		entries:    make(map[string]cacheEntry),
	}
}

func (c *IdempotencyCache) Get(key string) (contracts.CommandResult, bool) {
	if key == "" {
		return contracts.CommandResult{}, false
	}
	now := c.now().UTC()
	entry, ok := c.entries[key]
	if !ok {
		return contracts.CommandResult{}, false
	}
	if now.After(entry.ExpiresAt) {
		delete(c.entries, key)
		return contracts.CommandResult{}, false
	}
	return entry.Result, true
}

func (c *IdempotencyCache) Put(key string, result contracts.CommandResult) {
	if key == "" {
		return
	}
	c.pruneExpired()
	if _, exists := c.entries[key]; !exists {
		c.order = append(c.order, key)
	}
	c.entries[key] = cacheEntry{Result: result, ExpiresAt: c.now().UTC().Add(c.ttl)}
	for len(c.entries) > c.maxEntries && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		if _, ok := c.entries[oldest]; ok {
			delete(c.entries, oldest)
		}
	}
}

func (c *IdempotencyCache) pruneExpired() {
	now := c.now().UTC()
	for key, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			delete(c.entries, key)
		}
	}
}
