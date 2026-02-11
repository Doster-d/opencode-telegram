package backend

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

type Server struct {
	backend  PairingStore
	queue    CommandQueue
	mux      *http.ServeMux
	notifier ResultNotifier
}

type ResultNotifier interface {
	NotifyResult(telegramUserID string, result contracts.CommandResult)
}

type noopNotifier struct{}

func (n noopNotifier) NotifyResult(string, contracts.CommandResult) {}

func NewServer(backend PairingStore, queue CommandQueue) *Server {
	mux := http.NewServeMux()
	s := &Server{backend: backend, queue: queue, mux: mux, notifier: noopNotifier{}}
	mux.HandleFunc("/v1/pair/start", s.handlePairStart)
	mux.HandleFunc("/v1/pair/claim", s.handlePairClaim)
	mux.HandleFunc("/v1/command", s.handleCommand)
	mux.HandleFunc("/v1/poll", s.handlePoll)
	mux.HandleFunc("/v1/result", s.handleResult)
	mux.HandleFunc("/v1/projects", s.handleProjects)
	mux.HandleFunc("/v1/result/status", s.handleResultStatus)
	return s
}

func (s *Server) SetNotifier(notifier ResultNotifier) {
	if notifier == nil {
		s.notifier = noopNotifier{}
		return
	}
	s.notifier = notifier
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handlePairStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, contracts.APIError{Code: contracts.ErrValidationInvalidRequest, Message: "method not allowed"})
		return
	}
	req, ok := decodeJSONBody[contracts.PairStartRequest](w, r)
	if !ok {
		return
	}
	resp, err := s.backend.StartPairing(req.TelegramUserID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handlePairClaim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, contracts.APIError{Code: contracts.ErrValidationInvalidRequest, Message: "method not allowed"})
		return
	}
	req, ok := decodeJSONBody[contracts.PairClaimRequest](w, r)
	if !ok {
		return
	}
	resp, err := s.backend.ClaimPairing(req)
	if err != nil {
		writeServerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, contracts.APIError{Code: contracts.ErrValidationInvalidRequest, Message: "method not allowed"})
		return
	}
	agentID, ok := s.authAgent(w, r)
	if !ok {
		return
	}

	var cmd contracts.Command
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, contracts.APIError{Code: contracts.ErrValidationInvalidRequest, Message: err.Error()})
		return
	}
	if err := json.Unmarshal(body, &cmd); err != nil {
		writeError(w, http.StatusBadRequest, contracts.APIError{Code: contracts.ErrValidationInvalidRequest, Message: err.Error()})
		return
	}

	if err := contracts.ValidateCommand(cmd); err != nil {
		writeServerError(w, err)
		return
	}
	if backend, ok := s.backend.(*MemoryBackend); ok {
		if userID, ok := backend.UserIDForAgent(agentID); ok {
			meta := commandMeta{TelegramUserID: userID, CommandType: cmd.Type}
			if cmd.Type == contracts.CommandTypeRegisterProject {
				var payload contracts.RegisterProjectPayload
				_ = contracts.DecodeStrictJSON(cmd.Payload, &payload)
				meta.ProjectPath = payload.ProjectPathRaw
				meta.Alias = strings.TrimSpace(projectAliasFromPath(payload.ProjectPathRaw))
				if meta.Alias == "" {
					meta.Alias = fmt.Sprintf("project-%d", time.Now().Unix())
				}
			}
			if cmd.Type == contracts.CommandTypeStartServer || cmd.Type == contracts.CommandTypeRunTask || cmd.Type == contracts.CommandTypeApplyProjectPolicy {
				var payload struct {
					ProjectID string `json:"project_id"`
				}
				_ = contracts.DecodeStrictJSON(cmd.Payload, &payload)
				meta.ProjectID = payload.ProjectID
			}
			backend.RegisterCommandMeta(cmd.CommandID, meta)
		}
	}

	if err := s.queue.Enqueue(r.Context(), agentID, cmd); err != nil {
		writeServerError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (s *Server) handlePoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, contracts.APIError{Code: contracts.ErrValidationInvalidRequest, Message: "method not allowed"})
		return
	}
	agentID, ok := s.authAgent(w, r)
	if !ok {
		return
	}
	timeoutSeconds := 25
	if raw := r.URL.Query().Get("timeout_seconds"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 60 {
			writeError(w, http.StatusBadRequest, contracts.APIError{Code: contracts.ErrValidationInvalidRequest, Message: "timeout_seconds must be integer in range 1..60"})
			return
		}
		timeoutSeconds = v
	}
	cmd, err := s.queue.Poll(r.Context(), agentID, timeoutSeconds)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if cmd == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, contracts.PollResponse{Command: cmd})
}

func (s *Server) handleResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, contracts.APIError{Code: contracts.ErrValidationInvalidRequest, Message: "method not allowed"})
		return
	}
	agentID, ok := s.authAgent(w, r)
	if !ok {
		return
	}
	result, ok := decodeJSONBody[contracts.CommandResult](w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(result.CommandID) == "" {
		writeError(w, http.StatusBadRequest, contracts.APIError{Code: contracts.ErrValidationRequiredField, Message: "command_id is required"})
		return
	}
	if err := s.queue.StoreResult(r.Context(), agentID, result); err != nil {
		writeServerError(w, err)
		return
	}
	if backend, ok := s.backend.(*MemoryBackend); ok {
		if userID, ok := backend.UserIDForAgent(agentID); ok {
			s.notifier.NotifyResult(userID, result)
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, contracts.APIError{Code: contracts.ErrValidationInvalidRequest, Message: "method not allowed"})
		return
	}
	backend, ok := s.backend.(*MemoryBackend)
	if !ok {
		writeError(w, http.StatusBadRequest, contracts.APIError{Code: contracts.ErrValidationInvalidRequest, Message: "projects not supported"})
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("telegram_user_id"))
	if userID == "" {
		writeError(w, http.StatusBadRequest, contracts.APIError{Code: contracts.ErrValidationRequiredField, Message: "telegram_user_id is required"})
		return
	}
	projects := backend.ListProjects(userID)
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

func (s *Server) handleResultStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, contracts.APIError{Code: contracts.ErrValidationInvalidRequest, Message: "method not allowed"})
		return
	}
	backend, ok := s.backend.(*MemoryBackend)
	if !ok {
		writeError(w, http.StatusBadRequest, contracts.APIError{Code: contracts.ErrValidationInvalidRequest, Message: "result status not supported"})
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("telegram_user_id"))
	if userID == "" {
		writeError(w, http.StatusBadRequest, contracts.APIError{Code: contracts.ErrValidationRequiredField, Message: "telegram_user_id is required"})
		return
	}
	commandID := strings.TrimSpace(r.URL.Query().Get("command_id"))
	if commandID == "" {
		writeError(w, http.StatusBadRequest, contracts.APIError{Code: contracts.ErrValidationRequiredField, Message: "command_id is required"})
		return
	}
	agentID, ok := backend.AgentIDForUser(userID)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	result, err := s.queue.GetResult(r.Context(), agentID, commandID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if result == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if meta, ok := backend.commands[commandID]; ok && meta.CommandType == contracts.CommandTypeApplyProjectPolicy {
		backend.UpdateProjectPolicy(meta.TelegramUserID, meta.ProjectID, projectPolicy{
			Decision:  stringFromMeta(result.Meta["decision"], contracts.DecisionAllow),
			Scope:     scopeFromMeta(result.Meta["scope"]),
			ExpiresAt: expiresAtFromMeta(result.Meta["expires_at"]),
		})
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) authAgent(w http.ResponseWriter, r *http.Request) (string, bool) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(header, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		agentID, ok := s.backend.AuthenticateAgentKey(token)
		if !ok {
			writeError(w, http.StatusUnauthorized, contracts.APIError{Code: contracts.ErrAuthUnauthorized, Message: "invalid bearer token"})
			return "", false
		}
		return agentID, true
	}
	if userID := strings.TrimSpace(r.Header.Get("X-Telegram-User-ID")); userID != "" {
		agentID, ok := s.backend.AgentIDForUser(userID)
		if !ok {
			writeError(w, http.StatusUnauthorized, contracts.APIError{Code: contracts.ErrAuthUnauthorized, Message: "agent not paired"})
			return "", false
		}
		return agentID, true
	}
	writeError(w, http.StatusUnauthorized, contracts.APIError{Code: contracts.ErrAuthUnauthorized, Message: "missing bearer token"})
	return "", false
}

func decodeJSONBody[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var zero T
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, contracts.APIError{Code: contracts.ErrValidationInvalidRequest, Message: err.Error()})
		return zero, false
	}
	parsed, err := contracts.DecodeRequestStrict[T](body)
	if err != nil {
		apiErr, ok := err.(contracts.APIError)
		if !ok {
			apiErr = contracts.APIError{Code: contracts.ErrInternal, Message: err.Error()}
		}
		writeError(w, http.StatusBadRequest, apiErr)
		return zero, false
	}
	return parsed, true
}

func projectAliasFromPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "/")
	return strings.TrimSpace(parts[len(parts)-1])
}

func stringFromMeta(val any, fallback string) string {
	if s, ok := val.(string); ok && s != "" {
		return s
	}
	return fallback
}

func scopeFromMeta(val any) []string {
	if raw, ok := val.([]string); ok {
		return raw
	}
	if raw, ok := val.([]any); ok {
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func expiresAtFromMeta(val any) *time.Time {
	if s, ok := val.(string); ok && s != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return &parsed
		}
	}
	return nil
}

func writeServerError(w http.ResponseWriter, err error) {
	apiErr, ok := err.(contracts.APIError)
	if ok {
		status := http.StatusBadRequest
		if apiErr.Code == contracts.ErrPairingExpired || apiErr.Code == contracts.ErrPairingInvalidCode {
			status = http.StatusNotFound
		}
		writeError(w, status, apiErr)
		return
	}
	writeError(w, http.StatusInternalServerError, contracts.APIError{Code: contracts.ErrInternal, Message: err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, apiErr contracts.APIError) {
	writeJSON(w, status, map[string]any{"ok": false, "error": apiErr})
}
