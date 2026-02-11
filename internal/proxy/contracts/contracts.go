package contracts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	CommandTypeRegisterProject    = "register_project"
	CommandTypeApplyProjectPolicy = "apply_project_policy"
	CommandTypeStartServer        = "start_server"
	CommandTypeRunTask            = "run_task"
	CommandTypeStatus             = "status"
)

const (
	DecisionAllow = "ALLOW"
	DecisionDeny  = "DENY"
)

const (
	ScopeStartServer = "START_SERVER"
	ScopeRunTask     = "RUN_TASK"
)

const (
	ErrValidationInvalidRequest = "ERR_VALIDATION_INVALID_REQUEST"
	ErrValidationInvalidType    = "ERR_VALIDATION_INVALID_TYPE"
	ErrValidationInvalidPayload = "ERR_VALIDATION_INVALID_PAYLOAD"
	ErrValidationRequiredField  = "ERR_VALIDATION_REQUIRED_FIELD"
	ErrAuthUnauthorized         = "ERR_AUTH_UNAUTHORIZED"
	ErrPairingExpired           = "ERR_PAIRING_EXPIRED"
	ErrPairingInvalidCode       = "ERR_PAIRING_INVALID_CODE"
	ErrPairingReused            = "ERR_PAIRING_REUSED"
	ErrPolicyDenied             = "ERR_POLICY_DENIED"
	ErrPathForbidden            = "ERR_PATH_FORBIDDEN"
	ErrPathInvalid              = "ERR_PATH_INVALID"
	ErrPortExhausted            = "ERR_PORT_EXHAUSTED"
	ErrStartTimeout             = "ERR_START_TIMEOUT"
	ErrInternal                 = "ERR_INTERNAL"
)

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e APIError) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

type Command struct {
	CommandID      string          `json:"command_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	Type           string          `json:"type"`
	CreatedAt      time.Time       `json:"created_at"`
	Payload        json.RawMessage `json:"payload"`
}

type CommandResult struct {
	CommandID string         `json:"command_id"`
	OK        bool           `json:"ok"`
	ErrorCode string         `json:"error_code,omitempty"`
	Summary   string         `json:"summary,omitempty"`
	Stdout    string         `json:"stdout,omitempty"`
	Stderr    string         `json:"stderr,omitempty"`
	Meta      map[string]any `json:"meta,omitempty"`
}

type PairStartRequest struct {
	TelegramUserID string `json:"telegram_user_id"`
}

type PairStartResponse struct {
	PairingCode string    `json:"pairing_code"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type PairClaimRequest struct {
	PairingCode string `json:"pairing_code"`
	DeviceInfo  string `json:"device_info"`
}

type PairClaimResponse struct {
	AgentID  string `json:"agent_id"`
	AgentKey string `json:"agent_key"`
}

type PollResponse struct {
	Command *Command `json:"command"`
}

type RegisterProjectPayload struct {
	ProjectPathRaw string `json:"project_path_raw"`
}

type ApplyProjectPolicyPayload struct {
	ProjectID string     `json:"project_id"`
	Decision  string     `json:"decision"`
	ExpiresAt *time.Time `json:"expires_at"`
	Scope     []string   `json:"scope"`
}

type StartServerPayload struct {
	ProjectID string `json:"project_id"`
}

type RunTaskPayload struct {
	ProjectID string `json:"project_id"`
	Prompt    string `json:"prompt"`
}

type StatusPayload struct{}

func DecodeStrictJSON(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("multiple JSON values are not allowed")
	}
	return nil
}

func DecodeRequestStrict[T any](data []byte) (T, error) {
	var out T
	if err := DecodeStrictJSON(data, &out); err != nil {
		return out, APIError{Code: ErrValidationInvalidRequest, Message: err.Error()}
	}
	return out, nil
}

func ValidateCommand(cmd Command) error {
	if strings.TrimSpace(cmd.CommandID) == "" {
		return APIError{Code: ErrValidationRequiredField, Message: "command_id is required"}
	}
	if strings.TrimSpace(cmd.IdempotencyKey) == "" {
		return APIError{Code: ErrValidationRequiredField, Message: "idempotency_key is required"}
	}
	if cmd.CreatedAt.IsZero() {
		return APIError{Code: ErrValidationRequiredField, Message: "created_at is required"}
	}
	if err := validatePayload(cmd.Type, cmd.Payload); err != nil {
		return err
	}
	return nil
}

func validatePayload(commandType string, payload json.RawMessage) error {
	switch commandType {
	case CommandTypeRegisterProject:
		var p RegisterProjectPayload
		if err := DecodeStrictJSON(payload, &p); err != nil {
			return APIError{Code: ErrValidationInvalidPayload, Message: err.Error()}
		}
		if strings.TrimSpace(p.ProjectPathRaw) == "" {
			return APIError{Code: ErrValidationRequiredField, Message: "project_path_raw is required"}
		}
		return nil
	case CommandTypeApplyProjectPolicy:
		var p ApplyProjectPolicyPayload
		if err := DecodeStrictJSON(payload, &p); err != nil {
			return APIError{Code: ErrValidationInvalidPayload, Message: err.Error()}
		}
		if strings.TrimSpace(p.ProjectID) == "" {
			return APIError{Code: ErrValidationRequiredField, Message: "project_id is required"}
		}
		if p.Decision != DecisionAllow && p.Decision != DecisionDeny {
			return APIError{Code: ErrValidationInvalidPayload, Message: "decision must be ALLOW or DENY"}
		}
		for _, s := range p.Scope {
			if s != ScopeStartServer && s != ScopeRunTask {
				return APIError{Code: ErrValidationInvalidPayload, Message: fmt.Sprintf("invalid scope: %s", s)}
			}
		}
		return nil
	case CommandTypeStartServer:
		var p StartServerPayload
		if err := DecodeStrictJSON(payload, &p); err != nil {
			return APIError{Code: ErrValidationInvalidPayload, Message: err.Error()}
		}
		if strings.TrimSpace(p.ProjectID) == "" {
			return APIError{Code: ErrValidationRequiredField, Message: "project_id is required"}
		}
		return nil
	case CommandTypeRunTask:
		var p RunTaskPayload
		if err := DecodeStrictJSON(payload, &p); err != nil {
			return APIError{Code: ErrValidationInvalidPayload, Message: err.Error()}
		}
		if strings.TrimSpace(p.ProjectID) == "" {
			return APIError{Code: ErrValidationRequiredField, Message: "project_id is required"}
		}
		if strings.TrimSpace(p.Prompt) == "" {
			return APIError{Code: ErrValidationRequiredField, Message: "prompt is required"}
		}
		return nil
	case CommandTypeStatus:
		var p StatusPayload
		if len(payload) == 0 {
			return APIError{Code: ErrValidationInvalidPayload, Message: "payload is required"}
		}
		if err := DecodeStrictJSON(payload, &p); err != nil {
			return APIError{Code: ErrValidationInvalidPayload, Message: err.Error()}
		}
		return nil
	default:
		return APIError{Code: ErrValidationInvalidType, Message: "unsupported command type"}
	}
}
