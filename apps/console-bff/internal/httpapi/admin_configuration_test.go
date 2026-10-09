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

// runtimeControlProbe records the typed runtime admission command the BFF
// forwards, so a test can prove the platform scope, the owner's own idempotency
// key and the exact body reach the Runtime Control owner.
type runtimeControlProbe struct {
	api.RuntimeControlProductServiceClient
	received *api.RegisterRuntimeVersionRpcRequest
	calls    int
}

func (p *runtimeControlProbe) RegisterRuntimeVersion(_ context.Context, r *api.RegisterRuntimeVersionRpcRequest, _ ...grpc.CallOption) (*api.RuntimeVersion, error) {
	p.calls++
	p.received = r
	return &api.RuntimeVersion{Id: "runtime-1", Name: r.GetBody().GetName(), VersionLabel: r.GetBody().GetVersionLabel(),
		Status:               api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED,
		PublisherNamespaceId: r.GetBody().GetPublisherNamespaceId(), AdmissionReceiptId: r.GetBody().GetAdmissionReceiptId()}, nil
}

// TestRuntimeVersionRegisterRouteCarriesPlatformScopeAndOwnerIdempotency proves
// the administrator runtime admission route answers with the contract's 201,
// forwards only the authenticated caller's platform scope, the caller's
// idempotency key and the freshly issued authorization context (so the owner can
// revalidate the same action), and never lets a browser-supplied identity field
// reach the owner.
func TestRuntimeVersionRegisterRouteCarriesPlatformScopeAndOwnerIdempotency(t *testing.T) {
	identity := platformIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REGISTERRUNTIMEVERSION)
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL
	identity.decision.AuthorizationContextId = proto.String("runtime-authority-context")
	probe := &runtimeControlProbe{}
	handler := NewPublisherHandler(nil, probe, nil, identity)

	request := adminSessionRequest(http.MethodPost, "/api/v2/admin/catalog/runtime-versions")
	prepareAdminWrite(request, `{"name":"opl-default-app","versionLabel":"1.0.0","publisherNamespaceId":"publisher-9","admissionReceiptId":"receipt-9"}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("register runtime status=%d body=%s", response.Code, response.Body.String())
	}
	if probe.calls != 1 || probe.received == nil {
		t.Fatalf("owner calls=%d", probe.calls)
	}
	body := probe.received.GetBody()
	if body.GetName() != "opl-default-app" || body.GetVersionLabel() != "1.0.0" || body.GetPublisherNamespaceId() != "publisher-9" || body.GetAdmissionReceiptId() != "receipt-9" {
		t.Fatalf("owner body=%v", body)
	}
	call := probe.received.GetContext()
	if call.GetActorId() != "admin-1" || call.GetScope().GetPlatform() == nil || call.GetSessionId() != owneridentity.SessionReference("session-admin") ||
		call.GetIdempotencyKey() != "stable-catalog-command" || call.GetAuthorizationContextId() != "runtime-authority-context" {
		t.Fatalf("owner caller context=%v", call)
	}
	if len(identity.requests) != 1 {
		t.Fatalf("authorization requests=%d", len(identity.requests))
	}
	authorized := identity.requests[0]
	if authorized.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL ||
		authorized.GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REGISTERRUNTIMEVERSION ||
		authorized.GetResource().GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG ||
		authorized.GetScope().GetPlatform() == nil {
		t.Fatalf("authorization request=%v", authorized)
	}

	// A body that carries a browser-chosen identity field is refused before any
	// owner call: the public contract has no such property.
	probe.calls = 0
	request = adminSessionRequest(http.MethodPost, "/api/v2/admin/catalog/runtime-versions")
	prepareAdminWrite(request, `{"name":"opl-default-app","versionLabel":"1.0.0","publisherNamespaceId":"publisher-9","admissionReceiptId":"receipt-9","actorId":"forged"}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || probe.calls != 0 {
		t.Fatalf("forged body status=%d calls=%d", response.Code, probe.calls)
	}

	// The shared write guard applies unchanged: no CSRF token, no owner call.
	probe.calls = 0
	request = adminSessionRequest(http.MethodPost, "/api/v2/admin/catalog/runtime-versions")
	prepareAdminWrite(request, `{"name":"opl-default-app","versionLabel":"1.0.0","publisherNamespaceId":"publisher-9","admissionReceiptId":"receipt-9"}`)
	request.Header.Del("X-CSRF-Token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || probe.calls != 0 {
		t.Fatalf("missing CSRF status=%d calls=%d", response.Code, probe.calls)
	}
}

// walletBindingProbe is a typed GatewayCoordination double. The real owner
// implementation is exercised by the live stack; this double proves the BFF's
// route boundary, command mapping and response serialization.
type walletBindingProbe struct {
	api.GatewayCoordinationClient
	received *api.WalletBindingCommand
	calls    int
	refusal  error
}

func (p *walletBindingProbe) BindWallet(_ context.Context, r *api.WalletBindingCommand, _ ...grpc.CallOption) (*api.WalletBindingReadback, error) {
	p.calls++
	p.received = r
	if p.refusal != nil {
		return nil, p.refusal
	}
	return &api.WalletBindingReadback{TenantId: r.GetTargetTenantId(), BillingSub2ApiUserId: r.GetBillingSub2ApiUserId(),
		BindingVersion: r.GetExpectedBindingVersion() + 1, Outcome: api.Observation_OBSERVATION_CONFIRMED,
		AuthorizationReceiptId: r.GetAuthorizationReceiptId()}, nil
}

func walletPlatformIdentity() *fakeIdentity {
	identity := platformIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDTENANTWALLET)
	// The write guard resolves the browser cookie through CloudIdentity, so the
	// decision must name the same session reference the session cookie produces.
	identity.decision.SessionId = ptr(owneridentity.SessionReference("session-1"))
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_TENANT
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: ptr("tenant-9")}
	identity.decision.AuthorizationContextId = proto.String("bind-authority-context")
	return identity
}

func walletServer(probe *walletBindingProbe, identity IdentityReader) *Server {
	server := &Server{identity: identity, tenant: &tenantProbe{}, gatewayCoordination: probe}
	return server
}

func walletBindingWrite(body string) *http.Request {
	request := sessionRequest(http.MethodPut, "/api/v2/admin/tenants/tenant-9/wallet-binding")
	request.Header.Set("X-CSRF-Token", "csrf-admin")
	request.Header.Set("Idempotency-Key", "bind-tenant-wallet-1")
	request.Header.Set("Content-Type", "application/json")
	request.Body = http.NoBody
	if body != "" {
		request.Body = io.NopCloser(strings.NewReader(body))
	}
	return request
}

// TestWalletBindingRouteForwardsThePathTenantAndTheIssuedReceipt proves the
// administrator wallet binding command carries the path tenant, the contract
// body and the authorization context CloudIdentity just issued for this exact
// action, and answers with the declared 202 while the owner's own refusal keeps
// its typed status.
func TestWalletBindingRouteForwardsThePathTenantAndTheIssuedReceipt(t *testing.T) {
	identity := walletPlatformIdentity()
	identity.session.CsrfToken = "csrf-admin"
	probe := &walletBindingProbe{}
	server := walletServer(probe, identity)
	mux := http.NewServeMux()
	server.registerWalletBindingRoute(mux)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, walletBindingWrite(`{"billingSub2apiUserId":"58","expectedBindingVersion":"1"}`))
	if response.Code != http.StatusAccepted {
		t.Fatalf("bind wallet status=%d body=%s", response.Code, response.Body.String())
	}
	if probe.calls != 1 {
		t.Fatalf("owner calls=%d", probe.calls)
	}
	command := probe.received
	if command.GetTargetTenantId() != "tenant-9" || command.GetBillingSub2ApiUserId() != "58" || command.GetExpectedBindingVersion() != 1 ||
		command.GetAuthorizationReceiptId() != "bind-authority-context" {
		t.Fatalf("owner command=%v", command)
	}
	if command.GetContext().GetActorId() != "admin-1" || command.GetContext().GetScope().GetPlatform() == nil {
		t.Fatalf("owner caller context=%v", command.GetContext())
	}
	if len(identity.requests) != 1 {
		t.Fatalf("authorization requests=%d", len(identity.requests))
	}
	authorized := identity.requests[0]
	if authorized.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_TENANT || authorized.GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDTENANTWALLET ||
		authorized.GetResource().GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT || authorized.GetResource().GetId() != "tenant-9" {
		t.Fatalf("authorization request=%v", authorized)
	}
	// The record the owner persists is the owner's readback, not a BFF copy.
	for _, want := range []string{`"tenantId":"tenant-9"`, `"billingSub2apiUserId":"58"`, `"bindingVersion":2`, `"authorizationReceiptId":"bind-authority-context"`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("response %s missing %s", response.Body.String(), want)
		}
	}

	// A contract-invalid body (a bare JSON number where the contract types a
	// decimal string) is refused before any owner call.
	probe.calls = 0
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, walletBindingWrite(`{"billingSub2apiUserId":"58","expectedBindingVersion":1}`))
	if response.Code != http.StatusBadRequest || probe.calls != 0 {
		t.Fatalf("invalid body status=%d calls=%d", response.Code, probe.calls)
	}

	// Both write guards apply unchanged.
	probe.calls = 0
	request := walletBindingWrite(`{"billingSub2apiUserId":"58","expectedBindingVersion":"1"}`)
	request.Header.Del("X-CSRF-Token")
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || probe.calls != 0 {
		t.Fatalf("missing CSRF status=%d calls=%d", response.Code, probe.calls)
	}
	probe.calls = 0
	request = walletBindingWrite(`{"billingSub2apiUserId":"58","expectedBindingVersion":"1"}`)
	request.Header.Del("Idempotency-Key")
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || probe.calls != 0 {
		t.Fatalf("missing idempotency status=%d calls=%d", response.Code, probe.calls)
	}

	// An anonymous caller never reaches the owner.
	probe.calls = 0
	anonymous := httptest.NewRequest(http.MethodPut, "/api/v2/admin/tenants/tenant-9/wallet-binding", http.NoBody)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, anonymous)
	if response.Code != http.StatusUnauthorized || probe.calls != 0 {
		t.Fatalf("anonymous status=%d calls=%d", response.Code, probe.calls)
	}

	// The owner's own refusal keeps its typed status: a version conflict is 409.
	probe.refusal = status.Error(codes.Aborted, "wallet binding version conflict")
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, walletBindingWrite(`{"billingSub2apiUserId":"58","expectedBindingVersion":"0"}`))
	if response.Code != http.StatusConflict {
		t.Fatalf("owner refusal status=%d body=%s", response.Code, response.Body.String())
	}

	// An unwired coordination client is an unavailable owner, never a fabricated
	// success.
	unwired := &Server{identity: identity, tenant: &tenantProbe{}}
	mux = http.NewServeMux()
	unwired.registerWalletBindingRoute(mux)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, walletBindingWrite(`{"billingSub2apiUserId":"58","expectedBindingVersion":"1"}`))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired owner status=%d body=%s", response.Code, response.Body.String())
	}
}
