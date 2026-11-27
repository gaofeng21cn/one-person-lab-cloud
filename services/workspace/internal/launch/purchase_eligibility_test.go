package launch

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// purchaseEligibilityReadFunc injects an explicit owner answer, including for
// live fixtures whose acceptance goal does not exercise Account policy.
type purchaseEligibilityReadFunc func(context.Context, *api.CallContext) (*contracts.WorkspacePurchaseEligibility, error)

func (f purchaseEligibilityReadFunc) Read(ctx context.Context, call *api.CallContext) (*contracts.WorkspacePurchaseEligibility, error) {
	return f(ctx, call)
}

func allowPurchaseEligibilityForTest(_ context.Context, call *api.CallContext) (*contracts.WorkspacePurchaseEligibility, error) {
	enabled := true
	return &contracts.WorkspacePurchaseEligibility{ActorID: call.GetActorId(), AccountID: "account-purchase-fixture", WorkspacePurchaseEnabled: &enabled}, nil
}

func purchaseEligibilityCall() *api.CallContext {
	return &api.CallContext{ActorId: "actor-purchase", SessionId: proto.String(owneridentity.SessionReference("purchase-browser-cookie-fixture")), RequestId: "request-purchase"}
}

type stubCloudIdentityEligibilityClient struct {
	api.CloudIdentityAuthorizationClient
	fn    func(context.Context, *api.ReadWorkspacePurchaseEligibilityRequest) (*api.WorkspacePurchaseEligibilityReadback, error)
	calls atomic.Int64
}

func (s *stubCloudIdentityEligibilityClient) ReadWorkspacePurchaseEligibility(ctx context.Context, req *api.ReadWorkspacePurchaseEligibilityRequest, _ ...grpc.CallOption) (*api.WorkspacePurchaseEligibilityReadback, error) {
	s.calls.Add(1)
	return s.fn(ctx, req)
}

func TestCloudIdentityPurchaseEligibilityReadback(t *testing.T) {
	tests := []struct {
		name    string
		resp    *api.WorkspacePurchaseEligibilityReadback
		code    codes.Code
		enabled bool
	}{
		{name: "enabled", resp: &api.WorkspacePurchaseEligibilityReadback{ActorId: "actor-purchase", AccountId: "actor-purchase", WorkspacePurchaseEnabled: true}, code: codes.OK, enabled: true},
		{name: "explicit false remains known", resp: &api.WorkspacePurchaseEligibilityReadback{ActorId: "actor-purchase", AccountId: "actor-purchase", WorkspacePurchaseEnabled: false}, code: codes.OK, enabled: false},
		{name: "nil response", resp: nil, code: codes.DataLoss},
		{name: "wrong actor", resp: &api.WorkspacePurchaseEligibilityReadback{ActorId: "other-actor", AccountId: "actor-purchase", WorkspacePurchaseEnabled: true}, code: codes.DataLoss},
		{name: "empty account", resp: &api.WorkspacePurchaseEligibilityReadback{ActorId: "actor-purchase", AccountId: "", WorkspacePurchaseEnabled: true}, code: codes.DataLoss},
		{name: "padded account", resp: &api.WorkspacePurchaseEligibilityReadback{ActorId: "actor-purchase", AccountId: " actor-purchase ", WorkspacePurchaseEnabled: true}, code: codes.DataLoss},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &stubCloudIdentityEligibilityClient{
				fn: func(_ context.Context, req *api.ReadWorkspacePurchaseEligibilityRequest) (*api.WorkspacePurchaseEligibilityReadback, error) {
					if req.GetContext().GetActorId() != "actor-purchase" || req.GetContext().GetSessionId() != purchaseEligibilityCall().GetSessionId() {
						t.Errorf("unexpected request context: %+v", req.GetContext())
					}
					return tt.resp, nil
				},
			}
			reader, err := NewCloudIdentityPurchaseEligibilityReader(client)
			if err != nil {
				t.Fatal(err)
			}
			fact, err := reader.Read(t.Context(), purchaseEligibilityCall())
			if status.Code(err) != tt.code {
				t.Fatalf("code=%s want=%s: %v", status.Code(err), tt.code, err)
			}
			if tt.code != codes.OK {
				if fact != nil {
					t.Fatal("invalid readback returned an eligibility fact")
				}
			} else if fact == nil || fact.ActorID != "actor-purchase" || fact.AccountID != "actor-purchase" || fact.WorkspacePurchaseEnabled == nil || *fact.WorkspacePurchaseEnabled != tt.enabled {
				t.Fatalf("owner boolean or identity lost: %+v", fact)
			}
			if client.calls.Load() != 1 {
				t.Fatalf("calls=%d want=1", client.calls.Load())
			}
		})
	}
}

func TestCloudIdentityPurchaseEligibilityErrors(t *testing.T) {
	for _, tt := range []struct {
		ownerCode codes.Code
		wantCode  codes.Code
	}{
		{codes.Unauthenticated, codes.Unauthenticated},
		{codes.PermissionDenied, codes.PermissionDenied},
		{codes.FailedPrecondition, codes.FailedPrecondition},
		{codes.Unavailable, codes.Unavailable},
		{codes.Internal, codes.Unavailable},
		{codes.DeadlineExceeded, codes.Unavailable},
	} {
		t.Run(fmt.Sprint(tt.ownerCode), func(t *testing.T) {
			client := &stubCloudIdentityEligibilityClient{
				fn: func(context.Context, *api.ReadWorkspacePurchaseEligibilityRequest) (*api.WorkspacePurchaseEligibilityReadback, error) {
					return nil, status.Error(tt.ownerCode, "upstream failure")
				},
			}
			reader, err := NewCloudIdentityPurchaseEligibilityReader(client)
			if err != nil {
				t.Fatal(err)
			}
			fact, err := reader.Read(t.Context(), purchaseEligibilityCall())
			if fact != nil || status.Code(err) != tt.wantCode {
				t.Fatalf("fact=%+v code=%s want=%s", fact, status.Code(err), tt.wantCode)
			}
		})
	}
}

func TestCloudIdentityPurchaseEligibilityRequiresLiveSession(t *testing.T) {
	client := &stubCloudIdentityEligibilityClient{
		fn: func(context.Context, *api.ReadWorkspacePurchaseEligibilityRequest) (*api.WorkspacePurchaseEligibilityReadback, error) {
			t.Fatal("invalid session reached CloudIdentity")
			return nil, nil
		},
	}
	reader, err := NewCloudIdentityPurchaseEligibilityReader(client)
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []*api.CallContext{nil, {}, {ActorId: "actor-purchase"}, {SessionId: proto.String("purchase-session-secret")}} {
		fact, err := reader.Read(t.Context(), call)
		if fact != nil || status.Code(err) != codes.Unauthenticated {
			t.Fatalf("missing session or actor: fact=%+v err=%v", fact, err)
		}
	}
	if client.calls.Load() != 0 {
		t.Fatal("invalid session reached the owner")
	}
}

func TestCloudIdentityPurchaseEligibilityReaderRequiresClient(t *testing.T) {
	reader, err := NewCloudIdentityPurchaseEligibilityReader(nil)
	if err == nil || reader != nil {
		t.Fatalf("nil client must fail: reader=%v err=%v", reader, err)
	}
}
