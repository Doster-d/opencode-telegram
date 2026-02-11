package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

type Handler func(ctx context.Context, cmd contracts.Command) (contracts.CommandResult, error)

type PollClient interface {
	PollCommand(ctx context.Context, timeoutSeconds int) (*contracts.Command, error)
	PostResult(ctx context.Context, result contracts.CommandResult) error
}

type Daemon struct {
	now   func() time.Time
	sleep func(time.Duration)

	agentID string

	startTimeout   time.Duration
	commandTimeout time.Duration
	serveCommand   string
	runCommand     string
	headers        http.Header
	client         *http.Client
	execCommand    func(ctx context.Context, name string, args ...string) *exec.Cmd
	readinessCheck func(ctx context.Context, port int) bool

	mu             sync.RWMutex
	handlers       map[string]Handler
	mutatingTypes  map[string]bool
	mutatingLocker sync.Mutex

	idempotency *IdempotencyCache
	allocator   *PortAllocator
	projects    map[string]string
	policies    map[string]projectPolicy
	servers     map[string]*serverState

	backoffBase time.Duration
	backoffMax  time.Duration
	jitter      *rand.Rand
}

type serverState struct {
	ProjectID   string
	ProjectPath string
	Port        int
	Cmd         *exec.Cmd
}

type projectPolicy struct {
	Decision  string
	ExpiresAt *time.Time
	Scope     []string
}

func NewDaemon() *Daemon {
	d := &Daemon{
		now:            time.Now,
		sleep:          time.Sleep,
		handlers:       make(map[string]Handler),
		allocator:      NewPortAllocator(4096, 4196),
		servers:        make(map[string]*serverState),
		projects:       make(map[string]string),
		policies:       make(map[string]projectPolicy),
		startTimeout:   10 * time.Second,
		commandTimeout: 600 * time.Second,
		serveCommand:   "opencode",
		runCommand:     "opencode",
		client:         &http.Client{Timeout: 2 * time.Second},
		execCommand:    exec.CommandContext,
		readinessCheck: nil,
		mutatingTypes: map[string]bool{
			contracts.CommandTypeRegisterProject:    true,
			contracts.CommandTypeApplyProjectPolicy: true,
			contracts.CommandTypeStartServer:        true,
			contracts.CommandTypeRunTask:            true,
		},
		backoffBase: 500 * time.Millisecond,
		backoffMax:  10 * time.Second,
		jitter:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	d.idempotency = NewIdempotencyCache(1000, 24*time.Hour, d.now)
	d.readinessCheck = d.waitForReady
	d.handlers[contracts.CommandTypeRegisterProject] = d.handleRegisterProject
	d.handlers[contracts.CommandTypeApplyProjectPolicy] = d.handleApplyProjectPolicy
	d.handlers[contracts.CommandTypeStartServer] = d.handleStartServer
	d.handlers[contracts.CommandTypeRunTask] = d.handleRunTask
	d.handlers[contracts.CommandTypeStatus] = d.handleStatus
	return d
}

func (d *Daemon) SetHandler(commandType string, handler Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[commandType] = handler
}

func (d *Daemon) SetAgentID(agentID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.agentID = agentID
}

func (d *Daemon) HandleCommand(ctx context.Context, cmd contracts.Command) (contracts.CommandResult, error) {
	if err := contracts.ValidateCommand(cmd); err != nil {
		apiErr, ok := err.(contracts.APIError)
		if !ok {
			apiErr = contracts.APIError{Code: contracts.ErrInternal, Message: err.Error()}
		}
		return contracts.CommandResult{CommandID: cmd.CommandID, OK: false, ErrorCode: apiErr.Code, Summary: apiErr.Message}, nil
	}

	if cached, ok := d.idempotency.Get(cmd.IdempotencyKey); ok {
		return cached, nil
	}

	h, ok := d.getHandler(cmd.Type)
	if !ok {
		return contracts.CommandResult{CommandID: cmd.CommandID, OK: false, ErrorCode: contracts.ErrValidationInvalidType, Summary: "unsupported command type"}, nil
	}

	exec := func() contracts.CommandResult {
		result, err := h(ctx, cmd)
		if err != nil {
			apiErr, ok := err.(contracts.APIError)
			if !ok {
				apiErr = contracts.APIError{Code: contracts.ErrInternal, Message: err.Error()}
			}
			return contracts.CommandResult{CommandID: cmd.CommandID, OK: false, ErrorCode: apiErr.Code, Summary: apiErr.Message}
		}
		if strings.TrimSpace(result.CommandID) == "" {
			result.CommandID = cmd.CommandID
		}
		return result
	}

	var out contracts.CommandResult
	if d.mutatingTypes[cmd.Type] {
		d.mutatingLocker.Lock()
		out = exec()
		d.mutatingLocker.Unlock()
	} else {
		out = exec()
	}

	d.idempotency.Put(cmd.IdempotencyKey, out)
	return out, nil
}

func (d *Daemon) RunPollLoop(ctx context.Context, client PollClient, timeoutSeconds int) {
	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}
		cmd, err := client.PollCommand(ctx, timeoutSeconds)
		if err != nil {
			d.sleep(d.nextBackoff(attempt))
			attempt++
			continue
		}
		attempt = 0
		if cmd == nil {
			continue
		}
		result, _ := d.HandleCommand(ctx, *cmd)
		if err := client.PostResult(ctx, result); err != nil {
			d.sleep(d.nextBackoff(attempt))
			attempt++
		}
	}
}

func (d *Daemon) getHandler(commandType string) (Handler, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	h, ok := d.handlers[commandType]
	return h, ok
}

func (d *Daemon) nextBackoff(attempt int) time.Duration {
	delta := d.backoffBase << attempt
	if delta > d.backoffMax {
		delta = d.backoffMax
	}
	jitterMax := int64(delta / 5)
	if jitterMax <= 0 {
		return delta
	}
	return delta + time.Duration(d.jitter.Int63n(jitterMax))
}

func (d *Daemon) handleRegisterProject(_ context.Context, cmd contracts.Command) (contracts.CommandResult, error) {
	var payload contracts.RegisterProjectPayload
	if err := contracts.DecodeStrictJSON(cmd.Payload, &payload); err != nil {
		return contracts.CommandResult{}, contracts.APIError{Code: contracts.ErrValidationInvalidPayload, Message: err.Error()}
	}
	if strings.TrimSpace(payload.ProjectPathRaw) == "" {
		return contracts.CommandResult{}, contracts.APIError{Code: contracts.ErrPathInvalid, Message: "project_path_raw is required"}
	}
	path, err := normalizeProjectPath(payload.ProjectPathRaw)
	if err != nil {
		return contracts.CommandResult{}, contracts.APIError{Code: contracts.ErrPathInvalid, Message: err.Error()}
	}
	if isForbiddenPath(path) {
		return contracts.CommandResult{}, contracts.APIError{Code: contracts.ErrPathForbidden, Message: "project path forbidden"}
	}
	agentID := d.agentID
	if strings.TrimSpace(agentID) == "" {
		agentID = "unknown"
	}
	projectID := computeProjectID(agentID, path)
	d.mu.Lock()
	d.projects[projectID] = path
	d.policies[projectID] = projectPolicy{Decision: contracts.DecisionDeny}
	d.mu.Unlock()
	return contracts.CommandResult{CommandID: cmd.CommandID, OK: true, Summary: "project registered", Meta: map[string]any{"project_id": projectID, "project_path": path}}, nil
}

func (d *Daemon) handleApplyProjectPolicy(_ context.Context, cmd contracts.Command) (contracts.CommandResult, error) {
	var payload contracts.ApplyProjectPolicyPayload
	if err := contracts.DecodeStrictJSON(cmd.Payload, &payload); err != nil {
		return contracts.CommandResult{}, contracts.APIError{Code: contracts.ErrValidationInvalidPayload, Message: err.Error()}
	}
	d.mu.Lock()
	d.policies[payload.ProjectID] = projectPolicy{Decision: payload.Decision, ExpiresAt: payload.ExpiresAt, Scope: payload.Scope}
	d.mu.Unlock()
	meta := map[string]any{
		"decision": payload.Decision,
		"scope":    payload.Scope,
	}
	if payload.ExpiresAt != nil {
		meta["expires_at"] = payload.ExpiresAt.Format(time.RFC3339Nano)
	}
	return contracts.CommandResult{CommandID: cmd.CommandID, OK: true, Summary: "policy applied", Meta: meta}, nil
}

func (d *Daemon) handleStartServer(_ context.Context, cmd contracts.Command) (contracts.CommandResult, error) {
	var payload contracts.StartServerPayload
	if err := contracts.DecodeStrictJSON(cmd.Payload, &payload); err != nil {
		return contracts.CommandResult{}, contracts.APIError{Code: contracts.ErrValidationInvalidPayload, Message: err.Error()}
	}
	return d.startServer(cmd.CommandID, payload.ProjectID)
}

func (d *Daemon) handleRunTask(_ context.Context, cmd contracts.Command) (contracts.CommandResult, error) {
	var payload contracts.RunTaskPayload
	if err := contracts.DecodeStrictJSON(cmd.Payload, &payload); err != nil {
		return contracts.CommandResult{}, contracts.APIError{Code: contracts.ErrValidationInvalidPayload, Message: err.Error()}
	}
	if !d.policyAllows(payload.ProjectID, contracts.ScopeRunTask) {
		return contracts.CommandResult{}, contracts.APIError{Code: contracts.ErrPolicyDenied, Message: "policy denied"}
	}
	startRes, err := d.startServer(cmd.CommandID, payload.ProjectID)
	if err != nil {
		return contracts.CommandResult{}, err
	}
	port, _ := startRes.Meta["port"].(int)
	ctx, cancel := context.WithTimeout(context.Background(), d.commandTimeout)
	defer cancel()
	attach := fmt.Sprintf("http://127.0.0.1:%d", port)
	command := d.execCommand(ctx, d.runCommand, "run", "--attach", attach, payload.Prompt)
	if path, ok := d.projectPath(payload.ProjectID); ok {
		command.Dir = path
	}
	if err := command.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return contracts.CommandResult{}, contracts.APIError{Code: contracts.ErrStartTimeout, Message: "command timeout"}
		}
		return contracts.CommandResult{}, err
	}
	return contracts.CommandResult{CommandID: cmd.CommandID, OK: true, Summary: "task completed", Meta: map[string]any{"port": port}}, nil
}

func (d *Daemon) handleStatus(_ context.Context, cmd contracts.Command) (contracts.CommandResult, error) {
	var payload contracts.StatusPayload
	if err := contracts.DecodeStrictJSON(cmd.Payload, &payload); err != nil {
		return contracts.CommandResult{}, contracts.APIError{Code: contracts.ErrValidationInvalidPayload, Message: err.Error()}
	}
	return contracts.CommandResult{CommandID: cmd.CommandID, OK: true, Summary: "agent healthy"}, nil
}

func (d *Daemon) projectPath(projectID string) (string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	path, ok := d.projects[projectID]
	return path, ok
}

func (d *Daemon) policyAllows(projectID string, scope string) bool {
	d.mu.RLock()
	policy, ok := d.policies[projectID]
	d.mu.RUnlock()
	if !ok || policy.Decision != contracts.DecisionAllow {
		return false
	}
	if policy.ExpiresAt != nil && d.now().UTC().After(*policy.ExpiresAt) {
		return false
	}
	for _, s := range policy.Scope {
		if s == scope {
			return true
		}
	}
	return false
}

func normalizeProjectPath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "", errors.New("project_path_raw is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	if real != "/" {
		real = strings.TrimRight(real, string(filepath.Separator))
	}
	return real, nil
}

func isForbiddenPath(path string) bool {
	if path == "/" {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil {
		home = filepath.Clean(home)
		if path == home {
			return true
		}
	}
	if path == "/home" || path == "/Users" {
		return true
	}
	forbidden := []string{"/etc", "/bin", "/usr", "/var", "/System", "/Library"}
	for _, f := range forbidden {
		if path == f || strings.HasPrefix(path, f+"/") {
			return true
		}
	}
	return false
}

func computeProjectID(agentID, path string) string {
	data := []byte(agentID + "\n" + path)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (d *Daemon) startServer(commandID string, projectID string) (contracts.CommandResult, error) {
	if strings.TrimSpace(projectID) == "" {
		return contracts.CommandResult{}, contracts.APIError{Code: contracts.ErrValidationRequiredField, Message: "project_id is required"}
	}
	if !d.policyAllows(projectID, contracts.ScopeStartServer) {
		return contracts.CommandResult{}, contracts.APIError{Code: contracts.ErrPolicyDenied, Message: "policy denied"}
	}
	if current := d.serverForProject(projectID); current != nil {
		return contracts.CommandResult{CommandID: commandID, OK: true, Summary: "server ready", Meta: map[string]any{"port": current.Port}}, nil
	}
	path, ok := d.projectPath(projectID)
	if !ok {
		return contracts.CommandResult{}, contracts.APIError{Code: contracts.ErrPathInvalid, Message: "project not registered"}
	}
	port, err := d.allocator.Allocate(projectID)
	if err != nil {
		return contracts.CommandResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), d.startTimeout)
	defer cancel()
	cmd := d.execCommand(ctx, d.serveCommand, "serve", "--hostname", "127.0.0.1", "--port", fmt.Sprintf("%d", port))
	cmd.Dir = path
	if err := cmd.Start(); err != nil {
		return contracts.CommandResult{}, err
	}
	state := &serverState{ProjectID: projectID, ProjectPath: path, Port: port, Cmd: cmd}
	d.setServer(projectID, state)
	ready := d.readinessCheck(ctx, port)
	if !ready {
		_ = cmd.Process.Kill()
		d.clearServer(projectID)
		return contracts.CommandResult{}, contracts.APIError{Code: contracts.ErrStartTimeout, Message: "start timeout"}
	}
	go func() {
		_ = cmd.Wait()
		d.clearServer(projectID)
	}()
	return contracts.CommandResult{CommandID: commandID, OK: true, Summary: "server ready", Meta: map[string]any{"port": port}}, nil
}

func (d *Daemon) waitForReady(ctx context.Context, port int) bool {
	url := fmt.Sprintf("http://127.0.0.1:%d/global/health", port)
	for {
		if ctx.Err() != nil {
			return false
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := d.client.Do(req)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		d.sleep(200 * time.Millisecond)
	}
}

func (d *Daemon) serverForProject(projectID string) *serverState {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.servers[projectID]
}

func (d *Daemon) setServer(projectID string, state *serverState) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers[projectID] = state
}

func (d *Daemon) clearServer(projectID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.servers, projectID)
	d.allocator.Release(projectID)
}
