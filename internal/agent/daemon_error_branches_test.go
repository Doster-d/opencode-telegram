package agent

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

func TestDaemon_RegisterProjectAndStartServerErrorBranches(t *testing.T) {
	d := NewDaemon()
	d.SetAgentID("agent-e")

	badPayload := contracts.Command{
		CommandID:      "c-bad",
		IdempotencyKey: "k-bad",
		Type:           contracts.CommandTypeRegisterProject,
		CreatedAt:      time.Now().UTC(),
		Payload:        []byte(`{bad`),
	}
	res, err := d.HandleCommand(context.Background(), badPayload)
	if err != nil || res.OK || res.ErrorCode != contracts.ErrValidationInvalidPayload {
		t.Fatalf("expected invalid payload branch, err=%v res=%+v", err, res)
	}

	forbidden := contracts.Command{
		CommandID:      "c-root",
		IdempotencyKey: "k-root",
		Type:           contracts.CommandTypeRegisterProject,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.RegisterProjectPayload{ProjectPathRaw: "/"}),
	}
	res, err = d.HandleCommand(context.Background(), forbidden)
	if err != nil || res.OK || res.ErrorCode != contracts.ErrPathForbidden {
		t.Fatalf("expected forbidden path branch, err=%v res=%+v", err, res)
	}

	startMissing := contracts.Command{
		CommandID:      "c-start-missing",
		IdempotencyKey: "k-start-missing",
		Type:           contracts.CommandTypeStartServer,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.StartServerPayload{ProjectID: "missing"}),
	}
	res, err = d.HandleCommand(context.Background(), startMissing)
	if err != nil || res.OK || res.ErrorCode != contracts.ErrPolicyDenied {
		t.Fatalf("expected policy denied for missing policy, err=%v res=%+v", err, res)
	}

	reg := contracts.Command{
		CommandID:      "c-reg-ok",
		IdempotencyKey: "k-reg-ok",
		Type:           contracts.CommandTypeRegisterProject,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.RegisterProjectPayload{ProjectPathRaw: t.TempDir()}),
	}
	regRes, err := d.HandleCommand(context.Background(), reg)
	if err != nil || !regRes.OK {
		t.Fatalf("register project failed: %v %+v", err, regRes)
	}
	projectID, _ := regRes.Meta["project_id"].(string)
	exp := time.Now().UTC().Add(5 * time.Minute)
	pol := contracts.Command{
		CommandID:      "c-pol",
		IdempotencyKey: "k-pol",
		Type:           contracts.CommandTypeApplyProjectPolicy,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.ApplyProjectPolicyPayload{ProjectID: projectID, Decision: contracts.DecisionAllow, Scope: []string{contracts.ScopeStartServer}, ExpiresAt: &exp}),
	}
	if pRes, pErr := d.HandleCommand(context.Background(), pol); pErr != nil || !pRes.OK {
		t.Fatalf("apply policy failed: %v %+v", pErr, pRes)
	}

	d.readinessCheck = func(context.Context, int) bool { return false }
	d.startTimeout = 200 * time.Millisecond
	d.execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		_ = name
		_ = args
		return exec.CommandContext(ctx, "sleep", "0.1")
	}

	start := contracts.Command{
		CommandID:      "c-start-timeout",
		IdempotencyKey: "k-start-timeout",
		Type:           contracts.CommandTypeStartServer,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.StartServerPayload{ProjectID: projectID}),
	}
	res, err = d.HandleCommand(context.Background(), start)
	if err != nil || res.OK || res.ErrorCode != contracts.ErrStartTimeout {
		t.Fatalf("expected start timeout branch, err=%v res=%+v", err, res)
	}
}

func TestDaemonPolicyAllowsExpiryAndScope(t *testing.T) {
	d := NewDaemon()
	now := time.Date(2026, 2, 11, 12, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return now }

	expired := now.Add(-time.Minute)
	d.mu.Lock()
	d.policies["p1"] = projectPolicy{Decision: contracts.DecisionAllow, ExpiresAt: &expired, Scope: []string{contracts.ScopeRunTask}}
	d.policies["p2"] = projectPolicy{Decision: contracts.DecisionAllow, Scope: []string{contracts.ScopeStartServer}}
	d.mu.Unlock()

	if d.policyAllows("p1", contracts.ScopeRunTask) {
		t.Fatal("expected expired policy to be denied")
	}
	if d.policyAllows("p2", contracts.ScopeRunTask) {
		t.Fatal("expected missing scope to be denied")
	}
	if !d.policyAllows("p2", contracts.ScopeStartServer) {
		t.Fatal("expected matching scope to be allowed")
	}
}
