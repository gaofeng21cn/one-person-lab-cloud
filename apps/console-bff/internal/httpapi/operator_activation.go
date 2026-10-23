package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// OperatorProvisioner is the retained owner's activation command. It is a
// separate surface from the operator reads so a reader double never has to
// pretend it can write, and so the BFF composes activation only when the
// command is actually configured.
type OperatorProvisioner interface {
	OperatorProvisionAccount(context.Context, string, string, []byte) ([]byte, error)
}

// Activation step names are part of the receipt: they tell the caller exactly
// which step succeeded and which one did not.
const (
	activationStepAccount = "account"
	activationStepTenant  = "tenant"
	activationStepBinding = "billingBinding"
)

// activationReceipt is the single activation answer. It extends the retained
// activation receipt rather than introducing a second endpoint: the retained
// fields keep their names, and the tenant and billing readbacks are added beside
// them. A non-succeeded status is reported with the steps that did complete, so a
// partial activation is visible and can be resumed by re-running the same command.
type activationReceipt struct {
	OperationID              string                   `json:"operationId"`
	AccountID                string                   `json:"accountId"`
	Sub2APIUserID            string                   `json:"sub2apiUserId"`
	Status                   string                   `json:"status"`
	WorkspacePurchaseEnabled bool                     `json:"workspacePurchaseEnabled"`
	TenantID                 string                   `json:"tenantId,omitempty"`
	TenantOperationID        string                   `json:"tenantOperationId,omitempty"`
	BillingBinding           *activationBindingResult `json:"billingBinding,omitempty"`
	FailedStep               string                   `json:"failedStep,omitempty"`
	Steps                    []activationStepResult   `json:"steps"`
}

type activationStepResult struct {
	Step   string `json:"step"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type activationBindingResult struct {
	Status               string `json:"status"`
	BillingSub2APIUserID string `json:"billingSub2ApiUserId"`
	BindingVersion       int64  `json:"bindingVersion,omitempty"`
}

// activationInput is the exact command the retained owner already accepts. The
// BFF validates it before forwarding so a malformed command fails here, and
// re-encodes a canonical body rather than forwarding browser bytes.
type activationInput struct {
	Email     *string `json:"email"`
	Password  *string `json:"password"`
	Name      *string `json:"name,omitempty"`
	Admission *string `json:"admission"`
}

func (s *Server) registerOperatorActivationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v2/operator/accounts", s.operatorActivationHandler)
}

// operatorActivationHandler activates one customer in one tracked operation:
// the account and its Gateway user (retained owner), the Tenant whose owner is
// that Gateway subject (Tenant owner), and the billing binding (Gateway owner).
// Each owner keeps its own authorization; the BFF holds no state and invents no
// fact. The derived tenant and binding keys make a retry converge instead of
// admitting a second Tenant or a second binding.
func (s *Server) operatorActivationHandler(w http.ResponseWriter, r *http.Request) {
	publisherRequestID(r)
	w.Header().Set("Cache-Control", "private, no-store")
	caller, err := RequireSession(r.Context(), s.identity, r)
	if err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	// Activation is a platform operator command. The tenant is removed from the
	// session exactly as the admin publisher routes do, so a tenant administrator
	// cannot inherit platform scope.
	caller.Session = proto.Clone(caller.Session).(*api.Session)
	caller.Session.TenantId = nil
	ctx := WithCaller(r.Context(), caller, r.Header.Get(requestIDHeader))
	if !requirePublisherWriteGuards(w, r, caller, clients.CallContext(ctx)) {
		return
	}
	input, body, ok := decodeActivationInput(w, r)
	if !ok {
		return
	}
	if s.operatorProvisioner == nil || s.tenant == nil || s.gatewayCoordination == nil {
		writePublisherError(w, r, 503, "owner_unconfigured", "activation owners are not configured")
		return
	}
	requestID := r.Header.Get(requestIDHeader)
	if err := RequireAuthorizedAction(ctx, s.identity, caller, owneridentity.Capability,
		operatorPrivilegeAction,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG},
		requestID); err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}

	// Step 1: the account and its Gateway user. The retained owner keeps its own
	// session, CSRF and duplicate rules; the caller's idempotency key is forwarded
	// so the owner's operation id is stable across a retry.
	raw, err := s.operatorProvisioner.OperatorProvisionAccount(ctx, caller.SessionID, clients.CallContext(ctx).GetIdempotencyKey(), body)
	if err != nil {
		writeActivationFailure(w, activationReceipt{
			Status:     "account_failed",
			FailedStep: activationStepAccount,
			Steps:      []activationStepResult{{Step: activationStepAccount, Status: "failed", Detail: activationErrorDetail(err)}},
		}, activationStatusFor(err))
		return
	}
	receipt, ok := decodeRetainedActivation(w, r, raw)
	if !ok {
		return
	}
	if receipt.Status != "succeeded" || receipt.AccountID == "" || receipt.Sub2APIUserID == "" {
		writeActivationFailure(w, activationReceipt{
			Status:     "account_failed",
			FailedStep: activationStepAccount,
			Steps:      []activationStepResult{{Step: activationStepAccount, Status: "failed", Detail: "owner did not confirm the account and its Gateway subject"}},
		}, http.StatusBadGateway)
		return
	}
	steps := []activationStepResult{{Step: activationStepAccount, Status: "succeeded"}}

	// Step 2: the Tenant whose owner is the activated Gateway subject. The
	// idempotency key is derived from the account, not from the browser request, so
	// a retried activation with a new browser key still replays the same Tenant.
	tenantName := activationTenantName(input)
	tenantCall := activationCall(caller, requestID, platformScope(), activationTenantKey(receipt.AccountID))
	tenantCtx := clients.WithCallContext(r.Context(), tenantCall)
	tenantCaller := Caller{Session: platformSession(caller.Session), SessionID: caller.SessionID}
	if err := RequireAuthorizedAction(tenantCtx, s.identity, tenantCaller, owneridentity.Tenant,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATETENANT,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT},
		requestID); err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	operation, err := s.tenant.CreateTenant(tenantCtx, &api.CreateTenantRpcRequest{
		Context: tenantCall,
		Body: &api.CreateTenantRequest{
			Name: tenantName, OwnerGatewaySubjectId: receipt.Sub2APIUserID,
			BillingSub2ApiUserId: receipt.Sub2APIUserID, OwnerEmail: stringValueOf(input.Email),
		},
	})
	if err != nil {
		steps = append(steps, activationStepResult{Step: activationStepTenant, Status: "failed", Detail: activationErrorDetail(err)})
		writeActivationFailure(w, activationReceipt{
			OperationID: receipt.OperationID, AccountID: receipt.AccountID, Sub2APIUserID: receipt.Sub2APIUserID,
			Status: "tenant_failed", WorkspacePurchaseEnabled: receipt.WorkspacePurchaseEnabled,
			FailedStep: activationStepTenant, Steps: steps,
		}, http.StatusBadGateway)
		return
	}
	if operation.GetResourceId() == "" {
		steps = append(steps, activationStepResult{Step: activationStepTenant, Status: "failed", Detail: "tenant owner returned no tenant id"})
		writeActivationFailure(w, activationReceipt{
			OperationID: receipt.OperationID, AccountID: receipt.AccountID, Sub2APIUserID: receipt.Sub2APIUserID,
			Status: "tenant_failed", WorkspacePurchaseEnabled: receipt.WorkspacePurchaseEnabled,
			FailedStep: activationStepTenant, Steps: steps,
		}, http.StatusBadGateway)
		return
	}
	tenantID := operation.GetResourceId()
	steps = append(steps, activationStepResult{Step: activationStepTenant, Status: "succeeded"})
	receipt.TenantID = tenantID
	receipt.TenantOperationID = operation.GetOperationId()

	// Step 3: the billing binding. The Tenant readback reports the active binding,
	// so a resumed activation binds once and a re-run reports the existing binding
	// instead of attempting a second version.
	binding, err := s.bindActivatedTenant(r, caller, requestID, tenantID, receipt.Sub2APIUserID)
	if err != nil {
		steps = append(steps, activationStepResult{Step: activationStepBinding, Status: "failed", Detail: activationErrorDetail(err)})
		receipt.Status = "billing_binding_failed"
		receipt.FailedStep = activationStepBinding
		receipt.Steps = steps
		writeActivationFailure(w, receipt, http.StatusBadGateway)
		return
	}
	steps = append(steps, activationStepResult{Step: activationStepBinding, Status: binding.Status})
	receipt.BillingBinding = binding
	receipt.Status = "succeeded"
	receipt.Steps = steps
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(receipt)
}

// bindActivatedTenant binds the activated subject to the Tenant's billing. The
// binding identity is the derived command key, so the Gateway owner replays the
// binding this activation already made instead of attempting a second one; the
// BFF holds no binding state and reads no Tenant it is not a member of.
func (s *Server) bindActivatedTenant(r *http.Request, caller Caller, requestID, tenantID, subject string) (*activationBindingResult, error) {
	bindCall := activationCall(caller, requestID, tenantScopeOf(tenantID), activationBindingKey(tenantID))
	bindCtx := clients.WithCallContext(r.Context(), bindCall)
	tenantCaller := Caller{Session: tenantSession(caller.Session, tenantID), SessionID: caller.SessionID}
	if err := RequireAuthorizedAction(bindCtx, s.identity, tenantCaller, owneridentity.Tenant,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDTENANTWALLET,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: proto.String(tenantID)},
		requestID); err != nil {
		return nil, err
	}
	readback, err := s.gatewayCoordination.BindWallet(bindCtx, &api.WalletBindingCommand{
		Context: bindCall, TargetTenantId: tenantID, BillingSub2ApiUserId: subject,
		ExpectedBindingVersion: 0, AuthorizationReceiptId: bindCall.GetAuthorizationContextId(),
	})
	if err != nil {
		return nil, err
	}
	if readback.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED {
		return nil, status.Error(codes.Unavailable, "billing binding was not confirmed")
	}
	return &activationBindingResult{Status: "succeeded", BillingSub2APIUserID: readback.GetBillingSub2ApiUserId(), BindingVersion: readback.GetBindingVersion()}, nil
}

func decodeActivationInput(w http.ResponseWriter, r *http.Request) (activationInput, []byte, bool) {
	media := strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])
	if media != "application/json" {
		writePublisherError(w, r, 415, "json_required", "application/json required")
		return activationInput{}, nil, false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		writePublisherError(w, r, 400, "invalid_request", "invalid or oversized request body")
		return activationInput{}, nil, false
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var input activationInput
	if err := decoder.Decode(&input); err != nil {
		writePublisherError(w, r, 400, "invalid_request", "invalid or oversized request body")
		return activationInput{}, nil, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writePublisherError(w, r, 400, "invalid_request", "invalid or oversized request body")
		return activationInput{}, nil, false
	}
	if input.Email == nil || input.Password == nil || *input.Password == "" || strings.TrimSpace(*input.Email) == "" {
		writePublisherError(w, r, 400, "invalid_request", "email and password are required")
		return activationInput{}, nil, false
	}
	if input.Name != nil && (*input.Name == "" || *input.Name != strings.TrimSpace(*input.Name) || len([]rune(*input.Name)) > 100) {
		writePublisherError(w, r, 400, "invalid_request", "name must be a trimmed non-empty value of at most 100 characters")
		return activationInput{}, nil, false
	}
	if input.Admission != nil && *input.Admission != "full_cloud_customer" && *input.Admission != "gateway_only" {
		writePublisherError(w, r, 400, "invalid_request", "invalid admission")
		return activationInput{}, nil, false
	}
	body, err := json.Marshal(input)
	if err != nil {
		writePublisherError(w, r, 400, "invalid_request", "invalid request body")
		return activationInput{}, nil, false
	}
	return input, body, true
}

func decodeRetainedActivation(w http.ResponseWriter, r *http.Request, raw []byte) (activationReceipt, bool) {
	var retained struct {
		OperationID              string `json:"operationId"`
		AccountID                string `json:"accountId"`
		Sub2APIUserID            string `json:"sub2apiUserId"`
		Status                   string `json:"status"`
		WorkspacePurchaseEnabled bool   `json:"workspacePurchaseEnabled"`
	}
	if json.Unmarshal(raw, &retained) != nil {
		writeActivationFailure(w, activationReceipt{
			Status:     "account_failed",
			FailedStep: activationStepAccount,
			Steps:      []activationStepResult{{Step: activationStepAccount, Status: "failed", Detail: "owner returned an invalid receipt"}},
		}, http.StatusBadGateway)
		return activationReceipt{}, false
	}
	return activationReceipt{
		OperationID: retained.OperationID, AccountID: retained.AccountID, Sub2APIUserID: retained.Sub2APIUserID,
		Status: retained.Status, WorkspacePurchaseEnabled: retained.WorkspacePurchaseEnabled,
	}, true
}

func writeActivationFailure(w http.ResponseWriter, receipt activationReceipt, httpStatus int) {
	if receipt.Steps == nil {
		receipt.Steps = []activationStepResult{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(receipt)
}

// activationStatusFor maps an owner transport category onto the activation
// status. A refused caller keeps its refusal; an owner failure is a gateway
// failure, never a success.
func activationStatusFor(err error) int {
	switch status.Code(err) {
	case codes.InvalidArgument:
		return http.StatusBadRequest
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.NotFound, codes.FailedPrecondition:
		return http.StatusConflict
	default:
		return http.StatusBadGateway
	}
}

func activationErrorDetail(err error) string {
	if message := status.Convert(err).Message(); message != "" {
		return message
	}
	return "owner call failed"
}

// activationCall is the platform-scoped call the operator's own session already
// carries. The request id and the session are the operator's; only the scope and
// the derived idempotency key are set here.
func activationCall(caller Caller, requestID string, scope *api.AuthorizationScope, key string) *api.CallContext {
	call := &api.CallContext{
		ActorId: caller.Session.GetActorId(), SessionId: proto.String(caller.SessionID),
		Scope: scope, RequestId: requestID, DeadlineAt: timestamppb.New(time.Now().Add(30 * time.Second)),
	}
	if key != "" {
		call.IdempotencyKey = key
	}
	return call
}

func platformSession(session *api.Session) *api.Session {
	out := proto.Clone(session).(*api.Session)
	out.TenantId = nil
	return out
}

func tenantSession(session *api.Session, tenantID string) *api.Session {
	out := proto.Clone(session).(*api.Session)
	out.TenantId = proto.String(tenantID)
	return out
}

func platformScope() *api.AuthorizationScope {
	return &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
}

func tenantScopeOf(tenantID string) *api.AuthorizationScope {
	return &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenantID}}}
}

// activationTenantKey and activationBindingKey derive the owner command identity
// from the account and tenant rather than from a browser-generated key, so a
// retried activation that mints a new browser key still converges on one Tenant
// and one binding.
func activationTenantKey(accountID string) string { return "activation-tenant:" + accountID }

func activationBindingKey(tenantID string) string {
	return "activation-binding:" + tenantID
}

// activationTenantName uses the operator's own name when supplied and otherwise
// the account's email local part. The Tenant name is presentation only; ownership
// is the Gateway subject.
func activationTenantName(input activationInput) string {
	if input.Name != nil {
		return *input.Name
	}
	email := stringValueOf(input.Email)
	name := email
	if at := strings.IndexByte(email, '@'); at > 0 {
		name = email[:at]
	}
	if runes := []rune(strings.TrimSpace(name)); len(runes) > 100 {
		name = string(runes[:100])
	}
	return name
}

func stringValueOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
