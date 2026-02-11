package bot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opencode-telegram/internal/proxy/contracts"
	"opencode-telegram/pkg/store"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type recordingBot struct {
	sent []string
}

func (b *recordingBot) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	return &tgbotapi.APIResponse{}, nil
}

func (b *recordingBot) GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel {
	return nil
}

func (b *recordingBot) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	if msg, ok := c.(tgbotapi.MessageConfig); ok {
		b.sent = append(b.sent, msg.Text)
	}
	return tgbotapi.Message{}, nil
}

func TestBotApprovalAndAliasResolution(t *testing.T) {
	projects := []projectRecord{{
		Alias:     "demo",
		ProjectID: "proj-1",
		Policy:    approvalDecision{Decision: contracts.DecisionDeny, Scope: []string{}},
	}}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"projects": projects})
	})
	mux.HandleFunc("/v1/command", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	mux.HandleFunc("/v1/result/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	bot := &recordingBot{}
	app := &BotApp{
		tg:         bot,
		cfg:        &Config{},
		store:      store.NewMemoryStore(),
		httpClient: &http.Client{Timeout: 200 * time.Millisecond},
		backendURL: srv.URL,
	}
	_ = app.store.SetUserAgentKey(7, "agent-key")

	app.handleStartServer(1, "demo", 7)
	if len(bot.sent) == 0 || !strings.Contains(bot.sent[0], "Approval required") {
		t.Fatalf("expected approval prompt, got %v", bot.sent)
	}

	bot.sent = nil
	app.handleRun(1, "demo hello", 7)
	if len(bot.sent) == 0 || !strings.Contains(bot.sent[0], "Approval required") {
		t.Fatalf("expected approval prompt for run, got %v", bot.sent)
	}
}
