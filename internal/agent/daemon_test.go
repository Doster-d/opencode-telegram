package agent

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

func mustPayload(t *testing.T, payload any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}

func TestACMVP03IdempotencyReplay(t *testing.T) {
	d := NewDaemon()
	d.SetAgentID("agent-1")
	projectPath := t.TempDir()
	// register project + allow start
	regRes, err := d.HandleCommand(context.Background(), contracts.Command{
		CommandID:      "reg",
		IdempotencyKey: "idem-reg",
		Type:           contracts.CommandTypeRegisterProject,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.RegisterProjectPayload{ProjectPathRaw: projectPath}),
	})
	if err != nil || !regRes.OK {
		t.Fatalf("register project failed: %v %+v", err, regRes)
	}
	projectID, _ := regRes.Meta["project_id"].(string)
	if projectID == "" {
		t.Fatalf("expected project_id in register result")
	}
	_, _ = d.HandleCommand(context.Background(), contracts.Command{
		CommandID:      "pol",
		IdempotencyKey: "idem-pol",
		Type:           contracts.CommandTypeApplyProjectPolicy,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.ApplyProjectPolicyPayload{ProjectID: projectID, Decision: contracts.DecisionAllow, Scope: []string{contracts.ScopeStartServer}}),
	})
	var calls int32
	d.SetHandler(contracts.CommandTypeStartServer, func(_ context.Context, cmd contracts.Command) (contracts.CommandResult, error) {
		count := atomic.AddInt32(&calls, 1)
		return contracts.CommandResult{CommandID: cmd.CommandID, OK: true, Summary: "executed", Meta: map[string]any{"count": count}}, nil
	})

	cmdA := contracts.Command{
		CommandID:      "cmd-1",
		IdempotencyKey: "idem-1",
		Type:           contracts.CommandTypeStartServer,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.StartServerPayload{ProjectID: projectID}),
	}
	cmdB := contracts.Command{
		CommandID:      "cmd-2",
		IdempotencyKey: "idem-1",
		Type:           contracts.CommandTypeStartServer,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.StartServerPayload{ProjectID: projectID}),
	}

	resA, err := d.HandleCommand(context.Background(), cmdA)
	if err != nil {
		t.Fatalf("first execution failed: %v", err)
	}
	resB, err := d.HandleCommand(context.Background(), cmdB)
	if err != nil {
		t.Fatalf("second execution failed: %v", err)
	}

	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected one execution call, got %d", calls)
	}
	if resA.Summary != resB.Summary {
		t.Fatalf("expected replayed result summary, got %q vs %q", resA.Summary, resB.Summary)
	}
	if resA.CommandID != resB.CommandID {
		t.Fatalf("expected replayed result command id, got %q vs %q", resA.CommandID, resB.CommandID)
	}
}

func TestACMVP04PortAllocationExhaustion(t *testing.T) {
	alloc := NewPortAllocator(4096, 4097)
	if _, err := alloc.Allocate("p1"); err != nil {
		t.Fatalf("allocate p1: %v", err)
	}
	if _, err := alloc.Allocate("p2"); err != nil {
		t.Fatalf("allocate p2: %v", err)
	}
	_, err := alloc.Allocate("p3")
	if err == nil {
		t.Fatal("expected exhaustion error")
	}
	apiErr, ok := err.(contracts.APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != contracts.ErrPortExhausted {
		t.Fatalf("expected %s got %s", contracts.ErrPortExhausted, apiErr.Code)
	}
}

func TestACMVP04MutatingSerializationAndStatusImmediate(t *testing.T) {
	d := NewDaemon()
	d.SetAgentID("agent-1")
	path := t.TempDir()
	regRes, err := d.HandleCommand(context.Background(), contracts.Command{
		CommandID:      "reg",
		IdempotencyKey: "idem-reg",
		Type:           contracts.CommandTypeRegisterProject,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.RegisterProjectPayload{ProjectPathRaw: path}),
	})
	if err != nil || !regRes.OK {
		t.Fatalf("register project failed: %v %+v", err, regRes)
	}
	projectID, _ := regRes.Meta["project_id"].(string)
	_, _ = d.HandleCommand(context.Background(), contracts.Command{
		CommandID:      "pol",
		IdempotencyKey: "idem-pol",
		Type:           contracts.CommandTypeApplyProjectPolicy,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.ApplyProjectPolicyPayload{ProjectID: projectID, Decision: contracts.DecisionAllow, Scope: []string{contracts.ScopeStartServer, contracts.ScopeRunTask}}),
	})
	startEntered := make(chan struct{})
	releaseStart := make(chan struct{})
	runTaskEntered := make(chan struct{})
	d.SetHandler(contracts.CommandTypeStartServer, func(_ context.Context, cmd contracts.Command) (contracts.CommandResult, error) {
		close(startEntered)
		<-releaseStart
		return contracts.CommandResult{CommandID: cmd.CommandID, OK: true, Summary: "start done"}, nil
	})
	d.SetHandler(contracts.CommandTypeRunTask, func(_ context.Context, cmd contracts.Command) (contracts.CommandResult, error) {
		close(runTaskEntered)
		return contracts.CommandResult{CommandID: cmd.CommandID, OK: true, Summary: "run done"}, nil
	})

	startCmd := contracts.Command{
		CommandID:      "start-1",
		IdempotencyKey: "idem-start-1",
		Type:           contracts.CommandTypeStartServer,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.StartServerPayload{ProjectID: projectID}),
	}
	runCmd := contracts.Command{
		CommandID:      "run-1",
		IdempotencyKey: "idem-run-1",
		Type:           contracts.CommandTypeRunTask,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.RunTaskPayload{ProjectID: projectID, Prompt: "hello"}),
	}
	statusCmd := contracts.Command{
		CommandID:      "status-1",
		IdempotencyKey: "idem-status-1",
		Type:           contracts.CommandTypeStatus,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.StatusPayload{}),
	}

	go func() {
		_, _ = d.HandleCommand(context.Background(), startCmd)
	}()
	select {
	case <-startEntered:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("start handler did not enter")
	}

	statusDone := make(chan contracts.CommandResult, 1)
	go func() {
		res, _ := d.HandleCommand(context.Background(), statusCmd)
		statusDone <- res
	}()
	select {
	case res := <-statusDone:
		if !res.OK {
			t.Fatalf("expected immediate status OK result, got %+v", res)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("status should not wait on mutating lock")
	}

	runDone := make(chan struct{})
	go func() {
		_, _ = d.HandleCommand(context.Background(), runCmd)
		close(runDone)
	}()

	select {
	case <-runTaskEntered:
		t.Fatal("run_task entered before start_server released")
	case <-time.After(150 * time.Millisecond):
	}

	close(releaseStart)

	select {
	case <-runTaskEntered:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("run_task should enter after start_server release")
	}
	select {
	case <-runDone:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("run_task should complete")
	}
}
