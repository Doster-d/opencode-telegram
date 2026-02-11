package store

import (
	"testing"
)

func TestMemoryStore_SetGetDeleteSession(t *testing.T) {
	s := NewMemoryStore()
	sid := "ses_test"
	chat := int64(123)
	msg := 456

	if err := s.SetSession(sid, chat, msg); err != nil {
		t.Fatalf("SetSession returned error: %v", err)
	}

	c, m, ok := s.GetSession(sid)
	if !ok {
		t.Fatalf("expected session to exist")
	}
	if c != chat || m != msg {
		t.Fatalf("unexpected session values: got (%d,%d) want (%d,%d)", c, m, chat, msg)
	}

	if err := s.DeleteSession(sid); err != nil {
		t.Fatalf("DeleteSession returned error: %v", err)
	}

	_, _, ok = s.GetSession(sid)
	if ok {
		t.Fatalf("expected session to be deleted")
	}
}

func TestMemoryStore_UserSessionMapping(t *testing.T) {
	s := NewMemoryStore()
	uid := int64(42)
	sid := "ses_user"

	if err := s.SetUserSession(uid, sid); err != nil {
		t.Fatalf("SetUserSession error: %v", err)
	}
	got, ok := s.GetUserSession(uid)
	if !ok || got != sid {
		t.Fatalf("GetUserSession unexpected: got %q ok=%v want %q", got, ok, sid)
	}

	if err := s.DeleteUserSession(uid); err != nil {
		t.Fatalf("DeleteUserSession error: %v", err)
	}
	_, ok = s.GetUserSession(uid)
	if ok {
		t.Fatalf("expected user session to be deleted")
	}
}

func TestMemoryStore_DeleteSessionClearsUserSelection(t *testing.T) {
	s := NewMemoryStore()
	uid := int64(7)
	sid := "ses_clear"

	if err := s.SetUserSession(uid, sid); err != nil {
		t.Fatalf("SetUserSession error: %v", err)
	}
	if err := s.DeleteSession(sid); err != nil {
		t.Fatalf("DeleteSession error: %v", err)
	}
	_, ok := s.GetUserSession(uid)
	if ok {
		t.Fatalf("expected user session to be cleared after DeleteSession")
	}
}

func TestMemoryStore_AgentKeyManagement(t *testing.T) {
	s := NewMemoryStore()
	uid := int64(123)
	key := "agent-key-123"

	// Test Set and Get
	if err := s.SetUserAgentKey(uid, key); err != nil {
		t.Fatalf("SetUserAgentKey error: %v", err)
	}
	got, ok := s.GetUserAgentKey(uid)
	if !ok || got != key {
		t.Fatalf("GetUserAgentKey unexpected: got %q ok=%v want %q", got, ok, key)
	}

	// Test non-existent user
	_, ok = s.GetUserAgentKey(999)
	if ok {
		t.Fatalf("expected no agent key for non-existent user")
	}
}

func TestMemoryStore_PairingCodeManagement(t *testing.T) {
	s := NewMemoryStore()
	telegramUserID := "456"
	code := "PAIR-000456"

	// Test Set and Get
	if err := s.SetPairingCode(telegramUserID, code); err != nil {
		t.Fatalf("SetPairingCode error: %v", err)
	}
	got, ok := s.GetPairingCode(telegramUserID)
	if !ok || got != code {
		t.Fatalf("GetPairingCode unexpected: got %q ok=%v want %q", got, ok, code)
	}

	// Test non-existent user
	_, ok = s.GetPairingCode("999")
	if ok {
		t.Fatalf("expected no pairing code for non-existent user")
	}
}
