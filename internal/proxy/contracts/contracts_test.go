package contracts

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestACMVP01StrictUnknownFieldFails(t *testing.T) {
	body := []byte(`{"pairing_code":"abc","device_info":"x","extra":"boom"}`)
	_, err := DecodeRequestStrict[PairClaimRequest](body)
	if err == nil {
		t.Fatal("expected strict decode error")
	}
	apiErr, ok := err.(APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != ErrValidationInvalidRequest {
		t.Fatalf("expected %s got %s", ErrValidationInvalidRequest, apiErr.Code)
	}
}

func TestACMVP01UnknownCommandTypeRejected(t *testing.T) {
	cmd := Command{
		CommandID:      "c1",
		IdempotencyKey: "i1",
		Type:           "not_allowed",
		CreatedAt:      time.Now().UTC(),
		Payload:        json.RawMessage(`{}`),
	}
	err := ValidateCommand(cmd)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != ErrValidationInvalidType {
		t.Fatalf("expected %s got %s", ErrValidationInvalidType, apiErr.Code)
	}
}

func TestACMVP01RunTaskPayloadValidation(t *testing.T) {
	cmd := Command{
		CommandID:      "c2",
		IdempotencyKey: "i2",
		Type:           CommandTypeRunTask,
		CreatedAt:      time.Now().UTC(),
		Payload:        json.RawMessage(`{"project_id":"p1"}`),
	}
	err := ValidateCommand(cmd)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr := err.(APIError)
	if apiErr.Code != ErrValidationRequiredField {
		t.Fatalf("expected %s got %s", ErrValidationRequiredField, apiErr.Code)
	}
}

func TestAPIErrorFormatting(t *testing.T) {
	if got := (APIError{Code: ErrInternal}).Error(); got != ErrInternal {
		t.Fatalf("expected bare code, got %q", got)
	}
	if got := (APIError{Code: ErrInternal, Message: "boom"}).Error(); got != ErrInternal+": boom" {
		t.Fatalf("expected code+message, got %q", got)
	}
}

func TestDecodeStrictJSON(t *testing.T) {
	t.Run("rejects multiple values", func(t *testing.T) {
		var out PairStartRequest
		err := DecodeStrictJSON([]byte(`{"telegram_user_id":"1"} {}`), &out)
		if err == nil {
			t.Fatal("expected multiple values error")
		}
		if !strings.Contains(err.Error(), "multiple JSON values") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("strict unknown fields", func(t *testing.T) {
		var out PairStartRequest
		err := DecodeStrictJSON([]byte(`{"telegram_user_id":"1","x":1}`), &out)
		if err == nil {
			t.Fatal("expected unknown field error")
		}
	})
}

func TestValidateCommand_AllPayloadTypes(t *testing.T) {
	now := time.Now().UTC()
	validCases := []Command{
		{CommandID: "1", IdempotencyKey: "k1", Type: CommandTypeRegisterProject, CreatedAt: now, Payload: json.RawMessage(`{"project_path_raw":"/tmp/p"}`)},
		{CommandID: "2", IdempotencyKey: "k2", Type: CommandTypeApplyProjectPolicy, CreatedAt: now, Payload: json.RawMessage(`{"project_id":"p1","decision":"ALLOW","scope":["START_SERVER","RUN_TASK"]}`)},
		{CommandID: "3", IdempotencyKey: "k3", Type: CommandTypeStartServer, CreatedAt: now, Payload: json.RawMessage(`{"project_id":"p1"}`)},
		{CommandID: "4", IdempotencyKey: "k4", Type: CommandTypeRunTask, CreatedAt: now, Payload: json.RawMessage(`{"project_id":"p1","prompt":"hello"}`)},
		{CommandID: "5", IdempotencyKey: "k5", Type: CommandTypeStatus, CreatedAt: now, Payload: json.RawMessage(`{}`)},
	}
	for _, tc := range validCases {
		if err := ValidateCommand(tc); err != nil {
			t.Fatalf("expected valid command for type %s: %v", tc.Type, err)
		}
	}
}

func TestValidateCommand_ErrorBranches(t *testing.T) {
	now := time.Now().UTC()

	t.Run("missing envelope fields", func(t *testing.T) {
		err := ValidateCommand(Command{IdempotencyKey: "k", Type: CommandTypeStatus, CreatedAt: now, Payload: json.RawMessage(`{}`)})
		if err == nil {
			t.Fatal("expected missing command_id")
		}
		err = ValidateCommand(Command{CommandID: "c", Type: CommandTypeStatus, CreatedAt: now, Payload: json.RawMessage(`{}`)})
		if err == nil {
			t.Fatal("expected missing idempotency_key")
		}
		err = ValidateCommand(Command{CommandID: "c", IdempotencyKey: "k", Type: CommandTypeStatus, Payload: json.RawMessage(`{}`)})
		if err == nil {
			t.Fatal("expected missing created_at")
		}
	})

	t.Run("apply policy validation", func(t *testing.T) {
		err := ValidateCommand(Command{CommandID: "c", IdempotencyKey: "k", Type: CommandTypeApplyProjectPolicy, CreatedAt: now, Payload: json.RawMessage(`{"project_id":"p1","decision":"MAYBE","scope":[]}`)})
		if err == nil {
			t.Fatal("expected invalid decision")
		}
		err = ValidateCommand(Command{CommandID: "c", IdempotencyKey: "k", Type: CommandTypeApplyProjectPolicy, CreatedAt: now, Payload: json.RawMessage(`{"project_id":"p1","decision":"ALLOW","scope":["X"]}`)})
		if err == nil {
			t.Fatal("expected invalid scope")
		}
	})

	t.Run("status payload required", func(t *testing.T) {
		err := ValidateCommand(Command{CommandID: "c", IdempotencyKey: "k", Type: CommandTypeStatus, CreatedAt: now, Payload: nil})
		if err == nil {
			t.Fatal("expected payload required")
		}
	})

	t.Run("type specific missing fields", func(t *testing.T) {
		cases := []Command{
			{CommandID: "c1", IdempotencyKey: "k", Type: CommandTypeRegisterProject, CreatedAt: now, Payload: json.RawMessage(`{"project_path_raw":""}`)},
			{CommandID: "c2", IdempotencyKey: "k", Type: CommandTypeApplyProjectPolicy, CreatedAt: now, Payload: json.RawMessage(`{"decision":"ALLOW","scope":[]}`)},
			{CommandID: "c3", IdempotencyKey: "k", Type: CommandTypeStartServer, CreatedAt: now, Payload: json.RawMessage(`{"project_id":""}`)},
			{CommandID: "c4", IdempotencyKey: "k", Type: CommandTypeRunTask, CreatedAt: now, Payload: json.RawMessage(`{"project_id":"p1","prompt":""}`)},
		}
		for _, tc := range cases {
			if err := ValidateCommand(tc); err == nil {
				t.Fatalf("expected validation error for %s", tc.Type)
			}
		}
	})
}
