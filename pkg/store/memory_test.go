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
