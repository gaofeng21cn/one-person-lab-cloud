package identity

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// The administrator's balance change is the Gateway owner's own money command.
//
// A recharge credits the customer, a debit collects from the customer, and a
// business refund credits back a confirmed original charge the command names.
// The owner's existing wallet machinery moves the money: it reserves the
// operation under the operator's idempotency key, dispatches exactly one
// adjustment to the Gateway, and settles the operation from the Gateway's own
// balance history. An outcome the Gateway did not confirm stays unknown and is
// never re-dispatched.
//
// The operation row is the durable fact: the kind, the amount, the operator, the
// reason, the Gateway's own adjustment code and the receipt id the Ledger will
// record are one row, so a read can never attribute a balance change to nobody
// or to two different operators.

// walletAdjustmentReasonLimit is the canonical maximum length of the operator's
// reason. The published contract bounds it, so the owner refuses a longer one
// rather than storing a fact the contract cannot describe.
const walletAdjustmentReasonLimit = 1024

// walletAdjustmentKinds are the operator-initiated balance changes the owner
// accepts. They are distinct from the Workspace obligation kinds, so an
// operator action can never be read as a purchase or as a deletion refund.
var walletAdjustmentKinds = []string{"recharge", "debit", "business_refund"}

// operatorAdjustmentKind reports whether the owner's own wallet kind is an
// operator-initiated balance change rather than a Workspace obligation. Such
// an operation is tenant-scoped: it names the Tenant whose spendable balance
// changed and no Workspace.
func operatorAdjustmentKind(kind string) bool {
	switch kind {
	case "recharge", "debit", "business_refund":
		return true
	default:
		return false
	}
}

// AdjustTenantWallet applies one operator-initiated change to one Tenant's
// spendable balance.
//
// The command is idempotent by key: a replay returns the original operation
// instead of a second balance change, and the same key with a different command
// is a conflict rather than a second adjustment.
func (s *Service) AdjustTenantWallet(ctx context.Context, r *api.AdjustTenantWalletRpcRequest) (*api.Operation, error) {
	if e := peer(ctx, owneridentity.ConsoleBFF); e != nil {
		return nil, e
	}
	c := r.GetContext()
	if c == nil || c.GetScope().GetPlatform() == nil || strings.TrimSpace(c.GetActorId()) == "" || c.GetSessionId() == "" || strings.TrimSpace(c.GetRequestId()) == "" {
		return nil, denied()
	}
	tenantID := strings.TrimSpace(r.GetTenantId())
	if tenantID == "" {
		return nil, status.Error(codes.InvalidArgument, "a tenant id is required")
	}
	decision, e := s.AuthorizeAction(ctx, &api.AuthorizationRequest{
		Scope:         c.GetScope(),
		ActorId:       c.GetActorId(),
		SessionId:     c.SessionId,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_GATEWAY,
		Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ADJUSTTENANTWALLET,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: proto.String(tenantID)},
		RequestId:     c.GetRequestId(),
	})
	if e != nil {
		return nil, e
	}
	if decision.GetResult() != api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED {
		return nil, status.Error(codes.PermissionDenied, "platform administrator authorization required")
	}
	body := r.GetBody()
	kind, ok := walletAdjustmentKindOf(body.GetKind())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "an adjustment kind is required")
	}
	amount := body.GetAmountUsdMicros()
	if amount <= 0 {
		return nil, status.Error(codes.InvalidArgument, "a positive amount is required")
	}
	reason := strings.TrimSpace(body.GetReason())
	if reason == "" || utf8.RuneCountInString(reason) > walletAdjustmentReasonLimit {
		return nil, status.Error(codes.InvalidArgument, "a reason is required")
	}
	original := strings.TrimSpace(body.GetOriginalChargeOperationId())
	if kind == "business_refund" && original == "" {
		return nil, status.Error(codes.InvalidArgument, "a business refund names its original charge")
	}
	if kind != "business_refund" && original != "" {
		return nil, status.Error(codes.InvalidArgument, "only a business refund names an original charge")
	}
	if strings.TrimSpace(c.GetIdempotencyKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency key is required")
	}
	if s.GatewayStore == nil {
		return nil, status.Error(codes.Unavailable, "Gateway wallet persistence is not configured")
	}
	binding, err := s.GatewayStore.ReadActiveWalletBinding(ctx, tenantID)
	if errors.Is(err, ErrWalletBindingAbsent) {
		return nil, owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "the tenant has no active wallet binding"), api.ErrorCodeEnum_ERROR_CODE_ENUM_VALIDATION_FAILED)
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway wallet binding unavailable")
	}
	subject, err := parseGatewaySubject(binding.BillingSub2APIUserID)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "the tenant wallet binding subject is invalid")
	}

	// The operation is reserved before any money moves, under the operator's own
	// idempotency key. The fingerprint covers the whole command, so the same key
	// can never replay another tenant's or another kind's adjustment.
	businessKey := "adjust-wallet:" + tenantID + ":" + strings.TrimSpace(c.GetIdempotencyKey())
	record, existing, err := s.GatewayStore.ReserveWalletOperation(ctx, WalletRecord{
		ID:                  "wallet-op-" + shortDigest(businessKey),
		TenantID:            tenantID,
		WalletBindingID:     binding.ID,
		Kind:                kind,
		AmountUSDMicros:     amount,
		OriginalOperationID: original,
		BusinessKey:         businessKey,
		RequestFingerprint:  walletAdjustmentFingerprint(kind, tenantID, amount, original),
		ActorID:             c.GetActorId(),
		Reason:              reason,
	})
	if errors.Is(err, ErrGatewayWalletConflict) {
		return nil, owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "wallet command differs from its original execution"), api.ErrorCodeEnum_ERROR_CODE_ENUM_IDEMPOTENCY_CONFLICT)
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway wallet reservation unavailable")
	}
	if record.Status == "confirmed" || record.Status == "rejected" {
		// A terminal answer is the owner's recorded readback and is never
		// re-derived, so a replay never moves money a second time. A confirmed
		// balance change still owes its receipt, so the replay records the
		// receipt the original answer never managed to append.
		if record.Status == "confirmed" {
			if err := s.ensureAdjustmentReceipt(ctx, record, c); err != nil {
				return nil, err
			}
		}
		return walletAdjustmentOperation(record, c.GetRequestId()), nil
	}
	if existing {
		// The original answer was not persisted: the original dispatch may or may
		// not have reached the Gateway. The answer is resolved from the Gateway's
		// own balance history only, never by issuing the adjustment again.
		return s.reconcileWalletAdjustment(ctx, record, binding, c)
	}

	externalCode := gatewayAdjustmentCode(record.BusinessKey)
	operation := "subtract"
	if kind == "recharge" || kind == "business_refund" {
		operation = "add"
	}
	if err := s.Gateway.AdjustBalance(ctx, subject, externalCode, amount, operation); err != nil {
		if isUnknownAdjustmentError(err) {
			if _, settleErr := s.GatewayStore.SettleWalletOperation(ctx, record.ID, "unknown", "", "", "EXTERNAL_OUTCOME_UNKNOWN"); settleErr != nil {
				return nil, status.Error(codes.Unavailable, "Gateway wallet settlement unavailable")
			}
			return s.walletAdjustmentReadback(ctx, record.ID, c.GetRequestId())
		}
		if _, settleErr := s.GatewayStore.SettleWalletOperation(ctx, record.ID, "rejected", "", "", errorCodeForAdjustment(err)); settleErr != nil {
			return nil, status.Error(codes.Unavailable, "Gateway wallet settlement unavailable")
		}
		return s.walletAdjustmentReadback(ctx, record.ID, c.GetRequestId())
	}
	return s.confirmWalletAdjustment(ctx, record, subject, c)
}

// confirmWalletAdjustment records the confirmed answer using the Gateway's own
// balance history as positive evidence. A confirmed adjustment names the
// receipt id the Ledger records.
func (s *Service) confirmWalletAdjustment(ctx context.Context, record WalletRecord, subject int64, call *api.CallContext) (*api.Operation, error) {
	externalCode := gatewayAdjustmentCode(record.BusinessKey)
	history, err := s.Gateway.PositiveBalanceHistoryByCodes(ctx, subject, []string{externalCode})
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway balance history unavailable")
	}
	entry, ok := history[externalCode]
	if !ok || entry.Micros != nativeAdjustmentMicros(record) {
		// The dispatch reported success but the native audit record is not yet
		// visible. It is not proof the money moved, so the operation stays
		// non-terminal and the operator reads it back instead of being told the
		// balance changed.
		if _, err := s.GatewayStore.SettleWalletOperation(ctx, record.ID, "unknown", "", "", "EXTERNAL_OUTCOME_UNKNOWN"); err != nil {
			return nil, status.Error(codes.Unavailable, "Gateway wallet settlement unavailable")
		}
		return s.walletAdjustmentReadback(ctx, record.ID, call.GetRequestId())
	}
	receiptID := record.Kind + "-receipt-" + shortDigest(record.ID)
	settled, err := s.GatewayStore.SettleWalletOperation(ctx, record.ID, "confirmed", externalCode, receiptID, "")
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway wallet settlement unavailable")
	}
	if err := s.ensureAdjustmentReceipt(ctx, settled, call); err != nil {
		return nil, err
	}
	return s.walletAdjustmentReadback(ctx, record.ID, call.GetRequestId())
}

// reconcileWalletAdjustment resolves a non-terminal adjustment from the
// Gateway's balance history only; it never re-issues the adjustment.
func (s *Service) reconcileWalletAdjustment(ctx context.Context, record WalletRecord, binding WalletBinding, call *api.CallContext) (*api.Operation, error) {
	subject, err := parseGatewaySubject(binding.BillingSub2APIUserID)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "the tenant wallet binding subject is invalid")
	}
	externalCode := gatewayAdjustmentCode(record.BusinessKey)
	history, err := s.Gateway.PositiveBalanceHistoryByCodes(ctx, subject, []string{externalCode})
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway balance history unavailable")
	}
	entry, ok := history[externalCode]
	if !ok {
		return s.walletAdjustmentReadback(ctx, record.ID, call.GetRequestId())
	}
	if entry.Micros != nativeAdjustmentMicros(record) {
		return nil, owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "native wallet adjustment differs from the recorded amount"), api.ErrorCodeEnum_ERROR_CODE_ENUM_VALIDATION_FAILED)
	}
	receiptID := record.Kind + "-receipt-" + shortDigest(record.ID)
	settled, err := s.GatewayStore.SettleWalletOperation(ctx, record.ID, "confirmed", externalCode, receiptID, "")
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway wallet settlement unavailable")
	}
	if err := s.ensureAdjustmentReceipt(ctx, settled, call); err != nil {
		return nil, err
	}
	return s.walletAdjustmentReadback(ctx, record.ID, call.GetRequestId())
}

// ensureAdjustmentReceipt records the confirmed administrator balance change
// in the Ledger as this owner's own receipt.
//
// The evidence is the owner's own confirmed wallet operation: the operator,
// the reason, the amount, the native adjustment code and the receipt id. The
// Ledger re-reads that operation from this owner before it stores anything, so
// a fabricated or replayed balance change is refused. The append is
// idempotent under the operator's own key, so a lost response or a retried
// command replays the original receipt instead of recording a second balance
// change.
func (s *Service) ensureAdjustmentReceipt(ctx context.Context, record WalletRecord, call *api.CallContext) error {
	if s.Ledger == nil {
		return status.Error(codes.Unavailable, "Ledger receipt coordination is not configured")
	}
	if record.Status != "confirmed" || record.ReceiptID == "" {
		return status.Error(codes.FailedPrecondition, "a confirmed balance change names its receipt")
	}
	operation := WalletOperationMessage(record)
	digest, err := contracts.WalletOperationDigest(operation)
	if err != nil {
		return status.Error(codes.Unavailable, "Gateway wallet adjustment evidence unavailable")
	}
	receipt := &api.Receipt{
		Kind:            api.ReceiptKindEnum_RECEIPT_KIND_ENUM_GATEWAY_WALLET_ADJUSTMENT,
		Owner:           api.OwnerEnum_OWNER_ENUM_GATEWAY,
		OperationId:     proto.String(record.ID),
		Outcome:         api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED,
		EvidenceSummary: "Administrator balance change confirmed by the Gateway owner.",
	}
	request := &api.AppendReceiptRequest{
		Context:                adjustmentReceiptContext(call, record.TenantID),
		Receipt:                receipt,
		EvidenceDigest:         digest,
		OwnerEvidenceReference: record.ID,
		GatewayWalletAdjustmentEvidence: &api.GatewayWalletAdjustmentReceiptEvidence{
			Receipt:         proto.Clone(receipt).(*api.Receipt),
			WalletOperation: operation,
			EvidenceDigest:  digest,
		},
	}
	if _, err = s.Ledger.AppendReceipt(ctx, request); err != nil {
		return err
	}
	return nil
}

// adjustmentReceiptContext is the context the receipt owner sees. The
// administrator's own command is platform-scoped, but the balance change it
// ordered is a fact about one Tenant, so the receipt is bound to that Tenant
// and to the operator that ordered it. The operator's live session travels
// with it, because the receipt owner re-authorizes the append against the
// CloudIdentity authority rather than trusting the caller's claim.
func adjustmentReceiptContext(call *api.CallContext, tenantID string) *api.CallContext {
	return &api.CallContext{
		RequestId:      call.GetRequestId(),
		IdempotencyKey: call.GetIdempotencyKey(),
		ActorId:        call.GetActorId(),
		SessionId:      call.SessionId,
		Scope:          &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenantID}}},
	}
}

// walletAdjustmentReadback reads the settled operation back and publishes it as
// the command's answer.
func (s *Service) walletAdjustmentReadback(ctx context.Context, id, requestID string) (*api.Operation, error) {
	record, err := s.GatewayStore.ReadWalletOperation(ctx, id)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway wallet readback unavailable")
	}
	return walletAdjustmentOperation(record, requestID), nil
}

// walletAdjustmentOperation publishes the settled wallet operation as the
// command's answer. The operation id is the wallet operation's own id, so the
// operator reads the same fact back from the owner's wallet operation list.
func walletAdjustmentOperation(record WalletRecord, requestID string) *api.Operation {
	status := api.OperationStatusEnum_OPERATION_STATUS_ENUM_ACCEPTED
	stage := api.OperationStageEnum_OPERATION_STAGE_ENUM_PAYMENT_AUTHORIZATION
	switch record.Status {
	case "confirmed":
		status = api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED
		stage = api.OperationStageEnum_OPERATION_STAGE_ENUM_SUCCEEDED
	case "rejected":
		status = api.OperationStatusEnum_OPERATION_STATUS_ENUM_FAILED
		stage = api.OperationStageEnum_OPERATION_STAGE_ENUM_FAILURE_FENCE
	case "unknown":
		status = api.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION
		stage = api.OperationStageEnum_OPERATION_STAGE_ENUM_RECONCILIATION
	}
	operation := &api.Operation{
		OperationId: record.ID,
		Owner:       api.OperationOwnerEnum_OPERATION_OWNER_ENUM_GATEWAY,
		Kind:        api.OperationKindEnum_OPERATION_KIND_ENUM_WALLET_ADJUSTMENT,
		ResourceId:  record.TenantID,
		Status:      status,
		Stage:       stage,
		RequestId:   requestID,
		CreatedAt:   timestamppb.New(record.CreatedAt.UTC()),
		UpdatedAt:   timestamppb.New(record.UpdatedAt.UTC()),
	}
	if record.ErrorCode != "" {
		if code, ok := api.ErrorCodeEnum_value["ERROR_CODE_ENUM_"+record.ErrorCode]; ok {
			operation.ErrorCode = api.ErrorCodeEnum(code).Enum()
		}
	}
	if status != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED && status != api.OperationStatusEnum_OPERATION_STATUS_ENUM_FAILED {
		poll := int32(5)
		operation.PollAfterSeconds = &poll
	}
	return operation
}

// walletAdjustmentKindOf maps the published kind vocabulary onto the owner's own
// wallet kinds. An unknown kind is refused rather than stored.
func walletAdjustmentKindOf(kind api.WalletAdjustmentRequestKindEnum) (string, bool) {
	switch kind {
	case api.WalletAdjustmentRequestKindEnum_WALLET_ADJUSTMENT_REQUEST_KIND_ENUM_RECHARGE:
		return "recharge", true
	case api.WalletAdjustmentRequestKindEnum_WALLET_ADJUSTMENT_REQUEST_KIND_ENUM_DEBIT:
		return "debit", true
	case api.WalletAdjustmentRequestKindEnum_WALLET_ADJUSTMENT_REQUEST_KIND_ENUM_BUSINESS_REFUND:
		return "business_refund", true
	default:
		return "", false
	}
}

// walletAdjustmentFingerprint fixes the immutable identity of one adjustment
// command. A different kind, amount, tenant or original charge under the same
// idempotency key is a different command and is refused rather than silently
// re-interpreted.
func walletAdjustmentFingerprint(kind, tenantID string, amount int64, original string) string {
	return shortDigest(strings.Join([]string{"adjust_wallet", kind, tenantID, original, itoa(amount)}, "\x00"))
}

// ListRechargeRecords reads the administrator's balance changes. It never
// reconciles an unknown outcome and never moves money: the list is the owner's
// own record of what it did, not a second answer.
func (s *Service) ListRechargeRecords(ctx context.Context, r *api.ListRechargeRecordsRpcRequest) (*api.WalletOperationPage, error) {
	if e := peer(ctx, owneridentity.ConsoleBFF); e != nil {
		return nil, e
	}
	c := r.GetContext()
	if c == nil || c.GetScope().GetPlatform() == nil || strings.TrimSpace(c.GetActorId()) == "" || c.GetSessionId() == "" || strings.TrimSpace(c.GetRequestId()) == "" {
		return nil, denied()
	}
	decision, e := s.AuthorizeAction(ctx, &api.AuthorizationRequest{
		Scope:         c.GetScope(),
		ActorId:       c.GetActorId(),
		SessionId:     c.SessionId,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_GATEWAY,
		Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRECHARGERECORDS,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT},
		RequestId:     c.GetRequestId(),
	})
	if e != nil {
		return nil, e
	}
	if decision.GetResult() != api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED {
		return nil, status.Error(codes.PermissionDenied, "platform administrator authorization required")
	}
	after, afterID, ok := parseCursor(r.GetQueryCursor())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "invalid cursor")
	}
	limit := 25
	if r.QueryLimit != nil {
		if r.GetQueryLimit() < 1 || r.GetQueryLimit() > 100 {
			return nil, status.Error(codes.InvalidArgument, "limit must be between 1 and 100")
		}
		limit = int(r.GetQueryLimit())
	}
	if s.GatewayStore == nil {
		return nil, status.Error(codes.Unavailable, "Gateway persistence unavailable")
	}
	tenantID := strings.TrimSpace(r.GetQueryTenantId())
	rows, err := s.GatewayStore.db.QueryContext(ctx, walletSelect+`
		WHERE kind IN ('recharge','debit','business_refund')
		AND ($1='' OR tenant_id=$1)
		AND ($2::timestamptz IS NULL OR (created_at,id)<($2,$3))
		ORDER BY created_at DESC,id DESC LIMIT $4`, tenantID, sql.NullTime{Time: after, Valid: afterID != ""}, afterID, limit+1)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway persistence unavailable")
	}
	defer rows.Close()
	page := &api.WalletOperationPage{}
	for rows.Next() {
		record, scanErr := s.GatewayStore.scanWallet(ctx, rows)
		if scanErr != nil {
			return nil, status.Error(codes.Unavailable, "Gateway persistence unavailable")
		}
		page.Items = append(page.Items, WalletOperationMessage(record))
	}
	if err = rows.Err(); err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway persistence unavailable")
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[limit-1]
		page.NextCursor = proto.String(cursorOf(last.CreatedAt.AsTime(), last.GetId()))
	}
	return page, nil
}
