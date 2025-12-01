package bot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpencodeClient_GetSessionMessages_and_HTTP(t *testing.T) {
	mux := http.NewServeMux()

	// GET /session/{id}/message
	mux.HandleFunc("/session/test/messages", func(w http.ResponseWriter, r *http.Request) {
		// convenience path not used; keep for completeness
		http.NotFound(w, r)
	})

	mux.HandleFunc("/session/one/message", func(w http.ResponseWriter, r *http.Request) {
		// mixed thinking + final
		resp := []map[string]any{
			{"parts": []map[string]any{{"type": "thinking", "text": "thinking..."}, {"type": "text", "text": "final result"}}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/session/two/message", func(w http.ResponseWriter, r *http.Request) {
		// thinking only
		resp := []map[string]any{{"parts": []map[string]any{{"type": "thinking", "text": "still thinking"}}}}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/session/empty/message", func(w http.ResponseWriter, r *http.Request) {
		// empty array
		resp := []any{}
		_ = json.NewEncoder(w).Encode(resp)
	})

	// List sessions
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			sessions := []map[string]any{{"id": "ses_1", "title": "oct_1"}, {"id": "ses_2", "title": "other"}}
			_ = json.NewEncoder(w).Encode(sessions)
			return
		}
		if r.Method == "POST" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			title := fmt.Sprintf("%v", body["title"])
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "ses_new", "title": title})
			return
		}
		http.NotFound(w, r)
	})

	mux.HandleFunc("/session/ses_del", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			w.WriteHeader(200)
			return
		}
		http.NotFound(w, r)
	})

	mux.HandleFunc("/session/ses_prompt/message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			// echo back
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "body": body})
			return
		}
		http.NotFound(w, r)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := NewOpencodeClient(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}

	// Test GetSessionMessages mixed
	got, err := c.GetSessionMessages("one")
	if err != nil {
		t.Fatalf("GetSessionMessages error: %v", err)
	}
	if strings.TrimSpace(got) != "final result" {
		t.Fatalf("unexpected GetSessionMessages mixed: %q", got)
	}

	// Test thinking-only fallback
	got, err = c.GetSessionMessages("two")
	if err != nil {
		t.Fatalf("GetSessionMessages error: %v", err)
	}
	if strings.TrimSpace(got) != "still thinking" {
		t.Fatalf("unexpected GetSessionMessages thinking-only: %q", got)
	}

	// Test empty
	got, err = c.GetSessionMessages("empty")
	if err != nil {
		t.Fatalf("GetSessionMessages error: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty string for empty response, got: %q", got)
	}

	// Test ListSessions
	sess, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions error: %v", err)
	}
	if len(sess) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sess))
	}

	// Test CreateSession
	created, err := c.CreateSession("mytitle")
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}
	if id, ok := created["id"].(string); !ok || id != "ses_new" {
		t.Fatalf("unexpected create result: %v", created)
	}

	// Test DeleteSession
	if err := c.DeleteSession("ses_del"); err != nil {
		t.Fatalf("DeleteSession error: %v", err)
	}

	// Test PromptSession
	resp, err := c.PromptSession("ses_prompt", "hello")
	if err != nil {
		t.Fatalf("PromptSession error: %v", err)
	}
	if ok, _ := resp["ok"].(bool); !ok {
		t.Fatalf("PromptSession unexpected response: %v", resp)
	}
}

func TestOpencodeClient_CreateSession_InvalidJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.Write([]byte("invalid json"))
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, err := NewOpencodeClient(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}
	_, err = c.CreateSession("test")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestOpencodeClient_PromptSession_InvalidJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/session/ses_test/message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.Write([]byte("invalid json"))
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, err := NewOpencodeClient(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}
	_, err = c.PromptSession("ses_test", "test")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestOpencodeClient_SubscribeEvents_HTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/event", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, err := NewOpencodeClient(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}
	err = c.SubscribeEvents(func(ev map[string]any) {})
	if err == nil {
		t.Error("expected error for HTTP 500")
	}
}

func TestOpencodeClient_SubscribeEvents_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/event", func(w http.ResponseWriter, r *http.Request) {
		// Send SSE data
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		// Send a simple event
		w.Write([]byte("data: {\"type\":\"message.part.updated\",\"data\":{\"sessionID\":\"ses_123\"}}\n\n"))
		// Close after one event
		w.(http.Flusher).Flush()
		// Wait a bit, then close
		time.Sleep(10 * time.Millisecond)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, err := NewOpencodeClient(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}
	events := make(chan map[string]any, 1)
	err = c.SubscribeEvents(func(ev map[string]any) {
		events <- ev
	})
	if err != nil {
		t.Fatalf("SubscribeEvents error: %v", err)
	}
	// Wait for event
	select {
	case ev := <-events:
		if ev["type"] != "message.part.updated" {
			t.Errorf("expected type message.part.updated, got %v", ev["type"])
		}
		data, ok := ev["data"].(map[string]any)
		if !ok {
			t.Errorf("expected data to be map, got %T", ev["data"])
		}
		if data["sessionID"] != "ses_123" {
			t.Errorf("expected sessionID ses_123, got %v", data["sessionID"])
		}
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for event")
	}
}
