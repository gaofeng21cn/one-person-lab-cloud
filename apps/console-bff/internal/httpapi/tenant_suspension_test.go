package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

type suspendTenantProbe struct {
	api.TenantProductServiceClient
	received *api.SuspendTenantRpcRequest
	response *api.Operation
	err      error
}

func (p *suspendTenantProbe) SuspendTenant(_ context.Context, r *api.SuspendTenantRpcRequest, _ ...grpc.CallOption) (*api.Operation, error) {
	p.received = r
	if p.err != nil {
		return nil, p.err
	}
	if p.response != nil {
		return p.response, nil
	}
	return &api.Operation{
		OperationId: "op-suspend-1",
		Owner:       api.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT,
		Kind:        api.OperationKindEnum_OPERATION_KIND_ENUM_SUSPEND_TENANT,
		ResourceId:  r.GetTenantId(),
		Status:      api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED,
		Stage:       api.OperationStageEnum_OPERATION_STAGE_ENUM_SUCCEEDED,
		RequestId:   r.GetContext().GetRequestId(),
	}, nil
}

func suspendTenantIdentity(tenantID string) *fakeIdentity {
	identity := platformIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SUSPENDTENANT)
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_TENANT
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: ptr(tenantID)}
	identity.decision.AuthorizationContextId = proto.String("fresh-suspend-tenant-authority")
	return identity
}

func suspendTenantRequest(path, body string) *http.Request {
	request := adminSessionRequest(http.MethodPost, path)
	request.Body = io.NopCloser(strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-admin")
	request.Header.Set("Idempotency-Key", "suspend-tenant-command-1")
	request.Header.Set("x-opl-request-id", "req-suspend-tenant-1")
	return request
}

// TestSuspendTenantAdminRouteUsesTenantOwnerAndPlatformScope proves the
// published suspension command reaches the Tenant owner over the production
// client, with the operator's own platform scope and the tenant the path
// names.
func TestSuspendTenantAdminRouteUsesTenantOwnerAndPlatformScope(t *testing.T) {
	probe := &suspendTenantProbe{}
	reader := &tenantRouteReader{fakeReader: &fakeReader{}, tenant: probe}
	handler := NewServer(reader, suspendTenantIdentity("tenant-suspension")).Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, suspendTenantRequest("/api/v2/admin/tenants/tenant-suspension/suspend", `{"reason":"operator_requested"}`))
	if response.Code != http.StatusAccepted {
		t.Fatalf("suspend status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"operationId":"op-suspend-1"`) {
		t.Fatalf("suspend response omitted the owner operation: %s", response.Body.String())
	}
	if probe.received == nil {
		t.Fatal("SuspendTenant owner was not called")
	}
	if probe.received.GetTenantId() != "tenant-suspension" {
		t.Fatalf("the owner received another tenant: %q", probe.received.GetTenantId())
	}
	if probe.received.GetBody().GetReason() != "operator_requested" {
		t.Fatalf("the owner received another reason: %q", probe.received.GetBody().GetReason())
	}
	call := probe.received.GetContext()
	if call.GetActorId() != "admin-1" || call.GetSessionId() != owneridentity.SessionReference("session-admin") {
		t.Fatalf("authenticated caller was not preserved: %v", call)
	}
	if call.GetScope().GetPlatform() == nil || call.GetScope().GetTenant() != nil {
		t.Fatalf("admin route did not send platform scope: %v", call.GetScope())
	}
	if call.GetRequestId() != "req-suspend-tenant-1" || call.GetIdempotencyKey() != "suspend-tenant-command-1" || call.GetAuthorizationContextId() != "fresh-suspend-tenant-authority" {
		t.Fatalf("write context was not preserved: %v", call)
	}
}

// TestSuspendTenantAdminRouteRefusesMissingWriteGuards proves a suspension
// without the live write guards never reaches the owner.
func TestSuspendTenantAdminRouteRefusesMissingWriteGuards(t *testing.T) {
	for name, mutate := range map[string]func(*http.Request){
		"missing csrf":        func(r *http.Request) { r.Header.Del("X-CSRF-Token") },
		"missing idempotency": func(r *http.Request) { r.Header.Del("Idempotency-Key") },
		"cross site":          func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
	} {
		t.Run(name, func(t *testing.T) {
			probe := &suspendTenantProbe{}
			reader := &tenantRouteReader{fakeReader: &fakeReader{}, tenant: probe}
			handler := NewServer(reader, suspendTenantIdentity("tenant-suspension")).Handler()
			request := suspendTenantRequest("/api/v2/admin/tenants/tenant-suspension/suspend", `{"reason":"operator_requested"}`)
			mutate(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden && response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if probe.received != nil {
				t.Fatal("guard failure reached the SuspendTenant owner")
			}
		})
	}
}

// TestSuspendTenantAdminRouteRefusesAnUnauthorizedCaller proves a caller the
// identity owner did not allow never reaches the owner.
func TestSuspendTenantAdminRouteRefusesAnUnauthorizedCaller(t *testing.T) {
	probe := &suspendTenantProbe{}
	reader := &tenantRouteReader{fakeReader: &fakeReader{}, tenant: probe}
	identity := suspendTenantIdentity("tenant-suspension")
	identity.decision.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
	handler := NewServer(reader, identity).Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, suspendTenantRequest("/api/v2/admin/tenants/tenant-suspension/suspend", `{"reason":"operator_requested"}`))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if probe.received != nil {
		t.Fatal("a denied caller reached the SuspendTenant owner")
	}
}

// TestSuspendTenantAdminRoutePublishesTheOwnerRefusal proves the owner's own
// refusal keeps its canonical code instead of being masked as a bad gateway.
func TestSuspendTenantAdminRoutePublishesTheOwnerRefusal(t *testing.T) {
	probe := &suspendTenantProbe{err: owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "the tenant is not active"), api.ErrorCodeEnum_ERROR_CODE_ENUM_TENANT_INACTIVE)}
	reader := &tenantRouteReader{fakeReader: &fakeReader{}, tenant: probe}
	handler := NewServer(reader, suspendTenantIdentity("tenant-suspension")).Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, suspendTenantRequest("/api/v2/admin/tenants/tenant-suspension/suspend", `{"reason":"operator_requested"}`))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "TENANT_INACTIVE") {
		t.Fatalf("the owner code was not published: %s", response.Body.String())
	}
}
