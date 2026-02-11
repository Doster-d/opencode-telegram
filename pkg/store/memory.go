package store

import "sync"

// MemoryStore is a simple in-memory implementation of Store for session -> telegram message mapping
type MemoryStore struct {
	mu sync.RWMutex
	m  map[string]sessionRef
	// per-user selection: map[userID]sessionID
	um map[int64]string
	// agent key management: map[userID]agentKey
	ak map[int64]string
	// pairing code management: map[telegramUserID]code
	pc map[string]string
}

type sessionRef struct {
	ChatID    int64
	MessageID int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{m: make(map[string]sessionRef), um: make(map[int64]string), ak: make(map[int64]string), pc: make(map[string]string)}
}

func (s *MemoryStore) SetSession(sessionID string, chatID int64, messageID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[sessionID] = sessionRef{ChatID: chatID, MessageID: messageID}
	return nil
}

func (s *MemoryStore) GetSession(sessionID string) (int64, int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.m[sessionID]
	if !ok {
		return 0, 0, false
	}
	return r.ChatID, r.MessageID, true
}

func (s *MemoryStore) DeleteSession(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, sessionID)
	// also remove any user selections that point to this session
	for uid, sid := range s.um {
		if sid == sessionID {
			delete(s.um, uid)
		}
	}
	return nil
}

func (s *MemoryStore) SetUserSession(userID int64, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.um[userID] = sessionID
	return nil
}

func (s *MemoryStore) GetUserSession(userID int64) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sid, ok := s.um[userID]
	return sid, ok
}

func (s *MemoryStore) DeleteUserSession(userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.um, userID)
	return nil
}

func (s *MemoryStore) SetUserAgentKey(userID int64, agentKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ak[userID] = agentKey
	return nil
}

func (s *MemoryStore) GetUserAgentKey(userID int64) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key, ok := s.ak[userID]
	return key, ok
}

func (s *MemoryStore) SetPairingCode(telegramUserID string, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pc[telegramUserID] = code
	return nil
}

func (s *MemoryStore) GetPairingCode(telegramUserID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	code, ok := s.pc[telegramUserID]
	return code, ok
}
