package contracts

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDecodeRequestStrictSuccess(t *testing.T) {
	out, err := DecodeRequestStrict[PairStartRequest]([]byte(`{"telegram_user_id":"u1"}`))
	if err != nil {
		t.Fatalf("expected successful decode, got %v", err)
	}
	if out.TelegramUserID != "u1" {
		t.Fatalf("expected telegram_user_id u1, got %q", out.TelegramUserID)
	}
}

func TestValidateCommandInvalidJSONPayloadBranches(t *testing.T) {
	now := time.Now().UTC()
	cases := []Command{
		{CommandID: "c1", IdempotencyKey: "k1", Type: CommandTypeRegisterProject, CreatedAt: now, Payload: json.RawMessage(`{bad`)},
		{CommandID: "c2", IdempotencyKey: "k2", Type: CommandTypeApplyProjectPolicy, CreatedAt: now, Payload: json.RawMessage(`{bad`)},
		{CommandID: "c3", IdempotencyKey: "k3", Type: CommandTypeStartServer, CreatedAt: now, Payload: json.RawMessage(`{bad`)},
		{CommandID: "c4", IdempotencyKey: "k4", Type: CommandTypeRunTask, CreatedAt: now, Payload: json.RawMessage(`{bad`)},
		{CommandID: "c5", IdempotencyKey: "k5", Type: CommandTypeStatus, CreatedAt: now, Payload: json.RawMessage(`{bad`)},
	}
	for _, tc := range cases {
		err := ValidateCommand(tc)
		if err == nil {
			t.Fatalf("expected invalid payload error for %s", tc.Type)
		}
		apiErr, ok := err.(APIError)
		if !ok {
			t.Fatalf("expected APIError for %s, got %T", tc.Type, err)
		}
		if apiErr.Code != ErrValidationInvalidPayload {
			t.Fatalf("expected invalid payload code for %s, got %s", tc.Type, apiErr.Code)
		}
	}
}
