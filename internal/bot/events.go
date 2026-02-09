package bot

import (
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func findStringKeyRecursive(root any, target string) string {
	tgt := strings.ToLower(target)
	var out string
	var walk func(any)
	walk = func(x any) {
		if out != "" {
			return
		}
		switch m := x.(type) {
		case map[string]any:
			for k, v := range m {
				if strings.ToLower(k) == tgt {
					switch vv := v.(type) {
					case string:
						out = vv
						return
					case fmt.Stringer:
						out = vv.String()
						return
					default:
						out = fmt.Sprintf("%v", vv)
						return
					}
				}
				walk(v)
				if out != "" {
					return
				}
			}
		case map[any]any:
			for k, v := range m {
				if ks, ok := k.(string); ok && strings.ToLower(ks) == tgt {
					out = fmt.Sprintf("%v", v)
					return
				}
				walk(v)
				if out != "" {
					return
				}
			}
		case []any:
			for _, it := range m {
				walk(it)
				if out != "" {
					return
				}
			}
		}
	}
	walk(root)
	return out
}

func findSessionLikeID(root any) string {
	var out string
	var walk func(any)
	walk = func(x any) {
		if out != "" {
			return
		}
		switch m := x.(type) {
		case map[string]any:
			for k, v := range m {
				if strings.ToLower(k) == "id" {
					if s, ok := v.(string); ok && strings.HasPrefix(s, "ses_") {
						out = s
						return
					}
				}
				walk(v)
				if out != "" {
					return
				}
			}
		case map[any]any:
			for k, v := range m {
				if ks, ok := k.(string); ok && strings.ToLower(ks) == "id" {
					if s, ok := v.(string); ok && strings.HasPrefix(s, "ses_") {
						out = s
						return
					}
				}
				walk(v)
				if out != "" {
					return
				}
			}
		case []any:
			for _, it := range m {
				walk(it)
				if out != "" {
					return
				}
			}
		}
	}
	walk(root)
	return out
}

func isTerminalSessionEvent(eventType string, payload any, ev map[string]any) bool {
	if eventType != "session.updated" {
		return false
	}
	status := strings.ToLower(findStringKeyRecursive(payload, "status"))
	if status == "" {
		status = strings.ToLower(findStringKeyRecursive(ev, "status"))
	}
	return status == "completed" || status == "failed"
}

// StartEventListener subscribes to opencode SSE events and updates Telegram messages
// when session message parts are updated. This is a best-effort, minimal implementation
// that looks for event types commonly emitted by opencode (e.g., "message.part.updated").
func (a *BotApp) StartEventListener() error {
	return a.oc.SubscribeEvents(a.handleEvent)
}

func (a *BotApp) handleEvent(ev map[string]any) {
	log.Printf("DEBUG: received event: %+v", ev)

	// defensive parsing: try multiple fields for event type
	var eventType string
	if t, ok := ev["type"]; ok {
		if s, ok := t.(string); ok {
			eventType = s
		}
	}
	if eventType == "" {
		if n, ok := ev["name"]; ok {
			if s, ok := n.(string); ok {
				eventType = s
			}
		}
	}

	log.Printf("DEBUG: eventType=%s", eventType)

	// interested events
	if eventType == "message.part.updated" || eventType == "message.updated" || eventType == "session.message.part.updated" || eventType == "session.updated" {
		// payload may be under "data" or "payload"
		var payload any
		if d, ok := ev["data"]; ok {
			payload = d
		} else if p, ok := ev["payload"]; ok {
			payload = p
		} else {
			payload = ev
		}

		// extract session id from several possible locations using recursive helpers
		sid := ""
		text := ""

		// try payload first, then fall back to the full event map, for both 'sessionID' and fallback 'id'
		sid = findStringKeyRecursive(payload, "sessionID")
		if sid == "" {
			sid = findStringKeyRecursive(ev, "sessionID")
		}
		// fallback: look for 'id' that looks like a session id (starts with 'ses_')
		if sid == "" {
			sid = findSessionLikeID(payload)
			if sid == "" {
				sid = findSessionLikeID(ev)
			}
		}

		if sid == "" {
			// couldn't find session id; log with eventType and compact event for easier filtering
			// Only log the first 5 times per process to avoid log spam
			const maxMissingSessionIDLogs = 5
			// (removed unused missingSessionIDLogCountPtr)
			// static var
			var missingSessionIDLogCount int
			missingSessionIDLogCount++
			if missingSessionIDLogCount <= maxMissingSessionIDLogs {
				compact := func(v any) string {
					s := fmt.Sprintf("%#v", v)
					if len(s) > 500 {
						return s[:500] + "... (truncated)"
					}
					return s
				}
				log.Printf("DEBUG: could not extract session ID from event (eventType=%s) event=%s", eventType, compact(ev))
			}
			return
		}

		log.Printf("DEBUG: extracted sid=%s", sid)
		if isTerminalSessionEvent(eventType, payload, ev) {
			a.clearRunBySession(sid)
		}

		// lookup mapping
		chatID, msgID, ok := a.store.GetSession(sid)
		if !ok {
			log.Printf("DEBUG: session %s not in store (mapping not found)", sid)
			return
		}

		log.Printf("DEBUG: found session mapping: chatID=%d, msgID=%d", chatID, msgID)

		// Always fetch the latest session messages to ensure we get complete output
		log.Printf("DEBUG: fetching latest messages from session %s", sid)
		fetched, err := a.oc.GetSessionMessages(sid)
		if err != nil {
			log.Printf("failed to fetch session messages for %s: %v", sid, err)
			return
		}
		text = fetched
		log.Printf("DEBUG: fetched text: %s", text)

		if text == "" {
			log.Printf("DEBUG: still no text after fetch, skipping edit")
			return
		}

		log.Printf("DEBUG: debouncing edit for session %s", sid)
		// Use debouncer to avoid edit spam (500ms grace period)
		a.debouncer.Debounce(sid, text, func(latestText string) error {
			edit := tgbotapi.NewEditMessageText(chatID, msgID, latestText)
			log.Printf("DEBUG: sending edit to telegram: %s", latestText)
			err := a.requestWithRetry(edit)
			if err != nil {
				log.Printf("failed to edit telegram msg for session %s: %v", sid, err)
			}
			return err
		})
	}
}
