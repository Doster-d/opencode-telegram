package bot

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opencode-telegram/pkg/store"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type recordingTelegramBot struct {
	updates      tgbotapi.UpdatesChannel
	sentMessages []tgbotapi.MessageConfig
	requests     []tgbotapi.Chattable
	nextMsgID    int
	requestErrs  []error
}

func (m *recordingTelegramBot) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	if msg, ok := c.(tgbotapi.MessageConfig); ok {
		m.sentMessages = append(m.sentMessages, msg)
	}
	m.nextMsgID++
	return tgbotapi.Message{MessageID: m.nextMsgID}, nil
}

func (m *recordingTelegramBot) GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel {
	if m.updates == nil {
		m.updates = make(chan tgbotapi.Update)
	}
	return m.updates
}

func (m *recordingTelegramBot) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	m.requests = append(m.requests, c)
	if len(m.requestErrs) > 0 {
		err := m.requestErrs[0]
		m.requestErrs = m.requestErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	return &tgbotapi.APIResponse{}, nil
}

func testBotApp(cfg *Config, oc OpencodeClientInterface) (*BotApp, *recordingTelegramBot, *store.MemoryStore) {
	tg := &recordingTelegramBot{}
	st := store.NewMemoryStore()
	app := &BotApp{
		tg:           tg,
		cfg:          cfg,
		oc:           oc,
		store:        st,
		debouncer:    &mockDebouncer{},
		octSessionID: "ses_oct",
		activeRuns:   make(map[string]string),
		runOwners:    make(map[string]string),
		sleep:        func(time.Duration) {},
		httpClient:   &http.Client{Timeout: 2 * time.Second},
		backendURL:   "http://example.invalid",
	}
	return app, tg, st
}

func withMockTelegramFactory(t *testing.T, factory func(token string) (TelegramBotInterface, error)) {
	t.Helper()
	original := newTelegramBot
	newTelegramBot = factory
	t.Cleanup(func() {
		newTelegramBot = original
	})
}

func TestNewBotApp(t *testing.T) {
	withMockTelegramFactory(t, func(token string) (TelegramBotInterface, error) {
		return &recordingTelegramBot{}, nil
	})

	cfg := &Config{TelegramToken: "token", SessionPrefix: "oct_"}
	st := store.NewMemoryStore()

	t.Run("finds existing prefixed session", func(t *testing.T) {
		oc := &mockOpencodeClient{listSessions: func() ([]map[string]any, error) {
			return []map[string]any{{"id": "ses_existing", "title": "oct_existing"}}, nil
		}}

		app, err := NewBotApp(cfg, oc, st)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if app.octSessionID != "ses_existing" {
			t.Fatalf("expected existing session id, got %q", app.octSessionID)
		}
	})

	t.Run("creates session when none found", func(t *testing.T) {
		oc := &mockOpencodeClient{
			listSessions:  func() ([]map[string]any, error) { return []map[string]any{{"id": "ses_other", "title": "other"}}, nil },
			createSession: func(string) (map[string]any, error) { return map[string]any{"id": "ses_created"}, nil },
		}

		app, err := NewBotApp(cfg, oc, st)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if app.octSessionID != "ses_created" {
			t.Fatalf("expected created session id, got %q", app.octSessionID)
		}
	})

	t.Run("fails when bot init fails", func(t *testing.T) {
		withMockTelegramFactory(t, func(token string) (TelegramBotInterface, error) {
			return nil, fmt.Errorf("bad token")
		})
		oc := &mockOpencodeClient{listSessions: func() ([]map[string]any, error) { return nil, nil }}

		if _, err := NewBotApp(cfg, oc, st); err == nil {
			t.Fatalf("expected bot init error")
		}
	})

	t.Run("fails when list sessions errors", func(t *testing.T) {
		oc := &mockOpencodeClient{listSessions: func() ([]map[string]any, error) { return nil, fmt.Errorf("list failed") }}

		if _, err := NewBotApp(cfg, oc, st); err == nil || !strings.Contains(err.Error(), "failed to list sessions") {
			t.Fatalf("expected list sessions error, got %v", err)
		}
	})

	t.Run("fails when create session has no id", func(t *testing.T) {
		oc := &mockOpencodeClient{
			listSessions:  func() ([]map[string]any, error) { return nil, nil },
			createSession: func(string) (map[string]any, error) { return map[string]any{"title": "x"}, nil },
		}

		if _, err := NewBotApp(cfg, oc, st); err == nil || !strings.Contains(err.Error(), "session id not found") {
			t.Fatalf("expected missing id error, got %v", err)
		}
	})
}

func TestBotApp_AccessChecks(t *testing.T) {
	app, _, _ := testBotApp(&Config{AllowedIDs: map[int64]bool{1: true}, AdminIDs: map[int64]bool{9: true}}, &mockOpencodeClient{})

	if app.isAllowed(1) != true {
		t.Fatalf("expected user 1 to be allowed")
	}
	if app.isAllowed(2) != false {
		t.Fatalf("expected user 2 to be denied")
	}
	if app.isAdmin(9) != true {
		t.Fatalf("expected user 9 to be admin")
	}
	if app.isAdmin(1) != false {
		t.Fatalf("expected user 1 to be non-admin")
	}

	openApp, _, _ := testBotApp(&Config{AllowedIDs: map[int64]bool{}, AdminIDs: map[int64]bool{}}, &mockOpencodeClient{})
	if openApp.isAllowed(42) != true {
		t.Fatalf("empty allowed list should allow all users")
	}
}

func TestBotApp_HandleStatus(t *testing.T) {
	app, tg, _ := testBotApp(&Config{OpencodeBase: "http://local"}, &mockOpencodeClient{})
	app.handleStatus(123)

	if len(tg.sentMessages) != 1 {
		t.Fatalf("expected 1 status message, got %d", len(tg.sentMessages))
	}
	if tg.sentMessages[0].Text != "Opencode: http://local" {
		t.Fatalf("unexpected status text: %q", tg.sentMessages[0].Text)
	}
}

func TestBotApp_HandleSessions(t *testing.T) {
	t.Run("error path", func(t *testing.T) {
		oc := &mockOpencodeClient{listSessions: func() ([]map[string]any, error) { return nil, fmt.Errorf("boom") }}
		app, tg, _ := testBotApp(&Config{SessionPrefix: "oct_"}, oc)
		app.handleSessions(1)

		if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "Error listing sessions") {
			t.Fatalf("expected error message, got %+v", tg.sentMessages)
		}
	})

	t.Run("no sessions", func(t *testing.T) {
		oc := &mockOpencodeClient{listSessions: func() ([]map[string]any, error) { return []map[string]any{}, nil }}
		app, tg, _ := testBotApp(&Config{SessionPrefix: "oct_"}, oc)
		app.handleSessions(1)

		if len(tg.sentMessages) != 1 || tg.sentMessages[0].Text != "No sessions" {
			t.Fatalf("expected no sessions message, got %+v", tg.sentMessages)
		}
	})

	t.Run("prefix filter", func(t *testing.T) {
		oc := &mockOpencodeClient{listSessions: func() ([]map[string]any, error) {
			return []map[string]any{{"id": "ses_1", "title": "oct_alpha"}, {"id": "ses_2", "title": "other"}}, nil
		}}
		app, tg, _ := testBotApp(&Config{SessionPrefix: "oct_"}, oc)
		app.handleSessions(1)

		if len(tg.sentMessages) != 1 {
			t.Fatalf("expected one message, got %d", len(tg.sentMessages))
		}
		if strings.Contains(tg.sentMessages[0].Text, "ses_2") {
			t.Fatalf("did not expect non-prefixed session in output: %q", tg.sentMessages[0].Text)
		}
	})
}

func TestBotApp_HandleCreateSession(t *testing.T) {
	oc := &mockOpencodeClient{createSession: func(title string) (map[string]any, error) {
		return map[string]any{"id": "ses_new", "title": title}, nil
	}}
	app, tg, st := testBotApp(&Config{SessionPrefix: "oct_"}, oc)

	app.handleCreateSession(10, "", 20)

	if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "Created session: ses_new") {
		t.Fatalf("expected created message, got %+v", tg.sentMessages)
	}
	if sid, ok := st.GetUserSession(20); !ok || sid != "ses_new" {
		t.Fatalf("expected selected user session ses_new, got %q ok=%v", sid, ok)
	}
}

func TestBotApp_HandleDeleteSession(t *testing.T) {
	t.Run("usage", func(t *testing.T) {
		app, tg, _ := testBotApp(&Config{AdminIDs: map[int64]bool{1: true}}, &mockOpencodeClient{})
		app.handleDeleteSession(1, "", 1)
		if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "Usage: /deletesession") {
			t.Fatalf("expected usage message, got %+v", tg.sentMessages)
		}
	})

	t.Run("admin required", func(t *testing.T) {
		app, tg, _ := testBotApp(&Config{AdminIDs: map[int64]bool{}}, &mockOpencodeClient{})
		app.handleDeleteSession(1, "ses_x", 9)
		if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "Only admins") {
			t.Fatalf("expected admin rejection, got %+v", tg.sentMessages)
		}
	})

	t.Run("delete failure", func(t *testing.T) {
		oc := &mockOpencodeClient{deleteSession: func(string) error { return fmt.Errorf("failed") }}
		app, tg, _ := testBotApp(&Config{AdminIDs: map[int64]bool{1: true}}, oc)
		app.handleDeleteSession(1, "ses_x", 1)
		if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "Failed to delete") {
			t.Fatalf("expected failure message, got %+v", tg.sentMessages)
		}
	})
}

func TestBotApp_HandleSelectSession(t *testing.T) {
	t.Run("usage", func(t *testing.T) {
		app, tg, _ := testBotApp(&Config{}, &mockOpencodeClient{})
		app.handleSelectSession(1, "", 7)
		if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "Usage: /selectsession") {
			t.Fatalf("expected usage message, got %+v", tg.sentMessages)
		}
	})

	t.Run("direct id", func(t *testing.T) {
		app, tg, st := testBotApp(&Config{}, &mockOpencodeClient{})
		app.handleSelectSession(1, "ses_abc", 7)

		if sid, ok := st.GetUserSession(7); !ok || sid != "ses_abc" {
			t.Fatalf("expected ses_abc selected, got %q ok=%v", sid, ok)
		}
		if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "Selected session") {
			t.Fatalf("expected selected message, got %+v", tg.sentMessages)
		}
	})

	t.Run("find by title prefix", func(t *testing.T) {
		oc := &mockOpencodeClient{listSessions: func() ([]map[string]any, error) {
			return []map[string]any{{"id": "ses_1", "title": "alpha-chat"}}, nil
		}}
		app, tg, st := testBotApp(&Config{}, oc)
		app.handleSelectSession(1, "alpha", 7)

		if sid, ok := st.GetUserSession(7); !ok || sid != "ses_1" {
			t.Fatalf("expected ses_1 selected, got %q ok=%v", sid, ok)
		}
		if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "ses_1") {
			t.Fatalf("expected selected response, got %+v", tg.sentMessages)
		}
	})

	t.Run("list sessions failure", func(t *testing.T) {
		oc := &mockOpencodeClient{listSessions: func() ([]map[string]any, error) { return nil, fmt.Errorf("down") }}
		app, tg, _ := testBotApp(&Config{}, oc)
		app.handleSelectSession(1, "alpha", 7)
		if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "Error listing sessions") {
			t.Fatalf("expected list error message, got %+v", tg.sentMessages)
		}
	})

	t.Run("no match", func(t *testing.T) {
		oc := &mockOpencodeClient{listSessions: func() ([]map[string]any, error) {
			return []map[string]any{{"id": "ses_1", "title": "beta-chat"}}, nil
		}}
		app, tg, _ := testBotApp(&Config{}, oc)
		app.handleSelectSession(1, "alpha", 7)
		if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "No session found") {
			t.Fatalf("expected no-match message, got %+v", tg.sentMessages)
		}
	})
}

func TestBotApp_HandleMySessionAndAbort(t *testing.T) {
	t.Run("my session missing", func(t *testing.T) {
		app, tg, _ := testBotApp(&Config{}, &mockOpencodeClient{})
		app.handleMySession(1, 7)
		if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "have not selected") {
			t.Fatalf("expected not-selected message, got %+v", tg.sentMessages)
		}
	})

	t.Run("abort success", func(t *testing.T) {
		oc := &mockOpencodeClient{abortSession: func(string) error { return nil }}
		app, tg, _ := testBotApp(&Config{AdminIDs: map[int64]bool{7: true}}, oc)
		app.handleAbort(1, "ses_1", 7)
		if len(tg.sentMessages) != 1 || tg.sentMessages[0].Text != "Aborted session: ses_1" {
			t.Fatalf("expected success abort message, got %+v", tg.sentMessages)
		}
	})

	t.Run("abort usage/admin/error", func(t *testing.T) {
		oc := &mockOpencodeClient{abortSession: func(string) error { return fmt.Errorf("abort failed") }}
		app, tg, _ := testBotApp(&Config{AdminIDs: map[int64]bool{7: true}}, oc)

		app.handleAbort(1, "", 7)
		app.handleAbort(1, "ses_1", 8)
		app.handleAbort(1, "ses_1", 7)

		if len(tg.sentMessages) != 3 {
			t.Fatalf("expected 3 abort messages, got %d", len(tg.sentMessages))
		}
		if !strings.Contains(tg.sentMessages[0].Text, "Usage: /abort") {
			t.Fatalf("expected usage message, got %q", tg.sentMessages[0].Text)
		}
		if !strings.Contains(tg.sentMessages[1].Text, "Only admins") {
			t.Fatalf("expected admin message, got %q", tg.sentMessages[1].Text)
		}
		if !strings.Contains(tg.sentMessages[2].Text, "Abort failed") {
			t.Fatalf("expected abort failure message, got %q", tg.sentMessages[2].Text)
		}
	})
}

func TestBotApp_HandleRun(t *testing.T) {
	app, tg, st := testBotApp(&Config{}, &mockOpencodeClient{})
	_ = st.SetUserAgentKey(7, "agent-key")
	app.backendURL = "http://example.invalid"
	app.httpClient = &http.Client{Timeout: 200 * time.Millisecond}

	app.handleRun(1, "", 7)
	if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "Usage: /run") {
		t.Fatalf("expected usage message, got %+v", tg.sentMessages)
	}
}

func TestBotApp_StartPolling(t *testing.T) {
	t.Run("disallowed users get guidance except start/help", func(t *testing.T) {
		oc := &mockOpencodeClient{
			listSessions: func() ([]map[string]any, error) {
				return []map[string]any{{"id": "ses_user_1", "title": "oct_user_1"}}, nil
			},
		}
		app, tg, _ := testBotApp(&Config{AllowedIDs: map[int64]bool{1: true}, SessionPrefix: "oct_"}, oc)

		updates := make(chan tgbotapi.Update, 6)
		tg.updates = updates
		tg.nextMsgID = 100
		updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 2}, Text: "/start", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 6}}}}
		updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 2}, Text: "/help", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 5}}}}
		updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 2}, Text: "/run hi", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 4}}}}
		updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 2}, Text: "hello"}}
		close(updates)

		if err := app.StartPolling(); err != nil {
			t.Fatalf("StartPolling returned error: %v", err)
		}
		if len(tg.sentMessages) != 4 {
			t.Fatalf("expected 4 responses, got %d", len(tg.sentMessages))
		}
		if !strings.Contains(tg.sentMessages[2].Text, "Access required") || !strings.Contains(tg.sentMessages[3].Text, "Access required") {
			t.Fatalf("expected explicit guidance for protected paths, got %+v", tg.sentMessages)
		}
	})

	t.Run("baseline commands and callbacks are handled", func(t *testing.T) {
		oc := &mockOpencodeClient{
			listSessions: func() ([]map[string]any, error) {
				return []map[string]any{{"id": "ses_user_1", "title": "oct_user_1"}}, nil
			},
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/command", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"ok":true}`))
		})
		mux.HandleFunc("/v1/result/status", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		app, tg, st := testBotApp(&Config{AllowedIDs: map[int64]bool{1: true}, OpencodeBase: "http://local", SessionPrefix: "oct_"}, oc)
		app.backendURL = srv.URL
		_ = st.SetUserAgentKey(1, "agent-key")

		updates := make(chan tgbotapi.Update, 10)
		tg.updates = updates
		updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/status", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 7}}}}
		updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/settings", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 9}}}}
		updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/language", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 9}}}}
		updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/mute", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 5}}}}
		updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/unmute", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 7}}}}
		updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/status", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 7}}}}
		updates <- tgbotapi.Update{CallbackQuery: &tgbotapi.CallbackQuery{ID: "cb-1", Data: "settings:language", From: &tgbotapi.User{ID: 1}, Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}}}}
		close(updates)

		if err := app.StartPolling(); err != nil {
			t.Fatalf("StartPolling returned error: %v", err)
		}
		if len(tg.requests) != 1 {
			t.Fatalf("expected callback ack request, got %d", len(tg.requests))
		}
		if _, ok := tg.requests[0].(tgbotapi.CallbackConfig); !ok {
			t.Fatalf("expected CallbackConfig request, got %T", tg.requests[0])
		}
		if len(tg.sentMessages) < 7 {
			t.Fatalf("expected command and callback messages, got %d", len(tg.sentMessages))
		}
	})
}

func TestBotApp_RequestWithRetry(t *testing.T) {
	app, tg, _ := testBotApp(&Config{}, &mockOpencodeClient{})
	tg.requestErrs = []error{fmt.Errorf("429 too many requests"), nil}
	app.sleep = func(time.Duration) {}

	err := app.requestWithRetry(tgbotapi.NewCallback("cb", ""))
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if len(tg.requests) != 2 {
		t.Fatalf("expected two request attempts, got %d", len(tg.requests))
	}
}

func TestBotApp_HandleCallbackQuery_ErrorPathStillAcknowledges(t *testing.T) {
	app, tg, _ := testBotApp(&Config{}, &mockOpencodeClient{})
	tg.requestErrs = []error{fmt.Errorf("request failed")}

	app.handleCallbackQuery(&tgbotapi.CallbackQuery{
		ID:   "cb-err",
		Data: "settings:language",
		Message: &tgbotapi.Message{
			Chat: &tgbotapi.Chat{ID: 42},
		},
	})

	if len(tg.requests) != 1 {
		t.Fatalf("expected callback ack attempt, got %d requests", len(tg.requests))
	}
	if _, ok := tg.requests[0].(tgbotapi.CallbackConfig); !ok {
		t.Fatalf("expected CallbackConfig ack request, got %T", tg.requests[0])
	}
	if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "Unable to process action") {
		t.Fatalf("expected explicit fallback message on callback error, got %+v", tg.sentMessages)
	}
}

func TestBotApp_HandleCallbackQuery_UnknownActionFallbackRemains(t *testing.T) {
	app, tg, _ := testBotApp(&Config{}, &mockOpencodeClient{})

	app.handleCallbackQuery(&tgbotapi.CallbackQuery{
		ID:   "cb-unknown",
		Data: "settings:unknown",
		Message: &tgbotapi.Message{
			Chat: &tgbotapi.Chat{ID: 7},
		},
	})

	if len(tg.requests) != 1 {
		t.Fatalf("expected callback ack attempt, got %d requests", len(tg.requests))
	}
	if len(tg.sentMessages) != 1 || tg.sentMessages[0].Text != "Unknown settings action." {
		t.Fatalf("expected unknown-action fallback message, got %+v", tg.sentMessages)
	}
}
