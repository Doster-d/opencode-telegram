package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"opencode-telegram/internal/agent"
	"opencode-telegram/internal/proxy/contracts"
)

func main() {
	backendURL := os.Getenv("OCT_BACKEND_URL")
	agentKey := os.Getenv("OCT_AGENT_KEY")
	agentID := os.Getenv("OCT_AGENT_ID")
	if agentKey == "" {
		log.Fatal("OCT_AGENT_KEY is required")
	}
	if backendURL == "" {
		backendURL = "http://localhost:8080"
	}

	daemon := agent.NewDaemon()
	if agentID != "" {
		daemon.SetAgentID(agentID)
	}

	// HTTP server for readiness check
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// Ready if we can reach the backend
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(backendURL + "/healthz")
		if err != nil {
			log.Printf("backend health check failed: %v", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Ready"))
	})

	srv := &http.Server{
		Addr:    os.Getenv("OCT_AGENT_ADDR"),
		Handler: mux,
	}
	if srv.Addr == "" {
		srv.Addr = ":9090"
	}

	// Start HTTP server
	go func() {
		log.Printf("oct-agent HTTP server listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Create poll client
	pollClient := &BackendPollClient{
		backendURL: backendURL,
		agentKey:   agentKey,
		client:     &http.Client{Timeout: 60 * time.Second},
	}

	// Start poll loop in a goroutine
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		log.Println("starting poll loop")
		daemon.RunPollLoop(ctx, pollClient, 25)
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("shutting down...")

	// Graceful shutdown
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}
	log.Println("oct-agent stopped")
}

// BackendPollClient implements agent.PollClient
type BackendPollClient struct {
	backendURL string
	agentKey   string
	client     *http.Client
}

func (c *BackendPollClient) PollCommand(ctx context.Context, timeoutSeconds int) (*contracts.Command, error) {
	// Build request URL with timeout
	url := c.backendURL + "/v1/poll?timeout_seconds=" + strconv.Itoa(timeoutSeconds)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.agentKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &httpError{StatusCode: resp.StatusCode}
	}

	var pollResp struct {
		Command *contracts.Command `json:"command"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pollResp); err != nil {
		return nil, err
	}
	return pollResp.Command, nil
}

func (c *BackendPollClient) PostResult(ctx context.Context, result contracts.CommandResult) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.backendURL+"/v1/result", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.agentKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &httpError{StatusCode: resp.StatusCode}
	}
	return nil
}

type httpError struct {
	StatusCode int
}

func (e *httpError) Error() string {
	return http.StatusText(e.StatusCode)
}
