package contracts

import (
	"encoding/json"
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
