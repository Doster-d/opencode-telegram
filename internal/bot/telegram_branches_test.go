package bot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

func TestBotHandleStartServerAndRun_ErrorBranches(t *testing.T) {
	app, tg, st := testBotApp(&Config{}, &mockOpencodeClient{})

	// not paired
	app.handleStartServer(1, "demo", 7)
	app.handleRun(1, "demo hello", 7)
	if len(tg.sentMessages) != 2 || !strings.Contains(tg.sentMessages[0].Text, "not paired") || !strings.Contains(tg.sentMessages[1].Text, "not paired") {
		t.Fatalf("expected not paired messages, got %+v", tg.sentMessages)
	}

	_ = st.SetUserAgentKey(7, "agent-key")
	app.listProjectsFn = func(userID int64) ([]projectRecord, error) { return nil, nil }

	tg.sentMessages = nil
	app.handleStartServer(1, "demo", 7)
	app.handleRun(1, "demo hello", 7)
	if len(tg.sentMessages) != 2 || !strings.Contains(tg.sentMessages[0].Text, "Unknown project alias") || !strings.Contains(tg.sentMessages[1].Text, "Unknown project alias") {
		t.Fatalf("expected unknown alias messages, got %+v", tg.sentMessages)
	}

	// policy denied branch
	app.listProjectsFn = func(userID int64) ([]projectRecord, error) {
		return []projectRecord{{Alias: "demo", ProjectID: "p1", Policy: approvalDecision{Decision: contracts.DecisionDeny}}}, nil
	}
	tg.sentMessages = nil
	app.handleStartServer(1, "demo", 7)
	app.handleRun(1, "demo hello", 7)
	if len(tg.sentMessages) < 2 || !strings.Contains(tg.sentMessages[0].Text, "Approval required") || !strings.Contains(tg.sentMessages[1].Text, "Approval required") {
		t.Fatalf("expected approval prompts, got %+v", tg.sentMessages)
	}
}

func TestBotHandleAgentStatusAndFetchResultBranches(t *testing.T) {
	app, tg, st := testBotApp(&Config{}, &mockOpencodeClient{})

	// not paired branch
	app.handleAgentStatus(1, 7)
	if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "not paired") {
		t.Fatalf("expected not paired message, got %+v", tg.sentMessages)
	}

	_ = st.SetUserAgentKey(7, "agent-key")
	statusCode := http.StatusAccepted
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/command", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": statusCode == http.StatusAccepted})
	})
	mux.HandleFunc("/v1/result/status", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("command_id") {
		case "none":
			w.WriteHeader(http.StatusNoContent)
		case "bad":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{bad`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	app.backendURL = srv.URL
	app.httpClient = &http.Client{Timeout: 300 * time.Millisecond}

	tg.sentMessages = nil
	app.handleAgentStatus(1, 7)
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[0].Text, "Status command queued") {
		t.Fatalf("expected queued status message, got %+v", tg.sentMessages)
	}

	statusCode = http.StatusBadRequest
	tg.sentMessages = nil
	app.handleAgentStatus(1, 7)
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[0].Text, "Failed to queue command") {
		t.Fatalf("expected queue failure status message, got %+v", tg.sentMessages)
	}

	if res, err := app.fetchResult(7, "none"); err != nil || res != nil {
		t.Fatalf("expected nil no-content result, got res=%+v err=%v", res, err)
	}
	if _, err := app.fetchResult(7, "bad"); err == nil {
		t.Fatal("expected decode error for malformed json")
	}
	if _, err := app.fetchResult(7, "err"); err == nil {
		t.Fatal("expected backend status error")
	}
}

func TestBotResolveUserSessionMissingSelectionBranch(t *testing.T) {
	app, _, st := testBotApp(&Config{SessionPrefix: "oct_"}, &mockOpencodeClient{
		listSessions: func() ([]map[string]any, error) {
			return []map[string]any{{"id": "ses_other", "title": "other"}}, nil
		},
		createSession: func(title string) (map[string]any, error) {
			return map[string]any{"id": "ses_created", "title": title}, nil
		},
	})

	_ = st.SetUserSession(7, "ses_missing")
	sid, missing, err := app.resolveUserSession(7)
	if err == nil || !missing || sid != "" {
		t.Fatalf("expected missing selected session error, got sid=%q missing=%v err=%v", sid, missing, err)
	}

	_ = st.DeleteUserSession(7)
	sid, missing, err = app.resolveUserSession(7)
	if err != nil || missing || sid != "ses_created" {
		t.Fatalf("expected fallback-created session, got sid=%q missing=%v err=%v", sid, missing, err)
	}

	// sessionExists error branch
	app.oc = &mockOpencodeClient{listSessions: func() ([]map[string]any, error) { return nil, errSentinel("down") }}
	if _, err := app.sessionExists("ses-any"); err == nil {
		t.Fatal("expected sessionExists error when list sessions fails")
	}
}

func TestBotPollAndRelayErrorResultBranch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/result/status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(contracts.CommandResult{CommandID: "c1", OK: false, ErrorCode: contracts.ErrPolicyDenied})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	app, tg, _ := testBotApp(&Config{}, &mockOpencodeClient{})
	app.backendURL = srv.URL
	app.httpClient = &http.Client{Timeout: 200 * time.Millisecond}

	app.pollAndRelayResult(42, 7, "c1")
	time.Sleep(250 * time.Millisecond)
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[len(tg.sentMessages)-1].Text, "Result error") {
		t.Fatalf("expected error result relay message, got %+v", tg.sentMessages)
	}
}
