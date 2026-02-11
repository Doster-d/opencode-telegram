package bot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opencode-telegram/internal/proxy/contracts"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestBotApprovalDecision_Guardrails(t *testing.T) {
	app, tg, _ := testBotApp(&Config{}, &mockOpencodeClient{})

	app.handleApprovalDecision(&tgbotapi.CallbackQuery{ID: "cb1", Data: "approve:deny|demo"})
	if len(tg.sentMessages) != 0 {
		t.Fatalf("expected no message when callback has no message/from, got %+v", tg.sentMessages)
	}

	app.handleApprovalDecision(&tgbotapi.CallbackQuery{ID: "cb2", Data: "approve:deny", Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}}, From: &tgbotapi.User{ID: 7}})
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[len(tg.sentMessages)-1].Text, "Invalid approval payload") {
		t.Fatalf("expected invalid payload message, got %+v", tg.sentMessages)
	}
}

func TestBotApprovalDecision_ResolveAndPairingFailures(t *testing.T) {
	app, tg, st := testBotApp(&Config{}, &mockOpencodeClient{})
	app.listProjectsFn = func(userID int64) ([]projectRecord, error) { return nil, nil }

	cb := &tgbotapi.CallbackQuery{ID: "cb", Data: "approve:allow30:start|demo", Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}}, From: &tgbotapi.User{ID: 7}}
	app.handleApprovalDecision(cb)
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[len(tg.sentMessages)-1].Text, "Unable to resolve project") {
		t.Fatalf("expected resolve failure message, got %+v", tg.sentMessages)
	}

	app.listProjectsFn = func(userID int64) ([]projectRecord, error) {
		return []projectRecord{{Alias: "demo", ProjectID: "p1", Policy: approvalDecision{Decision: contracts.DecisionDeny}}}, nil
	}
	tg.sentMessages = nil
	_ = st.SetUserAgentKey(7, "")
	app.handleApprovalDecision(cb)
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[len(tg.sentMessages)-1].Text, "not paired") {
		t.Fatalf("expected not paired message, got %+v", tg.sentMessages)
	}
}

func TestBotApprovalDecision_BackendPathsAndSuccess(t *testing.T) {
	var lastPayload map[string]any
	mux := http.NewServeMux()
	status := http.StatusAccepted
	mux.HandleFunc("/v1/command", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&lastPayload)
		w.WriteHeader(status)
		if status != http.StatusAccepted {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": map[string]any{"code": "ERR"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	app, tg, st := testBotApp(&Config{}, &mockOpencodeClient{})
	app.backendURL = srv.URL
	app.httpClient = &http.Client{Timeout: 200 * time.Millisecond}
	app.listProjectsFn = func(userID int64) ([]projectRecord, error) {
		return []projectRecord{{Alias: "demo", ProjectID: "p1", Policy: approvalDecision{Decision: contracts.DecisionDeny}}}, nil
	}
	_ = st.SetUserAgentKey(7, "agent-key")

	cb := &tgbotapi.CallbackQuery{ID: "cb", Data: "approve:allow30:both|demo", Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}}, From: &tgbotapi.User{ID: 7}}
	app.handleApprovalDecision(cb)
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[len(tg.sentMessages)-1].Text, "Policy updated") {
		t.Fatalf("expected success message, got %+v", tg.sentMessages)
	}
	if lastPayload["type"] != contracts.CommandTypeApplyProjectPolicy {
		t.Fatalf("expected apply policy command, got %+v", lastPayload)
	}
	payload, _ := lastPayload["payload"].(map[string]any)
	if payload["decision"] != contracts.DecisionAllow {
		t.Fatalf("expected allow decision, got %+v", payload)
	}
	scopeRaw, _ := payload["scope"].([]any)
	if len(scopeRaw) != 2 {
		t.Fatalf("expected two scopes for allow30:both, got %+v", payload)
	}
	if _, ok := payload["expires_at"].(string); !ok {
		t.Fatalf("expected expires_at for allow30 option, got %+v", payload)
	}

	tg.sentMessages = nil
	status = http.StatusBadRequest
	app.handleApprovalDecision(cb)
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[len(tg.sentMessages)-1].Text, "Failed to queue approval") {
		t.Fatalf("expected queue failure message, got %+v", tg.sentMessages)
	}
}

func TestBotUpdateLocalPolicyNoopCoverage(t *testing.T) {
	app, _, _ := testBotApp(&Config{}, &mockOpencodeClient{})
	app.listProjectsFn = func(userID int64) ([]projectRecord, error) {
		exp := time.Now().UTC().Add(5 * time.Minute)
		return []projectRecord{{Alias: "demo", ProjectID: "p1", Policy: approvalDecision{Decision: contracts.DecisionDeny, ExpiresAt: &exp}}}, nil
	}

	app.updateLocalPolicy(7, "p1", contracts.DecisionAllow, []string{contracts.ScopeRunTask}, nil)
	app.listProjectsFn = func(userID int64) ([]projectRecord, error) { return nil, errSentinel("boom") }
	app.updateLocalPolicy(7, "p1", contracts.DecisionAllow, nil, nil)
}

type errSentinel string

func (e errSentinel) Error() string { return string(e) }
