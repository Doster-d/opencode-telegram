package bot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpencodeClient_EdgeCases(t *testing.T) {
	t.Run("malformed JSON for ListSessions", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("not-json"))
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		c, err := NewOpencodeClient(srv.URL, "")
		if err != nil {
			t.Fatalf("new client: %v", err)
		}

		_, err = c.ListSessions()
		if err == nil || !strings.Contains(err.Error(), "invalid character") {
			t.Fatalf("expected json unmarshal error, got: %v", err)
		}
	})

	t.Run("server 500 returns error", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
			w.Write([]byte("boom"))
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		c, err := NewOpencodeClient(srv.URL, "")
		if err != nil {
			t.Fatalf("new client: %v", err)
		}

		_, err = c.ListSessions()
		if err == nil || !strings.Contains(err.Error(), "500") {
			t.Fatalf("expected 500 error, got: %v", err)
		}
	})

	t.Run("GetSessionMessages missing parts and non-string text types", func(t *testing.T) {
		mux := http.NewServeMux()
		// missing parts
		mux.HandleFunc("/session/missing/message", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode([]map[string]any{{"info": map[string]any{"id": "x"}}})
		})

		// parts with non-string and final
		mux.HandleFunc("/session/mixed/message", func(w http.ResponseWriter, r *http.Request) {
			resp := []map[string]any{{"parts": []map[string]any{{"type": "text", "text": 123}, {"type": "text", "text": "final"}}}}
			_ = json.NewEncoder(w).Encode(resp)
		})

		srv := httptest.NewServer(mux)
		defer srv.Close()

		c, err := NewOpencodeClient(srv.URL, "")
		if err != nil {
			t.Fatalf("new client: %v", err)
		}

		got, err := c.GetSessionMessages("missing")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Fatalf("expected empty string for missing parts, got %q", got)
		}

		got, err = c.GetSessionMessages("mixed")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "final" {
			t.Fatalf("expected final, got %q", got)
		}
	})

	t.Run("client timeout on slow response", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/session/slow/message", func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			_ = json.NewEncoder(w).Encode([]map[string]any{{"parts": []map[string]any{{"type": "text", "text": "ok"}}}})
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		c, err := NewOpencodeClient(srv.URL, "")
		if err != nil {
			t.Fatalf("new client: %v", err)
		}
		// set a short timeout
		c.http.Timeout = 50 * time.Millisecond

		_, err = c.GetSessionMessages("slow")
		if err == nil {
			t.Fatalf("expected timeout error, got nil")
		}
	})
}
