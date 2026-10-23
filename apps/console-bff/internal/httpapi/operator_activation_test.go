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

type activationProvisionProbe struct {
	receipt []byte
	refusal error
	session string
	key     string
	body    []byte
	calls   int
}

func (p *activationProvisionProbe) OperatorProvisionAccount(_ context.Context, session, key string, body []byte) ([]byte, error) {
	p.calls++
	p.session, p.key, p.body = session, key, body
	if p.refusal != nil {
		return nil, p.refusal
	}
	return p.receipt, nil
}

// activationTenantProbe answers the Tenant owner's two calls: admission, and the
// billing readback the activation uses to decide whether it still has to bind.
type activationTenantProbe struct {
	api.TenantProductServiceClient
	create    *api.CreateTenantRpcRequest
	createErr error
}

func (p *activationTenantProbe) CreateTenant(_ context.Context, r *api.CreateTenantRpcRequest, _ ...grpc.CallOption) (*api.Operation, error) {
	p.create = r
	if p.createErr != nil {
		return nil, p.createErr
	}
	return &api.Operation{
		OperationId: "op-tenant-1", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT,
		Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_CREATE_TENANT, ResourceId: "tenant-created-1",
		Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_SUCCEEDED,
		RequestId: r.GetContext().GetRequestId(),
	}, nil
}

type activationBindingProbe struct {
	api.GatewayCoordinationClient
	received *api.WalletBindingCommand
	calls    int
	refusal  error
}

func (p *activationBindingProbe) BindWallet(_ context.Context, r *api.WalletBindingCommand, _ ...grpc.CallOption) (*api.WalletBindingReadback, error) {
	p.calls++
	p.received = r
	if p.refusal != nil {
		return nil, p.refusal
	}
	return &api.WalletBindingReadback{TenantId: r.GetTargetTenantId(), BillingSub2ApiUserId: r.GetBillingSub2ApiUserId(),
		BindingVersion: 1, Outcome: api.Observation_OBSERVATION_CONFIRMED, AuthorizationReceiptId: r.GetAuthorizationReceiptId()}, nil
}

// activationIdentity pins one decision per action so each composed owner check is
// exercised with the scope, audience and resource that owner requires.
func activationIdentity() *fakeIdentity {
	return &fakeIdentity{
		session: &api.Session{ActorId: "admin-1", CsrfToken: "csrf-admin"},
		decisions: map[api.AuthorizationActionEnum]*api.AuthorizationDecision{
			operatorPrivilegeAction: activationPlatformDecision(operatorPrivilegeAction, api.OwnerEnum_OWNER_ENUM_CAPABILITY, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, "", "operator-authority"),
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATETENANT:     activationPlatformDecision(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATETENANT, api.OwnerEnum_OWNER_ENUM_TENANT, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "", "tenant-authority"),
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETTENANT:        activationTenantDecision(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETTENANT, "tenant-read-authority"),
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDTENANTWALLET: activationTenantDecision(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDTENANTWALLET, "binding-authority"),
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

func activationTenantDecision(action api.AuthorizationActionEnum, contextID string) *api.AuthorizationDecision {
	resource := &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: proto.String("tenant-created-1")}
	return &api.AuthorizationDecision{
		ActorId: "admin-1", SessionId: ptr(owneridentity.SessionReference("session-admin")),
		Scope:             &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-created-1"}}},
		PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Minute)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
		Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED, Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY,
		Action: action, AudienceOwner: api.OwnerEnum_OWNER_ENUM_TENANT, Resource: resource, AuthorizationContextId: proto.String(contextID),
	}
}

func activationServer(provision *activationProvisionProbe, tenant *activationTenantProbe, binding *activationBindingProbe, identity IdentityReader) *Server {
	return &Server{identity: identity, operatorProvisioner: provision, tenant: tenant, gatewayCoordination: binding}
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

func activationReceiptOf(t *testing.T, recorder *httptest.ResponseRecorder) activationReceipt {
	t.Helper()
	var receipt activationReceipt
	if err := json.Unmarshal(recorder.Body.Bytes(), &receipt); err != nil {
		t.Fatalf("activation receipt %s: %v", recorder.Body.String(), err)
	}
	return receipt
}

// TestOperatorActivationComposesAccountTenantAndBinding proves the one activation
// operation activates the account, admits the Tenant whose owner is the activated
// Gateway subject, binds that subject to the Tenant's billing, and returns one
// receipt that reads all three back.
func TestOperatorActivationComposesAccountTenantAndBinding(t *testing.T) {
	provision := &activationProvisionProbe{receipt: []byte(`{"operationId":"account-provision-1","accountId":"acct-1","sub2apiUserId":"77","status":"succeeded","workspacePurchaseEnabled":true}`)}
	tenant := &activationTenantProbe{}
	binding := &activationBindingProbe{}
	handler := activationServer(provision, tenant, binding, activationIdentity()).Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, activationRequest(activationBody))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("activation status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	receipt := activationReceiptOf(t, recorder)
	if receipt.Status != "succeeded" || receipt.AccountID != "acct-1" || receipt.Sub2APIUserID != "77" ||
		receipt.TenantID != "tenant-created-1" || receipt.TenantOperationID != "op-tenant-1" ||
		receipt.BillingBinding == nil || receipt.BillingBinding.Status != "succeeded" ||
		receipt.BillingBinding.BillingSub2APIUserID != "77" || receipt.BillingBinding.BindingVersion != 1 {
		t.Fatalf("activation receipt=%+v", receipt)
	}
	if len(receipt.Steps) != 3 || receipt.Steps[0].Step != activationStepAccount || receipt.Steps[2].Step != activationStepBinding {
		t.Fatalf("activation steps=%+v", receipt.Steps)
	}

	// The retained owner receives the operator's own session and idempotency key,
	// and a canonical command body rather than browser bytes.
	if !strings.HasPrefix(provision.session, "session_") || provision.key != "activation-1" {
		t.Fatalf("retained activation call session=%q key=%q", provision.session, provision.key)
	}
	if !strings.Contains(string(provision.body), `"email":"customer@example.com"`) || strings.Contains(string(provision.body), "unknown") {
		t.Fatalf("retained activation body=%s", provision.body)
	}

	// The Tenant owner is called with the activated Gateway subject as its owner,
	// under the platform scope and a key derived from the account, so a retry with a
	// new browser key replays this same Tenant.
	call := tenant.create.GetContext()
	if tenant.create.GetBody().GetOwnerGatewaySubjectId() != "77" || tenant.create.GetBody().GetName() != "Customer One" ||
		tenant.create.GetBody().GetOwnerEmail() != "customer@example.com" || tenant.create.GetBody().GetBillingSub2ApiUserId() != "77" {
		t.Fatalf("tenant admission body=%+v", tenant.create.GetBody())
	}
	if call.GetScope().GetPlatform() == nil || call.GetIdempotencyKey() != "activation-tenant:acct-1" || call.GetActorId() != "admin-1" || !strings.HasPrefix(call.GetSessionId(), "session_") {
		t.Fatalf("tenant admission context=%+v", call)
	}
	if call.GetAuthorizationContextId() != "tenant-authority" {
		t.Fatalf("tenant admission authority=%q", call.GetAuthorizationContextId())
	}

	// The billing binding targets the Tenant the owner just returned and carries the
	// Tenant owner's own authorization receipt.
	command := binding.received
	if command.GetTargetTenantId() != "tenant-created-1" || command.GetBillingSub2ApiUserId() != "77" || command.GetExpectedBindingVersion() != 0 {
		t.Fatalf("binding command=%+v", command)
	}
	if command.GetAuthorizationReceiptId() != "binding-authority" || command.GetContext().GetScope().GetTenant().GetTenantId() != "tenant-created-1" {
		t.Fatalf("binding context=%+v receipt=%q", command.GetContext(), command.GetAuthorizationReceiptId())
	}
}

// TestOperatorActivationReRunReusesTheSameBindingKey proves a resumed activation
// derives the same binding command key, which is what makes the Gateway owner
// replay the binding it already made instead of admitting a second one.
func TestOperatorActivationReRunReusesTheSameBindingKey(t *testing.T) {
	provision := &activationProvisionProbe{receipt: []byte(`{"operationId":"account-provision-1","accountId":"acct-1","sub2apiUserId":"77","status":"succeeded","workspacePurchaseEnabled":true}`)}
	tenant := &activationTenantProbe{}
	keys := []string{}
	for _, browserKey := range []string{"activation-first", "activation-second"} {
		binding := &activationBindingProbe{}
		handler := activationServer(provision, tenant, binding, activationIdentity()).Handler()
		request := activationRequest(activationBody)
		request.Header.Set("Idempotency-Key", browserKey)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("activation status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		keys = append(keys, binding.received.GetContext().GetIdempotencyKey())
	}
	if keys[0] != keys[1] || keys[0] == "" {
		t.Fatalf("binding key depended on the browser key: %v", keys)
	}
}

// TestOperatorActivationReportsTheFailedStep proves a step failure is reported with
// what already succeeded, is never labelled succeeded, and does not proceed to the
// steps behind it.
func TestOperatorActivationReportsTheFailedStep(t *testing.T) {
	for name, test := range map[string]struct {
		provision *activationProvisionProbe
		tenant    *activationTenantProbe
		binding   *activationBindingProbe
		status    string
		failed    string
		code      int
		bindCalls int
	}{
		"account refused": {
			provision: &activationProvisionProbe{refusal: status.Error(codes.PermissionDenied, "operator refused")},
			tenant:    &activationTenantProbe{}, binding: &activationBindingProbe{},
			status: "account_failed", failed: activationStepAccount, code: http.StatusForbidden,
		},
		"tenant failed": {
			provision: &activationProvisionProbe{receipt: []byte(`{"operationId":"account-provision-1","accountId":"acct-1","sub2apiUserId":"77","status":"succeeded","workspacePurchaseEnabled":true}`)},
			tenant:    &activationTenantProbe{createErr: status.Error(codes.PermissionDenied, "tenant owner refused")},
			binding:   &activationBindingProbe{},
			status:    "tenant_failed", failed: activationStepTenant, code: http.StatusBadGateway,
		},
		"binding failed": {
			provision: &activationProvisionProbe{receipt: []byte(`{"operationId":"account-provision-1","accountId":"acct-1","sub2apiUserId":"77","status":"succeeded","workspacePurchaseEnabled":true}`)},
			tenant:    &activationTenantProbe{},
			binding:   &activationBindingProbe{refusal: status.Error(codes.Unavailable, "billing owner unavailable")},
			status:    "billing_binding_failed", failed: activationStepBinding, code: http.StatusBadGateway,
		},
	} {
		t.Run(name, func(t *testing.T) {
			handler := activationServer(test.provision, test.tenant, test.binding, activationIdentity()).Handler()
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, activationRequest(activationBody))
			if recorder.Code != test.code {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			receipt := activationReceiptOf(t, recorder)
			if receipt.Status != test.status || receipt.FailedStep != test.failed {
				t.Fatalf("failure receipt=%+v", receipt)
			}
			if receipt.Status == "succeeded" {
				t.Fatalf("failed activation reported success: %s", recorder.Body.String())
			}
			if test.failed == activationStepAccount && len(receipt.Steps) != 1 {
				t.Fatalf("account failure reported later steps: %+v", receipt.Steps)
			}
			if test.failed == activationStepTenant {
				if len(receipt.Steps) != 2 || receipt.Steps[0].Status != "succeeded" || receipt.Steps[1].Status != "failed" {
					t.Fatalf("tenant failure steps=%+v", receipt.Steps)
				}
				if receipt.AccountID != "acct-1" || receipt.Sub2APIUserID != "77" || receipt.TenantID != "" {
					t.Fatalf("tenant failure receipt=%+v", receipt)
				}
			}
			if test.failed == activationStepBinding {
				if receipt.TenantID != "tenant-created-1" || len(receipt.Steps) != 3 {
					t.Fatalf("binding failure receipt=%+v", receipt)
				}
			}
			if test.failed != activationStepBinding && test.binding.calls != 0 {
				t.Fatalf("a failed step still bound the wallet: %d", test.binding.calls)
			}
		})
	}
}

// TestOperatorActivationKeepsItsWriteAndOperatorGuards proves the composed command
// is not a new unguarded path: it keeps the shared CSRF, origin and idempotency
// guards and the existing platform-operator privilege.
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
	} {
		t.Run(name, func(t *testing.T) {
			provision := &activationProvisionProbe{receipt: []byte(`{"operationId":"account-provision-1","accountId":"acct-1","sub2apiUserId":"77","status":"succeeded"}`)}
			handler := activationServer(provision, &activationTenantProbe{}, &activationBindingProbe{}, activationIdentity()).Handler()
			request := activationRequest(activationBody)
			test.mutate(request)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.code {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if provision.calls != 0 {
				t.Fatalf("a guarded-out request reached the retained owner")
			}
		})
	}

	// A tenant administrator holds no platform privilege, so activation is denied
	// before any owner is called.
	identity := activationIdentity()
	identity.decisions[operatorPrivilegeAction] = &api.AuthorizationDecision{
		ActorId: "admin-1", SessionId: ptr(owneridentity.SessionReference("session-admin")),
		Scope:             &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-1"}}},
		PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Minute)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
		Result: api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED, Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY,
		Action: operatorPrivilegeAction, AudienceOwner: api.OwnerEnum_OWNER_ENUM_CAPABILITY,
		Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG},
	}
	provision := &activationProvisionProbe{}
	handler := activationServer(provision, &activationTenantProbe{}, &activationBindingProbe{}, identity).Handler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, activationRequest(activationBody))
	if recorder.Code != http.StatusForbidden || provision.calls != 0 {
		t.Fatalf("tenant administrator activation status=%d calls=%d", recorder.Code, provision.calls)
	}
}

// TestOperatorActivationReportsAVersionConflict proves a real version conflict
// (another binding already owns the Tenant) is reported as a failed step, never as
// a successful activation.
func TestOperatorActivationReportsAVersionConflict(t *testing.T) {
	provision := &activationProvisionProbe{receipt: []byte(`{"operationId":"account-provision-1","accountId":"acct-1","sub2apiUserId":"77","status":"succeeded","workspacePurchaseEnabled":true}`)}
	binding := &activationBindingProbe{refusal: status.Error(codes.Aborted, "wallet binding version conflict")}
	handler := activationServer(provision, &activationTenantProbe{}, binding, activationIdentity()).Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, activationRequest(activationBody))
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("conflict status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	receipt := activationReceiptOf(t, recorder)
	if receipt.Status != "billing_binding_failed" || receipt.FailedStep != activationStepBinding || receipt.TenantID != "tenant-created-1" {
		t.Fatalf("conflict receipt=%+v", receipt)
	}
}

// TestOperatorActivationUsesTheAccountForTheTenantKey proves the derived key does
// not depend on the browser idempotency key, so a retried activation that mints a
// new browser key still addresses the same Tenant operation.
func TestOperatorActivationUsesTheAccountForTheTenantKey(t *testing.T) {
	keys := []string{}
	for _, browserKey := range []string{"activation-first", "activation-second"} {
		provision := &activationProvisionProbe{receipt: []byte(`{"operationId":"account-provision-1","accountId":"acct-1","sub2apiUserId":"77","status":"succeeded","workspacePurchaseEnabled":true}`)}
		tenant := &activationTenantProbe{}
		handler := activationServer(provision, tenant, &activationBindingProbe{}, activationIdentity()).Handler()
		request := activationRequest(activationBody)
		request.Header.Set("Idempotency-Key", browserKey)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("activation status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		keys = append(keys, tenant.create.GetContext().GetIdempotencyKey())
		if provision.key != browserKey {
			t.Fatalf("retained owner did not receive the caller key: %q", provision.key)
		}
	}
	if keys[0] != keys[1] {
		t.Fatalf("tenant key depended on the browser key: %v", keys)
	}
}

const activationBodyWithoutName = `{"email":"customer@example.com","password":"CorrectHorseBatteryStaple!","admission":"full_cloud_customer"}`

// TestOperatorActivationForwardsAnAbsentNameAsAbsent proves an omitted browser
// name is forwarded as an absent field, not as JSON null. The retained owner
// accepts a missing name (it derives the Tenant name from the email) and rejects
// a non-string, so re-encoding the canonical body must preserve absence.
func TestOperatorActivationForwardsAnAbsentNameAsAbsent(t *testing.T) {
	provision := &activationProvisionProbe{receipt: []byte(`{"operationId":"account-provision-1","accountId":"acct-1","sub2apiUserId":"77","status":"succeeded","workspacePurchaseEnabled":true}`)}
	tenant := &activationTenantProbe{}
	handler := activationServer(provision, tenant, &activationBindingProbe{}, activationIdentity()).Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, activationRequest(activationBodyWithoutName))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("activation status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	var forwarded map[string]json.RawMessage
	if err := json.Unmarshal(provision.body, &forwarded); err != nil {
		t.Fatalf("forwarded body %s: %v", provision.body, err)
	}
	if raw, exists := forwarded["name"]; exists {
		t.Fatalf("forwarded body carries name=%s: %s", raw, provision.body)
	}
	if tenant.create.GetBody().GetName() != "customer" {
		t.Fatalf("tenant name=%q body=%+v", tenant.create.GetBody().GetName(), tenant.create.GetBody())
	}
}
