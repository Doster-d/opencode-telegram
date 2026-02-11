package backend

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

func TestHTTPHelpers_MetaParsersAndServerError(t *testing.T) {
	if got := stringFromMeta("ok", "fallback"); got != "ok" {
		t.Fatalf("expected ok, got %q", got)
	}
	if got := stringFromMeta(42, "fallback"); got != "fallback" {
		t.Fatalf("expected fallback, got %q", got)
	}

	if got := scopeFromMeta([]string{"A", "B"}); len(got) != 2 {
		t.Fatalf("expected []string passthrough, got %+v", got)
	}
	if got := scopeFromMeta([]any{"A", 1, "B"}); len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Fatalf("expected []any conversion, got %+v", got)
	}
	if got := scopeFromMeta("bad"); got != nil {
		t.Fatalf("expected nil for invalid scope, got %+v", got)
	}

	exp := time.Now().UTC().Truncate(time.Second)
	if got := expiresAtFromMeta(exp.Format(time.RFC3339Nano)); got == nil || !got.Equal(exp) {
		t.Fatalf("expected parsed expires_at, got %+v", got)
	}
	if got := expiresAtFromMeta("bad"); got != nil {
		t.Fatalf("expected nil for invalid expires_at, got %+v", got)
	}

	rec := httptest.NewRecorder()
	writeServerError(rec, contracts.APIError{Code: contracts.ErrPairingExpired, Message: "expired"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for pairing expired, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	writeServerError(rec, contracts.APIError{Code: contracts.ErrValidationInvalidRequest, Message: "bad"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for api error, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	writeServerError(rec, errors.New("boom"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for generic error, got %d", rec.Code)
	}
}

func TestServer_SetNotifierAndResultNotification(t *testing.T) {
	b := NewMemoryBackend()
	srv := NewServer(b, b)
	srv.SetNotifier(nil)

	agentKey := pairAgent(t, srv, "tg-notify")
	cmd := contracts.Command{CommandID: "cmd-notify", IdempotencyKey: "key-notify", Type: contracts.CommandTypeStatus, CreatedAt: time.Now().UTC(), Payload: json.RawMessage(`{}`)}
	req := httptest.NewRequest(http.MethodPost, "/v1/command", mustJSON(t, cmd))
	req.Header.Set("Authorization", "Bearer "+agentKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("queue command status=%d body=%s", rec.Code, rec.Body.String())
	}

	notified := 0
	var gotUser string
	var gotResult contracts.CommandResult
	srv.SetNotifier(resultNotifierFunc(func(user string, result contracts.CommandResult) {
		notified++
		gotUser = user
		gotResult = result
	}))

	resultReq := httptest.NewRequest(http.MethodPost, "/v1/result", mustJSON(t, contracts.CommandResult{CommandID: "cmd-notify", OK: true, Summary: "ok"}))
	resultReq.Header.Set("Authorization", "Bearer "+agentKey)
	resultReq.Header.Set("Content-Type", "application/json")
	resultRec := httptest.NewRecorder()
	srv.ServeHTTP(resultRec, resultReq)
	if resultRec.Code != http.StatusOK {
		t.Fatalf("result status=%d body=%s", resultRec.Code, resultRec.Body.String())
	}
	if notified != 1 || gotUser != "tg-notify" || gotResult.CommandID != "cmd-notify" {
		t.Fatalf("unexpected notifier calls=%d user=%q result=%+v", notified, gotUser, gotResult)
	}
}

type resultNotifierFunc func(telegramUserID string, result contracts.CommandResult)

func (f resultNotifierFunc) NotifyResult(telegramUserID string, result contracts.CommandResult) {
	f(telegramUserID, result)
}

func TestMemoryBackend_UpdateProjectPolicyPublicMethod(t *testing.T) {
	b := NewMemoryBackend()
	b.SetProject("u1", projectRecord{Alias: "demo", ProjectID: "p1", ProjectPath: "/tmp/demo", Policy: projectPolicy{Decision: contracts.DecisionDeny}})
	exp := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Second)
	b.UpdateProjectPolicy("u1", "p1", projectPolicy{Decision: contracts.DecisionAllow, Scope: []string{contracts.ScopeRunTask}, ExpiresAt: &exp})

	proj, ok := b.ResolveProject("u1", "p1")
	if !ok {
		t.Fatal("expected project to exist")
	}
	if proj.Policy.Decision != contracts.DecisionAllow || len(proj.Policy.Scope) != 1 || proj.Policy.Scope[0] != contracts.ScopeRunTask {
		t.Fatalf("unexpected updated policy: %+v", proj.Policy)
	}
	if proj.Policy.ExpiresAt == nil || !proj.Policy.ExpiresAt.Equal(exp) {
		t.Fatalf("expected expires_at %v, got %+v", exp, proj.Policy.ExpiresAt)
	}
}

func TestHTTPHandlers_AdditionalErrorBranches(t *testing.T) {
	b := NewMemoryBackend()
	srv := NewServer(b, b)
	agentKey := pairAgent(t, srv, "tg-http-more")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/pair/claim", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected pair/claim 405, got %d", rec.Code)
	}

	badCmdReq := httptest.NewRequest(http.MethodPost, "/v1/command", mustJSON(t, map[string]any{"command_id": "c1"}))
	badCmdReq.Header.Set("Authorization", "Bearer "+agentKey)
	badCmdReq.Header.Set("Content-Type", "application/json")
	badCmdRec := httptest.NewRecorder()
	srv.ServeHTTP(badCmdRec, badCmdReq)
	if badCmdRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid command 400, got %d body=%s", badCmdRec.Code, badCmdRec.Body.String())
	}

	pollReq := httptest.NewRequest(http.MethodGet, "/v1/poll?timeout_seconds=0", nil)
	pollReq.Header.Set("Authorization", "Bearer "+agentKey)
	pollRec := httptest.NewRecorder()
	srv.ServeHTTP(pollRec, pollReq)
	if pollRec.Code != http.StatusBadRequest {
		t.Fatalf("expected poll timeout validation 400, got %d", pollRec.Code)
	}

	resultReq := httptest.NewRequest(http.MethodPost, "/v1/result", mustJSON(t, contracts.CommandResult{OK: true}))
	resultReq.Header.Set("Authorization", "Bearer "+agentKey)
	resultReq.Header.Set("Content-Type", "application/json")
	resultRec := httptest.NewRecorder()
	srv.ServeHTTP(resultRec, resultReq)
	if resultRec.Code != http.StatusBadRequest {
		t.Fatalf("expected result missing command_id 400, got %d", resultRec.Code)
	}

	projectsReq := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
	projectsRec := httptest.NewRecorder()
	srv.ServeHTTP(projectsRec, projectsReq)
	if projectsRec.Code != http.StatusBadRequest {
		t.Fatalf("expected projects missing user 400, got %d", projectsRec.Code)
	}

	statusMissingReq := httptest.NewRequest(http.MethodGet, "/v1/result/status?telegram_user_id=tg-http-more", nil)
	statusMissingRec := httptest.NewRecorder()
	srv.ServeHTTP(statusMissingRec, statusMissingReq)
	if statusMissingRec.Code != http.StatusBadRequest {
		t.Fatalf("expected result/status missing command_id 400, got %d", statusMissingRec.Code)
	}

	statusNoPairReq := httptest.NewRequest(http.MethodGet, "/v1/result/status?telegram_user_id=missing&command_id=c1", nil)
	statusNoPairRec := httptest.NewRecorder()
	srv.ServeHTTP(statusNoPairRec, statusNoPairReq)
	if statusNoPairRec.Code != http.StatusNoContent {
		t.Fatalf("expected result/status no pair 204, got %d", statusNoPairRec.Code)
	}

	xHeaderCmd := contracts.Command{CommandID: "cmd-x", IdempotencyKey: "k-x", Type: contracts.CommandTypeStatus, CreatedAt: time.Now().UTC(), Payload: json.RawMessage(`{}`)}
	xReq := httptest.NewRequest(http.MethodPost, "/v1/command", mustJSON(t, xHeaderCmd))
	xReq.Header.Set("X-Telegram-User-ID", "tg-http-more")
	xReq.Header.Set("Content-Type", "application/json")
	xRec := httptest.NewRecorder()
	srv.ServeHTTP(xRec, xReq)
	if xRec.Code != http.StatusAccepted {
		t.Fatalf("expected x-telegram auth command accepted, got %d body=%s", xRec.Code, xRec.Body.String())
	}
}
