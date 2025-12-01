package bot

import (
	"fmt"
	"opencode-telegram/pkg/store"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type BotApp struct {
	tg           *tgbotapi.BotAPI
	cfg          *Config
	oc           *OpencodeClient
	store        store.Store
	debouncer    *Debouncer
	octSessionID string // persistent session whose title starts with "oct_"
}

func NewBotApp(cfg *Config, oc *OpencodeClient, st store.Store) (*BotApp, error) {
	bot, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		return nil, err
	}
	app := &BotApp{tg: bot, cfg: cfg, oc: oc, store: st, debouncer: NewDebouncer(500 * time.Millisecond)}

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
		if upd.Message == nil { // ignore non-message updates for now
			continue
		}
		userID := upd.Message.From.ID
		if !a.isAllowed(userID) {
			// ignore or optionally send a polite rejection
			continue
		}

		if upd.Message.IsCommand() {
			cmd := upd.Message.Command()
			args := upd.Message.CommandArguments()
			switch cmd {
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
			b += fmt.Sprintf("%v — %v\n", id, title)
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
	a.tg.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Created session: %s — %s", id, title)))
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
				a.tg.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Selected session: %s — %s", id, title)))
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

	// Use persistent oct_ session
	sid := a.octSessionID

	// Send initial message to Telegram showing it's running
	sent, _ := a.tg.Send(tgbotapi.NewMessage(chatID, "Running on Opencode..."))
	a.store.SetSession(sid, chatID, sent.MessageID)

	// Send prompt
	_, err := a.oc.PromptSession(sid, prompt)
	if err != nil {
		a.tg.Send(tgbotapi.NewMessage(chatID, "Error prompting session: "+err.Error()))
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
