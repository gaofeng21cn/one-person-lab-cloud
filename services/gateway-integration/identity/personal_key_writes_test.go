package identity

import (
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
)

// TestPersonalKeyUpdateCommandCarriesAClearedExpiry proves a cleared expiry
// reaches the provider as an explicit instruction, so the owner can tell it
// apart from a command that never named the expiry.
func TestPersonalKeyUpdateCommandCarriesAClearedExpiry(t *testing.T) {
	request, expected, err := personalKeyUpdateRequest(&api.UpdatePersonalGatewayKeyRequest{ClearExpiresAt: proto.Bool(true)})
	if err != nil {
		t.Fatalf("a cleared expiry was refused: %v", err)
	}
	expiry, present := request["expires_at"]
	if !present {
		t.Fatalf("the provider never received the clear: %v", request)
	}
	if expiry != "" {
		t.Fatalf("the provider received another clear: %v", request)
	}
	if expected.expiresAtCleared == nil || !*expected.expiresAtCleared {
		t.Fatalf("the clear is not an expected fact: %v", expected)
	}
	if expected.expiresAt != nil {
		t.Fatalf("the clear also expects an instant: %v", expected)
	}
	// A readback that still carries an expiry contradicts the command, and a
	// readback without one confirms it.
	if personalKeyUpdateConfirmed(&api.PersonalGatewayKey{ExpiresAt: timestamppb.New(time.Now())}, expected) {
		t.Fatal("a readback that still carries an expiry was accepted as a cleared one")
	}
	if !personalKeyUpdateConfirmed(&api.PersonalGatewayKey{}, expected) {
		t.Fatal("a readback without an expiry was refused as a cleared one")
	}
}

// TestPersonalKeyUpdateCommandRefusesAnExpiryAndAClearTogether proves a command
// that names both is refused instead of guessing which one was meant.
func TestPersonalKeyUpdateCommandRefusesAnExpiryAndAClearTogether(t *testing.T) {
	_, _, err := personalKeyUpdateRequest(&api.UpdatePersonalGatewayKeyRequest{
		ExpiresAt:      timestamppb.New(time.Now().Add(time.Hour)),
		ClearExpiresAt: proto.Bool(true),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a command naming both an expiry and a clear answered %v", err)
	}
}

// TestPersonalKeyUpdateCommandLeavesAnAbsentExpiryUnchanged proves a command
// that never named the expiry leaves it alone, and that a readback carrying one
// does not contradict such a command.
func TestPersonalKeyUpdateCommandLeavesAnAbsentExpiryUnchanged(t *testing.T) {
	request, expected, err := personalKeyUpdateRequest(&api.UpdatePersonalGatewayKeyRequest{Name: proto.String("personal key")})
	if err != nil {
		t.Fatalf("a command without an expiry was refused: %v", err)
	}
	if _, present := request["expires_at"]; present {
		t.Fatalf("a command that never named the expiry changed it: %v", request)
	}
	if expected.expiresAt != nil || expected.expiresAtCleared != nil {
		t.Fatalf("a command that never named the expiry expects one: %v", expected)
	}
	if !personalKeyUpdateConfirmed(&api.PersonalGatewayKey{Name: "personal key", ExpiresAt: timestamppb.New(time.Now())}, expected) {
		t.Fatal("a readback carrying an expiry contradicted a command that named none")
	}
}
