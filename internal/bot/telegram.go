package bot

import (
	"fmt"
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
}

func NewBotApp(cfg *Config, oc OpencodeClientInterface, st store.Store) (*BotApp, error) {
	bot, err := newTelegramBot(cfg.TelegramToken)
	if err != nil {
		return nil, err
	}
	app := &BotApp{
		tg:         bot,
		cfg:        cfg,
		oc:         oc,
		store:      st,
		debouncer:  NewDebouncer(500 * time.Millisecond),
		activeRuns: make(map[string]string),
		runOwners:  make(map[string]string),
		sleep:      time.Sleep,
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
				a.handleStatus(upd.Message.Chat.ID)
			case "sessions":
				a.handleSessions(upd.Message.Chat.ID)
			case "run":
				a.handleRun(upd.Message.Chat.ID, args, userID)
			case "abort":
				a.handleAbort(upd.Message.Chat.ID, args, userID)
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

func (a *BotApp) handleRun(chatID int64, prompt string, userID int64) {
	if prompt == "" {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Usage: /run <prompt>"))
		return
	}

	sid, selectedInvalid, err := a.resolveUserSession(userID)
	if err != nil {
		if selectedInvalid {
			a.tg.Send(tgbotapi.NewMessage(chatID, "Your selected session is no longer available. Use /selectsession or /createsession to choose a valid session."))
			return
		}
		a.tg.Send(tgbotapi.NewMessage(chatID, "Error resolving session: "+err.Error()))
		return
	}

	if !a.tryStartRun(chatID, userID, sid) {
		a.tg.Send(tgbotapi.NewMessage(chatID, "A run is already active for you in this chat. Use /abort or wait for it to finish."))
		return
	}

	// Send initial message to Telegram showing it's running
	sent, _ := a.tg.Send(tgbotapi.NewMessage(chatID, "Running on Opencode..."))
	a.store.SetSession(sid, chatID, sent.MessageID)

	// Send prompt
	_, err = a.oc.PromptSession(sid, prompt)
	if err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Error prompting session: "+err.Error()))
		a.clearRun(chatID, userID)
		return
	}

	// TODO: subscribe to SSE and edit message with parts as they arrive
	// a.tg.Send(tgbotapi.NewMessage(chatID, "Started session: "+sid))
}

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
