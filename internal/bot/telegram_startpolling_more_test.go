package bot

import (
	"net/http"
	"net/http/httptest"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestBotStartPolling_CommandRoutingCoverage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/command", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/v1/pair/start", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"pairing_code":"PAIR-1","expires_at":"soon"}`))
	})
	mux.HandleFunc("/v1/result/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	oc := &mockOpencodeClient{
		listSessions: func() ([]map[string]any, error) { return []map[string]any{{"id": "ses_1", "title": "demo"}}, nil },
		createSession: func(title string) (map[string]any, error) {
			return map[string]any{"id": "ses_new", "title": title}, nil
		},
		deleteSession: func(sessionID string) error { return nil },
	}
	app, tg, st := testBotApp(&Config{AllowedIDs: map[int64]bool{1: true}, AdminIDs: map[int64]bool{1: true}}, oc)
	app.backendURL = srv.URL
	app.listProjectsFn = func(userID int64) ([]projectRecord, error) {
		return []projectRecord{{Alias: "demo", ProjectID: "p1", Policy: approvalDecision{Decision: "ALLOW", Scope: []string{"START_SERVER", "RUN_TASK"}}}}, nil
	}
	_ = st.SetUserAgentKey(1, "agent-key")

	updates := make(chan tgbotapi.Update, 16)
	tg.updates = updates
	updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/createsession test", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 14}}}}
	updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/deletesession ses_x", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 14}}}}
	updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/selectsession ses_1", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 14}}}}
	updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/mysession", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 10}}}}
	updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/project", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 8}}}}
	updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/project list", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 8}}}}
	updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/project add /tmp/demo", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 8}}}}
	updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/project noop", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 8}}}}
	updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/start_server demo", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 13}}}}
	updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/pair", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 5}}}}
	updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/agent_status", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 13}}}}
	updates <- tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "/nope", Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 5}}}}
	close(updates)

	if err := app.StartPolling(); err != nil {
		t.Fatalf("start polling: %v", err)
	}
}
