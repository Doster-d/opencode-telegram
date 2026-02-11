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

func mustJSON(t *testing.T, v any) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return bytes.NewReader(b)
}

func pairAgent(t *testing.T, srv *Server, userID string) string {
	t.Helper()

	startReq := httptest.NewRequest(http.MethodPost, "/v1/pair/start", mustJSON(t, contracts.PairStartRequest{TelegramUserID: userID}))
	startReq.Header.Set("Content-Type", "application/json")
	startRec := httptest.NewRecorder()
	srv.ServeHTTP(startRec, startReq)
	if startRec.Code != http.StatusOK {
		t.Fatalf("pair/start status=%d body=%s", startRec.Code, startRec.Body.String())
	}
	var start contracts.PairStartResponse
	if err := json.Unmarshal(startRec.Body.Bytes(), &start); err != nil {
		t.Fatalf("unmarshal pair/start: %v", err)
	}

	claimReq := httptest.NewRequest(http.MethodPost, "/v1/pair/claim", mustJSON(t, contracts.PairClaimRequest{PairingCode: start.PairingCode, DeviceInfo: "test"}))
	claimReq.Header.Set("Content-Type", "application/json")
	claimRec := httptest.NewRecorder()
	srv.ServeHTTP(claimRec, claimReq)
	if claimRec.Code != http.StatusOK {
		t.Fatalf("pair/claim status=%d body=%s", claimRec.Code, claimRec.Body.String())
	}
	var claim contracts.PairClaimResponse
	if err := json.Unmarshal(claimRec.Body.Bytes(), &claim); err != nil {
		t.Fatalf("unmarshal pair/claim: %v", err)
	}
	if claim.AgentKey == "" {
		t.Fatal("expected non-empty agent key")
	}
	return claim.AgentKey
}

func TestHTTPPairingEndpoints_MethodAndValidation(t *testing.T) {
	b := NewMemoryBackend()
	q := NewRedisQueue(NewInMemoryRedisClient())
	srv := NewServer(b, q)

	req := httptest.NewRequest(http.MethodGet, "/v1/pair/start", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}

	bad := httptest.NewRequest(http.MethodPost, "/v1/pair/start", bytes.NewBufferString(`{"unexpected":1}`))
	bad.Header.Set("Content-Type", "application/json")
	badRec := httptest.NewRecorder()
	srv.ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid body, got %d", badRec.Code)
	}
}

func TestHTTPCommandPollResultFlow(t *testing.T) {
	b := NewMemoryBackend()
	q := NewRedisQueue(NewInMemoryRedisClient())
	srv := NewServer(b, q)
	agentKey := pairAgent(t, srv, "tg-1")

	cmd := contracts.Command{
		CommandID:      "cmd-1",
		IdempotencyKey: "idem-1",
		Type:           contracts.CommandTypeStatus,
		CreatedAt:      time.Now().UTC(),
		Payload:        json.RawMessage(`{}`),
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/command", mustJSON(t, cmd))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+agentKey)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected accepted command, got %d body=%s", rec.Code, rec.Body.String())
	}

	pollReq := httptest.NewRequest(http.MethodGet, "/v1/poll?timeout_seconds=1", nil)
	pollReq.Header.Set("Authorization", "Bearer "+agentKey)
	pollRec := httptest.NewRecorder()
	srv.ServeHTTP(pollRec, pollReq)
	if pollRec.Code != http.StatusOK {
		t.Fatalf("expected poll status 200, got %d", pollRec.Code)
	}
	var polled contracts.PollResponse
	if err := json.Unmarshal(pollRec.Body.Bytes(), &polled); err != nil {
		t.Fatalf("unmarshal poll: %v", err)
	}
	if polled.Command == nil || polled.Command.CommandID != "cmd-1" {
		t.Fatalf("unexpected poll command: %+v", polled.Command)
	}

	result := contracts.CommandResult{CommandID: "cmd-1", OK: true, Summary: "ok"}
	resultReq := httptest.NewRequest(http.MethodPost, "/v1/result", mustJSON(t, result))
	resultReq.Header.Set("Content-Type", "application/json")
	resultReq.Header.Set("Authorization", "Bearer "+agentKey)
	resultRec := httptest.NewRecorder()
	srv.ServeHTTP(resultRec, resultReq)
	if resultRec.Code != http.StatusOK {
		t.Fatalf("expected result status 200, got %d", resultRec.Code)
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/v1/result/status?telegram_user_id=tg-1&command_id=cmd-1", nil)
	statusRec := httptest.NewRecorder()
	srv.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("expected result/status 200, got %d body=%s", statusRec.Code, statusRec.Body.String())
	}
}

func TestHTTPProjectsAndPolicyUpdateFromResult(t *testing.T) {
	b := NewMemoryBackend()
	srv := NewServer(b, b)
	agentKey := pairAgent(t, srv, "tg-2")

	registerCmd := contracts.Command{
		CommandID:      "cmd-reg",
		IdempotencyKey: "idem-reg",
		Type:           contracts.CommandTypeRegisterProject,
		CreatedAt:      time.Now().UTC(),
		Payload:        json.RawMessage(`{"project_path_raw":"/tmp/demo"}`),
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/command", mustJSON(t, registerCmd))
	req.Header.Set("Authorization", "Bearer "+agentKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("register command status=%d body=%s", rec.Code, rec.Body.String())
	}

	pollReq := httptest.NewRequest(http.MethodGet, "/v1/poll?timeout_seconds=1", nil)
	pollReq.Header.Set("Authorization", "Bearer "+agentKey)
	pollRec := httptest.NewRecorder()
	srv.ServeHTTP(pollRec, pollReq)
	if pollRec.Code != http.StatusOK {
		t.Fatalf("poll status=%d", pollRec.Code)
	}

	regResult := contracts.CommandResult{CommandID: "cmd-reg", OK: true, Meta: map[string]any{"project_id": "pid-1", "project_path": "/tmp/demo"}}
	regResReq := httptest.NewRequest(http.MethodPost, "/v1/result", mustJSON(t, regResult))
	regResReq.Header.Set("Authorization", "Bearer "+agentKey)
	regResReq.Header.Set("Content-Type", "application/json")
	regResRec := httptest.NewRecorder()
	srv.ServeHTTP(regResRec, regResReq)
	if regResRec.Code != http.StatusOK {
		t.Fatalf("register result status=%d", regResRec.Code)
	}

	projectsReq := httptest.NewRequest(http.MethodGet, "/v1/projects?telegram_user_id=tg-2", nil)
	projectsRec := httptest.NewRecorder()
	srv.ServeHTTP(projectsRec, projectsReq)
	if projectsRec.Code != http.StatusOK {
		t.Fatalf("projects status=%d body=%s", projectsRec.Code, projectsRec.Body.String())
	}

	// Queue policy update and then submit result to verify projection update path.
	policyCmd := contracts.Command{
		CommandID:      "cmd-policy",
		IdempotencyKey: "idem-policy",
		Type:           contracts.CommandTypeApplyProjectPolicy,
		CreatedAt:      time.Now().UTC(),
		Payload:        json.RawMessage(`{"project_id":"pid-1","decision":"ALLOW","scope":["START_SERVER"]}`),
	}
	policyReq := httptest.NewRequest(http.MethodPost, "/v1/command", mustJSON(t, policyCmd))
	policyReq.Header.Set("Authorization", "Bearer "+agentKey)
	policyReq.Header.Set("Content-Type", "application/json")
	policyRec := httptest.NewRecorder()
	srv.ServeHTTP(policyRec, policyReq)
	if policyRec.Code != http.StatusAccepted {
		t.Fatalf("policy command status=%d body=%s", policyRec.Code, policyRec.Body.String())
	}

	pollReq2 := httptest.NewRequest(http.MethodGet, "/v1/poll?timeout_seconds=1", nil)
	pollReq2.Header.Set("Authorization", "Bearer "+agentKey)
	pollRec2 := httptest.NewRecorder()
	srv.ServeHTTP(pollRec2, pollReq2)
	if pollRec2.Code != http.StatusOK {
		t.Fatalf("poll2 status=%d", pollRec2.Code)
	}

	exp := time.Now().UTC().Add(5 * time.Minute)
	polResult := contracts.CommandResult{CommandID: "cmd-policy", OK: true, Meta: map[string]any{"decision": contracts.DecisionAllow, "scope": []string{contracts.ScopeStartServer}, "expires_at": exp.Format(time.RFC3339Nano)}}
	polResReq := httptest.NewRequest(http.MethodPost, "/v1/result", mustJSON(t, polResult))
	polResReq.Header.Set("Authorization", "Bearer "+agentKey)
	polResReq.Header.Set("Content-Type", "application/json")
	polResRec := httptest.NewRecorder()
	srv.ServeHTTP(polResRec, polResReq)
	if polResRec.Code != http.StatusOK {
		t.Fatalf("policy result status=%d", polResRec.Code)
	}

	projectsRec = httptest.NewRecorder()
	srv.ServeHTTP(projectsRec, projectsReq)
	if projectsRec.Code != http.StatusOK {
		t.Fatalf("projects status 2=%d", projectsRec.Code)
	}
	var projects map[string][]map[string]any
	if err := json.Unmarshal(projectsRec.Body.Bytes(), &projects); err != nil {
		t.Fatalf("unmarshal projects: %v", err)
	}
	if len(projects["projects"]) != 1 {
		t.Fatalf("expected one project, got %+v", projects)
	}
}

func TestHTTPAuthAndValidationErrors(t *testing.T) {
	b := NewMemoryBackend()
	q := NewRedisQueue(NewInMemoryRedisClient())
	srv := NewServer(b, q)

	cmd := contracts.Command{CommandID: "cmd", IdempotencyKey: "k", Type: contracts.CommandTypeStatus, CreatedAt: time.Now().UTC(), Payload: json.RawMessage(`{}`)}
	req := httptest.NewRequest(http.MethodPost, "/v1/command", mustJSON(t, cmd))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without auth, got %d", rec.Code)
	}

	reqBadTimeout := httptest.NewRequest(http.MethodGet, "/v1/poll?timeout_seconds=99", nil)
	reqBadTimeout.Header.Set("Authorization", "Bearer bad")
	recBadTimeout := httptest.NewRecorder()
	srv.ServeHTTP(recBadTimeout, reqBadTimeout)
	if recBadTimeout.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized for bad key, got %d", recBadTimeout.Code)
	}
}
