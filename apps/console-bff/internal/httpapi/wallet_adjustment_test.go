package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// walletAdjustmentOwnerServer hosts the administrator's balance-change command
// and its readback. The owner's own implementation is exercised by the live test
// in services/gateway-integration/identity (real PostgreSQL, real gRPC, real
// Sub2API HTTP fixture); this test proves the BFF's client transport, route
// boundary and serialization over a real gRPC connection.
type walletAdjustmentOwnerServer struct {
	api.UnimplementedGatewayProductServiceServer
	adjustRequest  *api.AdjustTenantWalletRpcRequest
	adjustResponse *api.Operation
	adjustErr      error
	listRequest    *api.ListRechargeRecordsRpcRequest
	listResponse   *api.WalletOperationPage
	listErr        error
	calls          int
}

func (s *walletAdjustmentOwnerServer) AdjustTenantWallet(_ context.Context, r *api.AdjustTenantWalletRpcRequest) (*api.Operation, error) {
	s.calls++
	s.adjustRequest = r
	return s.adjustResponse, s.adjustErr
}

func (s *walletAdjustmentOwnerServer) ListRechargeRecords(_ context.Context, r *api.ListRechargeRecordsRpcRequest) (*api.WalletOperationPage, error) {
	s.calls++
	s.listRequest = r
	return s.listResponse, s.listErr
}

// walletAdjustmentIdentity is the platform administrator's authorization
// decision for the Gateway owner's balance-change action.
func walletAdjustmentIdentity(action api.AuthorizationActionEnum, tenantID string) *fakeIdentity {
	identity := platformIdentity(action)
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_GATEWAY
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT}
	if tenantID != "" {
		identity.decision.Resource.Id = ptr(tenantID)
	}
	identity.decision.AuthorizationContextId = proto.String("fresh-wallet-adjustment-authority")
	return identity
}

func walletAdjustmentRequest(method, path, body string) *http.Request {
	request := adminSessionRequest(method, path)
	if body != "" {
		request.Body = io.NopCloser(strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("X-CSRF-Token", "csrf-admin")
	request.Header.Set("Idempotency-Key", "adjust-wallet-command-1")
	request.Header.Set("x-opl-request-id", "req-adjust-wallet-1")
	return request
}

// TestWalletAdjustmentRouteReachesTheGatewayOwner proves the published
// administrator balance-change command reaches the Gateway owner over the
// production typed client, with the tenant the path names, the command body and
// the operator's own write context.
func TestWalletAdjustmentRouteReachesTheGatewayOwner(t *testing.T) {
	owner := &walletAdjustmentOwnerServer{adjustResponse: &api.Operation{
		OperationId: "op-adjust-1",
		Owner:       api.OperationOwnerEnum_OPERATION_OWNER_ENUM_GATEWAY,
		Kind:        api.OperationKindEnum_OPERATION_KIND_ENUM_WALLET_ADJUSTMENT,
		ResourceId:  "tenant-adjust",
		Status:      api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED,
		Stage:       api.OperationStageEnum_OPERATION_STAGE_ENUM_SUCCEEDED,
		RequestId:   "req-adjust-wallet-1",
	}}
	client := dialGatewayOwner(t, owner)
	handler := NewServer(client, walletAdjustmentIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ADJUSTTENANTWALLET, "tenant-adjust")).Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, walletAdjustmentRequest(http.MethodPost, "/api/v2/admin/tenants/tenant-adjust/wallet-adjustments", `{"kind":"recharge","amountUSDMicros":"2500000","reason":"operator_requested"}`))
	if response.Code != http.StatusAccepted {
		t.Fatalf("adjust status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"operationId":"op-adjust-1"`) {
		t.Fatalf("the owner's operation was not published: %s", response.Body.String())
	}
	if owner.adjustRequest == nil {
		t.Fatal("the AdjustTenantWallet owner was not called")
	}
	if owner.adjustRequest.GetTenantId() != "tenant-adjust" {
		t.Fatalf("the owner received another tenant: %q", owner.adjustRequest.GetTenantId())
	}
	body := owner.adjustRequest.GetBody()
	if body.GetKind() != api.WalletAdjustmentRequestKindEnum_WALLET_ADJUSTMENT_REQUEST_KIND_ENUM_RECHARGE ||
		body.GetAmountUsdMicros() != 2_500_000 || body.GetReason() != "operator_requested" {
		t.Fatalf("the owner received another command: %+v", body)
	}
	call := owner.adjustRequest.GetContext()
	if call.GetActorId() != "admin-1" || call.GetSessionId() != owneridentity.SessionReference("session-admin") {
		t.Fatalf("the authenticated caller was not preserved: %v", call)
	}
	if call.GetScope().GetPlatform() == nil || call.GetScope().GetTenant() != nil {
		t.Fatalf("the admin route did not send platform scope: %v", call.GetScope())
	}
	if call.GetRequestId() != "req-adjust-wallet-1" || call.GetIdempotencyKey() != "adjust-wallet-command-1" || call.GetAuthorizationContextId() != "fresh-wallet-adjustment-authority" {
		t.Fatalf("the write context was not preserved: %v", call)
	}
}

// TestWalletAdjustmentRouteRefusesMissingWriteGuards proves a balance change
// without the live write guards never reaches the owner.
func TestWalletAdjustmentRouteRefusesMissingWriteGuards(t *testing.T) {
	for name, mutate := range map[string]func(*http.Request){
		"missing csrf":        func(r *http.Request) { r.Header.Del("X-CSRF-Token") },
		"missing idempotency": func(r *http.Request) { r.Header.Del("Idempotency-Key") },
		"cross site":          func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
	} {
		t.Run(name, func(t *testing.T) {
			owner := &walletAdjustmentOwnerServer{adjustResponse: &api.Operation{OperationId: "op-adjust-1"}}
			client := dialGatewayOwner(t, owner)
			handler := NewServer(client, walletAdjustmentIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ADJUSTTENANTWALLET, "tenant-adjust")).Handler()
			request := walletAdjustmentRequest(http.MethodPost, "/api/v2/admin/tenants/tenant-adjust/wallet-adjustments", `{"kind":"recharge","amountUSDMicros":"2500000","reason":"operator_requested"}`)
			mutate(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden && response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if owner.adjustRequest != nil {
				t.Fatal("the guard failure reached the AdjustTenantWallet owner")
			}
		})
	}
}

// TestWalletAdjustmentRouteRefusesAnUnauthorizedCaller proves a caller the
// identity owner did not allow never reaches the owner.
func TestWalletAdjustmentRouteRefusesAnUnauthorizedCaller(t *testing.T) {
	owner := &walletAdjustmentOwnerServer{adjustResponse: &api.Operation{OperationId: "op-adjust-1"}}
	client := dialGatewayOwner(t, owner)
	identity := walletAdjustmentIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ADJUSTTENANTWALLET, "tenant-adjust")
	identity.decision.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
	handler := NewServer(client, identity).Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, walletAdjustmentRequest(http.MethodPost, "/api/v2/admin/tenants/tenant-adjust/wallet-adjustments", `{"kind":"recharge","amountUSDMicros":"2500000","reason":"operator_requested"}`))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if owner.adjustRequest != nil {
		t.Fatal("a denied caller reached the AdjustTenantWallet owner")
	}
}

// TestWalletAdjustmentRoutePublishesTheOwnerRefusal proves the owner's own
// refusal keeps its canonical code instead of being masked as a bad gateway.
func TestWalletAdjustmentRoutePublishesTheOwnerRefusal(t *testing.T) {
	owner := &walletAdjustmentOwnerServer{adjustErr: owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "the tenant has no active wallet binding"), api.ErrorCodeEnum_ERROR_CODE_ENUM_VALIDATION_FAILED)}
	client := dialGatewayOwner(t, owner)
	handler := NewServer(client, walletAdjustmentIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ADJUSTTENANTWALLET, "tenant-adjust")).Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, walletAdjustmentRequest(http.MethodPost, "/api/v2/admin/tenants/tenant-adjust/wallet-adjustments", `{"kind":"recharge","amountUSDMicros":"2500000","reason":"operator_requested"}`))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "VALIDATION_FAILED") {
		t.Fatalf("the owner code was not published: %s", response.Body.String())
	}
}

// TestRechargeRecordsRouteReachesTheGatewayOwner proves the administrator's
// readback of the balance changes travels to the Gateway owner with the
// operator's own pagination, and that the browser never names a subject.
func TestRechargeRecordsRouteReachesTheGatewayOwner(t *testing.T) {
	owner := &walletAdjustmentOwnerServer{listResponse: &api.WalletOperationPage{
		Items: []*api.WalletOperation{{
			Id: "wallet-op-1", Kind: api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_RECHARGE,
			AmountUsdMicros: 2_500_000, Status: api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED,
		}},
	}}
	client := dialGatewayOwner(t, owner)
	handler := NewServer(client, walletAdjustmentIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRECHARGERECORDS, "")).Handler()

	response := httptest.NewRecorder()
	request := adminSessionRequest(http.MethodGet, "/api/v2/admin/recharge-records?tenantId=tenant-adjust&limit=10")
	request.Header.Set("x-opl-request-id", "req-adjust-read-1")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("read status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"wallet-op-1"`) {
		t.Fatalf("the owner's record was not published: %s", response.Body.String())
	}
	if owner.listRequest == nil {
		t.Fatal("the ListRechargeRecords owner was not called")
	}
	if owner.listRequest.GetQueryTenantId() != "tenant-adjust" || owner.listRequest.GetQueryLimit() != 10 {
		t.Fatalf("the owner received another query: %+v", owner.listRequest)
	}
	if owner.listRequest.GetContext().GetScope().GetPlatform() == nil {
		t.Fatalf("the admin read did not send platform scope: %v", owner.listRequest.GetContext())
	}
}
