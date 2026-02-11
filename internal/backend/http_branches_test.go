package backend

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

type captureNotifier struct {
	called bool
	userID string
	result contracts.CommandResult
}

func (n *captureNotifier) NotifyResult(userID string, result contracts.CommandResult) {
	n.called = true
	n.userID = userID
	n.result = result
}

func TestHTTPHandlers_MethodNotAllowedCoverage(t *testing.T) {
	b := NewMemoryBackend()
	q := NewRedisQueue(NewInMemoryRedisClient())
	srv := NewServer(b, q)

	paths := []string{"/v1/pair/start", "/v1/pair/claim", "/v1/command", "/v1/poll", "/v1/result", "/v1/projects", "/v1/result/status"}
	for _, p := range paths {
		req := httptest.NewRequest(http.MethodPatch, p, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 for %s, got %d", p, rec.Code)
		}
	}
}

func TestHTTPPollAndResultStatusValidationBranches(t *testing.T) {
	b := NewMemoryBackend()
	q := NewRedisQueue(NewInMemoryRedisClient())
	srv := NewServer(b, q)
	agentKey := pairAgent(t, srv, "tg-poll")

	// Invalid timeout format
	reqBad := httptest.NewRequest(http.MethodGet, "/v1/poll?timeout_seconds=abc", nil)
	reqBad.Header.Set("Authorization", "Bearer "+agentKey)
	recBad := httptest.NewRecorder()
	srv.ServeHTTP(recBad, reqBad)
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad timeout, got %d", recBad.Code)
	}

	// Out-of-range timeout
	reqRange := httptest.NewRequest(http.MethodGet, "/v1/poll?timeout_seconds=61", nil)
	reqRange.Header.Set("Authorization", "Bearer "+agentKey)
	recRange := httptest.NewRecorder()
	srv.ServeHTTP(recRange, reqRange)
	if recRange.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for timeout range, got %d", recRange.Code)
	}

	// result status validation
	reqNoUser := httptest.NewRequest(http.MethodGet, "/v1/result/status?command_id=x", nil)
	recNoUser := httptest.NewRecorder()
	srv.ServeHTTP(recNoUser, reqNoUser)
	if recNoUser.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing user, got %d", recNoUser.Code)
	}

	reqNoCmd := httptest.NewRequest(http.MethodGet, "/v1/result/status?telegram_user_id=tg-poll", nil)
	recNoCmd := httptest.NewRecorder()
	srv.ServeHTTP(recNoCmd, reqNoCmd)
	if recNoCmd.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing command, got %d", recNoCmd.Code)
	}
}

func TestHTTPResultNotifierAndProjectsBranches(t *testing.T) {
	b := NewMemoryBackend()
	q := NewRedisQueue(NewInMemoryRedisClient())
	srv := NewServer(b, q)
	n := &captureNotifier{}
	srv.SetNotifier(n)
	agentKey := pairAgent(t, srv, "tg-notify")

	cmd := contracts.Command{CommandID: "cmd-n", IdempotencyKey: "k-n", Type: contracts.CommandTypeStatus, CreatedAt: time.Now().UTC(), Payload: json.RawMessage(`{}`)}
	cmdReq := httptest.NewRequest(http.MethodPost, "/v1/command", mustJSON(t, cmd))
	cmdReq.Header.Set("Authorization", "Bearer "+agentKey)
	cmdReq.Header.Set("Content-Type", "application/json")
	cmdRec := httptest.NewRecorder()
	srv.ServeHTTP(cmdRec, cmdReq)
	if cmdRec.Code != http.StatusAccepted {
		t.Fatalf("command enqueue failed: %d", cmdRec.Code)
	}

	pollReq := httptest.NewRequest(http.MethodGet, "/v1/poll?timeout_seconds=1", nil)
	pollReq.Header.Set("Authorization", "Bearer "+agentKey)
	pollRec := httptest.NewRecorder()
	srv.ServeHTTP(pollRec, pollReq)
	if pollRec.Code != http.StatusOK {
		t.Fatalf("poll failed: %d", pollRec.Code)
	}

	res := contracts.CommandResult{CommandID: "cmd-n", OK: true, Summary: "ok"}
	resReq := httptest.NewRequest(http.MethodPost, "/v1/result", mustJSON(t, res))
	resReq.Header.Set("Authorization", "Bearer "+agentKey)
	resReq.Header.Set("Content-Type", "application/json")
	resRec := httptest.NewRecorder()
	srv.ServeHTTP(resRec, resReq)
	if resRec.Code != http.StatusOK {
		t.Fatalf("result failed: %d", resRec.Code)
	}
	if !n.called || n.userID != "tg-notify" || n.result.CommandID != "cmd-n" {
		t.Fatalf("expected notifier call, got called=%v user=%q result=%+v", n.called, n.userID, n.result)
	}

	// Missing command_id branch
	badResReq := httptest.NewRequest(http.MethodPost, "/v1/result", bytes.NewBufferString(`{"ok":true}`))
	badResReq.Header.Set("Authorization", "Bearer "+agentKey)
	badResReq.Header.Set("Content-Type", "application/json")
	badResRec := httptest.NewRecorder()
	srv.ServeHTTP(badResRec, badResReq)
	if badResRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing command_id, got %d", badResRec.Code)
	}

	// Projects validation
	noUserReq := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
	noUserRec := httptest.NewRecorder()
	srv.ServeHTTP(noUserRec, noUserReq)
	if noUserRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing telegram_user_id, got %d", noUserRec.Code)
	}
}

func TestHTTPAuthViaTelegramHeaderAndPolicyProjectionBranches(t *testing.T) {
	b := NewMemoryBackend()
	q := NewRedisQueue(NewInMemoryRedisClient())
	srv := NewServer(b, q)
	_ = pairAgent(t, srv, "tg-header")

	// Send command with telegram header auth path (no bearer).
	cmd := contracts.Command{
		CommandID:      "cmd-pol-head",
		IdempotencyKey: "idem-pol-head",
		Type:           contracts.CommandTypeApplyProjectPolicy,
		CreatedAt:      time.Now().UTC(),
		Payload:        json.RawMessage(`{"project_id":"pid-9","decision":"ALLOW","scope":["START_SERVER"]}`),
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/command", mustJSON(t, cmd))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Telegram-User-ID", "tg-header")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected accepted via telegram header auth, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Poll with bearer and post result with []any scope to exercise scopeFromMeta branch.
	agentID, _ := b.AgentIDForUser("tg-header")
	agentKey := b.agentKeyByAgent[agentID]
	pollReq := httptest.NewRequest(http.MethodGet, "/v1/poll?timeout_seconds=1", nil)
	pollReq.Header.Set("Authorization", "Bearer "+agentKey)
	pollRec := httptest.NewRecorder()
	srv.ServeHTTP(pollRec, pollReq)
	if pollRec.Code != http.StatusOK {
		t.Fatalf("expected poll 200, got %d", pollRec.Code)
	}

	result := contracts.CommandResult{CommandID: "cmd-pol-head", OK: true, Meta: map[string]any{"decision": contracts.DecisionAllow, "scope": []any{contracts.ScopeStartServer}}}
	resultReq := httptest.NewRequest(http.MethodPost, "/v1/result", mustJSON(t, result))
	resultReq.Header.Set("Authorization", "Bearer "+agentKey)
	resultReq.Header.Set("Content-Type", "application/json")
	resultRec := httptest.NewRecorder()
	srv.ServeHTTP(resultRec, resultReq)
	if resultRec.Code != http.StatusOK {
		t.Fatalf("expected result 200, got %d", resultRec.Code)
	}

	// result status query for unknown command path.
	missingReq := httptest.NewRequest(http.MethodGet, "/v1/result/status?telegram_user_id=tg-header&command_id=missing", nil)
	missingRec := httptest.NewRecorder()
	srv.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for missing command result, got %d", missingRec.Code)
	}

	// malformed command body for decode branch.
	badReq := httptest.NewRequest(http.MethodPost, "/v1/command", bytes.NewBufferString(`{bad`))
	badReq.Header.Set("Authorization", "Bearer "+agentKey)
	badRec := httptest.NewRecorder()
	srv.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed command body, got %d", badRec.Code)
	}
}
