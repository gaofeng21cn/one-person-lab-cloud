package launch

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
)

// TestSourceValidationRejectsRevoked verifies that validateSourceReadiness
// rejects revoked or unavailable sources with FailedPrecondition.
func TestSourceValidationRejectsRevoked(t *testing.T) {
	db := paidOrderDatabase(t)
	service, op, accepted := seedPaidOrder(t, db)
	
	// Inject a Capability client that returns NotFound (revoked version)
	service.Capability = &revokedCapabilityClient{}
	
	err := service.validateSourceReadiness(context.Background(), op, "grant-test", accepted)
	
	if err == nil {
		t.Fatal("expected error for revoked capability, got nil")
	}
	
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition, got %v", err)
	}
	
	if st.Message() != "application source is not available: rpc error: code = NotFound desc = capability version revoked" {
		t.Errorf("unexpected error message: %s", st.Message())
	}
}

// TestSourceValidationAcceptsValid verifies that validateSourceReadiness
// accepts valid approved sources.
func TestSourceValidationAcceptsValid(t *testing.T) {
	db := paidOrderDatabase(t)
	service, op, accepted := seedPaidOrder(t, db)
	
	// Use the default valid Capability client from seedPaidOrder
	err := service.validateSourceReadiness(context.Background(), op, "grant-test", accepted)
	
	if err != nil {
		t.Fatalf("expected no error for valid capability, got %v", err)
	}
}

// revokedCapabilityClient simulates a Capability service where versions are not found
type revokedCapabilityClient struct {
	api.CapabilityProductServiceClient
}

func (r *revokedCapabilityClient) GetCapabilityVersion(_ context.Context, _ *api.GetCapabilityVersionRpcRequest, _ ...grpc.CallOption) (*api.CapabilityVersion, error) {
	return nil, status.Error(codes.NotFound, "capability version revoked")
}
