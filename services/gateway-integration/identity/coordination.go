package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

// GatewayCoordination is the Gateway Integration owner's implementation of the
// typed internal settlement/key surface. It is the only writer that moves
// customer money: Sub2API remains the external wallet authority, and this owner
// persists the original charge/refund intent and its owner readback. The
// Workspace owner holds the client; this owner holds the truth.
func (s *Service) BindWallet(ctx context.Context, r *api.WalletBindingCommand) (*api.WalletBindingReadback, error) {
	if err := s.coordinationCaller(ctx, owneridentity.Tenant.Service(), owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	if s.GatewayStore == nil {
		return nil, status.Error(codes.Unavailable, "Gateway wallet persistence is not configured")
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	tenantID := strings.TrimSpace(r.GetTargetTenantId())
	if tenantID == "" || strings.TrimSpace(r.GetBillingSub2ApiUserId()) == "" || strings.TrimSpace(r.GetAuthorizationReceiptId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "target tenant, billing subject and authorization receipt are required")
	}
	// The wallet subject must be a real, active Gateway subject: Cloud never
	// fabricates a billing identity from browser input, so it is read through the
	// service's own administrative directory identity.
	subject, err := s.Gateway.directoryIdentity(ctx, r.GetBillingSub2ApiUserId())
	if err != nil {
		return nil, err
	}
	if subject.ID <= 0 || subject.Status != "active" {
		return nil, status.Error(codes.FailedPrecondition, "billing wallet subject is not an active Gateway identity")
	}
	if err := s.authorizeWalletBinding(ctx, r.GetContext(), tenantID); err != nil {
		return nil, err
	}
	binding, err := s.GatewayStore.BindWalletBinding(ctx, WalletBinding{
		ID:                   "wallet-binding-" + shortDigest(tenantID+":"+r.GetBillingSub2ApiUserId()+":"+r.GetAuthorizationReceiptId()),
		TenantID:             tenantID,
		BillingSub2APIUserID: r.GetBillingSub2ApiUserId(),
		DelegationRef:        r.GetAuthorizationReceiptId(),
		VerificationRef:      r.GetAuthorizationReceiptId(),
	}, r.GetExpectedBindingVersion())
	if err != nil {
		if err == ErrWalletBindingVersionConflict {
			return nil, status.Error(codes.Aborted, "wallet binding version conflict")
		}
		return nil, status.Error(codes.Unavailable, "Gateway wallet binding unavailable")
	}
	return &api.WalletBindingReadback{TenantId: tenantID, BillingSub2ApiUserId: binding.BillingSub2APIUserID,
		BindingVersion: binding.Version, Outcome: api.Observation_OBSERVATION_CONFIRMED, AuthorizationReceiptId: r.GetAuthorizationReceiptId()}, nil
}

// Debit issues at most one charge for an accepted Workspace obligation. The
// obligation is the durability key: a retry, a second worker or a lost response
// all replay this same command identity instead of moving money twice.
func (s *Service) Debit(ctx context.Context, r *api.WalletDebitCommand) (*api.WalletOperation, error) {
	return s.walletAction(ctx, walletActionRequest{
		call: r.GetContext(), kind: "charge", workspaceID: r.GetWorkspaceId(), amount: r.GetAmountUsdMicros(),
		currency: r.GetCurrency(), businessKey: walletBusinessKey(r.GetContext()), obligation: r.GetObligationId(),
		quoteAcceptanceID: r.GetQuoteAcceptanceId(),
	})
}

// Refund reverses one confirmed charge for a confirmed deletion. A refund names
// the original charge and its entitlement, and is never issued for an unknown
// original outcome.
func (s *Service) Refund(ctx context.Context, r *api.WalletRefundCommand) (*api.WalletOperation, error) {
	if strings.TrimSpace(r.GetOriginalWalletOperationId()) == "" || strings.TrimSpace(r.GetRefundPolicyVersionId()) == "" || strings.TrimSpace(r.GetConfirmedDeletionReceiptId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "original charge, refund policy and confirmed deletion receipt are required")
	}
	return s.walletAction(ctx, walletActionRequest{
		call: r.GetContext(), kind: "refund", workspaceID: r.GetWorkspaceId(), amount: r.GetAmountUsdMicros(),
		currency: "USD", businessKey: walletBusinessKey(r.GetContext()), original: r.GetOriginalWalletOperationId(),
		entitlement: r.GetRefundPolicyVersionId(), deletionReceipt: r.GetConfirmedDeletionReceiptId(),
	})
}

// ReadWalletAction returns the owner's recorded answer for one wallet operation.
// It is read by its own id or by the original idempotency key, never fabricated.
func (s *Service) ReadWalletAction(ctx context.Context, r *api.WalletReadbackRequest) (*api.WalletOperation, error) {
	if err := s.coordinationCaller(ctx, owneridentity.Workspace.Service(), owneridentity.Ledger.Service(), owneridentity.Fabric.Service()); err != nil {
		return nil, err
	}
	if s.GatewayStore == nil {
		return nil, status.Error(codes.Unavailable, "Gateway wallet persistence is not configured")
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	var record WalletRecord
	var err error
	switch {
	case strings.TrimSpace(r.GetWalletOperationId()) != "":
		record, err = s.GatewayStore.ReadWalletOperation(ctx, r.GetWalletOperationId())
	case strings.TrimSpace(r.GetOriginalIdempotencyKey()) != "":
		record, err = s.GatewayStore.ReadWalletOperationByBusinessKey(ctx, r.GetOriginalIdempotencyKey())
	default:
		return nil, status.Error(codes.InvalidArgument, "a wallet operation id or original idempotency key is required")
	}
	if err == ErrGatewayWalletUnknown {
		return nil, status.Error(codes.NotFound, "wallet operation not found")
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway wallet readback unavailable")
	}
	if err := s.authorizeWalletRead(ctx, r.GetContext(), record); err != nil {
		return nil, err
	}
	// A terminal answer is the owner's recorded authoritative readback. A
	// non-terminal answer - requested or unknown - converges from the native
	// history under its original code; the answer is only ever read, never a
	// second charge or refund. A code the native history still cannot confirm
	// stays explicitly non-terminal rather than being guessed.
	if record.Status == "confirmed" || record.Status == "rejected" {
		return WalletOperationMessage(record), nil
	}
	binding, err := s.GatewayStore.ReadActiveWalletBinding(ctx, record.TenantID)
	if err != nil {
		return WalletOperationMessage(record), nil
	}
	return s.reconcileWalletOperation(ctx, record, binding)
}

type walletActionRequest struct {
	call              *api.CallContext
	kind              string
	workspaceID       string
	amount            int64
	currency          string
	businessKey       string
	obligation        string
	quoteAcceptanceID string
	original          string
	entitlement       string
	deletionReceipt   string
}

// walletAction resolves at most one money movement for the business key. The
// accepted operation grant names either CHARGEACCEPTEDOBLIGATION or the matching
// refund action for the caller's audience, so a charge cannot be issued under a
// refund grant and the reverse.
func (s *Service) walletAction(ctx context.Context, req walletActionRequest) (*api.WalletOperation, error) {
	if err := s.coordinationCaller(ctx, owneridentity.Workspace.Service()); err != nil {
		return nil, err
	}
	if s.GatewayStore == nil {
		return nil, status.Error(codes.Unavailable, "Gateway wallet persistence is not configured")
	}
	if err := ownerservice.ValidateCallContext(ctx, req.call); err != nil {
		return nil, err
	}
	if req.workspaceID == "" || req.amount <= 0 || req.businessKey == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace, positive amount and business idempotency key are required")
	}
	if req.kind == "charge" && req.currency != "USD" {
		return nil, status.Error(codes.InvalidArgument, "only USD wallet operations are supported")
	}
	tenantID := req.call.GetScope().GetTenant().GetTenantId()
	if tenantID == "" {
		return nil, status.Error(codes.PermissionDenied, "a tenant-scoped wallet operation is required")
	}
	if err := s.authorizeWalletWrite(ctx, req.call, req.kind, tenantID, req.workspaceID); err != nil {
		return nil, err
	}
	binding, err := s.GatewayStore.ReadActiveWalletBinding(ctx, tenantID)
	if err == ErrWalletBindingAbsent {
		return nil, status.Error(codes.FailedPrecondition, "the tenant has no active wallet binding")
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway wallet binding unavailable")
	}
	fingerprint := walletRequestFingerprint(req)
	record, existing, err := s.GatewayStore.ReserveWalletOperation(ctx, WalletRecord{
		ID:                   "wallet-op-" + shortDigest(req.businessKey),
		TenantID:             tenantID,
		WorkspaceID:          req.workspaceID,
		WalletBindingID:      binding.ID,
		Kind:                 req.kind,
		AmountUSDMicros:      req.amount,
		OriginalOperationID:  req.original,
		BusinessKey:          req.businessKey,
		RequestFingerprint:   fingerprint,
		RefundEntitlementRef: req.entitlement,
	})
	if err == ErrGatewayWalletConflict {
		return nil, status.Error(codes.FailedPrecondition, "wallet command differs from its original execution")
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway wallet reservation unavailable")
	}
	// A terminal answer is the owner's recorded authoritative readback and is
	// never re-derived. A non-terminal answer - the original answer was not
	// persisted, or the external outcome was unknown - is resolved from the
	// native history by its original code, so an unknown outcome converges to
	// confirmed or stays explicitly unknown. It is never re-charged or refunded
	// to probe.
	if record.Status == "confirmed" || record.Status == "rejected" {
		return WalletOperationMessage(record), nil
	}
	// The original answer was not persisted: dispatch exactly one adjustment under
	// the business code, and if the response is lost leave the operation pending.
	// The caller reads it back rather than re-issuing it.
	if existing {
		// A row already exists but is not terminal: the original dispatch may or
		// may not have reached Sub2API. Resolve the answer from native history only.
		return s.reconcileWalletOperation(ctx, record, binding)
	}
	subject, err := parseGatewaySubject(binding.BillingSub2APIUserID)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "the tenant wallet binding subject is invalid")
	}
	operation := "subtract"
	if req.kind == "refund" {
		operation = "add"
	}
	externalCode := gatewayAdjustmentCode(record.BusinessKey)
	if err := s.Gateway.AdjustBalance(ctx, subject, externalCode, req.amount, operation); err != nil {
		if isUnknownAdjustmentError(err) {
			if _, settleErr := s.GatewayStore.SettleWalletOperation(ctx, record.ID, "unknown", "", "", "EXTERNAL_OUTCOME_UNKNOWN"); settleErr != nil {
				return nil, status.Error(codes.Unavailable, "Gateway wallet settlement unavailable")
			}
			return s.gatewayStoreRead(ctx, record.ID)
		}
		if _, settleErr := s.GatewayStore.SettleWalletOperation(ctx, record.ID, "rejected", "", "", errorCodeForAdjustment(err)); settleErr != nil {
			return nil, status.Error(codes.Unavailable, "Gateway wallet settlement unavailable")
		}
		return s.gatewayStoreRead(ctx, record.ID)
	}
	return s.confirmWalletOperation(ctx, record, binding)
}

// confirmWalletOperation records the confirmed answer using the native balance
// history as positive evidence. A confirmed charge names the receipt id the
// downstream Ledger will record; a refund names its own receipt id.
func (s *Service) confirmWalletOperation(ctx context.Context, record WalletRecord, binding WalletBinding) (*api.WalletOperation, error) {
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
	if !ok || entry.Micros != nativeAdjustmentMicros(record) {
		// The POST reported success but the native audit record is not yet visible.
		// It is not proof the money moved, so stay non-terminal and let the caller
		// read back instead of claiming a confirmed charge.
		if _, err := s.GatewayStore.SettleWalletOperation(ctx, record.ID, "unknown", "", "", "EXTERNAL_OUTCOME_UNKNOWN"); err != nil {
			return nil, status.Error(codes.Unavailable, "Gateway wallet settlement unavailable")
		}
		return s.gatewayStoreRead(ctx, record.ID)
	}
	receiptID := record.Kind + "-receipt-" + shortDigest(record.ID)
	if _, err := s.GatewayStore.SettleWalletOperation(ctx, record.ID, "confirmed", externalCode, receiptID, ""); err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway wallet settlement unavailable")
	}
	return s.gatewayStoreRead(ctx, record.ID)
}

// reconcileWalletOperation resolves a non-terminal row from native history only;
// it never re-issues the adjustment.
func (s *Service) reconcileWalletOperation(ctx context.Context, record WalletRecord, binding WalletBinding) (*api.WalletOperation, error) {
	subject, err := parseGatewaySubject(binding.BillingSub2APIUserID)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "the tenant wallet binding subject is invalid")
	}
	externalCode := gatewayAdjustmentCode(record.BusinessKey)
	history, err := s.Gateway.PositiveBalanceHistoryByCodes(ctx, subject, []string{externalCode})
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway balance history unavailable")
	}
	if entry, ok := history[externalCode]; ok {
		if entry.Micros != nativeAdjustmentMicros(record) {
			return nil, status.Error(codes.FailedPrecondition, "native wallet adjustment differs from the recorded amount")
		}
		receiptID := record.Kind + "-receipt-" + shortDigest(record.ID)
		if _, err := s.GatewayStore.SettleWalletOperation(ctx, record.ID, "confirmed", externalCode, receiptID, ""); err != nil {
			return nil, status.Error(codes.Unavailable, "Gateway wallet settlement unavailable")
		}
		return s.gatewayStoreRead(ctx, record.ID)
	}
	return WalletOperationMessage(record), nil
}

// nativeAdjustmentMicros is the signed delta the native wallet records for one
// wallet action. A charge subtracts from the balance, so the wallet owner's
// history carries its negative; a refund adds and stays positive. Evidence is
// matched against this signed value, exactly as the retained Control Plane
// settlement semantics did, so an opposite-sign record can never confirm the
// obligation.
func nativeAdjustmentMicros(record WalletRecord) int64 {
	if record.Kind == "charge" {
		return -record.AmountUSDMicros
	}
	return record.AmountUSDMicros
}

func (s *Service) gatewayStoreRead(ctx context.Context, id string) (*api.WalletOperation, error) {
	record, err := s.GatewayStore.ReadWalletOperation(ctx, id)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway wallet readback unavailable")
	}
	return WalletOperationMessage(record), nil
}

// CreateManagedKey mints one Workspace-scoped Gateway key and records only its
// opaque delivery reference and fingerprint. The raw key never enters the owner's
// normal tables, logs, Outbox or receipts: it is handed to the approved Secret
// store through the delivery reference. Both the key issuer and the Secret store
// are required dependencies; a deployment missing either fails closed rather than
// persisting or emitting a raw credential.
//
// The original command is registered in this owner's existing
// gateway.idempotency_records before any external issuance, under the caller's
// immutable idempotency key and normalized input digest. A replay of the same
// command reads the recorded answer back instead of issuing a second key; a
// replayed command with a different input is refused. When the external outcome
// was never confirmed - a lost ACK, or a recorded refusal the owner could not
// settle - the operation stays explicitly unresolved and is never re-dispatched:
// this owner has no approved idempotent lookup of an existing key by its original
// issue identity, so re-issuing could create a second key for one accepted
// command.
func (s *Service) CreateManagedKey(ctx context.Context, r *api.ManagedKeyCommand) (*api.ManagedKeyBinding, error) {
	if err := s.coordinationCaller(ctx, owneridentity.Workspace.Service()); err != nil {
		return nil, err
	}
	if s.GatewayStore == nil {
		return nil, status.Error(codes.Unavailable, "Gateway key persistence is not configured")
	}
	if s.KeyIssuer == nil {
		return nil, status.Error(codes.FailedPrecondition, "no approved Gateway key issuer is configured")
	}
	if s.SecretStore == nil {
		return nil, status.Error(codes.FailedPrecondition, "no approved Secret store is configured for managed key delivery")
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	if r.GetWorkspaceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "a workspace is required")
	}
	tenantID := r.GetContext().GetScope().GetTenant().GetTenantId()
	if tenantID == "" {
		return nil, status.Error(codes.PermissionDenied, "a tenant-scoped key operation is required")
	}
	if err := s.authorizeManagedKey(ctx, r.GetContext(), tenantID, r.GetWorkspaceId()); err != nil {
		return nil, err
	}
	// The declared model selection may be empty: the default App names no model
	// slot, so the approved concrete scope is resolved by the issuance authority
	// and recorded from its readback. An empty declared list is never treated as
	// "unrestricted" and never resolved locally.
	modelIDs := normalizeManagedKeyModelIDs(r.GetModelIds())
	call := r.GetContext()
	if strings.TrimSpace(call.GetIdempotencyKey()) == "" {
		return nil, owneridentity.WithErrorCode(status.Error(codes.InvalidArgument, "idempotency key is required"), api.ErrorCodeEnum_ERROR_CODE_ENUM_IDEMPOTENCY_REQUIRED)
	}
	// The wallet binding is a local read with no external effect, so it is resolved
	// before the original command is registered: only an issuance that may reach
	// the external authority needs the durable original-command record.
	binding, err := s.GatewayStore.ReadActiveWalletBinding(ctx, tenantID)
	if err == ErrWalletBindingAbsent {
		return nil, status.Error(codes.FailedPrecondition, "the tenant has no active wallet binding")
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway wallet binding unavailable")
	}
	reservation := ManagedKeyCommandReservation{
		TenantID: tenantID, ActorID: call.GetActorId(), WorkspaceID: r.GetWorkspaceId(),
		IdempotencyKey:     call.GetIdempotencyKey(),
		CommandFingerprint: managedKeyCommandFingerprint(r.GetWorkspaceId(), r.GetTargetRuntimeInstanceId(), modelIDs),
	}
	record, existing, err := s.GatewayStore.ReserveManagedKeyCommand(ctx, reservation)
	if errors.Is(err, ErrManagedKeyCommandConflict) {
		return nil, owneridentity.WithErrorCode(status.Error(codes.AlreadyExists, "idempotency key reused with a different managed key command"), api.ErrorCodeEnum_ERROR_CODE_ENUM_IDEMPOTENCY_CONFLICT)
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway key command registration unavailable")
	}
	if existing {
		return s.managedKeyCommandAnswer(ctx, reservation, record, nil)
	}
	issued, err := s.KeyIssuer.IssueWorkspaceKey(ctx, ManagedKeyIssueRequest{
		Subject: binding.BillingSub2APIUserID, WorkspaceID: r.GetWorkspaceId(), LaunchOperationID: call.GetIdempotencyKey(),
		ExactName: workspaceKeyExactName(r.GetWorkspaceId()), GroupName: s.WorkspaceKeyGroup, ModelIDs: modelIDs, IdempotencyKey: call.GetIdempotencyKey(),
	})
	if err != nil {
		if !definiteManagedKeyRefusal(err) {
			// The external effect may or may not exist. The original command stays
			// pending so a later attempt reads it back instead of issuing a second
			// key; no new effect is dispatched without an approved lookup.
			return nil, status.Error(codes.Unavailable, "managed key issuance outcome unknown")
		}
		body, marshalErr := json.Marshal(managedKeyCommandState{Outcome: "rejected", ErrorCode: status.Code(err).String()})
		if marshalErr != nil {
			return nil, status.Error(codes.Unavailable, "Gateway key settlement unavailable")
		}
		if settleErr := s.GatewayStore.SettleManagedKeyCommand(ctx, reservation, managedKeyCommandRejectedStatus, body, ""); settleErr != nil {
			return nil, status.Error(codes.Unavailable, "Gateway key settlement unavailable")
		}
		return nil, status.Error(codes.FailedPrecondition, "the managed key issuance was refused")
	}
	externalKeyID, raw := issued.ExternalKeyID, issued.Raw
	if _, parseErr := parseGatewaySubject(externalKeyID); parseErr != nil || strings.TrimSpace(raw) == "" {
		// The issuance returned an unusable identity. The original command stays
		// pending: re-issuing could mint a second key under the same accepted
		// command, and the observed identity must be reconciled by its original
		// issue identity instead.
		return nil, status.Error(codes.Unavailable, "managed key issuance returned an invalid key identity")
	}
	resolvedModelIDs, err := validateResolvedWorkspaceKeyModels(issued.ModelIDs)
	if err != nil {
		// The authority returned a resolved scope this owner cannot record. The
		// command stays unresolved and nothing is recorded: a scope that violates
		// the bounded contract is never stored as an approved allowlist.
		return nil, status.Error(codes.Unavailable, "managed key issuance returned an invalid resolved model scope")
	}
	// A declared non-empty selection is the exact approved scope: the authority
	// may neither widen, narrow nor substitute it. A mismatch is refused like an
	// unusable key identity - the observed key stays unconfirmed and nothing is
	// recorded, so it is reconciled by its original issue identity instead of
	// being delivered as approved for a scope the caller never declared. An empty
	// declared selection keeps the separate approved-scope branch: the authority
	// resolves the concrete allowlist and only the shape contract above applies.
	if len(modelIDs) > 0 && !equalDeclaredModelScope(modelIDs, resolvedModelIDs) {
		return nil, status.Error(codes.Unavailable, "managed key issuance returned a model scope that differs from the declared selection")
	}
	// The fingerprint is Fabric's format - the SHA-256 of the raw value in
	// lowercase hex - so the Serve-side Secret handover equality check against
	// Fabric's provider readback holds.
	digest := sha256.Sum256([]byte(raw))
	fingerprint := "sha256:" + hex.EncodeToString(digest[:])
	// The external identity this issuance actually produced is recorded before the
	// Secret write, so a lost delivery is read back as this one original key
	// instead of a second issuance.
	if err = s.GatewayStore.AdvanceManagedKeyCommandPending(ctx, reservation, externalKeyID, fingerprint, resolvedModelIDs); err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway key command readback unavailable")
	}
	delivery, err := s.SecretStore.PutSecret(ctx, SecretDelivery{
		TenantID: tenantID, WorkspaceID: r.GetWorkspaceId(), Purpose: "workspace_managed", Fingerprint: fingerprint, Raw: raw,
		ExternalKeyID: externalKeyID, Call: derivedManagedKeyCall(call),
	})
	if err != nil {
		// The key exists but its delivery is unconfirmed; the command stays
		// unresolved with the observed key identity recorded, and is read back
		// rather than being re-issued.
		return nil, status.Error(codes.Unavailable, "approved Secret store write failed")
	}
	if delivery.Reference != contracts.WorkspaceGatewaySecretRef(r.GetWorkspaceId()) || delivery.Fingerprint != fingerprint || delivery.Raw != "" {
		// The approved store did not confirm the exact delivered Secret identity.
		// The key exists, so the command stays unresolved with its observed
		// identity rather than being re-issued.
		return nil, status.Error(codes.Unavailable, "approved Secret store returned a different identity")
	}
	state := managedKeyCommandState{Outcome: "confirmed", ExternalKeyID: externalKeyID, Fingerprint: fingerprint,
		SecretRef: delivery.Reference, KeyBindingID: "gateway-key-" + shortDigest(tenantID+":"+r.GetWorkspaceId()+":"+externalKeyID),
		TargetRuntimeInstanceID: r.GetTargetRuntimeInstanceId(), ResolvedModelIDs: resolvedModelIDs}
	body, marshalErr := json.Marshal(state)
	if marshalErr != nil {
		return nil, status.Error(codes.Unavailable, "Gateway key settlement unavailable")
	}
	keyBinding, err := s.GatewayStore.InsertConfirmedManagedKeyBinding(ctx, ManagedKeyBinding{
		TenantID: tenantID, WorkspaceID: r.GetWorkspaceId(), ActorID: call.GetActorId(),
		ExternalKeyID: externalKeyID, Fingerprint: fingerprint, SecretRef: delivery.Reference,
		Purpose: "workspace_managed", ModelIDs: resolvedModelIDs, TargetRuntimeInstanceID: r.GetTargetRuntimeInstanceId(), TTL: managedKeyTTL,
	}, reservation, body)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway key binding unavailable")
	}
	// The binding row is the recorded effect; its id is the opaque readback
	// identity the command answer names. The stored answer already carries the
	// same identities, so resolving from the record keeps one source of truth.
	record, err = s.GatewayStore.ReadManagedKeyCommand(ctx, reservation)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway key command readback unavailable")
	}
	return s.managedKeyCommandAnswer(ctx, reservation, record, &keyBinding)
}

// managedKeyCommandAnswer resolves the recorded answer of one original managed
// key command. A confirmed answer is read back from the recorded Gateway key
// binding row, so the returned identity is the owner's stored fact rather than a
// re-derived one. A pending answer stays explicitly unresolved: the observed
// external key identity, when the issuance produced one, is reported so the
// original effect can be reconciled, and it is never re-issued.
func (s *Service) managedKeyCommandAnswer(ctx context.Context, reservation ManagedKeyCommandReservation, record ManagedKeyCommandRecord, recorded *ManagedKeyBinding) (*api.ManagedKeyBinding, error) {
	state := managedKeyCommandState{}
	if err := json.Unmarshal(record.Body, &state); err != nil {
		return nil, status.Error(codes.DataLoss, "stored managed key command answer is invalid")
	}
	switch record.ResponseStatus {
	case managedKeyCommandPendingStatus:
		if state.ExternalKeyID != "" {
			return nil, status.Error(codes.Unavailable, "managed key issuance outcome is unresolved for its observed external key; the original command is never re-issued")
		}
		return nil, status.Error(codes.Unavailable, "managed key issuance outcome is unresolved; the original command is never re-issued")
	case managedKeyCommandRejectedStatus:
		return nil, status.Error(codes.FailedPrecondition, "the managed key issuance was refused: "+state.ErrorCode)
	case managedKeyCommandConfirmedStatus:
	default:
		return nil, status.Error(codes.DataLoss, "stored managed key command answer has an unknown state")
	}
	keyBinding := ManagedKeyBinding{}
	if recorded != nil {
		keyBinding = *recorded
	} else {
		var err error
		keyBinding, err = s.GatewayStore.ReadManagedKeyBinding(ctx, state.KeyBindingID)
		if err != nil {
			return nil, status.Error(codes.Unavailable, "Gateway key binding readback unavailable")
		}
	}
	if keyBinding.TenantID != reservation.TenantID || keyBinding.WorkspaceID != reservation.WorkspaceID ||
		keyBinding.ExternalKeyID != state.ExternalKeyID || keyBinding.Fingerprint != state.Fingerprint ||
		keyBinding.SecretRef != state.SecretRef {
		return nil, status.Error(codes.DataLoss, "Gateway key binding differs from its original command")
	}
	if len(state.ResolvedModelIDs) == 0 || !equalStringSets(keyBinding.ModelIDs, state.ResolvedModelIDs) {
		return nil, status.Error(codes.DataLoss, "Gateway key binding model scope differs from its original command")
	}
	out := &api.ManagedKeyBinding{KeyBindingId: keyBinding.ID, Fingerprint: keyBinding.Fingerprint, SecretDeliveryReference: keyBinding.SecretRef,
		WorkspaceId: keyBinding.WorkspaceID, TargetRuntimeInstanceId: state.TargetRuntimeInstanceID}
	if !keyBinding.ExpiresAt.IsZero() {
		out.ExpiresAt = timestamppb.New(keyBinding.ExpiresAt)
	}
	return out, nil
}

// RevokeManagedKey retires one Workspace-managed key binding. The key is revoked
// through the issuer first; a lost revoke response leaves the binding non-terminal
// so a later pass re-reads the native state rather than claiming success.
func (s *Service) RevokeManagedKey(ctx context.Context, r *api.ManagedKeyRevoke) (*api.Operation, error) {
	if err := s.coordinationCaller(ctx, owneridentity.Workspace.Service()); err != nil {
		return nil, err
	}
	if s.GatewayStore == nil {
		return nil, status.Error(codes.Unavailable, "Gateway key persistence is not configured")
	}
	if s.KeyIssuer == nil {
		return nil, status.Error(codes.FailedPrecondition, "no approved Gateway key issuer is configured")
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	if strings.TrimSpace(r.GetKeyBindingId()) == "" || strings.TrimSpace(r.GetWorkspaceId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "key binding and workspace are required")
	}
	tenantID := r.GetContext().GetScope().GetTenant().GetTenantId()
	if tenantID == "" {
		return nil, status.Error(codes.PermissionDenied, "a tenant-scoped key operation is required")
	}
	if err := s.authorizeManagedKey(ctx, r.GetContext(), tenantID, r.GetWorkspaceId()); err != nil {
		return nil, err
	}
	key, err := s.GatewayStore.ReadManagedKeyBinding(ctx, r.GetKeyBindingId())
	if err != nil {
		return nil, status.Error(codes.NotFound, "Gateway key binding not found")
	}
	if key.TenantID != tenantID || key.WorkspaceID != r.GetWorkspaceId() {
		return nil, status.Error(codes.PermissionDenied, "key binding belongs to another tenant or workspace")
	}
	binding, err := s.GatewayStore.ReadActiveWalletBinding(ctx, tenantID)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "the tenant has no active wallet binding")
	}
	if key.RevokedAt.IsZero() {
		if err := s.KeyIssuer.RevokeWorkspaceKey(ctx, binding.BillingSub2APIUserID, key.ExternalKeyID); err != nil {
			return nil, err
		}
		if _, err := s.GatewayStore.MarkManagedKeyRevoked(ctx, key.ID); err != nil {
			return nil, status.Error(codes.Unavailable, "Gateway key revocation readback unavailable")
		}
	}
	now := time.Now().UTC()
	return &api.Operation{OperationId: key.ID, Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_GATEWAY, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_REVOKE_KEY,
		ResourceId: key.ID, Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_SUCCEEDED,
		RequestId: r.GetContext().GetRequestId(), CreatedAt: stampOf(key.CreatedAt), UpdatedAt: stampOf(now)}, nil
}

// coordinationCaller refuses a call whose authenticated peer is not one of the
// owners that may originate this settlement.
func (s *Service) coordinationCaller(ctx context.Context, allowed ...owneridentity.Service) error {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "verified calling owner required")
	}
	for _, service := range allowed {
		if peer == service {
			return nil
		}
	}
	return status.Error(codes.PermissionDenied, "calling owner may not use Gateway coordination")
}

func (s *Service) authorizeWalletBinding(ctx context.Context, call *api.CallContext, tenantID string) error {
	req := &api.AuthorizationRequest{
		Scope: call.GetScope(), ActorId: call.GetActorId(), SessionId: call.SessionId, AcceptedOperationGrantId: call.AcceptedOperationGrantId,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_TENANT, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDTENANTWALLET,
		Resource:  &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: proto.String(tenantID)},
		RequestId: call.GetRequestId(),
	}
	return s.authorizeInternal(ctx, owneridentity.ConsoleBFF, req)
}

func (s *Service) authorizeWalletWrite(ctx context.Context, call *api.CallContext, kind, tenantID, workspaceID string) error {
	action := api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CHARGEACCEPTEDOBLIGATION
	if kind == "refund" {
		action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REFUNDCONFIRMEDDELETION
	}
	return s.authorizeInternal(ctx, owneridentity.Gateway.Service(), &api.AuthorizationRequest{
		Scope: call.GetScope(), ActorId: call.GetActorId(), SessionId: call.SessionId, AcceptedOperationGrantId: call.AcceptedOperationGrantId,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_GATEWAY, Action: action,
		Resource:  &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String(workspaceID)},
		RequestId: call.GetRequestId(),
	})
}

func (s *Service) authorizeWalletRead(ctx context.Context, call *api.CallContext, record WalletRecord) error {
	return s.authorizeInternal(ctx, owneridentity.Gateway.Service(), &api.AuthorizationRequest{
		Scope: call.GetScope(), ActorId: call.GetActorId(), SessionId: call.SessionId, AcceptedOperationGrantId: call.AcceptedOperationGrantId,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_GATEWAY, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_READWALLETACTION,
		Resource:  &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String(record.WorkspaceID)},
		RequestId: call.GetRequestId(),
	})
}

func (s *Service) authorizeManagedKey(ctx context.Context, call *api.CallContext, tenantID, workspaceID string) error {
	return s.authorizeInternal(ctx, owneridentity.Gateway.Service(), &api.AuthorizationRequest{
		Scope: call.GetScope(), ActorId: call.GetActorId(), SessionId: call.SessionId, AcceptedOperationGrantId: call.AcceptedOperationGrantId,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_GATEWAY, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDMANAGEDSECRET,
		Resource:  &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String(workspaceID)},
		RequestId: call.GetRequestId(),
	})
}

// authorizeInternal resolves an authorization decision against the CloudIdentity
// authority that shares this process. The CloudIdentity caller check requires the
// presenting principal to be the audience owner, exactly as the typed
// ownerservice.Authorizer presents the receiving owner over its own gRPC
// connection. This method reproduces that identity locally before re-validating
// the returned decision, so an in-process owner cannot present a peer it does not
// hold.
func (s *Service) authorizeInternal(ctx context.Context, _ owneridentity.Service, req *api.AuthorizationRequest) error {
	audience := owneridentity.Service(strings.ToLower(strings.TrimPrefix(req.GetAudienceOwner().String(), "OWNER_ENUM_")))
	local := ownerservice.WithPeerOwner(ctx, audience)
	decision, err := s.AuthorizeAction(local, req)
	if err != nil {
		return err
	}
	if err := owneridentity.ValidateDecision(req, decision, time.Now()); err != nil {
		return status.Error(codes.PermissionDenied, err.Error())
	}
	return nil
}

func walletBusinessKey(call *api.CallContext) string {
	if call == nil {
		return ""
	}
	return call.GetIdempotencyKey()
}

func walletRequestFingerprint(req walletActionRequest) string {
	parts := []string{req.kind, req.workspaceID, req.businessKey, req.original, req.entitlement, req.deletionReceipt}
	return shortDigest(strings.Join(parts, "\x00") + "|" + itoa(req.amount))
}

func shortDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

// derivedManagedKeyCall is the continuation context of the approved Secret write.
// The caller's authorization context is never forwarded: Fabric reauthorizes the
// original accepted obligation from its own evidence. The idempotency key stays
// the original command's bounded key, so the approved store sees the same write
// identity on every retry of this command.
func derivedManagedKeyCall(call *api.CallContext) *api.CallContext {
	if call == nil {
		return nil
	}
	derived := proto.Clone(call).(*api.CallContext)
	derived.AuthorizationContextId = ""
	return derived
}

// equalDeclaredModelScope reports whether a shape-validated resolved scope is
// exactly the declared selection as a set: no superset, no subset, no
// substitution and no repeat. Ordering carries no meaning, and a repeated
// declared entry names the same model as its first occurrence.
func equalDeclaredModelScope(declared, resolved []string) bool {
	unique := make(map[string]struct{}, len(declared))
	for _, id := range declared {
		unique[id] = struct{}{}
	}
	if len(unique) != len(resolved) {
		return false
	}
	for _, id := range resolved {
		if _, ok := unique[id]; !ok {
			return false
		}
	}
	return true
}

// equalStringSets compares two model scopes as sets, because a resolved scope is
// an unordered selection and a readback must not fail on ordering alone.
func equalStringSets(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, value := range left {
		counts[value]++
	}
	for _, value := range right {
		if counts[value] == 0 {
			return false
		}
		counts[value]--
	}
	return true
}

// gatewayAdjustmentCode derives the stable business code carried in the native
// Sub2API balance adjustment. The upstream note code is limited to 32 characters,
// so the full obligation key is hashed; the hash is deterministic, so a replay
// lands on the one original native adjustment and its native history lookup.
func gatewayAdjustmentCode(businessKey string) string {
	sum := sha256.Sum256([]byte(businessKey))
	return "gw" + hex.EncodeToString(sum[:15])
}

func itoa(value int64) string { return strconv.FormatInt(value, 10) }

func parseGatewaySubject(subject string) (int64, error) {
	var out int64
	for _, r := range strings.TrimSpace(subject) {
		if r < '0' || r > '9' {
			return 0, status.Error(codes.FailedPrecondition, "invalid Gateway wallet subject")
		}
		out = out*10 + int64(r-'0')
	}
	if out <= 0 {
		return 0, status.Error(codes.FailedPrecondition, "invalid Gateway wallet subject")
	}
	return out, nil
}

func isUnknownAdjustmentError(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrGatewayChargeUnknown.Error())
}

func errorCodeForAdjustment(err error) string {
	if err == nil {
		return ""
	}
	if strings.Contains(err.Error(), ErrGatewayAdjustmentUnrepresentable.Error()) {
		return "INVALID_ARGUMENT"
	}
	return "DEPENDENCY_UNAVAILABLE"
}
