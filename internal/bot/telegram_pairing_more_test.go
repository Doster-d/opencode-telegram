package bot

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestBotPairing_StartAndClaimErrorBranches(t *testing.T) {
	mux := http.NewServeMux()
	mode := "start-bad-status"
	mux.HandleFunc("/v1/pair/start", func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "start-bad-status":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false}`))
		case "start-bad-json":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{bad`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"pairing_code":"PAIR-1","expires_at":"soon"}`))
		}
	})
	mux.HandleFunc("/v1/pair/claim", func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "claim-bad-status":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false}`))
		case "claim-bad-json":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{bad`))
		case "claim-empty-key":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"agent_id":"a1"}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"agent_id":"a1","agent_key":"k1"}`))
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	app, tg, st := testBotApp(&Config{}, &mockOpencodeClient{})
	app.backendURL = srv.URL

	app.startPairing(1, 7)
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[len(tg.sentMessages)-1].Text, "Pairing failed") {
		t.Fatalf("expected start bad status message, got %+v", tg.sentMessages)
	}

	mode = "start-bad-json"
	tg.sentMessages = nil
	app.startPairing(1, 7)
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[len(tg.sentMessages)-1].Text, "Failed to parse pairing response") {
		t.Fatalf("expected start parse error message, got %+v", tg.sentMessages)
	}

	mode = "claim-bad-status"
	tg.sentMessages = nil
	app.claimPairing(1, 7, "PAIR-1")
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[len(tg.sentMessages)-1].Text, "Pairing claim failed") {
		t.Fatalf("expected claim bad status message, got %+v", tg.sentMessages)
	}

	mode = "claim-bad-json"
	tg.sentMessages = nil
	app.claimPairing(1, 7, "PAIR-1")
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[len(tg.sentMessages)-1].Text, "Failed to parse pairing claim response") {
		t.Fatalf("expected claim parse error, got %+v", tg.sentMessages)
	}

	mode = "claim-empty-key"
	tg.sentMessages = nil
	app.claimPairing(1, 7, "PAIR-1")
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[len(tg.sentMessages)-1].Text, "returned no agent key") {
		t.Fatalf("expected empty-key error, got %+v", tg.sentMessages)
	}

	mode = "ok"
	tg.sentMessages = nil
	app.claimPairing(1, 7, "PAIR-1")
	if len(tg.sentMessages) == 0 || !strings.Contains(tg.sentMessages[len(tg.sentMessages)-1].Text, "Pairing completed") {
		t.Fatalf("expected claim success, got %+v", tg.sentMessages)
	}
	if key, ok := st.GetUserAgentKey(7); !ok || key != "k1" {
		t.Fatalf("expected stored key k1, got %q ok=%v", key, ok)
	}
}

func TestBotHandleMySessionSelectedPath(t *testing.T) {
	app, tg, st := testBotApp(&Config{}, &mockOpencodeClient{})
	_ = st.SetUserSession(7, "ses_123")
	app.handleMySession(1, 7)
	if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "ses_123") {
		t.Fatalf("expected selected session message, got %+v", tg.sentMessages)
	}

	app.handleCallbackQuery(&tgbotapi.CallbackQuery{ID: "cb", Data: "approve:deny|demo", Message: nil, From: &tgbotapi.User{ID: 7}})
}
