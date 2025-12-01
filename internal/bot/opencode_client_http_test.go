package bot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpencodeClient_HTTPHeadersAndSessionActions(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			// check auth header and content-type
			if got := r.Header.Get("Authorization"); got != "Bearer mytoken" {
				t.Fatalf("expected Authorization header, got %q", got)
			}
			if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Fatalf("expected application/json content-type, got %q", ct)
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			title := body["title"]
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "created", "title": title})
			return
		}
		if r.Method == "GET" {
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "created", "title": "created"}})
			return
		}
		http.NotFound(w, r)
	})

	mux.HandleFunc("/session/abortme/abort", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(200)
	})

	mux.HandleFunc("/session/delme", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			w.WriteHeader(200)
			return
		}
		http.NotFound(w, r)
	})

	mux.HandleFunc("/session/someid/message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Fatalf("expected application/json content-type for prompt, got %q", ct)
		}
		// read body to ensure parts present
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "parts") {
			t.Fatalf("expected parts in body, got %s", string(b))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := NewOpencodeClient(srv.URL, "mytoken")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	// CreateSession should include Authorization and JSON content-type
	out, err := c.CreateSession("t")
	if err != nil {
		t.Fatalf("CreateSession err: %v", err)
	}
	if id, _ := out["id"].(string); id != "created" {
		t.Fatalf("unexpected create id: %v", out)
	}

	// PromptSession should post with JSON
	_, err = c.PromptSession("someid", "hello")
	if err != nil {
		t.Fatalf("PromptSession err: %v", err)
	}

	// AbortSession
	if err := c.AbortSession("abortme"); err != nil {
		t.Fatalf("AbortSession err: %v", err)
	}

	// DeleteSession
	if err := c.DeleteSession("delme"); err != nil {
		t.Fatalf("DeleteSession err: %v", err)
	}

	// ListSessions
	sess, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions err: %v", err)
	}
	if len(sess) != 1 || sess[0]["id"] != "created" {
		t.Fatalf("unexpected sessions: %v", sess)
	}
}
