package bot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

func TestBotProjectAddPairingAndRegistrationFlow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/pair/start", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"pairing_code": "PAIR-1", "expires_at": time.Now().UTC().Format(time.RFC3339Nano)})
	})
	mux.HandleFunc("/v1/pair/claim", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"agent_id": "a1", "agent_key": "k1"})
	})
	var commandTypes []string
	mux.HandleFunc("/v1/command", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if tp, ok := body["type"].(string); ok {
			commandTypes = append(commandTypes, tp)
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	mux.HandleFunc("/v1/result/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	app, tg, st := testBotApp(&Config{}, &mockOpencodeClient{})
	app.backendURL = srv.URL
	app.httpClient = &http.Client{Timeout: 200 * time.Millisecond}

	// First call starts pairing
	app.handleProjectAdd(1, "/tmp/demo", 7)
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[0].Text, "Pairing initiated") {
		t.Fatalf("expected pairing initiated message, got %+v", tg.sentMessages)
	}

	// Second call claims pairing using stored code
	tg.sentMessages = nil
	app.handleProjectAdd(1, "/tmp/demo", 7)
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[0].Text, "Pairing completed") {
		t.Fatalf("expected pairing completed message, got %+v", tg.sentMessages)
	}

	// Third call queues register command
	tg.sentMessages = nil
	app.handleProjectAdd(1, "/tmp/demo", 7)
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[0].Text, "registration queued") {
		t.Fatalf("expected registration queued message, got %+v", tg.sentMessages)
	}
	if len(commandTypes) == 0 || commandTypes[len(commandTypes)-1] != contracts.CommandTypeRegisterProject {
		t.Fatalf("expected register_project command, got %v", commandTypes)
	}

	if key, ok := st.GetUserAgentKey(7); !ok || key != "k1" {
		t.Fatalf("expected agent key k1 in store, got %q ok=%v", key, ok)
	}
}

func TestBotProjectListResolveAndHelpers(t *testing.T) {
	exp := time.Now().UTC().Add(5 * time.Minute)
	projects := []projectRecord{{Alias: "demo", ProjectID: "p1", Policy: approvalDecision{Decision: contracts.DecisionAllow, ExpiresAt: &exp, Scope: []string{contracts.ScopeRunTask}}}}
	app, tg, _ := testBotApp(&Config{}, &mockOpencodeClient{})
	app.listProjectsFn = func(userID int64) ([]projectRecord, error) {
		return projects, nil
	}

	app.handleProjectList(10, 9)
	if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "demo") {
		t.Fatalf("expected project list message, got %+v", tg.sentMessages)
	}

	proj, err := app.resolveProject(9, "DEMO")
	if err != nil || proj == nil || proj.ProjectID != "p1" {
		t.Fatalf("expected resolve by alias, got proj=%+v err=%v", proj, err)
	}
	if !app.policyAllows(proj.Policy, contracts.ScopeRunTask) {
		t.Fatal("expected policy to allow run scope")
	}
	if app.policyAllows(proj.Policy, contracts.ScopeStartServer) {
		t.Fatal("did not expect policy to allow start scope")
	}

	if got := projectAliasFromPath("/tmp/demo/"); got != "demo" {
		t.Fatalf("unexpected alias from path: %q", got)
	}
	if got := projectAliasFromPath("   "); got != "" {
		t.Fatalf("expected empty alias for whitespace input, got %q", got)
	}
}

func TestBotCommandStorageAndFormattingHelpers(t *testing.T) {
	app, _, _ := testBotApp(&Config{}, &mockOpencodeClient{})
	now := time.Now().UTC()
	app.storeCommand(7, commandRecord{CommandID: "c1", Type: contracts.CommandTypeStatus, Alias: "demo", CreatedAt: now})
	app.storeCommand(7, commandRecord{CommandID: "c2", Type: contracts.CommandTypeRunTask, Alias: "demo", CreatedAt: now})

	if rec, ok := app.getLastCommand(7, contracts.CommandTypeRunTask, "demo"); !ok || rec.CommandID != "c2" {
		t.Fatalf("expected latest run command c2, got rec=%+v ok=%v", rec, ok)
	}

	long := strings.Repeat("x", 3000)
	if len(truncateOutput(long)) != 2051 { // 2048 + "..."
		t.Fatalf("truncateOutput length mismatch: %d", len(truncateOutput(long)))
	}
	formatted := formatSummary(&contracts.CommandResult{Summary: "ok", Stdout: "out", Stderr: "err"})
	if !strings.Contains(formatted, "ok") || !strings.Contains(formatted, "out") || !strings.Contains(formatted, "err") {
		t.Fatalf("unexpected formatted summary: %q", formatted)
	}
}

func TestBotFetchResultAndPollRelay(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/result/status", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("command_id") == "missing" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(contracts.CommandResult{CommandID: "c1", OK: true, Summary: "done"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	app, tg, _ := testBotApp(&Config{}, &mockOpencodeClient{})
	app.backendURL = srv.URL
	app.httpClient = &http.Client{Timeout: 200 * time.Millisecond}

	res, err := app.fetchResult(1, "c1")
	if err != nil || res == nil || res.Summary != "done" {
		t.Fatalf("expected fetch result success, got res=%+v err=%v", res, err)
	}
	none, err := app.fetchResult(1, "missing")
	if err != nil || none != nil {
		t.Fatalf("expected no content as nil result, got res=%+v err=%v", none, err)
	}

	app.pollAndRelayResult(42, 1, "c1")
	time.Sleep(250 * time.Millisecond)
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[len(tg.sentMessages)-1].Text, "Result:") {
		t.Fatalf("expected relayed result message, got %+v", tg.sentMessages)
	}
}

func TestBotStartServerAndRunPaths(t *testing.T) {
	projects := []projectRecord{{Alias: "demo", ProjectID: "p1", Policy: approvalDecision{Decision: contracts.DecisionAllow, Scope: []string{contracts.ScopeStartServer, contracts.ScopeRunTask}}}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/command", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	mux.HandleFunc("/v1/result/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	app, tg, st := testBotApp(&Config{}, &mockOpencodeClient{})
	app.backendURL = srv.URL
	app.httpClient = &http.Client{Timeout: 200 * time.Millisecond}
	app.listProjectsFn = func(userID int64) ([]projectRecord, error) { return projects, nil }
	_ = st.SetUserAgentKey(7, "agent-key")

	app.handleStartServer(1, "demo", 7)
	app.handleRun(1, "demo hello world", 7)

	if len(tg.sentMessages) < 2 {
		t.Fatalf("expected start/run queue messages, got %+v", tg.sentMessages)
	}
	joined := ""
	for _, m := range tg.sentMessages {
		joined += m.Text + "\n"
	}
	if !strings.Contains(joined, "start_server queued") || !strings.Contains(joined, "run_task queued") {
		t.Fatalf("expected queued confirmations, got %s", joined)
	}

	// Invalid usage branches
	tg.sentMessages = nil
	app.handleStartServer(1, "", 7)
	app.handleRun(1, "demo", 7)
	if len(tg.sentMessages) != 2 {
		t.Fatalf("expected two usage errors, got %+v", tg.sentMessages)
	}
	if !strings.Contains(tg.sentMessages[0].Text, "Usage: /start_server") || !strings.Contains(tg.sentMessages[1].Text, "Usage: /run") {
		t.Fatalf("unexpected usage responses: %+v", tg.sentMessages)
	}
}

func TestBotSessionRunHelpers(t *testing.T) {
	app, _, _ := testBotApp(&Config{}, &mockOpencodeClient{
		listSessions: func() ([]map[string]any, error) {
			return []map[string]any{{"id": "ses_1", "title": "oct_user_1"}}, nil
		},
		createSession: func(title string) (map[string]any, error) {
			return map[string]any{"id": "ses_new", "title": title}, nil
		},
	})

	if !app.tryStartRun(1, 2, "ses_1") {
		t.Fatal("expected first run lock to succeed")
	}
	if app.tryStartRun(1, 2, "ses_2") {
		t.Fatal("expected second run lock to fail for same key")
	}
	app.clearRun(1, 2)
	if !app.tryStartRun(1, 2, "ses_3") {
		t.Fatal("expected lock after clear")
	}
	if !app.clearRunBySession("ses_3") {
		t.Fatal("expected clear by session to succeed")
	}

	if exists, err := app.sessionExists("ses_1"); err != nil || !exists {
		t.Fatalf("expected sessionExists true, got exists=%v err=%v", exists, err)
	}
	if sid, missing, err := app.resolveUserSession(999); err != nil || missing || sid == "" {
		t.Fatalf("expected fallback/create session, got sid=%q missing=%v err=%v", sid, missing, err)
	}

	app.oc = &mockOpencodeClient{listSessions: func() ([]map[string]any, error) { return nil, fmt.Errorf("down") }}
	if _, _, err := app.resolveUserSession(123); err == nil {
		t.Fatal("expected resolveUserSession to fail when list sessions fails")
	}
}
