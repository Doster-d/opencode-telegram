package bot

import (
	"sync"
	"time"
)

// Debouncer holds pending operations per session and batches edits with a delay.
type Debouncer struct {
	mu      sync.Mutex
	pending map[string]*pendingEdit
	delay   time.Duration
}

type pendingEdit struct {
	timer *time.Timer
	text  string
}

func NewDebouncer(delay time.Duration) *Debouncer {
	return &Debouncer{
		pending: make(map[string]*pendingEdit),
		delay:   delay,
	}
}

// Debounce schedules a handler call after delay, cancelling any pending call for the same key.
// The handler is called with the latest text value after the delay expires.
func (d *Debouncer) Debounce(key string, text string, fn func(string) error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// cancel any existing timer for this key
	if pending, ok := d.pending[key]; ok {
		pending.timer.Stop()
	}

	// schedule new timer
	timer := time.AfterFunc(d.delay, func() {
		d.mu.Lock()
		pe, ok := d.pending[key]
		d.mu.Unlock()

		if ok && pe != nil {
			_ = fn(pe.text)
		}

		d.mu.Lock()
		delete(d.pending, key)
		d.mu.Unlock()
	})

	d.pending[key] = &pendingEdit{timer: timer, text: text}
}
