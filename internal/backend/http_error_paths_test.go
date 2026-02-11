package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

type stubPairingStore struct{}

func (s stubPairingStore) StartPairing(telegramUserID string) (contracts.PairStartResponse, error) {
	return contracts.PairStartResponse{}, nil
}
func (s stubPairingStore) ClaimPairing(req contracts.PairClaimRequest) (contracts.PairClaimResponse, error) {
	return contracts.PairClaimResponse{}, nil
}
func (s stubPairingStore) AuthenticateAgentKey(agentKey string) (string, bool) { return "", false }
func (s stubPairingStore) AgentIDForUser(telegramUserID string) (string, bool) { return "", false }
func (s stubPairingStore) UserIDForAgent(agentID string) (string, bool)        { return "", false }

type stubQueue struct {
	enqueueErr error
	pollErr    error
	storeErr   error
	getErr     error
	pollCmd    *contracts.Command
	getRes     *contracts.CommandResult
}

func (q stubQueue) Poll(ctx context.Context, agentID string, timeoutSeconds int) (*contracts.Command, error) {
	return q.pollCmd, q.pollErr
}
func (q stubQueue) StoreResult(ctx context.Context, agentID string, result contracts.CommandResult) error {
	return q.storeErr
}
func (q stubQueue) Enqueue(ctx context.Context, agentID string, cmd contracts.Command) error {
	return q.enqueueErr
}
func (q stubQueue) GetResult(ctx context.Context, agentID string, commandID string) (*contracts.CommandResult, error) {
	return q.getRes, q.getErr
}

func TestHTTPNonMemoryBackendBranches(t *testing.T) {
	s := NewServer(stubPairingStore{}, stubQueue{})

	reqProjects := httptest.NewRequest(http.MethodGet, "/v1/projects?telegram_user_id=u1", nil)
	recProjects := httptest.NewRecorder()
	s.ServeHTTP(recProjects, reqProjects)
	if recProjects.Code != http.StatusBadRequest || !strings.Contains(recProjects.Body.String(), "projects not supported") {
		t.Fatalf("expected projects unsupported, got code=%d body=%s", recProjects.Code, recProjects.Body.String())
	}

	reqStatus := httptest.NewRequest(http.MethodGet, "/v1/result/status?telegram_user_id=u1&command_id=c1", nil)
	recStatus := httptest.NewRecorder()
	s.ServeHTTP(recStatus, reqStatus)
	if recStatus.Code != http.StatusBadRequest || !strings.Contains(recStatus.Body.String(), "result status not supported") {
		t.Fatalf("expected result status unsupported, got code=%d body=%s", recStatus.Code, recStatus.Body.String())
	}
}

func TestHTTPQueueErrorBranches(t *testing.T) {
	b := NewMemoryBackend()
	b.agentByKey["agent-key"] = "agent-1"
	b.agentByUser["u1"] = "agent-1"

	cmd := contracts.Command{
		CommandID:      "cmd-e",
		IdempotencyKey: "id-e",
		Type:           contracts.CommandTypeStatus,
		CreatedAt:      time.Now().UTC(),
		Payload:        json.RawMessage(`{}`),
	}

	// enqueue error
	sEnq := NewServer(b, stubQueue{enqueueErr: errors.New("enqueue failed")})
	reqEnq := httptest.NewRequest(http.MethodPost, "/v1/command", mustJSON(t, cmd))
	reqEnq.Header.Set("Authorization", "Bearer agent-key")
	reqEnq.Header.Set("Content-Type", "application/json")
	recEnq := httptest.NewRecorder()
	sEnq.ServeHTTP(recEnq, reqEnq)
	if recEnq.Code != http.StatusInternalServerError {
		t.Fatalf("expected enqueue error 500, got %d", recEnq.Code)
	}

	// poll error
	sPoll := NewServer(b, stubQueue{pollErr: errors.New("poll failed")})
	reqPoll := httptest.NewRequest(http.MethodGet, "/v1/poll?timeout_seconds=1", nil)
	reqPoll.Header.Set("Authorization", "Bearer agent-key")
	recPoll := httptest.NewRecorder()
	sPoll.ServeHTTP(recPoll, reqPoll)
	if recPoll.Code != http.StatusInternalServerError {
		t.Fatalf("expected poll error 500, got %d", recPoll.Code)
	}

	// store error
	sRes := NewServer(b, stubQueue{storeErr: errors.New("store failed")})
	reqRes := httptest.NewRequest(http.MethodPost, "/v1/result", mustJSON(t, contracts.CommandResult{CommandID: "cmd-e", OK: true}))
	reqRes.Header.Set("Authorization", "Bearer agent-key")
	reqRes.Header.Set("Content-Type", "application/json")
	recRes := httptest.NewRecorder()
	sRes.ServeHTTP(recRes, reqRes)
	if recRes.Code != http.StatusInternalServerError {
		t.Fatalf("expected store error 500, got %d", recRes.Code)
	}

	// get result error
	sGet := NewServer(b, stubQueue{getErr: errors.New("get failed")})
	reqGet := httptest.NewRequest(http.MethodGet, "/v1/result/status?telegram_user_id=u1&command_id=cmd-e", nil)
	recGet := httptest.NewRecorder()
	sGet.ServeHTTP(recGet, reqGet)
	if recGet.Code != http.StatusInternalServerError {
		t.Fatalf("expected get error 500, got %d", recGet.Code)
	}
}
