package backend

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time { return f.now }

func TestACMVP05PairingCodeTTLExpiry(t *testing.T) {
	clk := &fakeClock{now: time.Date(2026, 2, 10, 10, 0, 0, 0, time.UTC)}
	b := NewMemoryBackend()
	b.SetClock(clk.Now)
	b.SetPairingTTL(10 * time.Minute)

	start, err := b.StartPairing("tg-user-1")
	if err != nil {
		t.Fatalf("start pairing: %v", err)
	}

	clk.now = clk.now.Add(11 * time.Minute)
	_, err = b.ClaimPairing(contracts.PairClaimRequest{PairingCode: start.PairingCode, DeviceInfo: "linux"})
	if err == nil {
		t.Fatal("expected expired pairing error")
	}
	apiErr, ok := err.(contracts.APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != contracts.ErrPairingExpired {
		t.Fatalf("expected %s got %s", contracts.ErrPairingExpired, apiErr.Code)
	}
}

func TestACMVP05OneActiveAgentReplacement(t *testing.T) {
	clk := &fakeClock{now: time.Date(2026, 2, 10, 10, 0, 0, 0, time.UTC)}
	b := NewMemoryBackend()
	b.SetClock(clk.Now)

	startA, err := b.StartPairing("tg-user-2")
	if err != nil {
		t.Fatalf("start A: %v", err)
	}
	claimA, err := b.ClaimPairing(contracts.PairClaimRequest{PairingCode: startA.PairingCode, DeviceInfo: "device-a"})
	if err != nil {
		t.Fatalf("claim A: %v", err)
	}

	startB, err := b.StartPairing("tg-user-2")
	if err != nil {
		t.Fatalf("start B: %v", err)
	}
	claimB, err := b.ClaimPairing(contracts.PairClaimRequest{PairingCode: startB.PairingCode, DeviceInfo: "device-b"})
	if err != nil {
		t.Fatalf("claim B: %v", err)
	}

	if claimA.AgentID == claimB.AgentID {
		t.Fatal("expected new claim to replace agent id")
	}
	if claimA.AgentKey == claimB.AgentKey {
		t.Fatal("expected new claim to replace agent key")
	}
	if _, ok := b.AuthenticateAgentKey(claimA.AgentKey); ok {
		t.Fatal("expected old key to be invalid after replacement")
	}
	if _, ok := b.AuthenticateAgentKey(claimB.AgentKey); !ok {
		t.Fatal("expected new key to be valid")
	}
}

func TestBackendProjectAliasResolution(t *testing.T) {
	b := NewMemoryBackend()
	b.SetProject("user-1", projectRecord{Alias: "demo", ProjectID: "proj-1", ProjectPath: "/tmp/demo", Policy: projectPolicy{Decision: contracts.DecisionDeny}})

	byAlias, ok := b.ResolveProject("user-1", "demo")
	if !ok || byAlias.ProjectID != "proj-1" {
		t.Fatalf("expected alias resolution, got %+v ok=%v", byAlias, ok)
	}
	byID, ok := b.ResolveProject("user-1", "proj-1")
	if !ok || byID.Alias != "demo" {
		t.Fatalf("expected id resolution, got %+v ok=%v", byID, ok)
	}
}

func TestMemoryBackendQueueAndResultsLifecycle(t *testing.T) {
	b := NewMemoryBackend()
	clk := &fakeClock{now: time.Date(2026, 2, 11, 10, 0, 0, 0, time.UTC)}
	b.SetClock(clk.Now)

	cmd := contracts.Command{
		CommandID:      "cmd-1",
		IdempotencyKey: "key-1",
		Type:           contracts.CommandTypeStatus,
		CreatedAt:      clk.now,
		Payload:        json.RawMessage(`{}`),
	}

	if err := b.Enqueue(context.Background(), "", cmd); err == nil {
		t.Fatal("expected enqueue to fail for empty agent id")
	}
	if err := b.Enqueue(context.Background(), "agent-1", cmd); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	polled, err := b.Poll(context.Background(), "agent-1", 1)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if polled == nil || polled.CommandID != "cmd-1" {
		t.Fatalf("unexpected poll result: %+v", polled)
	}

	if err := b.StoreResult(context.Background(), "", contracts.CommandResult{CommandID: "cmd-1", OK: true}); err == nil {
		t.Fatal("expected store result to fail for empty agent id")
	}
	if err := b.StoreResult(context.Background(), "agent-1", contracts.CommandResult{}); err == nil {
		t.Fatal("expected store result to require command id")
	}

	res := contracts.CommandResult{CommandID: "cmd-1", OK: true, Summary: "done"}
	if err := b.StoreResult(context.Background(), "agent-1", res); err != nil {
		t.Fatalf("store result: %v", err)
	}
	stored, err := b.GetResult(context.Background(), "agent-1", "cmd-1")
	if err != nil {
		t.Fatalf("get result: %v", err)
	}
	if stored == nil || stored.Summary != "done" {
		t.Fatalf("unexpected stored result: %+v", stored)
	}

	if got, ok := b.AgentIDForUser("missing"); ok || got != "" {
		t.Fatalf("expected no agent for missing user, got %q ok=%v", got, ok)
	}
	b.agentByUser["u1"] = "agent-1"
	if got, ok := b.AgentIDForUser("u1"); !ok || got != "agent-1" {
		t.Fatalf("agent lookup mismatch: %q ok=%v", got, ok)
	}
	if got, ok := b.UserIDForAgent("agent-1"); !ok || got != "u1" {
		t.Fatalf("user lookup mismatch: %q ok=%v", got, ok)
	}
}

func TestMemoryBackendApplyResultToProjectUpdatesState(t *testing.T) {
	b := NewMemoryBackend()
	now := time.Date(2026, 2, 11, 11, 0, 0, 0, time.UTC)
	b.SetClock(func() time.Time { return now })

	// register_project result creates project with DENY by default
	b.RegisterCommandMeta("cmd-register", commandMeta{
		TelegramUserID: "u1",
		CommandType:    contracts.CommandTypeRegisterProject,
		Alias:          "demo",
		ProjectPath:    "/tmp/demo",
	})
	err := b.StoreResult(context.Background(), "agent-1", contracts.CommandResult{
		CommandID: "cmd-register",
		OK:        true,
		Meta: map[string]any{
			"project_id":   "p1",
			"project_path": "/tmp/demo",
		},
	})
	if err != nil {
		t.Fatalf("store register result: %v", err)
	}
	proj, ok := b.ResolveProject("u1", "demo")
	if !ok {
		t.Fatal("expected project created from register result")
	}
	if proj.Policy.Decision != contracts.DecisionDeny {
		t.Fatalf("expected default deny policy, got %s", proj.Policy.Decision)
	}

	// apply_project_policy updates policy from result meta
	exp := now.Add(30 * time.Minute)
	b.RegisterCommandMeta("cmd-policy", commandMeta{
		TelegramUserID: "u1",
		CommandType:    contracts.CommandTypeApplyProjectPolicy,
		ProjectID:      "p1",
	})
	err = b.StoreResult(context.Background(), "agent-1", contracts.CommandResult{
		CommandID: "cmd-policy",
		OK:        true,
		Meta: map[string]any{
			"decision":   contracts.DecisionAllow,
			"scope":      []string{contracts.ScopeStartServer, contracts.ScopeRunTask},
			"expires_at": exp.Format(time.RFC3339Nano),
		},
	})
	if err != nil {
		t.Fatalf("store policy result: %v", err)
	}
	proj, ok = b.ResolveProject("u1", "p1")
	if !ok {
		t.Fatal("expected project to exist")
	}
	if proj.Policy.Decision != contracts.DecisionAllow {
		t.Fatalf("expected allow decision, got %s", proj.Policy.Decision)
	}
	if len(proj.Policy.Scope) != 2 {
		t.Fatalf("expected updated scope, got %+v", proj.Policy.Scope)
	}
	if proj.Policy.ExpiresAt == nil || !proj.Policy.ExpiresAt.Equal(exp) {
		t.Fatalf("expected expires_at %s, got %+v", exp, proj.Policy.ExpiresAt)
	}

	projects := b.ListProjects("u1")
	if len(projects) != 1 {
		t.Fatalf("expected one project in list, got %d", len(projects))
	}
}

func TestMemoryBackendPollRedeliveryBranch(t *testing.T) {
	b := NewMemoryBackend()
	clk := &fakeClock{now: time.Date(2026, 2, 11, 12, 0, 0, 0, time.UTC)}
	b.SetClock(clk.Now)
	b.redeliveryAfter = 2 * time.Second

	cmd := contracts.Command{CommandID: "cmd-r", IdempotencyKey: "key-r", Type: contracts.CommandTypeStatus, CreatedAt: clk.now, Payload: json.RawMessage(`{}`)}
	if err := b.Enqueue(context.Background(), "agent-r", cmd); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	first, err := b.Poll(context.Background(), "agent-r", 1)
	if err != nil || first == nil {
		t.Fatalf("first poll failed: cmd=%+v err=%v", first, err)
	}

	clk.now = clk.now.Add(3 * time.Second)
	second, err := b.Poll(context.Background(), "agent-r", 1)
	if err != nil || second == nil || second.CommandID != "cmd-r" {
		t.Fatalf("expected redelivery of cmd-r, got cmd=%+v err=%v", second, err)
	}
}
