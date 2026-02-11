package backend

import (
	"testing"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

type fakePairingStore struct {
	savePairCodeFn   func(code, telegramUserID string, expiresAt time.Time) error
	getPairCodeFn    func(code string) (string, time.Time, bool, error)
	deletePairCodeFn func(code string) error
	saveBindingFn    func(telegramUserID, agentID, agentKey string) error
	getAgentByKeyFn  func(agentKey string) (string, bool, error)
	getAgentByUserFn func(telegramUserID string) (string, bool, error)
	getUserByAgentFn func(agentID string) (string, bool, error)
}

func (f fakePairingStore) SavePairCode(code string, telegramUserID string, expiresAt time.Time) error {
	if f.savePairCodeFn != nil {
		return f.savePairCodeFn(code, telegramUserID, expiresAt)
	}
	return nil
}
func (f fakePairingStore) GetPairCode(code string) (string, time.Time, bool, error) {
	if f.getPairCodeFn != nil {
		return f.getPairCodeFn(code)
	}
	return "", time.Time{}, false, nil
}
func (f fakePairingStore) DeletePairCode(code string) error {
	if f.deletePairCodeFn != nil {
		return f.deletePairCodeFn(code)
	}
	return nil
}
func (f fakePairingStore) SaveAgentBinding(telegramUserID, agentID, agentKey string) error {
	if f.saveBindingFn != nil {
		return f.saveBindingFn(telegramUserID, agentID, agentKey)
	}
	return nil
}
func (f fakePairingStore) GetAgentIDByKey(agentKey string) (string, bool, error) {
	if f.getAgentByKeyFn != nil {
		return f.getAgentByKeyFn(agentKey)
	}
	return "", false, nil
}
func (f fakePairingStore) GetAgentIDByUser(telegramUserID string) (string, bool, error) {
	if f.getAgentByUserFn != nil {
		return f.getAgentByUserFn(telegramUserID)
	}
	return "", false, nil
}
func (f fakePairingStore) GetUserIDByAgent(agentID string) (string, bool, error) {
	if f.getUserByAgentFn != nil {
		return f.getUserByAgentFn(agentID)
	}
	return "", false, nil
}

func TestMemoryBackendPairingStorePaths(t *testing.T) {
	b := NewMemoryBackend()
	now := time.Date(2026, 2, 11, 12, 30, 0, 0, time.UTC)
	b.SetClock(func() time.Time { return now })
	b.SetPairingPersistence(fakePairingStore{})

	calledSavePair := false
	calledDelete := false
	calledBinding := false
	b.SetPairingPersistence(fakePairingStore{
		savePairCodeFn: func(code, telegramUserID string, expiresAt time.Time) error {
			calledSavePair = true
			if code == "" || telegramUserID != "u1" || expiresAt.IsZero() {
				t.Fatalf("unexpected save pair args code=%q user=%q exp=%v", code, telegramUserID, expiresAt)
			}
			return nil
		},
		getPairCodeFn: func(code string) (string, time.Time, bool, error) {
			if code != "PAIR-000001" {
				t.Fatalf("unexpected pair code lookup: %q", code)
			}
			return "u1", now.Add(10 * time.Minute), true, nil
		},
		deletePairCodeFn: func(code string) error {
			calledDelete = true
			return nil
		},
		saveBindingFn: func(telegramUserID, agentID, agentKey string) error {
			calledBinding = true
			if telegramUserID != "u1" || agentID == "" || agentKey == "" {
				t.Fatalf("unexpected save binding args user=%q aid=%q key=%q", telegramUserID, agentID, agentKey)
			}
			return nil
		},
	})

	start, err := b.StartPairing("u1")
	if err != nil {
		t.Fatalf("start pairing: %v", err)
	}
	if start.PairingCode != "PAIR-000001" || !calledSavePair {
		t.Fatalf("expected persisted pair start, got %+v called=%v", start, calledSavePair)
	}

	claim, err := b.ClaimPairing(contracts.PairClaimRequest{PairingCode: start.PairingCode, DeviceInfo: "test"})
	if err != nil {
		t.Fatalf("claim pairing: %v", err)
	}
	if claim.AgentID == "" || claim.AgentKey == "" || !calledDelete || !calledBinding {
		t.Fatalf("expected persisted claim path, claim=%+v delete=%v bind=%v", claim, calledDelete, calledBinding)
	}
}

func TestMemoryBackendPairingStoreErrorBranches(t *testing.T) {
	b := NewMemoryBackend()
	b.SetClock(time.Now)

	if _, err := b.StartPairing(""); err == nil {
		t.Fatal("expected validation error for empty telegram user")
	}
	if _, err := b.ClaimPairing(contracts.PairClaimRequest{}); err == nil {
		t.Fatal("expected validation error for empty pairing code")
	}

	b.SetPairingPersistence(fakePairingStore{
		savePairCodeFn: func(code, telegramUserID string, expiresAt time.Time) error {
			return contracts.APIError{Code: contracts.ErrInternal, Message: "save failed"}
		},
	})
	if _, err := b.StartPairing("u"); err == nil {
		t.Fatal("expected start pairing persistence error")
	}

	b.SetPairingPersistence(fakePairingStore{
		getPairCodeFn: func(code string) (string, time.Time, bool, error) {
			return "", time.Time{}, false, contracts.APIError{Code: contracts.ErrInternal, Message: "lookup failed"}
		},
	})
	if _, err := b.ClaimPairing(contracts.PairClaimRequest{PairingCode: "PAIR-000001"}); err == nil {
		t.Fatal("expected lookup error")
	}

	b.SetPairingPersistence(fakePairingStore{
		getPairCodeFn: func(code string) (string, time.Time, bool, error) { return "", time.Time{}, false, nil },
	})
	if _, err := b.ClaimPairing(contracts.PairClaimRequest{PairingCode: "PAIR-000001"}); err == nil {
		t.Fatal("expected missing code error")
	}

	b.SetPairingPersistence(fakePairingStore{
		getPairCodeFn: func(code string) (string, time.Time, bool, error) {
			return "u", time.Now().UTC().Add(time.Minute), true, nil
		},
		deletePairCodeFn: func(code string) error {
			return contracts.APIError{Code: contracts.ErrInternal, Message: "delete failed"}
		},
	})
	if _, err := b.ClaimPairing(contracts.PairClaimRequest{PairingCode: "PAIR-000001"}); err == nil {
		t.Fatal("expected delete error")
	}

	b.SetPairingPersistence(fakePairingStore{
		getPairCodeFn: func(code string) (string, time.Time, bool, error) {
			return "u", time.Now().UTC().Add(time.Minute), true, nil
		},
		deletePairCodeFn: func(code string) error { return nil },
		saveBindingFn: func(telegramUserID, agentID, agentKey string) error {
			return contracts.APIError{Code: contracts.ErrInternal, Message: "binding failed"}
		},
	})
	if _, err := b.ClaimPairing(contracts.PairClaimRequest{PairingCode: "PAIR-000001"}); err == nil {
		t.Fatal("expected binding error")
	}
}

func TestMemoryBackendPairingStoreLookupsAndFallbacks(t *testing.T) {
	b := NewMemoryBackend()

	b.SetPairingPersistence(fakePairingStore{
		getAgentByKeyFn:  func(agentKey string) (string, bool, error) { return "a1", true, nil },
		getAgentByUserFn: func(telegramUserID string) (string, bool, error) { return "a1", true, nil },
		getUserByAgentFn: func(agentID string) (string, bool, error) { return "u1", true, nil },
	})
	if aid, ok := b.AuthenticateAgentKey("k1"); !ok || aid != "a1" {
		t.Fatalf("expected store key lookup, got aid=%q ok=%v", aid, ok)
	}
	if aid, ok := b.AgentIDForUser("u1"); !ok || aid != "a1" {
		t.Fatalf("expected store user lookup, got aid=%q ok=%v", aid, ok)
	}
	if uid, ok := b.UserIDForAgent("a1"); !ok || uid != "u1" {
		t.Fatalf("expected store agent lookup, got uid=%q ok=%v", uid, ok)
	}

	b.SetPairingPersistence(fakePairingStore{
		getAgentByKeyFn:  func(agentKey string) (string, bool, error) { return "", false, nil },
		getAgentByUserFn: func(telegramUserID string) (string, bool, error) { return "", false, nil },
		getUserByAgentFn: func(agentID string) (string, bool, error) { return "", false, nil },
	})
	if _, ok := b.AuthenticateAgentKey("missing"); ok {
		t.Fatal("expected missing key to be denied")
	}
	if _, ok := b.AgentIDForUser("missing"); ok {
		t.Fatal("expected missing user to be denied")
	}
	if _, ok := b.UserIDForAgent("missing"); ok {
		t.Fatal("expected missing agent to be denied")
	}

	b.agentByKey["k2"] = "a2"
	b.agentByUser["u2"] = "a2"
	b.SetPairingPersistence(fakePairingStore{
		getAgentByKeyFn: func(agentKey string) (string, bool, error) {
			return "", false, contracts.APIError{Code: contracts.ErrInternal, Message: "store error"}
		},
		getAgentByUserFn: func(telegramUserID string) (string, bool, error) {
			return "", false, contracts.APIError{Code: contracts.ErrInternal, Message: "store error"}
		},
		getUserByAgentFn: func(agentID string) (string, bool, error) {
			return "", false, contracts.APIError{Code: contracts.ErrInternal, Message: "store error"}
		},
	})
	if aid, ok := b.AuthenticateAgentKey("k2"); !ok || aid != "a2" {
		t.Fatalf("expected memory fallback for key lookup, aid=%q ok=%v", aid, ok)
	}
	if aid, ok := b.AgentIDForUser("u2"); !ok || aid != "a2" {
		t.Fatalf("expected memory fallback for user lookup, aid=%q ok=%v", aid, ok)
	}
	if uid, ok := b.UserIDForAgent("a2"); !ok || uid != "u2" {
		t.Fatalf("expected memory fallback for agent lookup, uid=%q ok=%v", uid, ok)
	}
}
