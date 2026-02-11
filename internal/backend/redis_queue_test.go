package backend

import (
	"context"
	"testing"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

type testClock struct {
	now time.Time
}

func (c *testClock) Now() time.Time { return c.now }

// TestRedisQueueRedelivery tests that stale inflight commands are redelivered
func TestRedisQueueRedelivery(t *testing.T) {
	clk := &testClock{now: time.Date(2026, 2, 10, 10, 0, 0, 0, time.UTC)}
	client := NewInMemoryRedisClient()
	client.SetClock(clk.Now)

	queue := NewRedisQueue(client)
	queue.SetClock(clk.Now)
	agentID := "agent-001"

	// Create a test command
	cmd := contracts.Command{
		CommandID:      "cmd-001",
		IdempotencyKey: "key-001",
		Type:           contracts.CommandTypeStatus,
		CreatedAt:      clk.now,
		Payload:        []byte(`{}`),
	}

	// Enqueue the command
	ctx := context.Background()
	if err := queue.Enqueue(ctx, agentID, cmd); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Poll should return the command
	polled, err := queue.Poll(ctx, agentID, 5)
	if err != nil {
		t.Fatalf("first poll: %v", err)
	}
	if polled == nil {
		t.Fatal("first poll: expected command, got nil")
	}
	if polled.CommandID != cmd.CommandID {
		t.Fatalf("first poll: expected command_id %s, got %s", cmd.CommandID, polled.CommandID)
	}

	// Debug: Check state before advancing time
	qitems, _ := client.LRange(ctx, "oct:cmd:agent-001", 0, -1)
	ifiles, _ := client.LRange(ctx, "oct:inflight:agent-001", 0, -1)
	t.Logf("Before time advance - Queue: %v, Inflight: %v", qitems, ifiles)

	// Advance time past redelivery TTL
	clk.now = clk.now.Add(121 * time.Second)
	t.Logf("Time advanced to: %s", clk.now)

	// Debug: Check inflight timestamp
	timestampKey := "oct:inflight_at:agent-001"
	timestampStr, _ := client.HGet(ctx, timestampKey, "cmd-001")
	t.Logf("Inflight timestamp: %s", timestampStr)

	// Poll should redeliver the stale inflight command
	redelivered, err := queue.Poll(ctx, agentID, 5)
	if err != nil {
		t.Fatalf("redelivery poll: %v", err)
	}
	if redelivered == nil {
		t.Fatal("redelivery poll: expected stale command, got nil")
	}
	if redelivered.CommandID != cmd.CommandID {
		t.Fatalf("redelivery poll: expected command_id %s, got %s", cmd.CommandID, redelivered.CommandID)
	}

	// Store result to clear inflight
	result := contracts.CommandResult{
		CommandID: cmd.CommandID,
		OK:        true,
		Summary:   "completed",
	}
	if err := queue.StoreResult(ctx, agentID, result); err != nil {
		t.Fatalf("store result: %v", err)
	}
	if stored, err := queue.GetResult(ctx, agentID, cmd.CommandID); err != nil {
		t.Fatalf("get result: %v", err)
	} else if stored == nil || stored.CommandID != cmd.CommandID {
		t.Fatalf("expected stored result, got %+v", stored)
	}

	// Poll should not redeliver after result is stored
	clk.now = clk.now.Add(121 * time.Second)
	afterResult, err := queue.Poll(ctx, agentID, 5)
	if err != nil {
		t.Fatalf("after result poll: %v", err)
	}
	if afterResult != nil {
		t.Fatalf("after result poll: expected nil (no command), got command_id %s", afterResult.CommandID)
	}
}

// TestRedisQueueMultipleCommands tests FIFO ordering
func TestRedisQueueMultipleCommands(t *testing.T) {
	clk := &testClock{now: time.Date(2026, 2, 10, 10, 0, 0, 0, time.UTC)}
	client := NewInMemoryRedisClient()
	client.SetClock(clk.Now)

	queue := NewRedisQueue(client)
	queue.SetClock(clk.Now)
	agentID := "agent-002"
	ctx := context.Background()

	// Enqueue multiple commands
	commands := []contracts.Command{
		{CommandID: "cmd-001", IdempotencyKey: "key-001", Type: contracts.CommandTypeStatus, CreatedAt: clk.now, Payload: []byte(`{}`)},
		{CommandID: "cmd-002", IdempotencyKey: "key-002", Type: contracts.CommandTypeStatus, CreatedAt: clk.now, Payload: []byte(`{}`)},
		{CommandID: "cmd-003", IdempotencyKey: "key-003", Type: contracts.CommandTypeStatus, CreatedAt: clk.now, Payload: []byte(`{}`)},
	}

	for _, cmd := range commands {
		if err := queue.Enqueue(ctx, agentID, cmd); err != nil {
			t.Fatalf("enqueue %s: %v", cmd.CommandID, err)
		}
	}

	// Poll should return commands in FIFO order
	for i, expected := range commands {
		polled, err := queue.Poll(ctx, agentID, 5)
		if err != nil {
			t.Fatalf("poll %d: %v", i, err)
		}
		if polled == nil {
			t.Fatalf("poll %d: expected command, got nil", i)
		}
		if polled.CommandID != expected.CommandID {
			t.Fatalf("poll %d: expected command_id %s, got %s", i, expected.CommandID, polled.CommandID)
		}
	}

	// No more commands should be available
	nilPoll, err := queue.Poll(ctx, agentID, 1)
	if err != nil {
		t.Fatalf("final poll: %v", err)
	}
	if nilPoll != nil {
		t.Fatalf("final poll: expected nil, got command_id %s", nilPoll.CommandID)
	}
}

// TestRedisQueueStoreResultRemovesFromInflight tests that storing result removes from inflight
func TestRedisQueueStoreResultRemovesFromInflight(t *testing.T) {
	clk := &testClock{now: time.Date(2026, 2, 10, 10, 0, 0, 0, time.UTC)}
	client := NewInMemoryRedisClient()
	client.SetClock(clk.Now)

	queue := NewRedisQueue(client)
	queue.SetClock(clk.Now)
	agentID := "agent-003"
	ctx := context.Background()

	cmd := contracts.Command{
		CommandID:      "cmd-001",
		IdempotencyKey: "key-001",
		Type:           contracts.CommandTypeStatus,
		CreatedAt:      clk.now,
		Payload:        []byte(`{}`),
	}

	// Enqueue and poll to move to inflight
	if err := queue.Enqueue(ctx, agentID, cmd); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	polled, err := queue.Poll(ctx, agentID, 5)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if polled == nil {
		t.Fatal("poll: expected command, got nil")
	}

	// Store result
	result := contracts.CommandResult{
		CommandID: cmd.CommandID,
		OK:        true,
		Summary:   "completed",
	}
	if err := queue.StoreResult(ctx, agentID, result); err != nil {
		t.Fatalf("store result: %v", err)
	}

	// Advance time past redelivery TTL
	clk.now = clk.now.Add(121 * time.Second)

	// Poll should NOT redeliver (command was completed)
	afterStore, err := queue.Poll(ctx, agentID, 5)
	if err != nil {
		t.Fatalf("after store poll: %v", err)
	}
	if afterStore != nil {
		t.Fatalf("after store poll: expected nil, got command_id %s", afterStore.CommandID)
	}
}
