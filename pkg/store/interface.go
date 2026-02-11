package store

// Store defines the interface for session persistence
type Store interface {
	SetSession(sessionID string, chatID int64, messageID int) error
	GetSession(sessionID string) (chatID int64, messageID int, ok bool)
	DeleteSession(sessionID string) error
	// Per-user selected session
	SetUserSession(userID int64, sessionID string) error
	GetUserSession(userID int64) (sessionID string, ok bool)
	DeleteUserSession(userID int64) error
	// Agent key management for backend pairing
	SetUserAgentKey(userID int64, agentKey string) error
	GetUserAgentKey(userID int64) (agentKey string, ok bool)
	// Pairing code management
	SetPairingCode(telegramUserID string, code string) error
	GetPairingCode(telegramUserID string) (code string, ok bool)
}
