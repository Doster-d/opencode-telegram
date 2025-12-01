package bot

import (
	"sync"
	"testing"
	"time"
)

func TestDebouncer_CallsWithLatestValue(t *testing.T) {
	db := NewDebouncer(50 * time.Millisecond)

	var result string
	var mu sync.Mutex
	done := make(chan bool, 1)

	fn := func(text string) error {
		mu.Lock()
		result = text
		mu.Unlock()
		done <- true
		return nil
	}

	// Queue multiple updates for same key; only last should be called
	db.Debounce("key1", "first", fn)
	time.Sleep(10 * time.Millisecond)
	db.Debounce("key1", "second", fn)
	time.Sleep(10 * time.Millisecond)
	db.Debounce("key1", "final", fn)

	// Wait for callback
	select {
	case <-done:
		mu.Lock()
		defer mu.Unlock()
		if result != "final" {
			t.Fatalf("expected final, got %q", result)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for debounce callback")
	}
}

func TestDebouncer_RespectsTiming(t *testing.T) {
	db := NewDebouncer(100 * time.Millisecond)

	var callCount int
	var mu sync.Mutex
	done := make(chan bool, 1)

	fn := func(text string) error {
		mu.Lock()
		callCount++
		mu.Unlock()
		done <- true
		return nil
	}

	start := time.Now()
	db.Debounce("key", "text", fn)

	// Wait for callback
	<-done
	elapsed := time.Since(start)

	mu.Lock()
	defer mu.Unlock()

	// Should be called after ~100ms (allow some tolerance)
	if elapsed < 90*time.Millisecond || elapsed > 200*time.Millisecond {
		t.Fatalf("expected callback around 100ms, got %v", elapsed)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 call, got %d", callCount)
	}
}

func TestDebouncer_MultipleKeysIsolated(t *testing.T) {
	db := NewDebouncer(50 * time.Millisecond)

	results := make(map[string]string)
	var mu sync.Mutex
	done := make(chan string, 2)

	mkFn := func(key string) func(string) error {
		return func(text string) error {
			mu.Lock()
			results[key] = text
			mu.Unlock()
			done <- key
			return nil
		}
	}

	// Queue different keys
	db.Debounce("key1", "val1", mkFn("key1"))
	db.Debounce("key2", "val2", mkFn("key2"))

	// Wait for both
	got := make(map[string]bool)
	for i := 0; i < 2; i++ {
		select {
		case k := <-done:
			got[k] = true
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("timeout waiting for key %d", i+1)
		}
	}

	mu.Lock()
	defer mu.Unlock()

	if !got["key1"] || !got["key2"] {
		t.Fatalf("not all keys received: %v", got)
	}
	if results["key1"] != "val1" || results["key2"] != "val2" {
		t.Fatalf("values mismatch: %v", results)
	}
}

func TestDebouncer_CancelsOnNewCall(t *testing.T) {
	db := NewDebouncer(100 * time.Millisecond)

	var callCount int
	var mu sync.Mutex
	done := make(chan bool, 1)

	fn := func(text string) error {
		mu.Lock()
		callCount++
		mu.Unlock()
		done <- true
		return nil
	}

	db.Debounce("key", "first", fn)
	time.Sleep(50 * time.Millisecond)
	db.Debounce("key", "second", fn)  // should reset timer
	time.Sleep(60 * time.Millisecond) // total 110ms since first call, but only 60ms since second

	// Should get callback from second call
	select {
	case <-done:
		mu.Lock()
		defer mu.Unlock()
		if callCount != 1 {
			t.Fatalf("expected 1 call total, got %d", callCount)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for debounce callback")
	}
}
