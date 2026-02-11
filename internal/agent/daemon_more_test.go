package agent

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

func TestDaemonRunPollLoop_BackoffAndPostError(t *testing.T) {
	d := NewDaemon()
	d.jitter = rand.New(rand.NewSource(1))

	var sleeps []time.Duration
	var mu sync.Mutex
	d.sleep = func(dur time.Duration) {
		mu.Lock()
		sleeps = append(sleeps, dur)
		mu.Unlock()
	}

	cmd := contracts.Command{CommandID: "c1", IdempotencyKey: "i1", Type: contracts.CommandTypeStatus, CreatedAt: time.Now().UTC(), Payload: json.RawMessage(`{}`)}
	pc := &sequencePollClient{
		poll:      []pollStep{{err: errors.New("poll fail")}, {cmd: &cmd}, {stop: true}},
		postErrAt: map[int]error{1: errors.New("post fail")},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		d.RunPollLoop(ctx, pc, 1)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for poll loop activity")
		}
		mu.Lock()
		sleepCount := len(sleeps)
		mu.Unlock()
		if sleepCount >= 2 && pc.postCalls >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	mu.Lock()
	defer mu.Unlock()
	if len(sleeps) < 2 {
		t.Fatalf("expected at least two backoff sleeps, got %d", len(sleeps))
	}
	if sleeps[0] <= 0 || sleeps[1] <= 0 {
		t.Fatalf("expected positive backoff durations, got %+v", sleeps)
	}
}

func TestDaemonHandleRunTask_SuccessAndTimeout(t *testing.T) {
	d := NewDaemon()
	projectID := "p1"
	d.mu.Lock()
	d.projects[projectID] = t.TempDir()
	d.policies[projectID] = projectPolicy{Decision: contracts.DecisionAllow, Scope: []string{contracts.ScopeStartServer, contracts.ScopeRunTask}}
	d.servers[projectID] = &serverState{ProjectID: projectID, Port: 4321}
	d.mu.Unlock()

	d.execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		_ = ctx
		_ = name
		_ = args
		return exec.Command("true")
	}

	successCmd := contracts.Command{
		CommandID:      "run-1",
		IdempotencyKey: "idem-run-1",
		Type:           contracts.CommandTypeRunTask,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.RunTaskPayload{ProjectID: projectID, Prompt: "hello"}),
	}
	res, err := d.HandleCommand(context.Background(), successCmd)
	if err != nil || !res.OK {
		t.Fatalf("expected run_task success, err=%v res=%+v", err, res)
	}

	d.commandTimeout = 1 * time.Millisecond
	d.execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		_ = name
		_ = args
		return exec.CommandContext(ctx, "sleep", "0.1")
	}
	timeoutCmd := contracts.Command{
		CommandID:      "run-2",
		IdempotencyKey: "idem-run-2",
		Type:           contracts.CommandTypeRunTask,
		CreatedAt:      time.Now().UTC(),
		Payload:        mustPayload(t, contracts.RunTaskPayload{ProjectID: projectID, Prompt: "slow"}),
	}
	out, err := d.HandleCommand(context.Background(), timeoutCmd)
	if err != nil {
		t.Fatalf("expected command result, got error %v", err)
	}
	if out.OK || out.ErrorCode != contracts.ErrStartTimeout {
		t.Fatalf("expected timeout result, got %+v", out)
	}
}

func TestDaemonWaitForReadyAndHelpers(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}

	d := NewDaemon()
	d.client = srv.Client()
	d.sleep = func(time.Duration) {}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if !d.waitForReady(ctx, port) {
		t.Fatal("expected readiness to become true")
	}

	d.jitter = rand.New(rand.NewSource(1))
	if d.nextBackoff(0) <= 0 {
		t.Fatal("expected positive backoff")
	}

	cacheNow := time.Now().UTC()
	cache := NewIdempotencyCache(1, 10*time.Millisecond, func() time.Time { return cacheNow })
	cache.Put("k1", contracts.CommandResult{CommandID: "c1", OK: true})
	cache.Put("k2", contracts.CommandResult{CommandID: "c2", OK: true})
	if _, ok := cache.Get("k1"); ok {
		t.Fatal("expected oldest entry to be evicted at maxEntries=1")
	}
	if _, ok := cache.Get("k2"); !ok {
		t.Fatal("expected latest cache entry to exist")
	}
	cacheNow = cacheNow.Add(20 * time.Millisecond)
	if _, ok := cache.Get("k2"); ok {
		t.Fatal("expected cache entry to expire after ttl")
	}

	alloc := NewPortAllocator(5000, 5002)
	if _, err := alloc.Allocate("p1"); err != nil {
		t.Fatalf("allocate p1: %v", err)
	}
	if _, err := alloc.Allocate("p2"); err != nil {
		t.Fatalf("allocate p2: %v", err)
	}
	used := alloc.SnapshotUsed()
	if len(used) != 2 || used[0] >= used[1] {
		t.Fatalf("expected sorted used ports, got %+v", used)
	}
}

type pollStep struct {
	cmd  *contracts.Command
	err  error
	stop bool
}

type sequencePollClient struct {
	poll      []pollStep
	pollIndex int

	postCalls int
	postErrAt map[int]error
}

func (s *sequencePollClient) PollCommand(ctx context.Context, timeoutSeconds int) (*contracts.Command, error) {
	_ = timeoutSeconds
	if s.pollIndex >= len(s.poll) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	step := s.poll[s.pollIndex]
	s.pollIndex++
	if step.stop {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return step.cmd, step.err
}

func (s *sequencePollClient) PostResult(ctx context.Context, result contracts.CommandResult) error {
	_ = ctx
	_ = result
	s.postCalls++
	if err, ok := s.postErrAt[s.postCalls]; ok {
		return err
	}
	return nil
}
