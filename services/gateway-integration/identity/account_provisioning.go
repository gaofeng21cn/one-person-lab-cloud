package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerstore"
)

// provisionAccountOperation is the API operation name of the account-opening
// command. The contract defines no separate operation kind for it, so the
// command is registered in this owner's own idempotency_records under its API
// operation name, exactly as that table requires.
const provisionAccountOperation = "ProvisionOperatorAccount"

// provisionAccountKind is the owner operation kind of one account-opening
// command. It resolves through the contract's Operation kind vocabulary.
const provisionAccountKind = "provision_account"

// The admission a customer account may be opened with. A full Cloud customer
// may buy Workspaces; a Gateway-only customer may not.
const (
	accountAdmissionFullCloudCustomer = "full_cloud_customer"
	accountAdmissionGatewayOnly       = "gateway_only"
)

const (
	maxAccountEmailRunes    = 254
	maxAccountNameRunes     = 100
	maxAccountPasswordRunes = 200
	// accountPollSeconds is the cadence the owner reports for a command whose
	// external outcome is not confirmed yet.
	accountPollSeconds = 5
)

// ErrProvisionAccountConflict reports one idempotency key reused for a
// different account-opening command.
var ErrProvisionAccountConflict = errors.New("account provisioning command differs from its original execution")

// errProvisionAccountUnconfirmed reports that the external Gateway account
// write was sent but its outcome could not be confirmed. The owner never sends
// that write twice: it converges by reading the Gateway account back.
var errProvisionAccountUnconfirmed = errors.New("gateway account outcome is unconfirmed")

// provisionAccountInput is the canonical command body. The password is the
// customer's own Gateway credential: it is used once to create the Gateway
// account and is never stored, echoed or recorded.
type provisionAccountInput struct {
	Email     string
	Password  string
	Name      string
	Admission string
}

// provisionAccountState is the durable answer of one account-opening command.
// It carries opaque identities only: the account id, the Gateway subject, the
// Tenant id and the recorded stage.
type provisionAccountState struct {
	Outcome     string `json:"outcome"`
	OperationID string `json:"operationId"`
	RequestID   string `json:"requestId"`
	AccountID   string `json:"accountId,omitempty"`
	ActorID     string `json:"actorId,omitempty"`
	TenantID    string `json:"tenantId,omitempty"`
	Stage       string `json:"stage,omitempty"`
	// Dispatched records that the external Gateway account write was already
	// sent. While it is true the owner never sends it again.
	Dispatched bool `json:"dispatched"`
	// WorkspacePurchaseEnabled is the eligibility the owner granted the opened
	// account. It is the owner's own policy fact, reported back so the console
	// reads the policy it actually applied.
	WorkspacePurchaseEnabled bool   `json:"workspacePurchaseEnabled"`
	ErrorCode                string `json:"errorCode,omitempty"`
}

// provisionAccountReservation is the immutable identity of one original
// account-opening command.
type provisionAccountReservation struct {
	ActorID        string
	IdempotencyKey string
	Fingerprint    string
	RequestID      string
	Email          string
}

// customerAccountRow is the Cloud account fact this owner persists.
type customerAccountRow struct {
	ID          string
	ActorID     string
	Email       string
	DisplayName string
	Admission   string
	Status      string
	OpenedAt    time.Time
}

// ProvisionOperatorAccount opens one customer account: it creates the Gateway
// account, admits the single-tenant Workspace owner that account holds, grants
// its Workspace purchase eligibility and binds the Gateway wallet.
//
// The whole command is one owner operation with one persistent record and one
// idempotency key. A replay of the same command returns the same command
// receipt and converges on one account, one Tenant and one wallet binding:
// every step is separately idempotent and the external Gateway write is
// dispatched at most once.
func (s *Service) ProvisionOperatorAccount(ctx context.Context, r *api.ProvisionOperatorAccountRpcRequest) (*api.OperatorAccountCommand, error) {
	if e := peer(ctx, owneridentity.ConsoleBFF); e != nil {
		return nil, e
	}
	c := r.GetContext()
	if c == nil || c.GetScope().GetPlatform() == nil || strings.TrimSpace(c.GetActorId()) == "" || c.GetSessionId() == "" || strings.TrimSpace(c.GetRequestId()) == "" {
		return nil, denied()
	}
	key := strings.TrimSpace(c.GetIdempotencyKey())
	if key == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency key is required")
	}
	decision, e := s.AuthorizeAction(ctx, &api.AuthorizationRequest{
		Scope:         c.GetScope(),
		ActorId:       c.GetActorId(),
		SessionId:     c.SessionId,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_TENANT,
		Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONOPERATORACCOUNT,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM},
		RequestId:     c.GetRequestId(),
	})
	if e != nil {
		return nil, e
	}
	if decision.GetResult() != api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED {
		return nil, status.Error(codes.PermissionDenied, "platform administrator authorization required")
	}
	input, e := canonicalProvisionAccountInput(r.GetBody())
	if e != nil {
		return nil, e
	}
	reservation := provisionAccountReservation{
		ActorID:        c.GetActorId(),
		IdempotencyKey: key,
		Fingerprint:    s.mac(provisionAccountFingerprint(input)),
		RequestID:      c.GetRequestId(),
		Email:          input.Email,
	}
	command := &provisionAccountCommand{service: s, reservation: reservation, input: input, call: c, authorizationReceipt: decision.GetAuthorizationContextId()}
	state, found, e := s.reserveProvisionAccount(ctx, reservation)
	if errors.Is(e, ErrProvisionAccountConflict) {
		return nil, owneridentity.WithErrorCode(status.Error(codes.AlreadyExists, "idempotency key reused with a different request"), api.ErrorCodeEnum_ERROR_CODE_ENUM_IDEMPOTENCY_CONFLICT)
	}
	if e != nil {
		return nil, e
	}
	command.state = state
	if found {
		switch state.Outcome {
		case "succeeded":
			return command.receipt(), nil
		case "failed":
			return nil, provisionAccountFailure(state)
		}
		// A pending answer is the original command's open state. The command
		// continues from the owner's own facts; it never sends the external
		// account write a second time.
	}
	return command.run(ctx)
}

// provisionAccountFingerprint fixes the immutable identity of one
// account-opening command. The password is part of the command, so the
// fingerprint is keyed with the owner's own secret: the stored value proves a
// replay without ever exposing the credential.
func provisionAccountFingerprint(input provisionAccountInput) string {
	return strings.Join([]string{input.Email, input.Password, input.Name, input.Admission}, "\x00")
}

// canonicalProvisionAccountInput validates and normalizes one command body.
func canonicalProvisionAccountInput(body *api.ProvisionAccountRequest) (provisionAccountInput, error) {
	if body == nil {
		return provisionAccountInput{}, status.Error(codes.InvalidArgument, "an account request is required")
	}
	email := strings.ToLower(strings.TrimSpace(body.GetEmail()))
	if email == "" || utf8.RuneCountInString(email) > maxAccountEmailRunes || !strings.Contains(email, "@") {
		return provisionAccountInput{}, status.Error(codes.InvalidArgument, "a valid email is required")
	}
	password := body.GetPassword()
	if password == "" || utf8.RuneCountInString(password) > maxAccountPasswordRunes {
		return provisionAccountInput{}, status.Error(codes.InvalidArgument, "a password of at most 200 characters is required")
	}
	name := strings.TrimSpace(body.GetName())
	if name != "" && utf8.RuneCountInString(name) > maxAccountNameRunes {
		return provisionAccountInput{}, status.Error(codes.InvalidArgument, "name must be at most 100 characters")
	}
	admission := accountAdmissionFullCloudCustomer
	switch body.GetAdmission() {
	case api.ProvisionAccountRequestAdmissionEnum_PROVISION_ACCOUNT_REQUEST_ADMISSION_ENUM_UNSPECIFIED:
	case api.ProvisionAccountRequestAdmissionEnum_PROVISION_ACCOUNT_REQUEST_ADMISSION_ENUM_FULL_CLOUD_CUSTOMER:
	case api.ProvisionAccountRequestAdmissionEnum_PROVISION_ACCOUNT_REQUEST_ADMISSION_ENUM_GATEWAY_ONLY:
		admission = accountAdmissionGatewayOnly
	default:
		return provisionAccountInput{}, status.Error(codes.InvalidArgument, "an invalid admission")
	}
	return provisionAccountInput{Email: email, Password: password, Name: name, Admission: admission}, nil
}

// provisionAccountCommand is one account-opening command in flight.
type provisionAccountCommand struct {
	service     *Service
	reservation provisionAccountReservation
	input       provisionAccountInput
	call        *api.CallContext
	state       provisionAccountState
	// authorizationReceipt is the owner's own authorization decision for this
	// command. It is the delegation evidence the wallet binding records.
	authorizationReceipt string
}

// run performs the command's steps in order. Every step is idempotent, so a
// replay after a partial failure converges instead of duplicating an effect.
func (c *provisionAccountCommand) run(ctx context.Context) (*api.OperatorAccountCommand, error) {
	if e := c.service.writeProvisionAccountOperation(ctx, c); e != nil {
		return nil, e
	}
	account, e := c.resolveAccount(ctx)
	if e != nil {
		if errors.Is(e, errProvisionAccountUnconfirmed) {
			// The external outcome is unknown. The command stays open and the
			// caller reads it back; the write is never sent again.
			return c.settle(ctx, "awaiting_confirmation", "unknown", "")
		}
		return nil, e
	}
	c.state.AccountID = account.ID
	c.state.ActorID = account.ActorID

	if e := c.service.updateProvisionAccountState(ctx, c.reservation, c.state); e != nil {
		return nil, e
	}
	tenantID, e := c.admitTenant(ctx, account)
	if e != nil {
		return nil, c.fail(ctx, e)
	}
	c.state.TenantID = tenantID
	if e := c.service.updateProvisionAccountState(ctx, c.reservation, c.state); e != nil {
		return nil, e
	}

	eligibility, e := c.setPurchaseEligibility(ctx, account)
	if e != nil {
		return nil, c.fail(ctx, e)
	}
	// The granted policy is the owner's own fact, so the receipt reports what it
	// applied rather than what the command asked for.
	c.state.WorkspacePurchaseEnabled = eligibility
	if e := c.service.updateProvisionAccountState(ctx, c.reservation, c.state); e != nil {
		return nil, e
	}
	if e := c.bindWallet(ctx, account, tenantID); e != nil {
		return nil, c.fail(ctx, e)
	}
	return c.settle(ctx, "succeeded", "confirmed", "")
}

// resolveAccount settles the Gateway account this command opens. The owner's
// own account fact is read first, then the Gateway account is read back by
// email: only a command that never sent the write may send it.
func (c *provisionAccountCommand) resolveAccount(ctx context.Context) (customerAccountRow, error) {
	row, found, e := c.service.readCustomerAccountByEmail(ctx, c.input.Email)
	if e != nil {
		return customerAccountRow{}, e
	}
	if found {
		return row, nil
	}
	identity, e := c.service.Gateway.UserByEmail(ctx, c.input.Email)
	if e == nil {
		return c.persistAccount(ctx, identity)
	}
	if !errors.Is(e, ErrGatewayAccountUnknown) {
		return customerAccountRow{}, e
	}
	if c.state.Dispatched {
		// The write was already sent and its outcome is still unconfirmed. The
		// owner converges by reading the Gateway account back and never sends
		// the write twice.
		return customerAccountRow{}, errProvisionAccountUnconfirmed
	}
	if e := c.markDispatched(ctx); e != nil {
		return customerAccountRow{}, e
	}
	if _, e := c.service.Gateway.CreateUser(ctx, c.input.Email, c.input.Password); e != nil {
		return customerAccountRow{}, errProvisionAccountUnconfirmed
	}
	identity, e = c.service.Gateway.UserByEmail(ctx, c.input.Email)
	if e != nil {
		return customerAccountRow{}, errProvisionAccountUnconfirmed
	}
	return c.persistAccount(ctx, identity)
}

// persistAccount records the Cloud account fact and the Gateway identity
// binding that belongs to it.
func (c *provisionAccountCommand) persistAccount(ctx context.Context, identity GatewayIdentity) (customerAccountRow, error) {
	actorID := strconv.FormatInt(identity.ID, 10)
	row := customerAccountRow{
		ID:          provisionAccountID(c.input.Email),
		ActorID:     actorID,
		Email:       c.input.Email,
		DisplayName: c.input.Name,
		Admission:   c.input.Admission,
		Status:      "active",
		OpenedAt:    time.Now().UTC(),
	}
	if e := c.service.writeCustomerAccount(ctx, row); e != nil {
		return customerAccountRow{}, e
	}
	// The Gateway identity binding belongs to the Gateway owner and stays in
	// gateway.identity_mappings.
	if _, e := c.service.GatewayStore.bindAuthenticatedUser(ctx, actorID, actorID, "", "", ""); e != nil {
		return customerAccountRow{}, e
	}
	return row, nil
}

// admitTenant admits the single-tenant Workspace owner the account holds. The
// derived command key is stable, so a replay admits this same Tenant instead
// of a second one.
func (c *provisionAccountCommand) admitTenant(ctx context.Context, account customerAccountRow) (string, error) {
	call := c.derivedCall("provision-tenant:" + account.ID)
	operation, e := c.service.CreateTenant(ctx, &api.CreateTenantRpcRequest{
		Context: call,
		Body: &api.CreateTenantRequest{
			Name:                  c.tenantName(),
			OwnerGatewaySubjectId: account.ActorID,
			BillingSub2ApiUserId:  account.ActorID,
			OwnerEmail:            c.input.Email,
		},
	})
	if e != nil {
		return "", e
	}
	if strings.TrimSpace(operation.GetResourceId()) == "" {
		return "", status.Error(codes.Unavailable, "the tenant owner returned no tenant id")
	}
	return operation.GetResourceId(), nil
}

// setPurchaseEligibility grants the account its Workspace purchase policy. A
// Gateway-only customer is not eligible for a new Workspace. The granted
// eligibility is returned so the receipt reports the owner's own policy.
func (c *provisionAccountCommand) setPurchaseEligibility(ctx context.Context, account customerAccountRow) (bool, error) {
	enabled := c.input.Admission == accountAdmissionFullCloudCustomer
	call := c.derivedCall("provision-eligibility:" + account.ID)
	_, e := c.service.SetWorkspacePurchaseEligibility(ctx, &api.SetWorkspacePurchaseEligibilityRpcRequest{
		Context:       call,
		TargetActorId: account.ActorID,
		Body: &api.SetWorkspacePurchaseEligibilityRequest{
			ConfirmationAccountId: account.ActorID,
			Enabled:               enabled,
			Reason:                "account_activation",
		},
	})
	if e != nil {
		return false, e
	}
	return enabled, nil
}

// bindWallet binds the Gateway wallet the Tenant pays from. The derived
// command key is stable, so a replay reads the binding it already made back
// instead of attempting a second one.
func (c *provisionAccountCommand) bindWallet(ctx context.Context, account customerAccountRow, tenantID string) error {
	call := c.derivedCall("provision-binding:" + tenantID)
	call.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenantID}}}
	readback, e := c.service.BindWallet(ctx, &api.WalletBindingCommand{
		Context:                call,
		TargetTenantId:         tenantID,
		BillingSub2ApiUserId:   account.ActorID,
		ExpectedBindingVersion: 0,
		AuthorizationReceiptId: c.authorizationReceipt,
	})
	if e != nil {
		return e
	}
	if readback.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED {
		return status.Error(codes.Unavailable, "the wallet binding was not confirmed")
	}
	return nil
}

// tenantName is the presentation name of the admitted Tenant. It is the
// operator's supplied name or the account's email local part.
func (c *provisionAccountCommand) tenantName() string {
	if c.input.Name != "" {
		return c.input.Name
	}
	name := c.input.Email
	if at := strings.IndexByte(name, '@'); at > 0 {
		name = name[:at]
	}
	if runes := []rune(strings.TrimSpace(name)); len(runes) > maxAccountNameRunes {
		name = string(runes[:maxAccountNameRunes])
	}
	return name
}

// derivedCall is the continuation context of one owner step. The caller's
// session, request and scope are kept; only the step's own idempotency key is
// derived, so every step replays its original identity.
//
// The caller's authorization context is deliberately not carried across. That
// context proves the account-opening command, while each step authorizes its
// own action (create tenant, set purchase eligibility, bind wallet) against
// the live session. A context issued for one action is not evidence for
// another, so the owner re-authorizes every step instead of replaying it.
func (c *provisionAccountCommand) derivedCall(key string) *api.CallContext {
	derived := proto.Clone(c.call).(*api.CallContext)
	derived.IdempotencyKey = key
	derived.AuthorizationContextId = ""
	return derived
}

// markDispatched records that the external Gateway account write was sent.
func (c *provisionAccountCommand) markDispatched(ctx context.Context) error {
	c.state.Dispatched = true
	return c.service.updateProvisionAccountState(ctx, c.reservation, c.state)
}

// fail records one refused step. A definite refusal closes the command, so a
// replay reports the same refusal instead of repeating it. A transient failure
// leaves the command open: the caller replays the same command and it
// continues from the owner's own facts.
func (c *provisionAccountCommand) fail(ctx context.Context, cause error) error {
	if !definiteProvisionAccountRefusal(cause) {
		if e := c.service.updateProvisionAccountState(ctx, c.reservation, c.state); e != nil {
			return e
		}
		if e := c.service.writeProvisionAccountOperationState(ctx, c.state, "awaiting_confirmation", "activation", "unknown", ""); e != nil {
			return e
		}
		return cause
	}
	code := provisionAccountErrorCode(cause)
	if e := c.service.writeProvisionAccountOperationState(ctx, c.state, "failed", "failure_fence", "rejected", code); e != nil {
		return e
	}
	c.state.Outcome = "failed"
	c.state.ErrorCode = code
	if e := c.service.updateProvisionAccountState(ctx, c.reservation, c.state); e != nil {
		return e
	}
	return cause
}

// definiteProvisionAccountRefusal reports whether a step was refused for good.
// Any other failure is an unconfirmed outcome the owner keeps open.
func definiteProvisionAccountRefusal(err error) bool {
	switch status.Code(err) {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.AlreadyExists,
		codes.NotFound, codes.PermissionDenied, codes.Unauthenticated,
		codes.ResourceExhausted, codes.OutOfRange, codes.DataLoss, codes.Unimplemented:
		return true
	default:
		return false
	}
}

// settle records the terminal answer of the command and returns its receipt.
func (c *provisionAccountCommand) settle(ctx context.Context, outcome, observation, code string) (*api.OperatorAccountCommand, error) {
	c.state.Outcome = outcome
	c.state.Stage = outcome
	c.state.ErrorCode = code
	if e := c.service.updateProvisionAccountState(ctx, c.reservation, c.state); e != nil {
		return nil, e
	}
	stage := "activation"
	if outcome == "succeeded" {
		stage = "succeeded"
	}
	if e := c.service.writeProvisionAccountOperationState(ctx, c.state, outcome, stage, observation, code); e != nil {
		return nil, e
	}
	return c.receipt(), nil
}

// receipt is the contract's published answer to one account-opening command.
// It names the operation the browser polls, the account the command opened and
// the eligibility the owner granted it. A command that is still open carries the
// owner's own poll cadence.
func (c *provisionAccountCommand) receipt() *api.OperatorAccountCommand {
	out := &api.OperatorAccountCommand{
		OperationId:              c.state.OperationID,
		AccountId:                c.state.AccountID,
		Status:                   c.state.Outcome,
		WorkspacePurchaseEnabled: c.state.WorkspacePurchaseEnabled,
	}
	if c.state.Outcome != "succeeded" {
		poll := int32(accountPollSeconds)
		out.PollAfterSeconds = &poll
	}
	return out
}

// provisionAccountID is the stable Cloud account id of one customer email.
func provisionAccountID(email string) string {
	return "acct-" + hash("account\x00" + email + "\x00")[:18]
}

// provisionAccountOperationID is the stable owner operation id of one command.
func provisionAccountOperationID(actorID, key string) string {
	return "op_provision_" + shortDigest(actorID+"\x00"+key)
}

// provisionAccountFailure reports the recorded refusal of one command.
func provisionAccountFailure(state provisionAccountState) error {
	if state.ErrorCode == "" {
		return status.Error(codes.FailedPrecondition, "the account-opening command failed")
	}
	if value, ok := api.ErrorCodeEnum_value["ERROR_CODE_ENUM_"+state.ErrorCode]; ok {
		return owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "the account-opening command failed"), api.ErrorCodeEnum(value))
	}
	return status.Error(codes.FailedPrecondition, "the account-opening command failed")
}

// provisionAccountErrorCode maps a refused step onto the contract's error
// vocabulary.
func provisionAccountErrorCode(err error) string {
	switch status.Code(err) {
	case codes.InvalidArgument:
		return "VALIDATION_FAILED"
	case codes.PermissionDenied, codes.Unauthenticated:
		return "FORBIDDEN"
	case codes.NotFound:
		return "NOT_FOUND"
	case codes.AlreadyExists, codes.Aborted:
		return "VERSION_CONFLICT"
	case codes.ResourceExhausted:
		return "OWNER_CAPABILITY_UNAVAILABLE"
	case codes.FailedPrecondition:
		return "OWNER_CAPABILITY_UNAVAILABLE"
	default:
		return "DEPENDENCY_UNAVAILABLE"
	}
}

// reserveProvisionAccount registers the original command before any external
// effect and returns the recorded answer when the same command already exists.
// A different command under the same key is a conflict: the owner never
// reinterprets one command identity as another command.
func (s *Service) reserveProvisionAccount(ctx context.Context, reservation provisionAccountReservation) (provisionAccountState, bool, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return provisionAccountState{}, false, persistence(e)
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "tenant.provision_account:"+reservation.ActorID+":"+reservation.IdempotencyKey); e != nil {
		return provisionAccountState{}, false, persistence(e)
	}
	input := ownerstore.IdempotencyInput{
		ID:             "idem_provision_account_" + shortDigest(reservation.ActorID+"\x00"+reservation.IdempotencyKey),
		TenantScope:    "platform",
		ActorScope:     reservation.ActorID,
		OperationName:  provisionAccountOperation,
		IdempotencyKey: reservation.IdempotencyKey,
		RequestSHA256:  reservation.Fingerprint,
	}
	previous, found, e := s.store.LookupIdempotency(ctx, tx, input)
	if errors.Is(e, ownerstore.ErrIdempotencyConflict) {
		return provisionAccountState{}, false, ErrProvisionAccountConflict
	}
	if e != nil {
		return provisionAccountState{}, false, persistence(e)
	}
	if found {
		var state provisionAccountState
		if e = json.Unmarshal(previous.ResponseBody, &state); e != nil {
			return provisionAccountState{}, false, persistence(e)
		}
		if e = tx.Commit(); e != nil {
			return provisionAccountState{}, false, persistence(e)
		}
		return state, true, nil
	}
	state := provisionAccountState{
		Outcome:     "pending",
		OperationID: provisionAccountOperationID(reservation.ActorID, reservation.IdempotencyKey),
		RequestID:   reservation.RequestID,
		AccountID:   provisionAccountID(reservation.Email),
	}
	body, e := json.Marshal(state)
	if e != nil {
		return provisionAccountState{}, false, persistence(e)
	}
	input.ResourceID = state.AccountID
	input.OperationID = state.OperationID
	input.ResponseStatus = 202
	input.ResponseBody = body
	if e = s.store.RecordIdempotency(ctx, tx, input); e != nil {
		return provisionAccountState{}, false, persistence(e)
	}
	if e = tx.Commit(); e != nil {
		return provisionAccountState{}, false, persistence(e)
	}
	return state, false, nil
}

// updateProvisionAccountState records the current answer of one command. Only
// the owner's own record moves, so a settled answer is never overwritten by a
// later replay.
func (s *Service) updateProvisionAccountState(ctx context.Context, reservation provisionAccountReservation, state provisionAccountState) error {
	body, e := json.Marshal(state)
	if e != nil {
		return persistence(e)
	}
	result, e := s.DB.ExecContext(ctx, `
		UPDATE tenant.idempotency_records
		SET response_status = $5, response_body = $6, resource_id = $7
		WHERE tenant_scope = 'platform' AND actor_scope = $1 AND operation_name = $2
		  AND idempotency_key = $3 AND request_sha256 = $4`,
		reservation.ActorID, provisionAccountOperation, reservation.IdempotencyKey, reservation.Fingerprint,
		provisionAccountStatus(state.Outcome), body, state.AccountID)
	if e != nil {
		return persistence(e)
	}
	rows, e := result.RowsAffected()
	if e != nil {
		return persistence(e)
	}
	if rows != 1 {
		return status.Error(codes.Unavailable, "the account-opening command is not recorded in this owner")
	}
	return nil
}

// provisionAccountStatus is the recorded HTTP status of one command answer.
func provisionAccountStatus(outcome string) int {
	switch outcome {
	case "succeeded":
		return 200
	case "failed":
		return 409
	default:
		return 202
	}
}

// writeProvisionAccountOperation records the durable operation of one
// account-opening command before any external effect.
func (s *Service) writeProvisionAccountOperation(ctx context.Context, command *provisionAccountCommand) error {
	accepted, e := json.Marshal(map[string]any{
		"email":     command.input.Email,
		"name":      command.input.Name,
		"admission": command.input.Admission,
	})
	if e != nil {
		return persistence(e)
	}
	_, e = s.DB.ExecContext(ctx, `
		INSERT INTO tenant.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,observation_result,request_id,accepted_input,result,completed_at)
		VALUES($1,NULL,$2,$3,$4,'running','activation','unknown',$5,$6,NULL,NULL)
		ON CONFLICT (id) DO NOTHING`,
		command.state.OperationID, command.reservation.ActorID, provisionAccountKind, command.state.AccountID,
		command.reservation.RequestID, accepted)
	if e != nil {
		return persistence(e)
	}
	return nil
}

// writeProvisionAccountOperationState records the current state of one
// account-opening operation.
func (s *Service) writeProvisionAccountOperationState(ctx context.Context, state provisionAccountState, operationStatus, stage, observation, code string) error {
	var tenantID any
	if strings.TrimSpace(state.TenantID) != "" {
		tenantID = state.TenantID
	}
	var errorCode any
	if strings.TrimSpace(code) != "" {
		errorCode = code
	}
	resultBody, e := json.Marshal(map[string]string{
		"accountId": state.AccountID,
		"tenantId":  state.TenantID,
		"actorId":   state.ActorID,
	})
	if e != nil {
		return persistence(e)
	}
	result, e := s.DB.ExecContext(ctx, `
		UPDATE tenant.operations
		SET tenant_id = $2, status = $3, stage = $4, observation_result = $5, error_code = $6,
		    result = $7, completed_at = CASE WHEN $3 IN ('succeeded','failed') THEN now() ELSE NULL END,
		    updated_at = now()
		WHERE id = $1`,
		state.OperationID, tenantID, operationStatus, stage, observation, errorCode, resultBody)
	if e != nil {
		return persistence(e)
	}
	rows, e := result.RowsAffected()
	if e != nil {
		return persistence(e)
	}
	if rows != 1 {
		return status.Error(codes.Unavailable, "the account-opening operation is not recorded in this owner")
	}
	return nil
}

// readCustomerAccountByEmail reads one account fact this owner knows.
func (s *Service) readCustomerAccountByEmail(ctx context.Context, email string) (customerAccountRow, bool, error) {
	var row customerAccountRow
	var openedAt time.Time
	e := s.DB.QueryRowContext(ctx, `
		SELECT id,actor_id,email,display_name,admission,status,opened_at
		FROM tenant.customer_accounts WHERE email = $1`, email).
		Scan(&row.ID, &row.ActorID, &row.Email, &row.DisplayName, &row.Admission, &row.Status, &openedAt)
	if errors.Is(e, sql.ErrNoRows) {
		return customerAccountRow{}, false, nil
	}
	if e != nil {
		return customerAccountRow{}, false, persistence(e)
	}
	row.OpenedAt = openedAt
	return row, true, nil
}

// writeCustomerAccount records one account fact. The same email always names
// the same account, and an account whose Gateway subject changed is refused
// rather than reassigned.
func (s *Service) writeCustomerAccount(ctx context.Context, row customerAccountRow) error {
	_, e := s.DB.ExecContext(ctx, `
		INSERT INTO tenant.customer_accounts(id,actor_id,email,display_name,admission,status,opened_at)
		VALUES($1,$2,$3,$4,$5,$6,now())
		ON CONFLICT (email) DO UPDATE SET display_name = EXCLUDED.display_name, admission = EXCLUDED.admission, updated_at = now()`,
		row.ID, row.ActorID, row.Email, row.DisplayName, row.Admission, row.Status)
	if e != nil {
		return persistence(e)
	}
	stored, found, e := s.readCustomerAccountByEmail(ctx, row.Email)
	if e != nil {
		return e
	}
	if !found || stored.ActorID != row.ActorID {
		return status.Error(codes.AlreadyExists, "the customer account is already bound to another Gateway identity")
	}
	return nil
}
