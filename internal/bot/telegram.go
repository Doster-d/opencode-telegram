package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"opencode-telegram/internal/proxy/contracts"
	"opencode-telegram/pkg/store"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TelegramBotInterface interface {
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
	GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
}

type DebouncerInterface interface {
	Debounce(key string, text string, fn func(string) error)
}

var newTelegramBot = func(token string) (TelegramBotInterface, error) {
	return tgbotapi.NewBotAPI(token)
}

type BotApp struct {
	tg           TelegramBotInterface
	cfg          *Config
	oc           OpencodeClientInterface
	store        store.Store
	debouncer    DebouncerInterface
	octSessionID string // persistent session whose title starts with "oct_"
	runMu        sync.Mutex
	activeRuns   map[string]string
	runOwners    map[string]string
	sleep        func(time.Duration)

	// Backend client for command routing
	backendURL string
	httpClient *http.Client

	listProjectsFn func(userID int64) ([]projectRecord, error)
}

type approvalDecision struct {
	Decision  string     `json:"decision"`
	ExpiresAt *time.Time `json:"expires_at"`
	Scope     []string   `json:"scope"`
}

type projectRecord struct {
	Alias       string           `json:"alias"`
	ProjectID   string           `json:"project_id"`
	ProjectPath string           `json:"project_path"`
	Policy      approvalDecision `json:"policy"`
	LastUpdated time.Time        `json:"last_updated"`
}

type approvalRequest struct {
	TelegramUserID int64     `json:"telegram_user_id"`
	ProjectID      string    `json:"project_id"`
	Alias          string    `json:"alias"`
	Scopes         []string  `json:"scopes"`
	RequestedAt    time.Time `json:"requested_at"`
}

type commandRecord struct {
	CommandID string    `json:"command_id"`
	Type      string    `json:"type"`
	ProjectID string    `json:"project_id"`
	Alias     string    `json:"alias"`
	CreatedAt time.Time `json:"created_at"`
}

type storedCommands struct {
	Commands []commandRecord `json:"commands"`
}

func NewBotApp(cfg *Config, oc OpencodeClientInterface, st store.Store) (*BotApp, error) {
	bot, err := newTelegramBot(cfg.TelegramToken)
	if err != nil {
		return nil, err
	}
	app := &BotApp{
		tg:             bot,
		cfg:            cfg,
		oc:             oc,
		store:          st,
		debouncer:      NewDebouncer(500 * time.Millisecond),
		activeRuns:     make(map[string]string),
		runOwners:      make(map[string]string),
		sleep:          time.Sleep,
		backendURL:     cfg.BackendURL,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		listProjectsFn: nil,
	}

	// Find or create persistent session whose title starts with configured prefix
	sessions, err := oc.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	var foundID string
	prefix := cfg.SessionPrefix

	for _, s := range sessions {
		if title, ok := s["title"].(string); ok && strings.HasPrefix(title, prefix) {
			if id, ok := s["id"].(string); ok {
				foundID = id
				break
			}
		}
	}

	if foundID == "" {
		// create a new persistent session with unique prefix-based title
		title := fmt.Sprintf("%s%d", prefix, time.Now().Unix())
		session, err := oc.CreateSession(title)
		if err != nil {
			return nil, fmt.Errorf("failed to create persistent session: %w", err)
		}
		if id, ok := session["id"].(string); ok {
			foundID = id
		} else {
			return nil, fmt.Errorf("session id not found in response")
		}
	}

	app.octSessionID = foundID
	return app, nil
}

func (a *BotApp) StartPolling() error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := a.tg.GetUpdatesChan(u)
	for upd := range updates {
		if upd.CallbackQuery != nil {
			a.handleCallbackQuery(upd.CallbackQuery)
			continue
		}

		if upd.Message == nil {
			continue
		}
		if upd.Message.From == nil {
			continue
		}

		userID := upd.Message.From.ID
		if upd.Message.IsCommand() {
			cmd := upd.Message.Command()
			args := upd.Message.CommandArguments()

			if !a.isAllowed(userID) && cmd != "start" && cmd != "help" {
				a.sendAccessGuidance(upd.Message.Chat.ID)
				continue
			}

			switch cmd {
			case "start":
				a.handleStart(upd.Message.Chat.ID)
			case "help":
				a.handleHelp(upd.Message.Chat.ID)
			case "settings":
				a.handleSettings(upd.Message.Chat.ID)
			case "language":
				a.handleLanguage(upd.Message.Chat.ID)
			case "mute":
				a.handleMute(upd.Message.Chat.ID)
			case "unmute":
				a.handleUnmute(upd.Message.Chat.ID)
			case "createsession":
				a.handleCreateSession(upd.Message.Chat.ID, args, userID)
			case "deletesession":
				a.handleDeleteSession(upd.Message.Chat.ID, args, userID)
			case "selectsession":
				a.handleSelectSession(upd.Message.Chat.ID, args, userID)
			case "mysession":
				a.handleMySession(upd.Message.Chat.ID, userID)
			case "status":
				a.handleAgentStatus(upd.Message.Chat.ID, userID)
			case "sessions":
				a.handleSessions(upd.Message.Chat.ID)
			case "run":
				a.handleRun(upd.Message.Chat.ID, args, userID)
			case "abort":
				a.handleAbort(upd.Message.Chat.ID, args, userID)
			case "project":
				// Handle /project add/list subcommand
				fields := strings.Fields(args)
				if len(fields) == 0 {
					a.tg.Send(tgbotapi.NewMessage(upd.Message.Chat.ID, "Usage: /project add <ABS_PATH> | /project list"))
					break
				}
				sub := fields[0]
				rest := strings.TrimSpace(strings.TrimPrefix(args, sub))
				switch sub {
				case "add":
					a.handleProjectAdd(upd.Message.Chat.ID, rest, userID)
				case "list":
					a.handleProjectList(upd.Message.Chat.ID, userID)
				default:
					a.tg.Send(tgbotapi.NewMessage(upd.Message.Chat.ID, "Usage: /project add <ABS_PATH> | /project list"))
				}
			case "start_server":
				a.handleStartServer(upd.Message.Chat.ID, args, userID)
			case "pair":
				a.startPairing(upd.Message.Chat.ID, userID)
			case "agent_status":
				a.handleAgentStatus(upd.Message.Chat.ID, userID)
			default:
				a.tg.Send(tgbotapi.NewMessage(upd.Message.Chat.ID, "Unknown command"))
			}
		} else if upd.Message.Text != "" {
			if !a.isAllowed(userID) {
				a.sendAccessGuidance(upd.Message.Chat.ID)
				continue
			}
			// treat any non-command message as a prompt
			a.handleRun(upd.Message.Chat.ID, upd.Message.Text, userID)
		}
	}
	return nil
}

func (a *BotApp) isAllowed(userID int64) bool {
	if len(a.cfg.AllowedIDs) == 0 {
		return true
	}
	return a.cfg.AllowedIDs[userID]
}

func (a *BotApp) isAdmin(userID int64) bool {
	return a.cfg.AdminIDs[userID]
}

func (a *BotApp) sendAccessGuidance(chatID int64) {
	a.tg.Send(tgbotapi.NewMessage(chatID, "Access required. Ask an admin to add your Telegram ID to ALLOWED_TELEGRAM_IDS."))
}

func (a *BotApp) handleStart(chatID int64) {
	a.tg.Send(tgbotapi.NewMessage(chatID, "Welcome. Use /help to see available commands."))
}

func (a *BotApp) handleHelp(chatID int64) {
	text := "Commands:\n" +
		"/start, /help, /settings, /status, /language, /run <prompt>, /abort <session_id>, /mute, /unmute\n\n" +
		"Advanced: /sessions, /createsession, /deletesession, /selectsession, /mysession"
	a.tg.Send(tgbotapi.NewMessage(chatID, text))
}

func (a *BotApp) handleSettings(chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "Settings")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Language", "settings:language"),
			tgbotapi.NewInlineKeyboardButtonData("Mute", "settings:mute"),
			tgbotapi.NewInlineKeyboardButtonData("Unmute", "settings:unmute"),
		),
	)
	a.tg.Send(msg)
}

func (a *BotApp) handleLanguage(chatID int64) {
	a.tg.Send(tgbotapi.NewMessage(chatID, "Language selection is coming soon. Current language: English."))
}

func (a *BotApp) handleMute(chatID int64) {
	a.tg.Send(tgbotapi.NewMessage(chatID, "Notifications muted for now."))
}

func (a *BotApp) handleUnmute(chatID int64) {
	a.tg.Send(tgbotapi.NewMessage(chatID, "Notifications unmuted."))
}

func (a *BotApp) handleCallbackQuery(cb *tgbotapi.CallbackQuery) {
	ack := tgbotapi.NewCallback(cb.ID, "")
	if err := a.requestWithRetry(ack); err != nil {
		if cb.Message != nil {
			a.tg.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "Unable to process action right now. Please try again."))
		}
		return
	}

	if cb.Message == nil {
		return
	}

	if strings.HasPrefix(cb.Data, "approve:") {
		a.handleApprovalDecision(cb)
		return
	}

	switch cb.Data {
	case "settings:language":
		a.handleLanguage(cb.Message.Chat.ID)
	case "settings:mute":
		a.handleMute(cb.Message.Chat.ID)
	case "settings:unmute":
		a.handleUnmute(cb.Message.Chat.ID)
	default:
		a.tg.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "Unknown settings action."))
	}
}

func (a *BotApp) handleApprovalDecision(cb *tgbotapi.CallbackQuery) {
	if cb.Message == nil || cb.From == nil {
		return
	}
	parts := strings.Split(cb.Data, "|")
	if len(parts) < 2 {
		a.tg.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "Invalid approval payload."))
		return
	}
	decisionPart := strings.TrimPrefix(parts[0], "approve:")
	alias := parts[1]
	project, err := a.resolveProject(cb.From.ID, alias)
	if err != nil || project == nil {
		a.tg.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "Unable to resolve project for approval."))
		return
	}
	decision := contracts.DecisionDeny
	var expiresAt *time.Time
	scopes := []string{}
	switch decisionPart {
	case "deny":
		decision = contracts.DecisionDeny
	case "allow30:start":
		decision = contracts.DecisionAllow
		exp := time.Now().UTC().Add(30 * time.Minute)
		expiresAt = &exp
		scopes = []string{contracts.ScopeStartServer}
	case "allow30:both":
		decision = contracts.DecisionAllow
		exp := time.Now().UTC().Add(30 * time.Minute)
		expiresAt = &exp
		scopes = []string{contracts.ScopeStartServer, contracts.ScopeRunTask}
	case "allow:both":
		decision = contracts.DecisionAllow
		scopes = []string{contracts.ScopeStartServer, contracts.ScopeRunTask}
	default:
		decision = contracts.DecisionDeny
	}
	agentKey, ok := a.store.GetUserAgentKey(cb.From.ID)
	if !ok || agentKey == "" {
		a.tg.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "You are not paired. Use /project add to pair first."))
		return
	}
	commandID := fmt.Sprintf("cmd-%d", time.Now().UnixNano())
	payload := map[string]any{
		"project_id": project.ProjectID,
		"decision":   decision,
		"scope":      scopes,
	}
	if expiresAt != nil {
		payload["expires_at"] = expiresAt.Format(time.RFC3339Nano)
	}
	cmd := map[string]any{
		"type":            contracts.CommandTypeApplyProjectPolicy,
		"command_id":      commandID,
		"idempotency_key": fmt.Sprintf("key-%d", time.Now().UnixNano()),
		"created_at":      time.Now().UTC().Format(time.RFC3339Nano),
		"payload":         payload,
	}
	cmdBody, _ := json.Marshal(cmd)
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/v1/command", a.backendURL), bytes.NewBuffer(cmdBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+agentKey)
	req.Header.Set("X-Telegram-User-ID", strconv.FormatInt(cb.From.ID, 10))
	resp, err := a.httpClient.Do(req)
	if err != nil {
		a.tg.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "Failed to send approval: "+err.Error()))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		var errResp map[string]any
		json.NewDecoder(resp.Body).Decode(&errResp)
		a.tg.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, fmt.Sprintf("Failed to queue approval: %v", errResp)))
		return
	}
	a.storeCommand(cb.From.ID, commandRecord{CommandID: commandID, Type: contracts.CommandTypeApplyProjectPolicy, ProjectID: project.ProjectID, Alias: project.Alias, CreatedAt: time.Now().UTC()})
	a.tg.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, fmt.Sprintf("Policy updated for %s.", project.Alias)))
	// Optimistically update local view
	a.updateLocalPolicy(cb.From.ID, project.ProjectID, decision, scopes, expiresAt)
}

func (a *BotApp) updateLocalPolicy(userID int64, projectID string, decision string, scopes []string, expiresAt *time.Time) {
	projects, err := a.listProjects(userID)
	if err != nil {
		return
	}
	for _, p := range projects {
		if p.ProjectID != projectID {
			continue
		}
		break
	}
}

func (a *BotApp) isRetryableTelegramErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "too many requests") || strings.Contains(msg, "429") || strings.Contains(msg, "retry after")
}

func (a *BotApp) requestWithRetry(c tgbotapi.Chattable) error {
	backoff := 100 * time.Millisecond
	var lastErr error
	for i := 0; i < 3; i++ {
		_, err := a.tg.Request(c)
		if err == nil {
			return nil
		}
		lastErr = err
		if !a.isRetryableTelegramErr(err) || i == 2 {
			break
		}
		a.sleep(backoff)
		backoff *= 2
	}
	return lastErr
}

func (a *BotApp) runKey(chatID, userID int64) string {
	return strconv.FormatInt(chatID, 10) + ":" + strconv.FormatInt(userID, 10)
}

func (a *BotApp) tryStartRun(chatID, userID int64, sessionID string) bool {
	key := a.runKey(chatID, userID)
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if a.activeRuns == nil {
		a.activeRuns = make(map[string]string)
	}
	if a.runOwners == nil {
		a.runOwners = make(map[string]string)
	}
	if _, exists := a.activeRuns[key]; exists {
		return false
	}
	a.activeRuns[key] = sessionID
	a.runOwners[sessionID] = key
	return true
}

func (a *BotApp) clearRun(chatID, userID int64) {
	key := a.runKey(chatID, userID)
	a.runMu.Lock()
	defer a.runMu.Unlock()
	sid := a.activeRuns[key]
	delete(a.activeRuns, key)
	if sid != "" {
		if ownerKey, ok := a.runOwners[sid]; ok && ownerKey == key {
			delete(a.runOwners, sid)
		}
	}
}

func (a *BotApp) clearRunBySession(sessionID string) bool {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	key, ok := a.runOwners[sessionID]
	if !ok {
		return false
	}
	delete(a.runOwners, sessionID)
	delete(a.activeRuns, key)
	return true
}

func (a *BotApp) sessionExists(sessionID string) (bool, error) {
	sessions, err := a.oc.ListSessions()
	if err != nil {
		return false, err
	}
	for _, s := range sessions {
		if id, ok := s["id"].(string); ok && id == sessionID {
			return true, nil
		}
	}
	return false, nil
}

func (a *BotApp) resolveUserSession(userID int64) (string, bool, error) {
	if sid, ok := a.store.GetUserSession(userID); ok {
		exists, err := a.sessionExists(sid)
		if err != nil {
			return "", false, err
		}
		if !exists {
			return "", true, fmt.Errorf("selected session %s is no longer available", sid)
		}
		return sid, false, nil
	}

	fallbackTitle := fmt.Sprintf("%suser_%d", a.cfg.SessionPrefix, userID)
	sessions, err := a.oc.ListSessions()
	if err != nil {
		return "", false, err
	}
	for _, s := range sessions {
		title, _ := s["title"].(string)
		if title == fallbackTitle {
			if id, ok := s["id"].(string); ok && id != "" {
				_ = a.store.SetUserSession(userID, id)
				return id, false, nil
			}
		}
	}

	created, err := a.oc.CreateSession(fallbackTitle)
	if err != nil {
		return "", false, err
	}
	id, _ := created["id"].(string)
	if id == "" {
		return "", false, fmt.Errorf("session id not found in response")
	}
	_ = a.store.SetUserSession(userID, id)
	return id, false, nil
}

func (a *BotApp) handleStatus(chatID int64) {
	msg := fmt.Sprintf("Opencode: %s", a.cfg.OpencodeBase)
	a.tg.Send(tgbotapi.NewMessage(chatID, msg))
}

func (a *BotApp) handleSessions(chatID int64) {
	sessions, err := a.oc.ListSessions()
	if err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Error listing sessions: "+err.Error()))
		return
	}
	if len(sessions) == 0 {
		a.tg.Send(tgbotapi.NewMessage(chatID, "No sessions"))
		return
	}
	var b string
	prefix := a.cfg.SessionPrefix
	for _, s := range sessions {
		title, _ := s["title"].(string)
		if prefix == "" || strings.HasPrefix(title, prefix) {
			id := s["id"]
			b += fmt.Sprintf("%v - %v\n", id, title)
		}
	}
	a.tg.Send(tgbotapi.NewMessage(chatID, b))
}

func (a *BotApp) handleCreateSession(chatID int64, title string, userID int64) {
	if title == "" {
		title = fmt.Sprintf("%s%d", a.cfg.SessionPrefix, time.Now().Unix())
	}
	session, err := a.oc.CreateSession(title)
	if err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Error creating session: "+err.Error()))
		return
	}
	id, _ := session["id"].(string)
	a.tg.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Created session: %s - %s", id, title)))
	// auto-select for the user who created it
	if id != "" {
		_ = a.store.SetUserSession(userID, id)
	}
}

func (a *BotApp) handleDeleteSession(chatID int64, args string, userID int64) {
	if args == "" {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Usage: /deletesession <session_id>"))
		return
	}
	if !a.isAdmin(userID) {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Only admins can delete sessions."))
		return
	}
	if err := a.oc.DeleteSession(args); err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Failed to delete session: "+err.Error()))
		return
	}
	// remove from store mapping(s)
	_ = a.store.DeleteSession(args)
	a.tg.Send(tgbotapi.NewMessage(chatID, "Deleted session: "+args))
}

func (a *BotApp) handleSelectSession(chatID int64, args string, userID int64) {
	if args == "" {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Usage: /selectsession <session_id|title_prefix>"))
		return
	}
	// if args looks like an id (starts with ses_ or oct_), treat as id
	if strings.HasPrefix(args, "ses_") || strings.HasPrefix(args, "oct_") {
		_ = a.store.SetUserSession(userID, args)
		a.tg.Send(tgbotapi.NewMessage(chatID, "Selected session: "+args))
		return
	}
	// otherwise, try to find a session by title prefix
	sessions, err := a.oc.ListSessions()
	if err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Error listing sessions: "+err.Error()))
		return
	}
	for _, s := range sessions {
		if title, ok := s["title"].(string); ok && strings.HasPrefix(title, args) {
			if id, ok := s["id"].(string); ok {
				_ = a.store.SetUserSession(userID, id)
				a.tg.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Selected session: %s - %s", id, title)))
				return
			}
		}
	}
	a.tg.Send(tgbotapi.NewMessage(chatID, "No session found matching: "+args))
}

func (a *BotApp) handleMySession(chatID int64, userID int64) {
	if sid, ok := a.store.GetUserSession(userID); ok {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Your selected session: "+sid))
		return
	}
	a.tg.Send(tgbotapi.NewMessage(chatID, "You have not selected a session. Use /selectsession <id|title_prefix>"))
}

// handleRun now routes to backend run_task command.

func (a *BotApp) handleAbort(chatID int64, args string, userID int64) {
	if args == "" {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Usage: /abort <session_id>"))
		return
	}
	// only allow the user if they're admin or the allowed list contains them
	if !a.isAdmin(userID) {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Only admins can abort sessions."))
		return
	}
	err := a.oc.AbortSession(args)
	if err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Abort failed: "+err.Error()))
		return
	}
	a.tg.Send(tgbotapi.NewMessage(chatID, "Aborted session: "+args))
}

// handleProjectAdd initiates pairing and registers a project
func (a *BotApp) handleProjectAdd(chatID int64, args string, userID int64) {
	// Check if user is already paired
	agentKey, ok := a.store.GetUserAgentKey(userID)
	if ok && agentKey != "" {
		if strings.TrimSpace(args) == "" {
			a.tg.Send(tgbotapi.NewMessage(chatID, "Usage: /project add <ABS_PATH>"))
			return
		}
		projectPath := strings.TrimSpace(args)
		a.enqueueProjectRegister(chatID, userID, agentKey, projectPath)
		return
	}

	// Not paired yet - either claim existing pairing code or start new
	telegramUserID := strconv.FormatInt(userID, 10)
	if code, ok := a.store.GetPairingCode(telegramUserID); ok && code != "" {
		a.claimPairing(chatID, userID, code)
		return
	}
	// initiate pairing flow
	a.startPairing(chatID, userID)
}

func (a *BotApp) startPairing(chatID int64, userID int64) {
	telegramUserID := strconv.FormatInt(userID, 10)
	reqBody, _ := json.Marshal(map[string]string{"telegram_user_id": telegramUserID})
	resp, err := a.httpClient.Post(
		fmt.Sprintf("%s/v1/pair/start", a.backendURL),
		"application/json",
		bytes.NewBuffer(reqBody),
	)
	if err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Failed to initiate pairing: "+err.Error()))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]any
		json.NewDecoder(resp.Body).Decode(&errResp)
		a.tg.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Pairing failed: %v", errResp)))
		return
	}

	var pairResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&pairResp); err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Failed to parse pairing response"))
		return
	}

	pairingCode, _ := pairResp["pairing_code"].(string)
	expiresAt, _ := pairResp["expires_at"].(string)
	_ = a.store.SetPairingCode(telegramUserID, pairingCode)

	msg := fmt.Sprintf("Pairing initiated!\n\nPairing Code: `%s`\n\nExpires at: %s\n\nRun the following on your machine to complete pairing:\n\n`oct-agent pair %s`",
		pairingCode, expiresAt, pairingCode)
	a.tg.Send(tgbotapi.NewMessage(chatID, msg))
}

func (a *BotApp) claimPairing(chatID int64, userID int64, pairingCode string) {
	reqBody, _ := json.Marshal(map[string]string{"pairing_code": pairingCode, "device_info": "telegram"})
	resp, err := a.httpClient.Post(
		fmt.Sprintf("%s/v1/pair/claim", a.backendURL),
		"application/json",
		bytes.NewBuffer(reqBody),
	)
	if err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Failed to claim pairing: "+err.Error()))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var errResp map[string]any
		json.NewDecoder(resp.Body).Decode(&errResp)
		a.tg.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Pairing claim failed: %v", errResp)))
		return
	}
	var claimResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&claimResp); err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Failed to parse pairing claim response"))
		return
	}
	agentKey, _ := claimResp["agent_key"].(string)
	if agentKey == "" {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Pairing claim returned no agent key"))
		return
	}
	_ = a.store.SetUserAgentKey(userID, agentKey)
	a.tg.Send(tgbotapi.NewMessage(chatID, "Pairing completed. You can now add projects."))
}

func (a *BotApp) enqueueProjectRegister(chatID int64, userID int64, agentKey string, projectPath string) {
	alias := strings.TrimSpace(projectAliasFromPath(projectPath))
	if alias == "" {
		alias = fmt.Sprintf("project-%d", time.Now().Unix())
	}
	cmd := map[string]any{
		"type":            contracts.CommandTypeRegisterProject,
		"command_id":      fmt.Sprintf("cmd-%d", time.Now().UnixNano()),
		"idempotency_key": fmt.Sprintf("key-%d", time.Now().UnixNano()),
		"created_at":      time.Now().UTC().Format(time.RFC3339Nano),
		"payload": map[string]string{
			"project_path_raw": projectPath,
		},
	}
	commandID := cmd["command_id"].(string)
	cmdBody, _ := json.Marshal(cmd)
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/v1/command", a.backendURL), bytes.NewBuffer(cmdBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+agentKey)
	req.Header.Set("X-Telegram-User-ID", strconv.FormatInt(userID, 10))
	resp, err := a.httpClient.Do(req)
	if err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Failed to send command: "+err.Error()))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		a.storeCommand(userID, commandRecord{CommandID: commandID, Type: contracts.CommandTypeRegisterProject, Alias: alias, CreatedAt: time.Now().UTC()})
		a.tg.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Project registration queued for %s (alias: %s).", projectPath, alias)))
		return
	}
	var errResp map[string]any
	json.NewDecoder(resp.Body).Decode(&errResp)
	a.tg.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Failed to queue project registration: %v", errResp)))
}

func projectAliasFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = strings.TrimRight(path, "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	return strings.TrimSpace(parts[len(parts)-1])
}

func (a *BotApp) handleProjectList(chatID int64, userID int64) {
	entries, err := a.listProjects(userID)
	if err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Failed to load projects: "+err.Error()))
		return
	}
	if len(entries) == 0 {
		a.tg.Send(tgbotapi.NewMessage(chatID, "No projects registered yet."))
		return
	}
	var b strings.Builder
	for _, p := range entries {
		policy := p.Policy.Decision
		if policy == "" {
			policy = contracts.DecisionDeny
		}
		b.WriteString(fmt.Sprintf("%s (%s) - %s\n", p.Alias, p.ProjectID, policy))
	}
	a.tg.Send(tgbotapi.NewMessage(chatID, b.String()))
}

func (a *BotApp) handleStartServer(chatID int64, args string, userID int64) {
	if strings.TrimSpace(args) == "" {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Usage: /start_server <project>"))
		return
	}
	agentKey, ok := a.store.GetUserAgentKey(userID)
	if !ok || agentKey == "" {
		a.tg.Send(tgbotapi.NewMessage(chatID, "You are not paired. Use /project add to pair first."))
		return
	}
	projectAlias := strings.TrimSpace(args)
	project, err := a.resolveProject(userID, projectAlias)
	if err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Failed to resolve project: "+err.Error()))
		return
	}
	if project == nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Unknown project alias. Use /project list."))
		return
	}
	if !a.policyAllows(project.Policy, contracts.ScopeStartServer) {
		a.promptApproval(chatID, userID, project, []string{contracts.ScopeStartServer})
		return
	}
	commandID := fmt.Sprintf("cmd-%d", time.Now().UnixNano())
	cmd := map[string]any{
		"type":            contracts.CommandTypeStartServer,
		"command_id":      commandID,
		"idempotency_key": fmt.Sprintf("key-%d", time.Now().UnixNano()),
		"created_at":      time.Now().UTC().Format(time.RFC3339Nano),
		"payload": map[string]string{
			"project_id": project.ProjectID,
		},
	}
	cmdBody, _ := json.Marshal(cmd)
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/v1/command", a.backendURL), bytes.NewBuffer(cmdBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+agentKey)
	req.Header.Set("X-Telegram-User-ID", strconv.FormatInt(userID, 10))
	resp, err := a.httpClient.Do(req)
	if err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Failed to send command: "+err.Error()))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		var errResp map[string]any
		json.NewDecoder(resp.Body).Decode(&errResp)
		a.tg.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Failed to queue command: %v", errResp)))
		return
	}
	a.storeCommand(userID, commandRecord{CommandID: commandID, Type: contracts.CommandTypeStartServer, ProjectID: project.ProjectID, Alias: project.Alias, CreatedAt: time.Now().UTC()})
	a.tg.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("start_server queued for %s.", project.Alias)))
	a.pollAndRelayResult(chatID, userID, commandID)
}

func (a *BotApp) handleRun(chatID int64, prompt string, userID int64) {
	if prompt == "" {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Usage: /run <project> <prompt>"))
		return
	}
	parts := strings.Fields(prompt)
	if len(parts) < 2 {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Usage: /run <project> <prompt>"))
		return
	}
	projectAlias := parts[0]
	userPrompt := strings.TrimSpace(strings.TrimPrefix(prompt, projectAlias))
	if userPrompt == "" {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Usage: /run <project> <prompt>"))
		return
	}
	agentKey, ok := a.store.GetUserAgentKey(userID)
	if !ok || agentKey == "" {
		a.tg.Send(tgbotapi.NewMessage(chatID, "You are not paired. Use /project add to pair first."))
		return
	}
	project, err := a.resolveProject(userID, projectAlias)
	if err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Failed to resolve project: "+err.Error()))
		return
	}
	if project == nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Unknown project alias. Use /project list."))
		return
	}
	if !a.policyAllows(project.Policy, contracts.ScopeRunTask) {
		a.promptApproval(chatID, userID, project, []string{contracts.ScopeRunTask})
		return
	}
	commandID := fmt.Sprintf("cmd-%d", time.Now().UnixNano())
	cmd := map[string]any{
		"type":            contracts.CommandTypeRunTask,
		"command_id":      commandID,
		"idempotency_key": fmt.Sprintf("key-%d", time.Now().UnixNano()),
		"created_at":      time.Now().UTC().Format(time.RFC3339Nano),
		"payload": map[string]string{
			"project_id": project.ProjectID,
			"prompt":     strings.TrimSpace(userPrompt),
		},
	}
	cmdBody, _ := json.Marshal(cmd)
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/v1/command", a.backendURL), bytes.NewBuffer(cmdBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+agentKey)
	req.Header.Set("X-Telegram-User-ID", strconv.FormatInt(userID, 10))
	resp, err := a.httpClient.Do(req)
	if err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Failed to send command: "+err.Error()))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		var errResp map[string]any
		json.NewDecoder(resp.Body).Decode(&errResp)
		a.tg.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Failed to queue command: %v", errResp)))
		return
	}
	a.storeCommand(userID, commandRecord{CommandID: commandID, Type: contracts.CommandTypeRunTask, ProjectID: project.ProjectID, Alias: project.Alias, CreatedAt: time.Now().UTC()})
	a.tg.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("run_task queued for %s.", project.Alias)))
	a.pollAndRelayResult(chatID, userID, commandID)
}

func (a *BotApp) listProjects(userID int64) ([]projectRecord, error) {
	if a.listProjectsFn != nil {
		return a.listProjectsFn(userID)
	}
	resp, err := a.httpClient.Get(fmt.Sprintf("%s/v1/projects?telegram_user_id=%d", a.backendURL, userID))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("backend status %d", resp.StatusCode)
	}
	var out struct {
		Projects []projectRecord `json:"projects"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Projects, nil
}

func (a *BotApp) resolveProject(userID int64, aliasOrID string) (*projectRecord, error) {
	projects, err := a.listProjects(userID)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if p.ProjectID == aliasOrID || strings.EqualFold(p.Alias, aliasOrID) {
			copy := p
			return &copy, nil
		}
	}
	return nil, nil
}

func (a *BotApp) policyAllows(policy approvalDecision, scope string) bool {
	if policy.Decision != contracts.DecisionAllow {
		return false
	}
	if policy.ExpiresAt != nil && time.Now().UTC().After(*policy.ExpiresAt) {
		return false
	}
	for _, s := range policy.Scope {
		if s == scope {
			return true
		}
	}
	return false
}

func (a *BotApp) storeCommand(userID int64, cmd commandRecord) {
	key := fmt.Sprintf("oct.commands.%d", userID)
	var rec storedCommands
	if raw, ok := a.store.GetPairingCode(key); ok {
		_ = json.Unmarshal([]byte(raw), &rec)
	}
	rec.Commands = append(rec.Commands, cmd)
	if len(rec.Commands) > 20 {
		rec.Commands = rec.Commands[len(rec.Commands)-20:]
	}
	bytes, _ := json.Marshal(rec)
	_ = a.store.SetPairingCode(key, string(bytes))
}

func (a *BotApp) getLastCommand(userID int64, commandType string, projectAlias string) (commandRecord, bool) {
	key := fmt.Sprintf("oct.commands.%d", userID)
	raw, ok := a.store.GetPairingCode(key)
	if !ok {
		return commandRecord{}, false
	}
	var rec storedCommands
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return commandRecord{}, false
	}
	for i := len(rec.Commands) - 1; i >= 0; i-- {
		c := rec.Commands[i]
		if c.Type != commandType {
			continue
		}
		if projectAlias != "" && !strings.EqualFold(c.Alias, projectAlias) {
			continue
		}
		return c, true
	}
	return commandRecord{}, false
}

func (a *BotApp) promptApproval(chatID int64, userID int64, project *projectRecord, scopes []string) {
	decisionOptions := []struct {
		Label string
		Data  string
	}{
		{"Deny", "approve:deny"},
		{"Allow 30m: START_SERVER", "approve:allow30:start"},
		{"Allow 30m: START_SERVER + RUN_TASK", "approve:allow30:both"},
		{"Allow until revoked: START_SERVER + RUN_TASK", "approve:allow:both"},
	}
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(decisionOptions))
	for _, opt := range decisionOptions {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(opt.Label, fmt.Sprintf("%s|%s", opt.Data, project.Alias))))
	}
	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Approval required for %s.", project.Alias))
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	a.tg.Send(msg)
}

// handleStartServer queues a start_server command to the backend.

// handleAgentStatus queues a status command to the backend
func (a *BotApp) handleAgentStatus(chatID int64, userID int64) {
	// Get agent key from store
	agentKey, ok := a.store.GetUserAgentKey(userID)
	if !ok || agentKey == "" {
		a.tg.Send(tgbotapi.NewMessage(chatID, "You are not paired. Use /project add to pair first."))
		return
	}

	// Create command
	cmd := map[string]any{
		"type":            contracts.CommandTypeStatus,
		"command_id":      fmt.Sprintf("cmd-%d", time.Now().UnixNano()),
		"idempotency_key": fmt.Sprintf("key-%d", time.Now().UnixNano()),
		"created_at":      time.Now().UTC().Format(time.RFC3339Nano),
		"payload":         map[string]any{},
	}

	cmdBody, _ := json.Marshal(cmd)
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/v1/command", a.backendURL), bytes.NewBuffer(cmdBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+agentKey)
	req.Header.Set("X-Telegram-User-ID", strconv.FormatInt(userID, 10))

	resp, err := a.httpClient.Do(req)
	if err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Failed to send command: "+err.Error()))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusAccepted {
		commandID := cmd["command_id"].(string)
		a.storeCommand(userID, commandRecord{CommandID: commandID, Type: contracts.CommandTypeStatus, CreatedAt: time.Now().UTC()})
		a.tg.Send(tgbotapi.NewMessage(chatID, "Status command queued."))
		a.pollAndRelayResult(chatID, userID, commandID)
	} else {
		var errResp map[string]any
		json.NewDecoder(resp.Body).Decode(&errResp)
		a.tg.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Failed to queue command: %v", errResp)))
	}
}

func (a *BotApp) pollAndRelayResult(chatID int64, userID int64, commandID string) {
	go func() {
		timeout := time.After(2 * time.Second)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-timeout:
				return
			case <-ticker.C:
				res, err := a.fetchResult(userID, commandID)
				if err != nil || res == nil {
					continue
				}
				if res.OK {
					a.tg.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Result: %s", formatSummary(res))))
				} else {
					a.tg.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Result error: %s", res.ErrorCode)))
				}
				return
			}
		}
	}()
}

func formatSummary(res *contracts.CommandResult) string {
	if res == nil {
		return ""
	}
	parts := []string{}
	if res.Summary != "" {
		parts = append(parts, res.Summary)
	}
	if res.Stdout != "" {
		parts = append(parts, truncateOutput(res.Stdout))
	}
	if res.Stderr != "" {
		parts = append(parts, truncateOutput(res.Stderr))
	}
	return strings.Join(parts, "\n")
}

func truncateOutput(s string) string {
	const max = 2048
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func (a *BotApp) fetchResult(userID int64, commandID string) (*contracts.CommandResult, error) {
	resp, err := a.httpClient.Get(fmt.Sprintf("%s/v1/result/status?telegram_user_id=%d&command_id=%s", a.backendURL, userID, commandID))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("backend status %d", resp.StatusCode)
	}
	var result contracts.CommandResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}
