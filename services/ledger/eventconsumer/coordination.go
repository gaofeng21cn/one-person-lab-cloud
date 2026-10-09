package eventconsumer

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/ledger/internal/ledger"
)

func (s *Server) AppendReceipt(ctx context.Context, r *api.AppendReceiptRequest) (*api.Receipt, error) {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || peer != owneridentity.Workspace.Service() {
		return nil, status.Error(codes.Unauthenticated, "verified Workspace peer required")
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	// The receipt kind decides the writer and the evidence authority: a Local
	// no-charge receipt is funded by its own accepted zero-charge order, a wallet
	// action receipt is funded by the Gateway charge it names. Neither can stand in
	// for the other, so an unknown kind is refused rather than routed to a default.
	switch r.GetReceipt().GetKind() {
	case api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION:
		return s.appendWalletActionReceipt(ctx, r)
	case api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE:
		return s.appendLocalNoChargeReceipt(ctx, r)
	case api.ReceiptKindEnum_RECEIPT_KIND_ENUM_PROVIDER_ACTION:
		return s.appendDeletionReceipt(ctx, r)
	default:
		return nil, status.Error(codes.InvalidArgument, "unsupported receipt kind for this owner")
	}
}

// appendDeletionReceipt records the confirmed Workspace deletion. The evidence is
// the Workspace owner's own DELETEWORKSPACE commit, re-read from that owner, so a
// caller cannot record a deletion the owner never accepted and a refund can rely
// on evidence that really exists.
func (s *Server) appendDeletionReceipt(ctx context.Context, r *api.AppendReceiptRequest) (*api.Receipt, error) {
	if err := ledger.ValidateDeletionReceiptInput(r); err != nil {
		return nil, receiptError(err)
	}
	commit := r.OwnerCommitEvidence
	if err := s.authorizeReceipt(ctx, r.Context, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT, commit.ResourceId, commit.Scope.GetTenant().GetTenantId(), commit.ActorId); err != nil {
		return nil, err
	}
	if s.Workspace == nil {
		return nil, status.Error(codes.Unavailable, "Workspace owner readback required")
	}
	if err := s.readWorkspaceCommit(ctx, r); err != nil {
		return nil, err
	}
	stored, err := s.store.RecordDeletionReceipt(ctx, r, func(ctx context.Context) error {
		freshCall := proto.Clone(r.Context).(*api.CallContext)
		freshCall.AuthorizationContextId = ""
		return s.authorizeReceipt(ctx, freshCall, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT, commit.ResourceId, commit.Scope.GetTenant().GetTenantId(), commit.ActorId)
	})
	if err != nil {
		return nil, receiptError(err)
	}
	return stored.Evidence, nil
}

func (s *Server) appendLocalNoChargeReceipt(ctx context.Context, r *api.AppendReceiptRequest) (*api.Receipt, error) {
	if err := ledger.ValidateLocalNoChargeReceiptInput(r); err != nil {
		return nil, receiptError(err)
	}
	q, commit := r.QuoteAcceptance, r.OwnerCommitEvidence
	if err := s.authorizeReceipt(ctx, r.Context, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT, q.WorkspaceId, commit.Scope.GetTenant().GetTenantId(), commit.ActorId); err != nil {
		return nil, err
	}
	if s.Catalog == nil || s.Workspace == nil {
		return nil, status.Error(codes.Unavailable, "Catalog and Workspace owner readback required")
	}
	if err := s.readAcceptedQuote(ctx, r, false); err != nil {
		return nil, err
	}
	if err := s.readWorkspaceCommit(ctx, r); err != nil {
		return nil, err
	}
	stored, err := s.store.RecordLocalNoChargeReceipt(ctx, r, func(ctx context.Context) error {
		freshCall := proto.Clone(r.Context).(*api.CallContext)
		freshCall.AuthorizationContextId = ""
		return s.authorizeReceipt(ctx, freshCall, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT, q.WorkspaceId, commit.Scope.GetTenant().GetTenantId(), commit.ActorId)
	})
	if err != nil {
		return nil, receiptError(err)
	}
	return stored.Evidence.Receipt, nil
}

// appendWalletActionReceipt records the owner-authoritative funded charge for one
// paid Workspace obligation. The Gateway wallet operation named in the evidence is
// re-read from its own owner before it is stored, so a fabricated or replayed
// amount, workspace or receipt id is refused. Only a confirmed charge is stored.
func (s *Server) appendWalletActionReceipt(ctx context.Context, r *api.AppendReceiptRequest) (*api.Receipt, error) {
	if err := ledger.ValidateWalletActionReceiptInput(r); err != nil {
		return nil, receiptError(err)
	}
	q, commit := r.QuoteAcceptance, r.OwnerCommitEvidence
	if err := s.authorizeReceipt(ctx, r.Context, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT, q.WorkspaceId, commit.Scope.GetTenant().GetTenantId(), commit.ActorId); err != nil {
		return nil, err
	}
	if s.Catalog == nil || s.Workspace == nil || s.Gateway == nil {
		return nil, status.Error(codes.Unavailable, "Catalog, Workspace and Gateway owner readback required")
	}
	if err := s.readAcceptedQuote(ctx, r, true); err != nil {
		return nil, err
	}
	if err := s.readWorkspaceCommit(ctx, r); err != nil {
		return nil, err
	}
	if err := s.readGatewayCharge(ctx, r); err != nil {
		return nil, err
	}
	stored, err := s.store.RecordWalletActionReceipt(ctx, r, func(ctx context.Context) error {
		freshCall := proto.Clone(r.Context).(*api.CallContext)
		freshCall.AuthorizationContextId = ""
		return s.authorizeReceipt(ctx, freshCall, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT, q.WorkspaceId, commit.Scope.GetTenant().GetTenantId(), commit.ActorId)
	})
	if err != nil {
		return nil, receiptError(err)
	}
	return stored.Evidence.Receipt, nil
}

// readAcceptedQuote re-reads the frozen accepted offer from its owning Catalog and
// refuses evidence that differs from it. paid selects the money branch so a paid
// receipt can never be satisfied by a zero-charge plan or the reverse.
func (s *Server) readAcceptedQuote(ctx context.Context, r *api.AppendReceiptRequest, paid bool) error {
	q := r.QuoteAcceptance
	// An authorization context is bound to its owner audience. Catalog obtains
	// its own live decision from the same session or accepted operation grant.
	catalogCall := proto.Clone(r.Context).(*api.CallContext)
	catalogCall.AuthorizationContextId = ""
	actualQuote, err := s.Catalog.ReadQuoteResourcePlan(ctx, &api.QuoteResourcePlanRequest{Context: catalogCall, QuoteId: q.Quote.Id})
	if err != nil {
		return err
	}
	if !proto.Equal(actualQuote, q) {
		return status.Error(codes.FailedPrecondition, "accepted Catalog quote differs from submitted evidence")
	}
	if paid == (q.GetResourcePlan().GetBillingMode() == "LOCAL_NO_CHARGE") {
		return status.Error(codes.FailedPrecondition, "receipt kind does not match the accepted billing mode")
	}
	return nil
}

func (s *Server) readWorkspaceCommit(ctx context.Context, r *api.AppendReceiptRequest) error {
	commit := r.OwnerCommitEvidence
	actualCommit, err := s.Workspace.ReadOwnerCommit(ctx, &api.ReadOwnerCommitRequest{Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: commit.OperationId, ResourceId: commit.ResourceId})
	if err != nil {
		return err
	}
	if !proto.Equal(actualCommit, commit) {
		return status.Error(codes.FailedPrecondition, "Workspace commit differs from submitted evidence")
	}
	return nil
}

// readGatewayCharge re-reads the named wallet operation from Gateway Integration.
// The returned operation must equal the submitted evidence: the same owner,
// amount, kind, confirmed status and receipt id. A charge that the wallet owner
// has not already confirmed is not a funded obligation.
func (s *Server) readGatewayCharge(ctx context.Context, r *api.AppendReceiptRequest) error {
	call := proto.Clone(r.Context).(*api.CallContext)
	call.AuthorizationContextId = ""
	actual, err := s.Gateway.ReadWalletAction(ctx, &api.WalletReadbackRequest{Context: call, WalletOperationId: r.WalletOperation.GetId()})
	if err != nil {
		return err
	}
	if !proto.Equal(actual, r.WalletOperation) {
		return status.Error(codes.FailedPrecondition, "Gateway wallet charge differs from submitted evidence")
	}
	return nil
}

func (s *Server) ReadReceiptByReference(ctx context.Context, r *api.GetReceiptByReferenceRequest) (*api.Receipt, error) {
	// Each typed obligation is recorded exactly one way, so the stored receipt
	// types are disjoint: one owner reference resolves to a funded charge, a Local
	// no-charge obligation, or a confirmed deletion. A reference absent from all
	// three is not evidence of any of them.
	evidence, err := s.ReadWalletActionReceipt(ctx, r)
	if err == nil {
		return evidence.Receipt, nil
	}
	if status.Code(err) != codes.NotFound {
		return nil, err
	}
	local, localErr := s.ReadLocalNoChargeReceipt(ctx, r)
	if localErr == nil {
		return local.Receipt, nil
	}
	if status.Code(localErr) != codes.NotFound {
		return nil, localErr
	}
	return s.readDeletionReceipt(ctx, r)
}

// readDeletionReceipt returns the recorded deletion receipt by the Workspace
// owner's own reference. The contract reads it through the one
// ReadReceiptByReference operation, so this is that path's deletion arm rather
// than another RPC. DELETEWORKSPACE is not the sole accepted action that may
// record a deletion: releasing a resource set currently authorizes itself with
// the accepted resources write, but the deletion evidence is always recorded
// against the deletion's own operation. The same GETRECEIPT admission the other
// typed reads use decides delivery, so the refunding owner and the recording
// owner read the identical receipt.
func (s *Server) readDeletionReceipt(ctx context.Context, r *api.GetReceiptByReferenceRequest) (*api.Receipt, error) {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || (peer != owneridentity.Workspace.Service() && peer != owneridentity.Fabric.Service() && peer != owneridentity.Gateway.Service()) {
		return nil, status.Error(codes.Unauthenticated, "verified Workspace, Fabric or Gateway peer required")
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	if r.GetOwner() != "workspace" || strings.TrimSpace(r.GetOwnerEvidenceReference()) == "" {
		return nil, status.Error(codes.InvalidArgument, "original Workspace owner reference required")
	}
	stored, err := s.store.ReadDeletionReceipt(ctx, r.OwnerEvidenceReference)
	if err != nil {
		return nil, receiptError(err)
	}
	if err := s.authorizeReceipt(ctx, r.Context, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETRECEIPT, stored.WorkspaceID, stored.TenantID, stored.ActorID); err != nil {
		return nil, err
	}
	return stored.Evidence, nil
}

func (s *Server) ReadWalletActionReceipt(ctx context.Context, r *api.GetReceiptByReferenceRequest) (*api.WalletActionReceiptEvidence, error) {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || (peer != owneridentity.Workspace.Service() && peer != owneridentity.Fabric.Service()) {
		return nil, status.Error(codes.Unauthenticated, "verified Workspace or Fabric peer required")
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	if r.GetOwner() != "workspace" || strings.TrimSpace(r.GetOwnerEvidenceReference()) == "" {
		return nil, status.Error(codes.InvalidArgument, "original Workspace owner reference required")
	}
	stored, err := s.store.ReadWalletActionReceipt(ctx, r.OwnerEvidenceReference)
	if err != nil {
		return nil, receiptError(err)
	}
	if err := s.authorizeReceipt(ctx, r.Context, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETRECEIPT, stored.WorkspaceID, stored.TenantID, stored.ActorID); err != nil {
		return nil, err
	}
	return stored.Evidence, nil
}

func (s *Server) ReadLocalNoChargeReceipt(ctx context.Context, r *api.GetReceiptByReferenceRequest) (*api.LocalNoChargeReceiptEvidence, error) {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || (peer != owneridentity.Workspace.Service() && peer != owneridentity.Fabric.Service()) {
		return nil, status.Error(codes.Unauthenticated, "verified Workspace or Fabric peer required")
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	if r.GetOwner() != "workspace" || strings.TrimSpace(r.GetOwnerEvidenceReference()) == "" {
		return nil, status.Error(codes.InvalidArgument, "original Workspace owner reference required")
	}
	stored, err := s.store.ReadLocalNoChargeReceipt(ctx, r.OwnerEvidenceReference)
	if err != nil {
		return nil, receiptError(err)
	}
	if err := s.authorizeReceipt(ctx, r.Context, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETRECEIPT, stored.WorkspaceID, stored.TenantID, stored.ActorID); err != nil {
		return nil, err
	}
	return stored.Evidence, nil
}

func (s *Server) authorizeReceipt(ctx context.Context, call *api.CallContext, action api.AuthorizationActionEnum, workspaceID, tenantID, actorID string) error {
	resource := &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String(workspaceID)}
	return s.Authorizer.Authorize(ctx, call, action, resource, ownerservice.ResourceScope{TenantID: tenantID, ActorID: actorID})
}

func receiptError(err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}
	switch {
	case errors.Is(err, ledger.ErrInvalidReceiptInput):
		return status.Error(codes.InvalidArgument, "invalid Local no-charge receipt evidence")
	case errors.Is(err, ledger.ErrIdempotencyConflict):
		return status.Error(codes.AlreadyExists, "original receipt or idempotency key has conflicting evidence")
	case errors.Is(err, ledger.ErrReceiptNotFound):
		return status.Error(codes.NotFound, "original receipt not found")
	default:
		return status.Error(codes.Unavailable, "Ledger receipt persistence unavailable")
	}
}
