package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

type createTenantProbe struct {
	api.TenantProductServiceClient
	received *api.CreateTenantRpcRequest
}

func (p *createTenantProbe) CreateTenant(_ context.Context, request *api.CreateTenantRpcRequest, _ ...grpc.CallOption) (*api.Operation, error) {
	p.received = request
	return &api.Operation{
		OperationId: "op-tenant-1",
		Owner:       api.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT,
		Kind:        api.OperationKindEnum_OPERATION_KIND_ENUM_CREATE_TENANT,
		ResourceId:  "tenant-created-1",
		Status:      api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED,
		Stage:       api.OperationStageEnum_OPERATION_STAGE_ENUM_SUCCEEDED,
		RequestId:   request.GetContext().GetRequestId(),
	}, nil
}

type tenantRouteReader struct {
	*fakeReader
	tenant api.TenantProductServiceClient
}

func (r *tenantRouteReader) TenantClient() api.TenantProductServiceClient { return r.tenant }

func TestCreateTenantAdminRouteUsesTenantOwnerAndPlatformScope(t *testing.T) {
	identity := platformIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATETENANT)
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_TENANT
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT}
	identity.decision.AuthorizationContextId = proto.String("fresh-create-tenant-authority")
	probe := &createTenantProbe{}
	reader := &tenantRouteReader{fakeReader: &fakeReader{}, tenant: probe}

	request := adminSessionRequest(http.MethodPost, "/api/v2/admin/tenants")
	request.Body = io.NopCloser(strings.NewReader(`{"name":"TKE customer","billingSub2apiUserId":"58","ownerGatewaySubjectId":"58"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-admin")
	request.Header.Set("Idempotency-Key", "create-tke-customer-1")
	request.Header.Set("x-opl-request-id", "req-create-tke-customer-1")
	request.Header.Set("x-opl-actor", "forged-actor")
	request.Header.Set("x-opl-tenant", "forged-tenant")

	response := httptest.NewRecorder()
	NewServer(reader, identity).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("create tenant status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"operationId":"op-tenant-1"`) {
		t.Fatalf("create tenant response omitted owner operation: %s", response.Body.String())
	}
	if probe.received == nil {
		t.Fatal("CreateTenant owner was not called")
	}
	call := probe.received.GetContext()
	if call.GetActorId() != "admin-1" || call.GetSessionId() != owneridentity.SessionReference("session-admin") {
		t.Fatalf("authenticated caller was not preserved: %v", call)
	}
	if call.GetScope().GetPlatform() == nil || call.GetScope().GetTenant() != nil {
		t.Fatalf("admin route did not send platform scope: %v", call.GetScope())
	}
	if call.GetRequestId() != "req-create-tke-customer-1" || call.GetIdempotencyKey() != "create-tke-customer-1" || call.GetAuthorizationContextId() != "fresh-create-tenant-authority" {
		t.Fatalf("write context was not preserved: %v", call)
	}
	body := probe.received.GetBody()
	if body.GetName() != "TKE customer" || body.GetBillingSub2ApiUserId() != "58" || body.GetOwnerGatewaySubjectId() != "58" {
		t.Fatalf("request body was not forwarded exactly: %v", body)
	}
}

func TestCreateTenantAdminRouteRefusesMissingWriteGuards(t *testing.T) {
	identity := platformIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATETENANT)
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_TENANT
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT}
	probe := &createTenantProbe{}
	reader := &tenantRouteReader{fakeReader: &fakeReader{}, tenant: probe}
	handler := NewServer(reader, identity).Handler()

	for name, mutate := range map[string]func(*http.Request){
		"missing csrf":        func(r *http.Request) { r.Header.Del("X-CSRF-Token") },
		"missing idempotency": func(r *http.Request) { r.Header.Del("Idempotency-Key") },
		"cross site":          func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
	} {
		t.Run(name, func(t *testing.T) {
			request := adminSessionRequest(http.MethodPost, "/api/v2/admin/tenants")
			request.Body = io.NopCloser(strings.NewReader(`{"name":"TKE customer","ownerGatewaySubjectId":"58"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-CSRF-Token", "csrf-admin")
			request.Header.Set("Idempotency-Key", "create-tke-customer-guard")
			mutate(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden && response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if probe.received != nil {
				t.Fatal("guard failure reached CreateTenant owner")
			}
		})
	}
}
