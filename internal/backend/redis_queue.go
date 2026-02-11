package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

const (
	// Redis keys
	queueKeyPrefix      = "oct:cmd:"
	inflightKeyPrefix   = "oct:inflight:"
	inflightAtKeyPrefix = "oct:inflight_at:"
	resultKeyPrefix     = "oct:result:"
)

// RedisClient defines the interface for Redis-like operations
// This allows swapping between real Redis and in-memory implementations
type RedisClient interface {
	LPush(ctx context.Context, key string, values ...interface{}) error
	BRPopLPush(ctx context.Context, source, destination string, timeout time.Duration) (string, error)
	LRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	LRem(ctx context.Context, key string, count int64, value interface{}) error
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, keys ...string) error
	HSet(ctx context.Context, key string, values ...interface{}) error
	HGet(ctx context.Context, key, field string) (string, error)
	HDel(ctx context.Context, key string, fields ...string) error
	Expire(ctx context.Context, key string, expiration time.Duration) error
}

// InMemoryRedisClient provides an in-memory implementation of RedisClient for testing
type InMemoryRedisClient struct {
	mu       sync.Mutex
	lists    map[string][]string
	values   map[string]string
	hashes   map[string]map[string]string
	expiries map[string]time.Time
	now      func() time.Time
}

// NewInMemoryRedisClient creates a new in-memory Redis client
func NewInMemoryRedisClient() *InMemoryRedisClient {
	return &InMemoryRedisClient{
		lists:    make(map[string][]string),
		values:   make(map[string]string),
		hashes:   make(map[string]map[string]string),
		expiries: make(map[string]time.Time),
		now:      time.Now,
	}
}

func (c *InMemoryRedisClient) SetClock(nowFn func() time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = nowFn
}

func (c *InMemoryRedisClient) LPush(ctx context.Context, key string, values ...interface{}) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()

	vals := make([]string, len(values))
	for i, v := range values {
		switch val := v.(type) {
		case []byte:
			vals[i] = string(val)
		case string:
			vals[i] = val
		default:
			vals[i] = fmt.Sprintf("%v", v)
		}
	}

	// LPUSH adds values to the HEAD (left) of the list
	// Redis behavior: LPUSH x a b c results in [c, b, a]
	// We iterate values in reverse order to achieve this
	for i := len(vals) - 1; i >= 0; i-- {
		c.lists[key] = append([]string{vals[i]}, c.lists[key]...)
	}
	return nil
}

func (c *InMemoryRedisClient) BRPopLPush(ctx context.Context, source, destination string, timeout time.Duration) (string, error) {
	_ = ctx

	start := time.Now()
	for {
		c.mu.Lock()
		if list, ok := c.lists[source]; ok && len(list) > 0 {
			// RPOP takes from the TAIL (last element)
			val := list[len(list)-1]
			c.lists[source] = list[:len(list)-1]
			// LPUSH to HEAD of destination
			c.lists[destination] = append([]string{val}, c.lists[destination]...)
			c.mu.Unlock()
			return val, nil
		}
		c.mu.Unlock()

		if time.Since(start) >= timeout {
			return "", errors.New("redis: nil")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (c *InMemoryRedisClient) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()

	list := c.lists[key]
	if list == nil {
		return []string{}, nil
	}

	// Handle negative indices
	if start < 0 {
		start = int64(len(list)) + start
		if start < 0 {
			start = 0
		}
	}
	if stop < 0 {
		stop = int64(len(list)) + stop
		if stop < 0 {
			return []string{}, nil
		}
	}

	// Clamp to valid range
	if start >= int64(len(list)) {
		return []string{}, nil
	}
	if stop >= int64(len(list)) {
		stop = int64(len(list)) - 1
	}

	result := make([]string, 0, stop-start+1)
	for i := start; i <= stop; i++ {
		result = append(result, list[i])
	}
	return result, nil
}

func (c *InMemoryRedisClient) LRem(ctx context.Context, key string, count int64, value interface{}) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()

	list := c.lists[key]
	if list == nil {
		return nil
	}

	valStr := fmt.Sprintf("%v", value)
	var removed int
	var result []string

	if count > 0 {
		// Remove first count occurrences from head
		for _, item := range list {
			if removed < int(count) && item == valStr {
				removed++
				continue
			}
			result = append(result, item)
		}
	} else if count < 0 {
		// Remove first abs(count) occurrences from tail
		// Collect indices from end
		toRemove := []int{}
		for i := len(list) - 1; i >= 0 && len(toRemove) < int(-count); i-- {
			if list[i] == valStr {
				toRemove = append(toRemove, i)
			}
		}
		removeSet := make(map[int]bool)
		for _, idx := range toRemove {
			removeSet[idx] = true
		}
		for i, item := range list {
			if !removeSet[i] {
				result = append(result, item)
			}
		}
	} else {
		// Remove all occurrences
		for _, item := range list {
			if item != valStr {
				result = append(result, item)
			}
		}
	}

	c.lists[key] = result
	return nil
}

func (c *InMemoryRedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()

	switch val := value.(type) {
	case []byte:
		c.values[key] = string(val)
	case string:
		c.values[key] = val
	default:
		c.values[key] = fmt.Sprintf("%v", value)
	}
	if expiration > 0 {
		c.expiries[key] = c.now().Add(expiration)
	}
	return nil
}

func (c *InMemoryRedisClient) Get(ctx context.Context, key string) (string, error) {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()

	if expiry, ok := c.expiries[key]; ok && c.now().After(expiry) {
		delete(c.lists, key)
		delete(c.values, key)
		delete(c.expiries, key)
		return "", errors.New("redis: nil")
	}

	if val, ok := c.values[key]; ok {
		return val, nil
	}
	return "", errors.New("redis: nil")
}

func (c *InMemoryRedisClient) Del(ctx context.Context, keys ...string) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, key := range keys {
		delete(c.lists, key)
		delete(c.values, key)
		delete(c.hashes, key)
		delete(c.expiries, key)
	}
	return nil
}

func (c *InMemoryRedisClient) HSet(ctx context.Context, key string, values ...interface{}) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(values)%2 != 0 {
		return errors.New("hset requires even number of values")
	}
	if _, ok := c.hashes[key]; !ok {
		c.hashes[key] = make(map[string]string)
	}
	if expiry, ok := c.expiries[key]; ok && c.now().After(expiry) {
		delete(c.hashes, key)
		delete(c.expiries, key)
		c.hashes[key] = make(map[string]string)
	}
	for i := 0; i < len(values); i += 2 {
		field := fmt.Sprintf("%v", values[i])
		val := fmt.Sprintf("%v", values[i+1])
		c.hashes[key][field] = val
	}
	return nil
}

func (c *InMemoryRedisClient) HGet(ctx context.Context, key, field string) (string, error) {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()

	if expiry, ok := c.expiries[key]; ok && c.now().After(expiry) {
		delete(c.hashes, key)
		delete(c.expiries, key)
		return "", errors.New("redis: nil")
	}

	fields, ok := c.hashes[key]
	if !ok {
		return "", errors.New("redis: nil")
	}
	val, ok := fields[field]
	if !ok {
		return "", errors.New("redis: nil")
	}
	return val, nil
}

func (c *InMemoryRedisClient) HDel(ctx context.Context, key string, fields ...string) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.hashes[key]; !ok {
		return nil
	}
	for _, field := range fields {
		delete(c.hashes[key], field)
	}
	if len(c.hashes[key]) == 0 {
		delete(c.hashes, key)
	}
	return nil
}

func (c *InMemoryRedisClient) Expire(ctx context.Context, key string, expiration time.Duration) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	if expiration <= 0 {
		delete(c.expiries, key)
		return nil
	}
	c.expiries[key] = c.now().Add(expiration)
	return nil
}

// RedisQueue implements CommandQueue using Redis for at-least-once delivery
type RedisQueue struct {
	client        RedisClient
	redeliveryTTL time.Duration
	now           func() time.Time
}

// NewRedisQueue creates a new Redis-backed command queue
func NewRedisQueue(client RedisClient) *RedisQueue {
	return &RedisQueue{
		client:        client,
		redeliveryTTL: DefaultRedeliveryTTL,
		now:           time.Now,
	}
}

// SetClock sets the clock function (for testing)
func (q *RedisQueue) SetClock(nowFn func() time.Time) {
	q.now = nowFn
}

func (q *RedisQueue) queueKey(agentID string) string {
	return queueKeyPrefix + agentID
}

func (q *RedisQueue) inflightKey(agentID string) string {
	return inflightKeyPrefix + agentID
}

func (q *RedisQueue) inflightAtKey(agentID string) string {
	return inflightAtKeyPrefix + agentID
}

func (q *RedisQueue) resultKey(agentID, commandID string) string {
	return fmt.Sprintf("%s%s:%s", resultKeyPrefix, agentID, commandID)
}

// Enqueue adds a command to the queue using LPUSH
func (q *RedisQueue) Enqueue(ctx context.Context, agentID string, cmd contracts.Command) error {
	if agentID == "" {
		return errors.New("agentID is required")
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal command: %w", err)
	}
	return q.client.LPush(ctx, q.queueKey(agentID), data)
}

// Poll waits for a command using BRPOPLPUSH from queue to inflight with timeout
// It also checks for stale inflight commands (older than redeliveryTTL) and returns them first
func (q *RedisQueue) Poll(ctx context.Context, agentID string, timeoutSeconds int) (*contracts.Command, error) {
	if agentID == "" {
		return nil, errors.New("agentID is required")
	}

	// First, check for stale inflight commands to redeliver
	staleCmd, err := q.findStaleInflight(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if staleCmd != nil {
		// Update inflight timestamp
		if err := q.setInflightTimestamp(ctx, agentID, staleCmd.CommandID); err != nil {
			return nil, err
		}
		return staleCmd, nil
	}

	// Use BRPOPLPUSH to atomically move from queue to inflight with timeout
	timeout := time.Duration(timeoutSeconds) * time.Second
	result, err := q.client.BRPopLPush(ctx, q.queueKey(agentID), q.inflightKey(agentID), timeout)
	if err != nil && err.Error() == "redis: nil" {
		// Timeout with no command available
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("brpoplpush: %w", err)
	}

	var cmd contracts.Command
	if err := json.Unmarshal([]byte(result), &cmd); err != nil {
		return nil, fmt.Errorf("unmarshal command: %w", err)
	}

	// Set inflight timestamp for redelivery tracking
	if err := q.setInflightTimestamp(ctx, agentID, cmd.CommandID); err != nil {
		return nil, err
	}

	return &cmd, nil
}

// StoreResult removes the command from inflight using LREM
func (q *RedisQueue) StoreResult(ctx context.Context, agentID string, result contracts.CommandResult) error {
	if agentID == "" {
		return errors.New("agentID is required")
	}
	if result.CommandID == "" {
		return contracts.APIError{Code: contracts.ErrValidationRequiredField, Message: "command_id is required"}
	}

	// Remove from inflight list
	_, err := q.removeFromInflight(ctx, agentID, result.CommandID)
	if err != nil {
		return err
	}

	// Delete inflight timestamp from hash
	_ = q.client.HDel(ctx, q.inflightAtKey(agentID), result.CommandID)

	// Store result with TTL
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	if err := q.client.Set(ctx, q.resultKey(agentID, result.CommandID), data, 14*24*time.Hour); err != nil {
		return fmt.Errorf("store result: %w", err)
	}

	return nil
}

func (q *RedisQueue) GetResult(ctx context.Context, agentID string, commandID string) (*contracts.CommandResult, error) {
	if agentID == "" || commandID == "" {
		return nil, nil
	}
	val, err := q.client.Get(ctx, q.resultKey(agentID, commandID))
	if err != nil {
		if err.Error() == "redis: nil" {
			return nil, nil
		}
		return nil, err
	}
	var out contracts.CommandResult
	if err := json.Unmarshal([]byte(val), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// findStaleInflight looks for inflight commands older than redeliveryTTL and returns the first one
func (q *RedisQueue) findStaleInflight(ctx context.Context, agentID string) (*contracts.Command, error) {
	now := q.now().UTC()
	cutoff := now.Add(-q.redeliveryTTL)

	// Get all inflight commands
	// LRange 0 -1 returns items from head (left) to tail (right)
	items, err := q.client.LRange(ctx, q.inflightKey(agentID), 0, -1)
	if err != nil {
		return nil, fmt.Errorf("lrange inflight: %w", err)
	}

	// Track the oldest stale command
	var oldestStale *contracts.Command
	var oldestInflightAt time.Time

	for _, item := range items {
		var cmd contracts.Command
		if err := json.Unmarshal([]byte(item), &cmd); err != nil {
			continue // Skip malformed entries
		}

		// Check inflight timestamp
		timestampStr, err := q.client.HGet(ctx, q.inflightAtKey(agentID), cmd.CommandID)
		if err != nil && err.Error() != "redis: nil" {
			continue // Skip on error
		}
		if timestampStr == "" {
			continue // No timestamp, assume fresh
		}

		inflightAt, err := time.Parse(time.RFC3339Nano, timestampStr)
		if err != nil {
			continue // Skip malformed timestamp
		}

		if inflightAt.Before(cutoff) {
			// Found stale command - it's eligible for redelivery
			// Track the oldest one
			if oldestStale == nil || inflightAt.Before(oldestInflightAt) {
				oldestStale = &cmd
				oldestInflightAt = inflightAt
			}
		}
	}

	if oldestStale != nil {
		// Update inflight timestamp and return it
		// The command stays in the inflight list - this ensures consistent state
		if err := q.setInflightTimestamp(ctx, agentID, oldestStale.CommandID); err != nil {
			return nil, err
		}
		return oldestStale, nil
	}

	return nil, nil
}

// removeFromInflight removes a command by CommandID from the inflight list
func (q *RedisQueue) removeFromInflight(ctx context.Context, agentID, commandID string) (string, error) {
	items, err := q.client.LRange(ctx, q.inflightKey(agentID), 0, -1)
	if err != nil {
		return "", fmt.Errorf("lrange inflight: %w", err)
	}

	for _, item := range items {
		var cmd contracts.Command
		if err := json.Unmarshal([]byte(item), &cmd); err != nil {
			continue // Skip malformed entries
		}
		if cmd.CommandID == commandID {
			// Remove this item from the inflight list
			if err := q.client.LRem(ctx, q.inflightKey(agentID), 1, item); err != nil {
				return "", fmt.Errorf("lrem: %w", err)
			}
			return item, nil
		}
	}

	// Not found - already removed or never existed
	return "", nil
}

func (q *RedisQueue) setInflightTimestamp(ctx context.Context, agentID, commandID string) error {
	key := q.inflightAtKey(agentID)
	if err := q.client.HSet(ctx, key, commandID, q.now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return q.client.Expire(ctx, key, q.redeliveryTTL*2)
}
