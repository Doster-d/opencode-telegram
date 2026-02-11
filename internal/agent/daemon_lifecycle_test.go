package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

type fakePollClient struct {
	commands []*contracts.Command
	index    int32
	results  []contracts.CommandResult
}

func (f *fakePollClient) PollCommand(ctx context.Context, timeoutSeconds int) (*contracts.Command, error) {
	_ = ctx
	_ = timeoutSeconds
	idx := atomic.AddInt32(&f.index, 1) - 1
	if int(idx) >= len(f.commands) {
		return nil, nil
	}
	return f.commands[idx], nil
}

func (f *fakePollClient) PostResult(ctx context.Context, result contracts.CommandResult) error {
	_ = ctx
	f.results = append(f.results, result)
	return nil
}

func TestDaemonReadinessAndRestart(t *testing.T) {
	call := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDaemon()
	d.SetAgentID("agent-1")
	// override readiness check for deterministic lifecycle test
	d.client = srv.Client()
	d.readinessCheck = func(context.Context, int) bool { return true }

	projectPath := t.TempDir()

	reg := contracts.Command{
		CommandID:      "reg",
		IdempotencyKey: "idem-reg",
		Type:           contracts.CommandTypeRegisterProject,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.RegisterProjectPayload{ProjectPathRaw: projectPath}),
	}
	regRes, err := d.HandleCommand(context.Background(), reg)
	if err != nil || !regRes.OK {
		t.Fatalf("register project failed: %v %+v", err, regRes)
	}
	projectID, _ := regRes.Meta["project_id"].(string)
	if projectID == "" {
		t.Fatalf("expected project_id in register result")
	}
	policy := contracts.Command{
		CommandID:      "pol",
		IdempotencyKey: "idem-pol",
		Type:           contracts.CommandTypeApplyProjectPolicy,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.ApplyProjectPolicyPayload{ProjectID: projectID, Decision: contracts.DecisionAllow, Scope: []string{contracts.ScopeStartServer}}),
	}
	_, _ = d.HandleCommand(context.Background(), policy)

	cmd := contracts.Command{
		CommandID:      "c1",
		IdempotencyKey: "i1",
		Type:           contracts.CommandTypeStartServer,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.StartServerPayload{ProjectID: projectID}),
	}
	d.execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		_ = ctx
		atomic.AddInt32(&call, 1)
		cmd := exec.Command("sleep", "0.1")
		return cmd
	}
	res, err := d.HandleCommand(context.Background(), cmd)
	if err != nil {
		t.Fatalf("handle command: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK result, got %+v", res)
	}
	if _, ok := res.Meta["port"]; !ok {
		t.Fatalf("expected port in meta")
	}

	if atomic.LoadInt32(&call) == 0 {
		t.Fatalf("expected exec command to be called")
	}
}
