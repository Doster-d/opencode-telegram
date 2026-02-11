package backend

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

const (
	DefaultPairingTTL    = 10 * time.Minute
	DefaultRedeliveryTTL = 120 * time.Second
)

type PairingStore interface {
	StartPairing(telegramUserID string) (contracts.PairStartResponse, error)
	ClaimPairing(req contracts.PairClaimRequest) (contracts.PairClaimResponse, error)
	AuthenticateAgentKey(agentKey string) (string, bool)
	AgentIDForUser(telegramUserID string) (string, bool)
	UserIDForAgent(agentID string) (string, bool)
}

type CommandQueue interface {
	Poll(ctx context.Context, agentID string, timeoutSeconds int) (*contracts.Command, error)
	StoreResult(ctx context.Context, agentID string, result contracts.CommandResult) error
	Enqueue(ctx context.Context, agentID string, cmd contracts.Command) error
	GetResult(ctx context.Context, agentID string, commandID string) (*contracts.CommandResult, error)
}

type MemoryBackend struct {
	mu              sync.Mutex
	now             func() time.Time
	pairingTTL      time.Duration
	redeliveryAfter time.Duration

	pairCounter  int
	agentCounter int
	keyCounter   int

	pairCodes       map[string]pairCodeRecord
	agentByUser     map[string]string
	agentKeyByAgent map[string]string
	agentByKey      map[string]string

	queued   map[string][]contracts.Command
	inflight map[string][]inflightCommand
	results  map[string]map[string]contracts.CommandResult
	projects map[string]map[string]*projectRecord
	aliases  map[string]map[string]string
	commands map[string]commandMeta
}

type pairCodeRecord struct {
	TelegramUserID string
	ExpiresAt      time.Time
}

type inflightCommand struct {
	Command    contracts.Command
	InflightAt time.Time
}

type projectPolicy struct {
	Decision  string     `json:"decision"`
	ExpiresAt *time.Time `json:"expires_at"`
	Scope     []string   `json:"scope"`
}

type projectRecord struct {
	Alias       string        `json:"alias"`
	ProjectID   string        `json:"project_id"`
	ProjectPath string        `json:"project_path"`
	Policy      projectPolicy `json:"policy"`
	LastUpdated time.Time     `json:"last_updated"`
}

type commandMeta struct {
	TelegramUserID string
	CommandType    string
	ProjectID      string
	Alias          string
	ProjectPath    string
}

func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{
		now:             time.Now,
		pairingTTL:      DefaultPairingTTL,
		redeliveryAfter: DefaultRedeliveryTTL,
		pairCodes:       make(map[string]pairCodeRecord),
		agentByUser:     make(map[string]string),
		agentKeyByAgent: make(map[string]string),
		agentByKey:      make(map[string]string),
		queued:          make(map[string][]contracts.Command),
		inflight:        make(map[string][]inflightCommand),
		results:         make(map[string]map[string]contracts.CommandResult),
		projects:        make(map[string]map[string]*projectRecord),
		aliases:         make(map[string]map[string]string),
		commands:        make(map[string]commandMeta),
	}
}

func (b *MemoryBackend) SetClock(nowFn func() time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.now = nowFn
}

func (b *MemoryBackend) SetPairingTTL(ttl time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pairingTTL = ttl
}

func (b *MemoryBackend) StartPairing(telegramUserID string) (contracts.PairStartResponse, error) {
	if strings.TrimSpace(telegramUserID) == "" {
		return contracts.PairStartResponse{}, contracts.APIError{Code: contracts.ErrValidationRequiredField, Message: "telegram_user_id is required"}
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	b.pairCounter++
	code := fmt.Sprintf("PAIR-%06d", b.pairCounter)
	expiresAt := b.now().UTC().Add(b.pairingTTL)
	b.pairCodes[code] = pairCodeRecord{TelegramUserID: telegramUserID, ExpiresAt: expiresAt}
	return contracts.PairStartResponse{PairingCode: code, ExpiresAt: expiresAt}, nil
}

func (b *MemoryBackend) ClaimPairing(req contracts.PairClaimRequest) (contracts.PairClaimResponse, error) {
	if strings.TrimSpace(req.PairingCode) == "" {
		return contracts.PairClaimResponse{}, contracts.APIError{Code: contracts.ErrValidationRequiredField, Message: "pairing_code is required"}
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	rec, ok := b.pairCodes[req.PairingCode]
	if !ok {
		return contracts.PairClaimResponse{}, contracts.APIError{Code: contracts.ErrPairingInvalidCode, Message: "pairing code not found"}
	}
	delete(b.pairCodes, req.PairingCode)
	if b.now().UTC().After(rec.ExpiresAt) {
		return contracts.PairClaimResponse{}, contracts.APIError{Code: contracts.ErrPairingExpired, Message: "pairing code expired"}
	}

	if oldAgentID, ok := b.agentByUser[rec.TelegramUserID]; ok {
		if oldKey, ok := b.agentKeyByAgent[oldAgentID]; ok {
			delete(b.agentByKey, oldKey)
		}
		delete(b.agentKeyByAgent, oldAgentID)
	}

	b.agentCounter++
	b.keyCounter++
	agentID := fmt.Sprintf("agent-%06d", b.agentCounter)
	agentKey := fmt.Sprintf("key-%06d", b.keyCounter)
	b.agentByUser[rec.TelegramUserID] = agentID
	b.agentKeyByAgent[agentID] = agentKey
	b.agentByKey[agentKey] = agentID
	return contracts.PairClaimResponse{AgentID: agentID, AgentKey: agentKey}, nil
}

func (b *MemoryBackend) AuthenticateAgentKey(agentKey string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	agentID, ok := b.agentByKey[agentKey]
	return agentID, ok
}

func (b *MemoryBackend) AgentIDForUser(telegramUserID string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	agentID, ok := b.agentByUser[telegramUserID]
	return agentID, ok
}

func (b *MemoryBackend) UserIDForAgent(agentID string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for userID, agent := range b.agentByUser {
		if agent == agentID {
			return userID, true
		}
	}
	return "", false
}

// Enqueue satisfies CommandQueue by ignoring context for in-memory queue.
func (b *MemoryBackend) Enqueue(ctx context.Context, agentID string, cmd contracts.Command) error {
	_ = ctx
	if strings.TrimSpace(agentID) == "" {
		return errors.New("agentID is required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.queued[agentID] = append(b.queued[agentID], cmd)
	return nil
}

func (b *MemoryBackend) Poll(ctx context.Context, agentID string, timeoutSeconds int) (*contracts.Command, error) {
	_ = ctx
	_ = timeoutSeconds
	if strings.TrimSpace(agentID) == "" {
		return nil, errors.New("agentID is required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now().UTC()
	inflight := b.inflight[agentID]
	for i := range inflight {
		if now.Sub(inflight[i].InflightAt) >= b.redeliveryAfter {
			inflight[i].InflightAt = now
			b.inflight[agentID] = inflight
			cmd := inflight[i].Command
			return &cmd, nil
		}
	}

	queued := b.queued[agentID]
	if len(queued) == 0 {
		return nil, nil
	}
	cmd := queued[0]
	b.queued[agentID] = queued[1:]
	b.inflight[agentID] = append(b.inflight[agentID], inflightCommand{Command: cmd, InflightAt: now})
	return &cmd, nil
}

func (b *MemoryBackend) StoreResult(ctx context.Context, agentID string, result contracts.CommandResult) error {
	_ = ctx
	if strings.TrimSpace(agentID) == "" {
		return errors.New("agentID is required")
	}
	if strings.TrimSpace(result.CommandID) == "" {
		return contracts.APIError{Code: contracts.ErrValidationRequiredField, Message: "command_id is required"}
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	items := b.inflight[agentID]
	out := items[:0]
	for _, item := range items {
		if item.Command.CommandID != result.CommandID {
			out = append(out, item)
		}
	}
	b.inflight[agentID] = out
	if _, ok := b.results[agentID]; !ok {
		b.results[agentID] = make(map[string]contracts.CommandResult)
	}
	b.results[agentID][result.CommandID] = result
	if meta, ok := b.commands[result.CommandID]; ok {
		b.applyResultToProject(meta, result)
	}
	return nil
}

func (b *MemoryBackend) GetResult(ctx context.Context, agentID string, commandID string) (*contracts.CommandResult, error) {
	_ = ctx
	b.mu.Lock()
	defer b.mu.Unlock()
	if resByAgent, ok := b.results[agentID]; ok {
		if res, ok := resByAgent[commandID]; ok {
			cpy := res
			return &cpy, nil
		}
	}
	return nil, nil
}

func (b *MemoryBackend) RegisterCommandMeta(commandID string, meta commandMeta) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.commands[commandID] = meta
}

func (b *MemoryBackend) SetProject(userID string, record projectRecord) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.projects[userID]; !ok {
		b.projects[userID] = make(map[string]*projectRecord)
	}
	if _, ok := b.aliases[userID]; !ok {
		b.aliases[userID] = make(map[string]string)
	}
	alias := record.Alias
	if alias != "" {
		b.aliases[userID][strings.ToLower(alias)] = record.ProjectID
	}
	copy := record
	b.projects[userID][record.ProjectID] = &copy
}

func (b *MemoryBackend) UpdateProjectPolicy(userID string, projectID string, policy projectPolicy) {
	b.mu.Lock()
	defer b.mu.Unlock()
	projects := b.projects[userID]
	if projects == nil {
		return
	}
	if rec, ok := projects[projectID]; ok {
		rec.Policy = policy
		rec.LastUpdated = b.now().UTC()
	}
}

func (b *MemoryBackend) ResolveProject(userID, aliasOrID string) (*projectRecord, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	projects := b.projects[userID]
	if projects == nil {
		return nil, false
	}
	if rec, ok := projects[aliasOrID]; ok {
		cpy := *rec
		return &cpy, true
	}
	if pid, ok := b.aliases[userID][strings.ToLower(aliasOrID)]; ok {
		if rec, ok := projects[pid]; ok {
			cpy := *rec
			return &cpy, true
		}
	}
	return nil, false
}

func (b *MemoryBackend) ListProjects(userID string) []projectRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	projects := b.projects[userID]
	if projects == nil {
		return nil
	}
	out := make([]projectRecord, 0, len(projects))
	for _, rec := range projects {
		out = append(out, *rec)
	}
	return out
}

func (b *MemoryBackend) applyResultToProject(meta commandMeta, result contracts.CommandResult) {
	if meta.ProjectID == "" || meta.TelegramUserID == "" {
		if meta.CommandType != contracts.CommandTypeRegisterProject {
			return
		}
	}
	if result.OK {
		switch meta.CommandType {
		case contracts.CommandTypeRegisterProject:
			policy := projectPolicy{Decision: contracts.DecisionDeny}
			projectID := meta.ProjectID
			if pid, ok := result.Meta["project_id"].(string); ok && pid != "" {
				projectID = pid
			}
			if projectID == "" {
				return
			}
			projectPath := meta.ProjectPath
			if p, ok := result.Meta["project_path"].(string); ok && p != "" {
				projectPath = p
			}
			b.SetProject(meta.TelegramUserID, projectRecord{
				Alias:       meta.Alias,
				ProjectID:   projectID,
				ProjectPath: projectPath,
				Policy:      policy,
				LastUpdated: b.now().UTC(),
			})
		case contracts.CommandTypeApplyProjectPolicy:
			policy := projectPolicy{Decision: contracts.DecisionAllow}
			if decision, ok := result.Meta["decision"].(string); ok {
				policy.Decision = decision
			}
			if scope, ok := result.Meta["scope"].([]string); ok {
				policy.Scope = scope
			}
			if expStr, ok := result.Meta["expires_at"].(string); ok {
				if exp, err := time.Parse(time.RFC3339Nano, expStr); err == nil {
					policy.ExpiresAt = &exp
				}
			}
			b.UpdateProjectPolicy(meta.TelegramUserID, meta.ProjectID, policy)
		}
		return
	}
}
