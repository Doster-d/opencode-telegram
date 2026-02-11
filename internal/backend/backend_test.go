package backend

import (
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
