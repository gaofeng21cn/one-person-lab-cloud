package openapi

import "testing"

func TestDecodeStrictAcceptsValidContractAndRejectsUnknownFields(t *testing.T) {
	raw := []byte(`{"username":"owner@example.com","password":"secret-value"}`)
	req, err := DecodeStrict[LoginRequest](raw)
	if err != nil {
		t.Fatalf("DecodeStrict valid LoginRequest: %v", err)
	}
	if req.Username != "owner@example.com" || req.Password == nil || *req.Password != "secret-value" {
		t.Fatalf("decoded LoginRequest = %+v", req)
	}

	unknown := []byte(`{"username":"owner@example.com","password":"secret-value","extra":"nope"}`)
	if _, err := DecodeStrict[LoginRequest](unknown); err == nil {
		t.Fatal("DecodeStrict accepted unknown field")
	}

	trailing := []byte(`{"username":"owner@example.com","password":"secret-value"}{"extra":1}`)
	if _, err := DecodeStrict[LoginRequest](trailing); err == nil {
		t.Fatal("DecodeStrict accepted trailing JSON token")
	}
}

func TestGeneratedErrorCodeValidation(t *testing.T) {
	errDTO := NewError(UNAUTHENTICATED, "session required", "req-1")
	if !errDTO.Code.Valid() || !CSRFINVALID.Valid() {
		t.Fatal("generated ErrorCode enum failed to validate canonical codes")
	}
	if ErrorCode("NOT_A_REAL_CODE").Valid() {
		t.Fatal("generated ErrorCode enum accepted bogus code")
	}
}
