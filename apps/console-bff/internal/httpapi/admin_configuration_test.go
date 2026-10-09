package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// runtimeControlProbe records the typed runtime commands the BFF forwards, so a
// test can prove the platform scope, the owner's own idempotency key and the
// exact body reach the Runtime Control owner.
type runtimeControlProbe struct {
	api.RuntimeControlProductServiceClient
	received    *api.RegisterRuntimeVersionRpcRequest
	calls       int
	readPolicy  *api.GetBuildRuntimePolicyRpcRequest
	setPolicy   *api.SetBuildRuntimePolicyRpcRequest
	policyReads int
	policySets  int
}

func (p *runtimeControlProbe) RegisterRuntimeVersion(_ context.Context, r *api.RegisterRuntimeVersionRpcRequest, _ ...grpc.CallOption) (*api.RuntimeVersion, error) {
	p.calls++
	p.received = r
	return &api.RuntimeVersion{Id: "runtime-1", Name: r.GetBody().GetName(), VersionLabel: r.GetBody().GetVersionLabel(),
		Status:               api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED,
		PublisherNamespaceId: r.GetBody().GetPublisherNamespaceId(), AdmissionReceiptId: r.GetBody().GetAdmissionReceiptId()}, nil
}

func (p *runtimeControlProbe) GetBuildRuntimePolicy(_ context.Context, r *api.GetBuildRuntimePolicyRpcRequest, _ ...grpc.CallOption) (*api.BuildRuntimePolicy, error) {
	p.policyReads++
	p.readPolicy = r
	return &api.BuildRuntimePolicy{Id: "policy-1", RuntimeVersionId: "runtime-1", DefaultWebuiVersionId: proto.String("webui-1"), PolicyVersion: "policy-v1",
		EffectiveAt: timestamppb.New(time.Unix(1759000000, 0)), CreatedAt: timestamppb.New(time.Unix(1759000000, 0))}, nil
}

func (p *runtimeControlProbe) SetBuildRuntimePolicy(_ context.Context, r *api.SetBuildRuntimePolicyRpcRequest, _ ...grpc.CallOption) (*api.BuildRuntimePolicy, error) {
	p.policySets++
	p.setPolicy = r
	return &api.BuildRuntimePolicy{Id: "policy-2", RuntimeVersionId: r.GetBody().GetRuntimeVersionId(), DefaultWebuiVersionId: proto.String("webui-1"), PolicyVersion: "policy-v2",
		EffectiveAt: timestamppb.New(time.Unix(1759000000, 0)), CreatedAt: timestamppb.New(time.Unix(1759000000, 0))}, nil
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

// TestBuildRuntimePolicyRoutesForwardTheOwnerReadAndCommand proves the two
// administrator build-policy paths answer the owner's own readback in the
// contract spelling, forward the contract body and idempotency key to the
// Runtime Control owner, and keep the shared boundary: a denied caller, a missing
// CSRF token and an unwired owner are all refused before any policy is served.
func TestBuildRuntimePolicyRoutesForwardTheOwnerReadAndCommand(t *testing.T) {
	identity := platformIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETBUILDRUNTIMEPOLICY)
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL
	probe := &runtimeControlProbe{}
	handler := NewPublisherHandler(nil, probe, nil, identity)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, adminSessionRequest(http.MethodGet, "/api/v2/admin/catalog/build-policy"))
	if response.Code != http.StatusOK {
		t.Fatalf("build policy read status=%d body=%s", response.Code, response.Body.String())
	}
	if probe.policyReads != 1 || probe.readPolicy == nil {
		t.Fatalf("owner reads=%d", probe.policyReads)
	}
	if read := probe.readPolicy; read.GetContext().GetActorId() != "admin-1" || read.GetContext().GetScope().GetPlatform() == nil {
		t.Fatalf("owner read context=%v", read.GetContext())
	}
	if len(identity.requests) != 1 {
		t.Fatalf("authorization requests=%d", len(identity.requests))
	}
	if authorized := identity.requests[0]; authorized.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL ||
		authorized.GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETBUILDRUNTIMEPOLICY ||
		authorized.GetResource().GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG ||
		authorized.GetResource().GetId() != "" || authorized.GetScope().GetPlatform() == nil {
		t.Fatalf("authorization request=%v", authorized)
	}
	for _, want := range []string{`"id":"policy-1"`, `"runtimeVersionId":"runtime-1"`, `"defaultWebuiVersionId":"webui-1"`, `"policyVersion":"policy-v1"`, `"effectiveAt":"`, `"createdAt":"`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("response %s missing %s", response.Body.String(), want)
		}
	}

	// The command carries the contract body and the caller's own idempotency key
	// under the same platform scope.
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETBUILDRUNTIMEPOLICY
	request := adminSessionRequest(http.MethodPut, "/api/v2/admin/catalog/build-policy")
	prepareAdminWrite(request, `{"runtimeVersionId":"runtime-1","expectedPolicyVersionId":"policy-1"}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("build policy write status=%d body=%s", response.Code, response.Body.String())
	}
	if probe.policySets != 1 || probe.setPolicy == nil {
		t.Fatalf("owner commands=%d", probe.policySets)
	}
	if command := probe.setPolicy; command.GetBody().GetRuntimeVersionId() != "runtime-1" || command.GetBody().GetExpectedPolicyVersionId() != "policy-1" ||
		command.GetContext().GetIdempotencyKey() != "stable-catalog-command" || command.GetContext().GetScope().GetPlatform() == nil {
		t.Fatalf("owner command=%v", command)
	}
	if len(identity.requests) != 2 {
		t.Fatalf("authorization requests=%d", len(identity.requests))
	}
	if authorized := identity.requests[1]; authorized.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL ||
		authorized.GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETBUILDRUNTIMEPOLICY ||
		authorized.GetResource().GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG ||
		authorized.GetScope().GetPlatform() == nil {
		t.Fatalf("authorization request=%v", authorized)
	}
	if !strings.Contains(response.Body.String(), `"runtimeVersionId":"runtime-1"`) || !strings.Contains(response.Body.String(), `"policyVersion":"policy-v2"`) {
		t.Fatalf("response %s missing the owner readback", response.Body.String())
	}

	// A caller CloudIdentity denies is refused before the owner is called.
	denied := platformIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETBUILDRUNTIMEPOLICY)
	denied.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL
	denied.decision.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
	handler = NewPublisherHandler(nil, probe, nil, denied)
	probe.policySets = 0
	request = adminSessionRequest(http.MethodPut, "/api/v2/admin/catalog/build-policy")
	prepareAdminWrite(request, `{"runtimeVersionId":"runtime-1","expectedPolicyVersionId":"policy-1"}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || probe.policySets != 0 {
		t.Fatalf("denied caller status=%d commands=%d", response.Code, probe.policySets)
	}

	// The shared write guard applies unchanged: no CSRF token, no owner call.
	probe.policySets = 0
	request = adminSessionRequest(http.MethodPut, "/api/v2/admin/catalog/build-policy")
	prepareAdminWrite(request, `{"runtimeVersionId":"runtime-1","expectedPolicyVersionId":"policy-1"}`)
	request.Header.Del("X-CSRF-Token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || probe.policySets != 0 {
		t.Fatalf("missing CSRF status=%d commands=%d", response.Code, probe.policySets)
	}

	// A write without an idempotency key is refused for the same reason: the
	// owner's idempotent command record can never be addressed without it.
	probe.policySets = 0
	request = adminSessionRequest(http.MethodPut, "/api/v2/admin/catalog/build-policy")
	prepareAdminWrite(request, `{"runtimeVersionId":"runtime-1","expectedPolicyVersionId":"policy-1"}`)
	request.Header.Del("Idempotency-Key")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || probe.policySets != 0 {
		t.Fatalf("missing idempotency status=%d commands=%d", response.Code, probe.policySets)
	}

	// An unwired Runtime Control owner is unavailable, never a fabricated policy.
	unwiredHandler := NewPublisherHandler(nil, nil, nil, identity)
	request = adminSessionRequest(http.MethodPut, "/api/v2/admin/catalog/build-policy")
	prepareAdminWrite(request, `{"runtimeVersionId":"runtime-1","expectedPolicyVersionId":"policy-1"}`)
	response = httptest.NewRecorder()
	unwiredHandler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired owner status=%d body=%s", response.Code, response.Body.String())
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
