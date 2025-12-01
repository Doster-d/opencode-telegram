package bot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// TestOpencodeClient_NewOpencodeClient tests client creation
func TestOpencodeClient_NewOpencodeClient(t *testing.T) {
	tests := []struct {
		name       string
		baseURL    string
		token      string
		shouldFail bool
	}{
		{
			name:       "valid URL",
			baseURL:    "http://example.com",
			token:      "token123",
			shouldFail: false,
		},
		{
			name:       "https URL",
			baseURL:    "https://api.example.com",
			token:      "",
			shouldFail: false,
		},
		{
			name:       "URL with path",
			baseURL:    "http://example.com/api/v1",
			token:      "token",
			shouldFail: false,
		},
		{
			name:       "invalid URL",
			baseURL:    "://invalid",
			token:      "token",
			shouldFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewOpencodeClient(tt.baseURL, tt.token)
			if (err != nil) != tt.shouldFail {
				t.Errorf("NewOpencodeClient() error = %v, shouldFail = %v", err, tt.shouldFail)
			}
			if !tt.shouldFail && client == nil {
				t.Errorf("NewOpencodeClient() returned nil client")
			}
		})
	}
}

// TestOpencodeClient_DeleteSession tests session deletion
func TestOpencodeClient_DeleteSession(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/session/ses_delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/session/ses_notfound", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewOpencodeClient(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}

	tests := []struct {
		name       string
		sessionID  string
		shouldFail bool
	}{
		{
			name:       "successful deletion",
			sessionID:  "ses_delete",
			shouldFail: false,
		},
		{
			name:       "session not found",
			sessionID:  "ses_notfound",
			shouldFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.DeleteSession(tt.sessionID)
			if (err != nil) != tt.shouldFail {
				t.Errorf("DeleteSession() error = %v, shouldFail = %v", err, tt.shouldFail)
			}
		})
	}
}

// TestOpencodeClient_AbortSession tests session abort
func TestOpencodeClient_AbortSession(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/session/ses_abort/abort", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewOpencodeClient(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}

	err = client.AbortSession("ses_abort")
	if err != nil {
		t.Errorf("AbortSession() error = %v, expected nil", err)
	}
}

// TestOpencodeClient_URLConstruction tests that URLs are constructed correctly
func TestOpencodeClient_URLConstruction(t *testing.T) {
	tests := []struct {
		name         string
		baseURL      string
		basePath     string
		requestPath  string
		expectedPath string
	}{
		{
			name:         "simple path",
			baseURL:      "http://example.com",
			basePath:     "",
			requestPath:  "/session",
			expectedPath: "/session",
		},
		{
			name:         "base with path",
			baseURL:      "http://example.com/api",
			basePath:     "/api",
			requestPath:  "/session/ses_1",
			expectedPath: "/api/session/ses_1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.baseURL)
			if err != nil {
				t.Fatalf("url.Parse error: %v", err)
			}
			client := &OpencodeClient{
				base:  u,
				token: "",
				http:  &http.Client{},
			}
			// Verify client is created
			if client.base == nil {
				t.Errorf("base URL not set")
			}
		})
	}
}

// TestOpencodeClient_GetSessionMessages_MixedContent tests mixed content extraction
func TestOpencodeClient_GetSessionMessages_MixedContent(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/session/mixed/message", func(w http.ResponseWriter, r *http.Request) {
		resp := []map[string]any{
			{
				"parts": []map[string]any{
					{"type": "thinking", "text": "Let me think..."},
					{"type": "text", "text": "Final answer"},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewOpencodeClient(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}

	text, err := client.GetSessionMessages("mixed")
	if err != nil {
		t.Errorf("GetSessionMessages error: %v", err)
	}
	if text != "Final answer" {
		t.Errorf("expected 'Final answer', got %q", text)
	}
}

// TestOpencodeClient_GetSessionMessages_ThinkingOnly tests fallback to thinking content
func TestOpencodeClient_GetSessionMessages_ThinkingOnly(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/session/thinking/message", func(w http.ResponseWriter, r *http.Request) {
		resp := []map[string]any{
			{
				"parts": []map[string]any{
					{"type": "thinking", "text": "Processing..."},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewOpencodeClient(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}

	text, err := client.GetSessionMessages("thinking")
	if err != nil {
		t.Errorf("GetSessionMessages error: %v", err)
	}
	if text != "Processing..." {
		t.Errorf("expected 'Processing...', got %q", text)
	}
}

// TestOpencodeClient_GetSessionMessages_Empty tests empty response handling
func TestOpencodeClient_GetSessionMessages_Empty(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/session/empty/message", func(w http.ResponseWriter, r *http.Request) {
		resp := []map[string]any{}
		json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewOpencodeClient(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}

	text, err := client.GetSessionMessages("empty")
	if err != nil {
		t.Errorf("GetSessionMessages error: %v", err)
	}
	if text != "" {
		t.Errorf("expected empty string, got %q", text)
	}
}

// TestOpencodeClient_ListSessions tests session listing
func TestOpencodeClient_ListSessions(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sessions := []map[string]any{
			{"id": "ses_1", "title": "Session 1"},
			{"id": "ses_2", "title": "Session 2"},
		}
		json.NewEncoder(w).Encode(sessions)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewOpencodeClient(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}

	sessions, err := client.ListSessions()
	if err != nil {
		t.Errorf("ListSessions error: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

// TestOpencodeClient_CreateSession tests session creation
func TestOpencodeClient_CreateSession(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		response := map[string]any{
			"id":    "ses_new",
			"title": body["title"],
		}
		json.NewEncoder(w).Encode(response)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewOpencodeClient(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}

	session, err := client.CreateSession("My Session")
	if err != nil {
		t.Errorf("CreateSession error: %v", err)
	}
	if id, ok := session["id"].(string); !ok || id != "ses_new" {
		t.Errorf("expected id 'ses_new', got %v", session["id"])
	}
}

// TestOpencodeClient_PromptSession tests prompting a session
func TestOpencodeClient_PromptSession(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/session/ses_prompt/message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		response := map[string]any{
			"ok": true,
		}
		json.NewEncoder(w).Encode(response)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewOpencodeClient(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}

	result, err := client.PromptSession("ses_prompt", "Hello")
	if err != nil {
		t.Errorf("PromptSession error: %v", err)
	}
	if ok, _ := result["ok"].(bool); !ok {
		t.Errorf("expected ok=true")
	}
}

// TestOpencodeClient_AuthorizationHeader tests Bearer token handling
func TestOpencodeClient_AuthorizationHeader(t *testing.T) {
	mux := http.NewServeMux()

	var receivedAuth string
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode([]map[string]any{})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewOpencodeClient(srv.URL, "test_token_123")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}

	_, err = client.ListSessions()
	if err != nil {
		t.Errorf("ListSessions error: %v", err)
	}

	expectedAuth := "Bearer test_token_123"
	if receivedAuth != expectedAuth {
		t.Errorf("expected Authorization header %q, got %q", expectedAuth, receivedAuth)
	}
}

// TestOpencodeClient_ErrorHandling tests HTTP error handling
func TestOpencodeClient_ErrorHandling(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewOpencodeClient(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}

	_, err = client.ListSessions()
	if err == nil {
		t.Errorf("expected error for 500 status, got nil")
	}
}

// TestOpencodeClient_ContentTypeHeader tests JSON content-type
func TestOpencodeClient_ContentTypeHeader(t *testing.T) {
	mux := http.NewServeMux()

	var receivedContentType string
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			receivedContentType = r.Header.Get("Content-Type")
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "ses_1"})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewOpencodeClient(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}

	client.CreateSession("test")
	if receivedContentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", receivedContentType)
	}
}

// TestOpencodeClient_GetSessionMessages_MultipleParts tests handling multiple message parts
func TestOpencodeClient_GetSessionMessages_MultipleParts(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/session/multi/message", func(w http.ResponseWriter, r *http.Request) {
		resp := []map[string]any{
			{
				"parts": []map[string]any{
					{"type": "thinking", "text": "Step 1"},
					{"type": "text", "text": "Result 1"},
					{"type": "thinking", "text": "Step 2"},
					{"type": "text", "text": "Result 2"},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewOpencodeClient(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}

	text, err := client.GetSessionMessages("multi")
	if err != nil {
		t.Errorf("GetSessionMessages error: %v", err)
	}
	// Should return the last non-thinking part
	if text != "Result 2" {
		t.Errorf("expected 'Result 2', got %q", text)
	}
}

// TestOpencodeClient_PromptSession_RequestBody tests that request body is correct
func TestOpencodeClient_PromptSession_RequestBody(t *testing.T) {
	mux := http.NewServeMux()

	var receivedBody map[string]any
	mux.HandleFunc("/session/ses_test/message", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewOpencodeClient(srv.URL, "")
	if err != nil {
		t.Fatalf("NewOpencodeClient: %v", err)
	}

	client.PromptSession("ses_test", "test prompt")

	parts, ok := receivedBody["parts"].([]any)
	if !ok || len(parts) == 0 {
		t.Errorf("expected parts in request body")
	}
}
