package agent

import (
	"context"
	"testing"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

func TestDaemonStatusAndStartServerPayloadValidation(t *testing.T) {
	d := NewDaemon()

	statusBad := contracts.Command{
		CommandID:      "s1",
		IdempotencyKey: "k-s1",
		Type:           contracts.CommandTypeStatus,
		CreatedAt:      time.Now().UTC(),
		Payload:        []byte(`{"extra":1}`),
	}
	res, err := d.HandleCommand(context.Background(), statusBad)
	if err != nil {
		t.Fatalf("expected wrapped result, got err=%v", err)
	}
	if res.OK || res.ErrorCode != contracts.ErrValidationInvalidPayload {
		t.Fatalf("expected invalid payload result for status, got %+v", res)
	}

	startBad := contracts.Command{
		CommandID:      "st1",
		IdempotencyKey: "k-st1",
		Type:           contracts.CommandTypeStartServer,
		CreatedAt:      time.Now().UTC(),
		Payload:        []byte(`{"project_id":""}`),
	}
	res, err = d.HandleCommand(context.Background(), startBad)
	if err != nil {
		t.Fatalf("expected wrapped result, got err=%v", err)
	}
	if res.OK || res.ErrorCode == "" {
		t.Fatalf("expected start_server validation failure result, got %+v", res)
	}
}

func TestNormalizeProjectPathAndForbiddenPathHelpers(t *testing.T) {
	if _, err := normalizeProjectPath(""); err == nil {
		t.Fatal("expected error for empty project path")
	}
	if _, err := normalizeProjectPath("/definitely/nonexistent/path/for/opencode/telegram/tests"); err == nil {
		t.Fatal("expected error for nonexistent path")
	}

	for _, p := range []string{"/", "/home", "/Users", "/etc", "/usr/local"} {
		if !isForbiddenPath(p) {
			t.Fatalf("expected path %q to be forbidden", p)
		}
	}
	if isForbiddenPath(t.TempDir()) {
		t.Fatal("expected temp dir to be allowed")
	}
}

func TestIdempotencyCacheDefaultClockBranch(t *testing.T) {
	c := NewIdempotencyCache(2, time.Minute, nil)
	c.Put("a", contracts.CommandResult{CommandID: "c1", OK: true})
	if _, ok := c.Get("a"); !ok {
		t.Fatal("expected cache entry to exist with default clock")
	}
}
