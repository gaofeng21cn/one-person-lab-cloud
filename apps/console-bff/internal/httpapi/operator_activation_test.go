package httpapi

import (
	"context"
	"encoding/json"
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

// activationTenantProbe answers the owner that holds the customer account fact.
// The account-opening command is that owner's own now, so the BFF forwards one
// command and publishes the owner's Operation.
type activationTenantProbe struct {
	api.TenantProductServiceClient
	provision    *api.ProvisionOperatorAccountRpcRequest
	provisionErr error
	command      *api.OperatorAccountCommand
	calls        int
}

func (p *activationTenantProbe) ProvisionOperatorAccount(_ context.Context, r *api.ProvisionOperatorAccountRpcRequest, _ ...grpc.CallOption) (*api.OperatorAccountCommand, error) {
	p.calls++
	p.provision = r
	if p.provisionErr != nil {
		return nil, p.provisionErr
	}
	if p.command != nil {
		return p.command, nil
	}
	return activationSucceededCommand("op_provision_1", "acct-1"), nil
}

// activationIdentity pins the platform-administrator decision the account-opening
// command is authorized with. The owner decides the same action again for
// itself; the BFF asks for it once, at platform scope.
func activationIdentity() *fakeIdentity {
	return &fakeIdentity{
		session: &api.Session{ActorId: "admin-1", CsrfToken: "csrf-admin"},
		decisions: map[api.AuthorizationActionEnum]*api.AuthorizationDecision{
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONOPERATORACCOUNT: activationPlatformDecision(
				api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONOPERATORACCOUNT,
				api.OwnerEnum_OWNER_ENUM_TENANT,
				api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM,
				"", "provision-authority"),
		},
	}
}

func activationPlatformDecision(action api.AuthorizationActionEnum, audience api.OwnerEnum, kind api.AuthorizationResourceKind, resourceID, contextID string) *api.AuthorizationDecision {
	resource := &api.AuthorizationResource{Kind: kind}
	if resourceID != "" {
		resource.Id = proto.String(resourceID)
	}
	return &api.AuthorizationDecision{
		ActorId: "admin-1", SessionId: ptr(owneridentity.SessionReference("session-admin")),
		Scope:             &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
		PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Minute)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
		Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED, Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY,
		Action: action, AudienceOwner: audience, Resource: resource, AuthorizationContextId: proto.String(contextID),
	}
}

func activationServer(tenant *activationTenantProbe, identity IdentityReader) *Server {
	return &Server{identity: identity, tenant: tenant}
}

func activationRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v2/operator/accounts", strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-admin")
	request.Header.Set("Idempotency-Key", "activation-1")
	return request
}

const activationBody = `{"email":"customer@example.com","password":"CorrectHorseBatteryStaple!","name":"Customer One","admission":"full_cloud_customer"}`

// activationSucceededCommand is the owner's terminal answer for one finished
// account-opening command: the opened account, its operation and the purchase
// eligibility the owner granted it.
func activationSucceededCommand(operationID, accountID string) *api.OperatorAccountCommand {
	return &api.OperatorAccountCommand{
		OperationId: operationID, AccountId: accountID, Status: "succeeded",
		WorkspacePurchaseEnabled: true,
	}
}

// activationCommandOf reads the owner's published command receipt.
func activationCommandOf(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var command map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &command); err != nil {
		t.Fatalf("activation command %s: %v", recorder.Body.String(), err)
	}
	return command
}

// TestOperatorActivationPublishesTheOwnerOperation proves the account-opening
// command is the owner's own now: the BFF authorizes the action once, forwards
// the caller's own session and idempotency key with a canonical body, and
// publishes the owner's Operation as the accepted answer.
func TestOperatorActivationPublishesTheOwnerOperation(t *testing.T) {
	tenant := &activationTenantProbe{}
	identity := activationIdentity()
	handler := activationServer(tenant, identity).Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, activationRequest(activationBody))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("activation status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("activation content type=%q", recorder.Header().Get("Content-Type"))
	}
	// A terminal operation carries no poll cadence: the browser stops reading.
	if recorder.Header().Get("Retry-After") != "" {
		t.Fatalf("terminal activation asked the browser to poll: %q", recorder.Header().Get("Retry-After"))
	}
	command := activationCommandOf(t, recorder)
	if command["operationId"] != "op_provision_1" || command["accountId"] != "acct-1" ||
		command["status"] != "succeeded" || command["workspacePurchaseEnabled"] != true {
		t.Fatalf("activation command=%v", command)
	}

	// The owner receives the operator's own session, request and idempotency key,
	// and a canonical command body rather than browser bytes.
	if tenant.calls != 1 || tenant.provision == nil {
		t.Fatalf("owner calls=%d", tenant.calls)
	}
	call := tenant.provision.GetContext()
	if call.GetActorId() != "admin-1" || !strings.HasPrefix(call.GetSessionId(), "session_") ||
		call.GetScope().GetPlatform() == nil || call.GetRequestId() == "" || call.GetIdempotencyKey() != "activation-1" {
		t.Fatalf("owner call context=%+v", call)
	}
	body := tenant.provision.GetBody()
	if body.GetEmail() != "customer@example.com" || body.GetPassword() != "CorrectHorseBatteryStaple!" ||
		body.GetName() != "Customer One" || body.GetAdmission() != api.ProvisionAccountRequestAdmissionEnum_PROVISION_ACCOUNT_REQUEST_ADMISSION_ENUM_FULL_CLOUD_CUSTOMER {
		t.Fatalf("owner command body=%+v", body)
	}

	// The BFF asks for exactly this one action, at the tenant audience and
	// platform scope: the owner decides the same action again for itself.
	if len(identity.requests) != 1 {
		t.Fatalf("authorization requests=%d", len(identity.requests))
	}
	request := identity.requests[0]
	if request.GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONOPERATORACCOUNT ||
		request.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_TENANT ||
		request.GetResource().GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM ||
		request.GetScope().GetPlatform() == nil {
		t.Fatalf("authorization request=%v", request)
	}
}

// TestOperatorActivationReportsThePendingOperation proves a command whose
// outcome is not confirmed yet answers with the owner's poll cadence, so the
// browser reads the same command back instead of retrying it.
func TestOperatorActivationReportsThePendingOperation(t *testing.T) {
	poll := int32(5)
	tenant := &activationTenantProbe{command: &api.OperatorAccountCommand{
		OperationId: "op_provision_pending", AccountId: "acct-1", Status: "awaiting_confirmation",
		PollAfterSeconds: &poll,
	}}
	handler := activationServer(tenant, activationIdentity()).Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, activationRequest(activationBody))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("activation status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Retry-After") != "5" {
		t.Fatalf("pending activation retry-after=%q", recorder.Header().Get("Retry-After"))
	}
	command := activationCommandOf(t, recorder)
	if command["status"] != "awaiting_confirmation" || command["pollAfterSeconds"] != float64(5) {
		t.Fatalf("pending activation command=%v", command)
	}
}

// TestOperatorActivationRefusesADeniedAction proves a caller the owner does not
// authorize is refused before the owner is called at all.
func TestOperatorActivationRefusesADeniedAction(t *testing.T) {
	identity := activationIdentity()
	denied := activationPlatformDecision(
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONOPERATORACCOUNT,
		api.OwnerEnum_OWNER_ENUM_TENANT,
		api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM,
		"", "provision-authority")
	denied.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
	identity.decisions[api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONOPERATORACCOUNT] = denied
	tenant := &activationTenantProbe{}
	handler := activationServer(tenant, identity).Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, activationRequest(activationBody))
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"FORBIDDEN"`) {
		t.Fatalf("denied activation status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if tenant.calls != 0 {
		t.Fatalf("a denied activation reached the owner: %d", tenant.calls)
	}
}

// TestOperatorActivationKeepsItsWriteAndOperatorGuards proves the command is
// not a new unguarded path: it keeps the shared CSRF, origin and idempotency
// guards, and a refused request never reaches the owner.
func TestOperatorActivationKeepsItsWriteAndOperatorGuards(t *testing.T) {
	for name, test := range map[string]struct {
		mutate func(*http.Request)
		code   int
	}{
		"missing csrf":        {mutate: func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, code: http.StatusForbidden},
		"cross site":          {mutate: func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, code: http.StatusForbidden},
		"foreign origin":      {mutate: func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, code: http.StatusForbidden},
		"missing idempotency": {mutate: func(r *http.Request) { r.Header.Del("Idempotency-Key") }, code: http.StatusBadRequest},
		"wrong content type":  {mutate: func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, code: http.StatusUnsupportedMediaType},
		"unknown field": {mutate: func(r *http.Request) {
			r.Body = io.NopCloser(strings.NewReader(`{"email":"a@b.c","password":"x","sub2apiUserId":"99"}`))
		}, code: http.StatusBadRequest},
		"missing password": {mutate: func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader(`{"email":"a@b.c"}`)) }, code: http.StatusBadRequest},
		"invalid admission": {mutate: func(r *http.Request) {
			r.Body = io.NopCloser(strings.NewReader(`{"email":"a@b.c","password":"x","admission":"enterprise"}`))
		}, code: http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			tenant := &activationTenantProbe{}
			handler := activationServer(tenant, activationIdentity()).Handler()
			request := activationRequest(activationBody)
			test.mutate(request)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.code {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if tenant.calls != 0 {
				t.Fatalf("a guarded-out request reached the owner: %d", tenant.calls)
			}
		})
	}

	// A tenant administrator holds no platform privilege, so the activation is
	// denied before any owner is called.
	identity := activationIdentity()
	denied := activationPlatformDecision(
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONOPERATORACCOUNT,
		api.OwnerEnum_OWNER_ENUM_TENANT,
		api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM,
		"", "provision-authority")
	denied.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
	identity.decisions[api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONOPERATORACCOUNT] = denied
	tenant := &activationTenantProbe{}
	handler := activationServer(tenant, identity).Handler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, activationRequest(activationBody))
	if recorder.Code != http.StatusForbidden || tenant.calls != 0 {
		t.Fatalf("tenant administrator activation status=%d calls=%d", recorder.Code, tenant.calls)
	}
}

// TestOperatorActivationSurfacesTheOwnerRefusal proves the owner's own refusal
// reaches the browser with its typed business code, so a reused idempotency key
// is reported as a conflict instead of a generic failure.
func TestOperatorActivationSurfacesTheOwnerRefusal(t *testing.T) {
	for name, test := range map[string]struct {
		refusal error
		code    int
		typed   string
	}{
		"reused key": {
			refusal: owneridentity.WithErrorCode(status.Error(codes.AlreadyExists, "idempotency key reused"), api.ErrorCodeEnum_ERROR_CODE_ENUM_IDEMPOTENCY_CONFLICT),
			code:    http.StatusConflict, typed: "IDEMPOTENCY_CONFLICT",
		},
		"owner refused": {
			refusal: status.Error(codes.PermissionDenied, "platform administrator authorization required"),
			code:    http.StatusForbidden, typed: "FORBIDDEN",
		},
		"owner unavailable": {
			refusal: status.Error(codes.Unavailable, "gateway unavailable"),
			code:    http.StatusServiceUnavailable, typed: "DEPENDENCY_UNAVAILABLE",
		},
	} {
		t.Run(name, func(t *testing.T) {
			tenant := &activationTenantProbe{provisionErr: test.refusal}
			handler := activationServer(tenant, activationIdentity()).Handler()
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, activationRequest(activationBody))
			if recorder.Code != test.code {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), test.typed) {
				t.Fatalf("owner refusal body=%s", recorder.Body.String())
			}
			if tenant.calls != 1 {
				t.Fatalf("owner calls=%d", tenant.calls)
			}
		})
	}
}

const activationBodyWithoutName = `{"email":"customer@example.com","password":"CorrectHorseBatteryStaple!","admission":"full_cloud_customer"}`

// TestOperatorActivationForwardsAnAbsentNameAsAbsent proves an omitted browser
// name is forwarded as an absent field, not as JSON null: the owner derives the
// Tenant name from the email when no name is given.
func TestOperatorActivationForwardsAnAbsentNameAsAbsent(t *testing.T) {
	tenant := &activationTenantProbe{}
	handler := activationServer(tenant, activationIdentity()).Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, activationRequest(activationBodyWithoutName))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("activation status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if tenant.provision.GetBody().GetName() != "" {
		t.Fatalf("forwarded name=%q", tenant.provision.GetBody().GetName())
	}
}
