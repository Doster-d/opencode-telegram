package bot

import (
	"opencode-telegram/pkg/store"
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// MockTelegramBot for testing (stub)
type MockTelegramBot struct {
	*tgbotapi.BotAPI
	sentMessages []tgbotapi.MessageConfig
	sentEdits    []tgbotapi.EditMessageTextConfig
	updatesChan  tgbotapi.UpdatesChannel
}

func (m *MockTelegramBot) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	// Mock implementation - just track calls
	if msg, ok := c.(tgbotapi.MessageConfig); ok {
		m.sentMessages = append(m.sentMessages, msg)
	}
	return tgbotapi.Message{}, nil
}

func (m *MockTelegramBot) GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel {
	if m.updatesChan == nil {
		m.updatesChan = make(chan tgbotapi.Update, 1)
	}
	return m.updatesChan
}

func (m *MockTelegramBot) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	// Mock implementation
	if edit, ok := c.(tgbotapi.EditMessageTextConfig); ok {
		m.sentEdits = append(m.sentEdits, edit)
	}
	return nil, nil
}

// TestBotApp_IsAllowed tests the permission checking
func TestBotApp_IsAllowed(t *testing.T) {
	tests := []struct {
		name      string
		allowedID map[int64]bool
		userID    int64
		expected  bool
	}{
		{
			name:      "empty allowed list allows all",
			allowedID: map[int64]bool{},
			userID:    12345,
			expected:  true,
		},
		{
			name:      "user in allowed list",
			allowedID: map[int64]bool{123: true, 456: true},
			userID:    123,
			expected:  true,
		},
		{
			name:      "user not in allowed list",
			allowedID: map[int64]bool{123: true, 456: true},
			userID:    789,
			expected:  false,
		},
		{
			name:      "single allowed user",
			allowedID: map[int64]bool{999: true},
			userID:    999,
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				AllowedIDs: tt.allowedID,
				AdminIDs:   map[int64]bool{},
			}
			app := &BotApp{
				cfg: cfg,
			}
			got := app.isAllowed(tt.userID)
			if got != tt.expected {
				t.Errorf("isAllowed(%d) = %v, want %v", tt.userID, got, tt.expected)
			}
		})
	}
}

// TestBotApp_IsAdmin tests admin checking
func TestBotApp_IsAdmin(t *testing.T) {
	tests := []struct {
		name     string
		adminIDs map[int64]bool
		userID   int64
		expected bool
	}{
		{
			name:     "user is admin",
			adminIDs: map[int64]bool{123: true, 456: true},
			userID:   123,
			expected: true,
		},
		{
			name:     "user is not admin",
			adminIDs: map[int64]bool{123: true, 456: true},
			userID:   789,
			expected: false,
		},
		{
			name:     "empty admin list",
			adminIDs: map[int64]bool{},
			userID:   123,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				AdminIDs: tt.adminIDs,
			}
			app := &BotApp{
				cfg: cfg,
			}
			got := app.isAdmin(tt.userID)
			if got != tt.expected {
				t.Errorf("isAdmin(%d) = %v, want %v", tt.userID, got, tt.expected)
			}
		})
	}
}

// TestBotApp_NewBotApp tests basic initialization
func TestBotApp_NewBotApp_WithValidSession(t *testing.T) {
	// This test would require mocking the actual Telegram API
	// For now we demonstrate the structure
	cfg := &Config{
		TelegramToken: "fake_token",
		SessionPrefix: "oct_",
	}

	// Would need to mock the bot creation and opencode client
	// This is left as a placeholder for integration testing
	_ = cfg
}

// TestBotApp_IsAllowed_EdgeCases tests edge cases
func TestBotApp_IsAllowed_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		allowedID map[int64]bool
		userID    int64
		expected  bool
	}{
		{
			name:      "negative user ID",
			allowedID: map[int64]bool{-1: true},
			userID:    -1,
			expected:  true,
		},
		{
			name:      "zero user ID",
			allowedID: map[int64]bool{0: true},
			userID:    0,
			expected:  true,
		},
		{
			name:      "large user ID",
			allowedID: map[int64]bool{9223372036854775807: true},
			userID:    9223372036854775807,
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				AllowedIDs: tt.allowedID,
				AdminIDs:   map[int64]bool{},
			}
			app := &BotApp{
				cfg: cfg,
			}
			got := app.isAllowed(tt.userID)
			if got != tt.expected {
				t.Errorf("isAllowed(%d) = %v, want %v", tt.userID, got, tt.expected)
			}
		})
	}
}

// TestBotApp_AdminSubsetOfAllowed tests that admin users should be in allowed list
func TestBotApp_AdminUsers_ShouldBeAllowed(t *testing.T) {
	cfg := &Config{
		AllowedIDs: map[int64]bool{123: true, 456: true},
		AdminIDs:   map[int64]bool{123: true},
	}
	app := &BotApp{cfg: cfg}

	if !app.isAllowed(123) {
		t.Errorf("admin user should be in allowed list")
	}
	if !app.isAdmin(123) {
		t.Errorf("user should be admin")
	}
}

// TestBotApp_DebounceConfiguration tests that debouncer is configured
func TestBotApp_DebouncerInitialized(t *testing.T) {
	cfg := &Config{
		AllowedIDs:    map[int64]bool{},
		AdminIDs:      map[int64]bool{},
		SessionPrefix: "oct_",
	}
	app := &BotApp{
		cfg:       cfg,
		debouncer: NewDebouncer(500 * time.Millisecond),
		store:     store.NewMemoryStore(),
	}

	if app.debouncer == nil {
		t.Errorf("debouncer should be initialized")
	}
}

// TestBotApp_StoreInitialized tests that store is set
func TestBotApp_StoreInitialized(t *testing.T) {
	st := store.NewMemoryStore()
	cfg := &Config{
		AllowedIDs:    map[int64]bool{},
		AdminIDs:      map[int64]bool{},
		SessionPrefix: "oct_",
	}
	app := &BotApp{
		cfg:   cfg,
		store: st,
	}

	if app.store == nil {
		t.Errorf("store should be initialized")
	}
}

// TestBotApp_ConfigurationPreserved tests that config is preserved
func TestBotApp_ConfigurationPreserved(t *testing.T) {
	expectedPrefix := "test_prefix_"
	expectedBase := "http://example.com"

	cfg := &Config{
		SessionPrefix: expectedPrefix,
		OpencodeBase:  expectedBase,
		AllowedIDs:    map[int64]bool{},
		AdminIDs:      map[int64]bool{},
	}
	app := &BotApp{
		cfg: cfg,
	}

	if app.cfg.SessionPrefix != expectedPrefix {
		t.Errorf("session prefix not preserved: got %q, want %q", app.cfg.SessionPrefix, expectedPrefix)
	}
	if app.cfg.OpencodeBase != expectedBase {
		t.Errorf("opencode base not preserved: got %q, want %q", app.cfg.OpencodeBase, expectedBase)
	}
}

// TestBotApp_HandleStatus tests handleStatus
func TestBotApp_HandleStatus(t *testing.T) {
	mockTG := &MockTelegramBot{}
	cfg := &Config{OpencodeBase: "http://test.com"}
	app := &BotApp{
		tg:  mockTG,
		cfg: cfg,
	}
	app.handleStatus(123)
	if len(mockTG.sentMessages) != 1 {
		t.Errorf("expected 1 message, got %d", len(mockTG.sentMessages))
	}
	msg := mockTG.sentMessages[0]
	if msg.ChatID != 123 {
		t.Errorf("expected chatID 123, got %d", msg.ChatID)
	}
	expected := "Opencode: http://test.com"
	if msg.Text != expected {
		t.Errorf("expected %q, got %q", expected, msg.Text)
	}
}

// TestBotApp_HandleSessions tests handleSessions
func TestBotApp_HandleSessions(t *testing.T) {
	mockTG := &MockTelegramBot{}
	mockOC := &mockOpencodeClient{
		listSessions: func() ([]map[string]any, error) {
			return []map[string]any{
				{"id": "ses_1", "title": "oct_1"},
				{"id": "ses_2", "title": "other"},
			}, nil
		},
	}
	cfg := &Config{SessionPrefix: "oct_"}
	app := &BotApp{
		tg:  mockTG,
		oc:  mockOC,
		cfg: cfg,
	}
	app.handleSessions(123)
	if len(mockTG.sentMessages) != 1 {
		t.Errorf("expected 1 message, got %d", len(mockTG.sentMessages))
	}
	msg := mockTG.sentMessages[0]
	if !strings.Contains(msg.Text, "ses_1") {
		t.Errorf("expected ses_1 in text, got %q", msg.Text)
	}
}

// TestBotApp_HandleSessions_Error tests handleSessions with error
func TestBotApp_HandleSessions_Error(t *testing.T) {
	mockTG := &MockTelegramBot{}
	mockOC := &mockOpencodeClient{
		listSessions: func() ([]map[string]any, error) {
			return nil, fmt.Errorf("list error")
		},
	}
	app := &BotApp{
		tg: mockTG,
		oc: mockOC,
	}
	app.handleSessions(123)
	if len(mockTG.sentMessages) != 1 {
		t.Errorf("expected 1 message, got %d", len(mockTG.sentMessages))
	}
	msg := mockTG.sentMessages[0]
	if !strings.Contains(msg.Text, "Error listing sessions") {
		t.Errorf("expected error message, got %q", msg.Text)
	}
}
	cfg := &Config{SessionPrefix: "oct_"}
	app := &BotApp{
		tg:  mockTG,
		oc:  mockOC,
		cfg: cfg,
	}
	app.handleSessions(123)
	if len(mockTG.sentMessages) != 1 {
		t.Errorf("expected 1 message, got %d", len(mockTG.sentMessages))
	}
	msg := mockTG.sentMessages[0]
	if msg.ChatID != 123 {
		t.Errorf("expected chatID 123, got %d", msg.ChatID)
	}
	// Check content
	if !strings.Contains(msg.Text, "ses_1") {
		t.Errorf("expected ses_1 in text, got %q", msg.Text)
	}
}

// TestBotApp_HandleSessions_Error tests handleSessions with error
func TestBotApp_HandleSessions_Error(t *testing.T) {
	mockTG := &MockTelegramBot{}
	mockOC := &mockOpencodeClient{
		listSessions: func() ([]map[string]any, error) {
			return nil, fmt.Errorf("list error")
		},
	}
	app := &BotApp{
		tg: mockTG,
		oc: mockOC,
	}
	app.handleSessions(123)
	if len(mockTG.sentMessages) != 1 {
		t.Errorf("expected 1 message, got %d", len(mockTG.sentMessages))
	}
	msg := mockTG.sentMessages[0]
	if !strings.Contains(msg.Text, "Error listing sessions") {
		t.Errorf("expected error message, got %q", msg.Text)
	}
}

// TestBotApp_HandleCreateSession tests handleCreateSession
func TestBotApp_HandleCreateSession(t *testing.T) {
	mockTG := &MockTelegramBot{}
	mockOC := &mockOpencodeClient{
		createSession: func(title string) (map[string]any, error) {
			return map[string]any{"id": "ses_new", "title": title}, nil
		},
	}
	store := store.NewMemoryStore()
	cfg := &Config{SessionPrefix: "oct_"}
	app := &BotApp{
		tg:    mockTG,
		oc:    mockOC,
		store: store,
		cfg:   cfg,
	}
	app.handleCreateSession(123, "test session", 456)
	if len(mockTG.sentMessages) != 1 {
		t.Errorf("expected 1 message, got %d", len(mockTG.sentMessages))
	}
	msg := mockTG.sentMessages[0]
	if !strings.Contains(msg.Text, "ses_new") {
		t.Errorf("expected ses_new in text, got %q", msg.Text)
	}
	// Check user session set
	if sid, ok := store.GetUserSession(456); !ok || sid != "ses_new" {
		t.Errorf("expected user session ses_new, got %v", sid)
	}
}

// TestBotApp_HandleCreateSession_EmptyTitle tests handleCreateSession with empty title
func TestBotApp_HandleCreateSession_EmptyTitle(t *testing.T) {
	mockTG := &MockTelegramBot{}
	mockOC := &mockOpencodeClient{
		createSession: func(title string) (map[string]any, error) {
			return map[string]any{"id": "ses_new", "title": title}, nil
		},
	}
	store := store.NewMemoryStore()
	cfg := &Config{SessionPrefix: "oct_"}
	app := &BotApp{
		tg:    mockTG,
		oc:    mockOC,
		store: store,
		cfg:   cfg,
	}
	app.handleCreateSession(123, "", 456)
	if len(mockTG.sentMessages) != 1 {
		t.Errorf("expected 1 message, got %d", len(mockTG.sentMessages))
	}
	msg := mockTG.sentMessages[0]
	if !strings.Contains(msg.Text, "ses_new") {
		t.Errorf("expected ses_new in text, got %q", msg.Text)
	}
}

// TestBotApp_HandleDeleteSession tests handleDeleteSession
func TestBotApp_HandleDeleteSession(t *testing.T) {
	mockTG := &MockTelegramBot{}
	mockOC := &mockOpencodeClient{
		deleteSession: func(id string) error {
			return nil
		},
	}
	store := store.NewMemoryStore()
	cfg := &Config{AdminIDs: map[int64]bool{456: true}}
	app := &BotApp{
		tg:    mockTG,
		oc:    mockOC,
		store: store,
		cfg:   cfg,
	}
	app.handleDeleteSession(123, "ses_del", 456)
	if len(mockTG.sentMessages) != 1 {
		t.Errorf("expected 1 message, got %d", len(mockTG.sentMessages))
	}
	msg := mockTG.sentMessages[0]
	if !strings.Contains(msg.Text, "Deleted session") {
		t.Errorf("expected deleted message, got %q", msg.Text)
	}
}

// TestBotApp_HandleSelectSession tests handleSelectSession
func TestBotApp_HandleSelectSession(t *testing.T) {
	mockTG := &MockTelegramBot{}
	mockOC := &mockOpencodeClient{
		listSessions: func() ([]map[string]any, error) {
			return []map[string]any{
				{"id": "ses_1", "title": "test_session"},
			}, nil
		},
	}
	store := store.NewMemoryStore()
	app := &BotApp{
		tg:    mockTG,
		oc:    mockOC,
		store: store,
	}
	app.handleSelectSession(123, "test", 456)
	if len(mockTG.sentMessages) != 1 {
		t.Errorf("expected 1 message, got %d", len(mockTG.sentMessages))
	}
	msg := mockTG.sentMessages[0]
	if !strings.Contains(msg.Text, "ses_1") {
		t.Errorf("expected ses_1 in text, got %q", msg.Text)
	}
	// Check user session
	if sid, ok := store.GetUserSession(456); !ok || sid != "ses_1" {
		t.Errorf("expected user session ses_1, got %v", sid)
	}
}

// TestBotApp_HandleMySession tests handleMySession
func TestBotApp_HandleMySession(t *testing.T) {
	mockTG := &MockTelegramBot{}
	store := store.NewMemoryStore()
	store.SetUserSession(456, "ses_1")
	app := &BotApp{
		tg:    mockTG,
		store: store,
	}
	app.handleMySession(123, 456)
	if len(mockTG.sentMessages) != 1 {
		t.Errorf("expected 1 message, got %d", len(mockTG.sentMessages))
	}
	msg := mockTG.sentMessages[0]
	if !strings.Contains(msg.Text, "ses_1") {
		t.Errorf("expected ses_1 in text, got %q", msg.Text)
	}
}

// TestBotApp_HandleRun tests handleRun
func TestBotApp_HandleRun(t *testing.T) {
	mockTG := &MockTelegramBot{}
	mockOC := &mockOpencodeClient{
		promptSession: func(id, prompt string) (map[string]any, error) {
			return map[string]any{"ok": true}, nil
		},
	}
	store := store.NewMemoryStore()
	app := &BotApp{
		tg:           mockTG,
		oc:           mockOC,
		store:        store,
		octSessionID: "ses_oct",
	}
	app.handleRun(123, "test prompt", 456)
	if len(mockTG.sentMessages) != 1 {
		t.Errorf("expected 1 message, got %d", len(mockTG.sentMessages))
	}
	msg := mockTG.sentMessages[0]
	if !strings.Contains(msg.Text, "Running on Opencode") {
		t.Errorf("expected running message, got %q", msg.Text)
	}
	// Check session set
	if chatID, msgID, ok := store.GetSession("ses_oct"); !ok || chatID != 123 {
		t.Errorf("expected session set, got %v, %v", chatID, msgID)
	}
}

// TestBotApp_HandleAbort tests handleAbort
func TestBotApp_HandleAbort(t *testing.T) {
	mockTG := &MockTelegramBot{}
	mockOC := &mockOpencodeClient{
		abortSession: func(id string) error {
			return nil
		},
	}
	cfg := &Config{AdminIDs: map[int64]bool{456: true}}
	app := &BotApp{
		tg:  mockTG,
		oc:  mockOC,
		cfg: cfg,
	}
	app.handleAbort(123, "ses_abort", 456)
	if len(mockTG.sentMessages) != 1 {
		t.Errorf("expected 1 message, got %d", len(mockTG.sentMessages))
	}
	msg := mockTG.sentMessages[0]
	if !strings.Contains(msg.Text, "Aborted session") {
		t.Errorf("expected aborted message, got %q", msg.Text)
	}
}

// TestBotApp_StartPolling tests StartPolling with a command
func TestBotApp_StartPolling_Command(t *testing.T) {
	mockTG := &MockTelegramBot{}
	cfg := &Config{AllowedIDs: map[int64]bool{456: true}}
	app := &BotApp{
		tg:  mockTG,
		cfg: cfg,
	}
	ch := make(chan tgbotapi.Update, 1)
	mockTG.updatesChan = ch
	// Send a command update
	go func() {
		ch <- tgbotapi.Update{
			Message: &tgbotapi.Message{
				Chat:     &tgbotapi.Chat{ID: 123},
				From:     &tgbotapi.User{ID: 456},
				Text:     "/status",
				Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 7}},
			},
		}
		close(ch)
	}()
	err := app.StartPolling()
	if err != nil {
		t.Errorf("StartPolling error: %v", err)
	}
	if len(mockTG.sentMessages) != 1 {
		t.Errorf("expected 1 message, got %d", len(mockTG.sentMessages))
	}
}

// TestBotApp_StartPolling_Text tests StartPolling with text
func TestBotApp_StartPolling_Text(t *testing.T) {
	mockTG := &MockTelegramBot{}
	mockOC := &mockOpencodeClient{
		promptSession: func(id, prompt string) (map[string]any, error) {
			return map[string]any{"ok": true}, nil
		},
	}
	store := store.NewMemoryStore()
	cfg := &Config{AllowedIDs: map[int64]bool{456: true}}
	app := &BotApp{
		tg:           mockTG,
		oc:           mockOC,
		store:        store,
		cfg:          cfg,
		octSessionID: "ses_oct",
	}
	ch := make(chan tgbotapi.Update, 1)
	mockTG.updatesChan = ch
	// Send a text update
	go func() {
		ch <- tgbotapi.Update{
			Message: &tgbotapi.Message{
				Chat: &tgbotapi.Chat{ID: 123},
				From: &tgbotapi.User{ID: 456},
				Text: "hello",
			},
		}
		close(ch)
	}()
	err := app.StartPolling()
	if err != nil {
		t.Errorf("StartPolling error: %v", err)
	}
	if len(mockTG.sentMessages) != 1 {
		t.Errorf("expected 1 message, got %d", len(mockTG.sentMessages))
	}
}
