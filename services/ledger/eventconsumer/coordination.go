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

// receiptKindAccess names, for every receipt kind, the authenticated peer
// principal that may write it and the peers whose read of it is accepted.
//
// The kind decides the writer and the evidence authority: a Local no-charge
// receipt is funded by its own accepted zero-charge order, a wallet action
// receipt is funded by the Gateway charge it names, a provider action receipt
// is bound to the Workspace owner commit that accepted the action, and an
// administrator balance change is bound to the Gateway owner's own confirmed
// wallet operation. None can stand in for another, so an unknown kind is
// refused rather than routed to a default, and no principal may write a kind
// that is not its own.
//
// The Gateway Integration process is the tenant deployment unit: it carries
// the CloudIdentity and the Gateway owner in one process and presents the
// tenant transport principal, because no process can present a gateway
// certificate. The Gateway owner therefore writes its own receipt kind under
// that principal, and the Ledger proves the fact by re-reading the named
// wallet operation from the Gateway owner itself before it stores anything.
type receiptKindAccess struct {
	peer    owneridentity.Service
	readers []owneridentity.Service
	writer  func(*Server, context.Context, *api.AppendReceiptRequest) (*api.Receipt, error)
}

var receiptKindAccessTable = map[api.ReceiptKindEnum]receiptKindAccess{
	api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION: {
		peer:    owneridentity.Workspace.Service(),
		readers: []owneridentity.Service{owneridentity.Workspace.Service(), owneridentity.Fabric.Service()},
		writer:  (*Server).appendWalletActionReceipt,
	},
	api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE: {
		peer:    owneridentity.Workspace.Service(),
		readers: []owneridentity.Service{owneridentity.Workspace.Service(), owneridentity.Fabric.Service()},
		writer:  (*Server).appendLocalNoChargeReceipt,
	},
	api.ReceiptKindEnum_RECEIPT_KIND_ENUM_PROVIDER_ACTION: {
		peer:    owneridentity.Workspace.Service(),
		readers: []owneridentity.Service{owneridentity.Workspace.Service(), owneridentity.Fabric.Service()},
		writer:  (*Server).appendProviderActionReceipt,
	},
	api.ReceiptKindEnum_RECEIPT_KIND_ENUM_GATEWAY_WALLET_ADJUSTMENT: {
		peer:    owneridentity.Tenant.Service(),
		readers: []owneridentity.Service{owneridentity.Tenant.Service()},
		writer:  (*Server).appendGatewayWalletAdjustmentReceipt,
	},
}

// peerMayWriteAnyKind reports whether the authenticated peer is a principal
// that may write at least one receipt kind. A peer that may write nothing is
// refused before the kind is read, so an unknown kind never turns a peer that
// writes nothing into an accepted caller.
func peerMayWriteAnyKind(peer owneridentity.Service) bool {
	for _, access := range receiptKindAccessTable {
		if access.peer == peer {
			return true
		}
	}
	return false
}

// peerMayReadKind reports whether the authenticated peer may read the named
// receipt kind.
func peerMayReadKind(peer owneridentity.Service, kind api.ReceiptKindEnum) bool {
	access, known := receiptKindAccessTable[kind]
	if !known {
		return false
	}
	for _, reader := range access.readers {
		if reader == peer {
			return true
		}
	}
	return false
}

func (s *Server) AppendReceipt(ctx context.Context, r *api.AppendReceiptRequest) (*api.Receipt, error) {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || !peerMayWriteAnyKind(peer) {
		return nil, status.Error(codes.Unauthenticated, "verified receipt owner peer required")
	}
	kind := r.GetReceipt().GetKind()
	access, known := receiptKindAccessTable[kind]
	if !known {
		return nil, status.Error(codes.InvalidArgument, "unsupported receipt kind for this owner")
	}
	if access.peer != peer {
		return nil, status.Error(codes.Unauthenticated, "verified "+access.peer.String()+" peer required")
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	return access.writer(s, ctx, r)
}

// appendProviderActionReceipt records the owner-authoritative absence proof for a
// confirmed Workspace provider action, such as a deletion whose runtime and
// provider resources are both gone. The evidence authority is the Workspace owner
// commit itself, which is re-read from its own owner before anything is stored,
// so a fabricated or replayed deletion is refused. There is no accepted quote and
// no wallet operation to read: this receipt moves no money, and a confirmed
// absence is stored only against its own Workspace commit.
func (s *Server) appendProviderActionReceipt(ctx context.Context, r *api.AppendReceiptRequest) (*api.Receipt, error) {
	if err := ledger.ValidateProviderActionReceiptInput(r); err != nil {
		return nil, receiptError(err)
	}
	commit := r.OwnerCommitEvidence
	workspaceID := commit.GetResourceId()
	if err := s.authorizeReceipt(ctx, r.Context, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT, workspaceID, commit.Scope.GetTenant().GetTenantId(), commit.ActorId); err != nil {
		return nil, err
	}
	if s.Workspace == nil {
		return nil, status.Error(codes.Unavailable, "Workspace owner readback required")
	}
	if err := s.readWorkspaceCommit(ctx, r); err != nil {
		return nil, err
	}
	stored, err := s.store.RecordProviderActionReceipt(ctx, r, func(ctx context.Context) error {
		freshCall := proto.Clone(r.Context).(*api.CallContext)
		freshCall.AuthorizationContextId = ""
		return s.authorizeReceipt(ctx, freshCall, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT, workspaceID, commit.Scope.GetTenant().GetTenantId(), commit.ActorId)
	})
	if err != nil {
		return nil, receiptError(err)
	}
	return stored.Evidence.Receipt, nil
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

// appendGatewayWalletAdjustmentReceipt records the owner-authoritative proof of
// one confirmed administrator balance change. The Gateway wallet operation
// named in the evidence is re-read from its own owner before it is stored, so a
// fabricated or replayed operator, amount, reason or receipt id is refused.
// The adjustment is tenant-scoped, so the authorization binds the Tenant it
// belongs to rather than a Workspace.
func (s *Server) appendGatewayWalletAdjustmentReceipt(ctx context.Context, r *api.AppendReceiptRequest) (*api.Receipt, error) {
	if err := ledger.ValidateGatewayWalletAdjustmentReceiptInput(r); err != nil {
		return nil, receiptError(err)
	}
	wallet := r.GatewayWalletAdjustmentEvidence.GetWalletOperation()
	tenantID := r.Context.GetScope().GetTenant().GetTenantId()
	if err := s.authorizeTenantReceipt(ctx, r.Context, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT, tenantID, wallet.GetActorId()); err != nil {
		return nil, err
	}
	if s.Gateway == nil {
		return nil, status.Error(codes.Unavailable, "Gateway owner readback required")
	}
	if err := s.readGatewayAdjustment(ctx, r); err != nil {
		return nil, err
	}
	stored, err := s.store.RecordGatewayWalletAdjustmentReceipt(ctx, r, func(ctx context.Context) error {
		freshCall := proto.Clone(r.Context).(*api.CallContext)
		freshCall.AuthorizationContextId = ""
		return s.authorizeTenantReceipt(ctx, freshCall, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT, tenantID, wallet.GetActorId())
	})
	if err != nil {
		return nil, receiptError(err)
	}
	return stored.Evidence.Receipt, nil
}

// readGatewayAdjustment re-reads the named wallet operation from the Gateway
// owner. The returned operation must equal the submitted evidence: the same
// owner, tenant, kind, amount, operator, reason, confirmed status and receipt
// id. A balance change the Gateway owner has not already confirmed is not a
// recorded fact.
func (s *Server) readGatewayAdjustment(ctx context.Context, r *api.AppendReceiptRequest) error {
	call := proto.Clone(r.Context).(*api.CallContext)
	call.AuthorizationContextId = ""
	actual, err := s.Gateway.ReadWalletAction(ctx, &api.WalletReadbackRequest{Context: call, WalletOperationId: r.GatewayWalletAdjustmentEvidence.GetWalletOperation().GetId()})
	if err != nil {
		return err
	}
	if !proto.Equal(actual, r.GatewayWalletAdjustmentEvidence.GetWalletOperation()) {
		return status.Error(codes.FailedPrecondition, "Gateway wallet adjustment differs from submitted evidence")
	}
	return nil
}

func (s *Server) ReadReceiptByReference(ctx context.Context, r *api.GetReceiptByReferenceRequest) (*api.Receipt, error) {
	// The stored receipt types are disjoint and each names the owner that wrote
	// it, so the reference is resolved under the owner it names: a Workspace
	// reference resolves to the Workspace-owned evidence, a Gateway reference to
	// the Gateway owner's own administrator balance change.
	switch r.GetOwner() {
	case "gateway":
		evidence, err := s.ReadGatewayWalletAdjustmentReceipt(ctx, r)
		if err != nil {
			return nil, err
		}
		return evidence.Receipt, nil
	case "workspace":
		// The three stored Workspace receipt types are disjoint: an obligation is
		// funded exactly one way, and a provider action is a confirmed absence
		// that funds nothing. The reference is read as a wallet action, then as
		// a Local no-charge receipt, then as a provider action; a reference
		// absent from all three is not a recorded fact.
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
		// The provider action is the third arm of this resolver and is deliberately not
		// a separate RPC: no owner reads its typed evidence envelope, because a caller
		// that already holds the appended Receipt reads it back in Receipt space. So the
		// arm reaches the store directly, under the same Workspace authorization the two
		// funded readbacks apply, and returns the stored Receipt.
		stored, storeErr := s.store.ReadProviderActionReceipt(ctx, r.GetOwnerEvidenceReference())
		if storeErr != nil {
			return nil, receiptError(storeErr)
		}
		if err = s.authorizeStoredReceipt(ctx, r, stored.WorkspaceID, stored.TenantID, stored.Evidence.GetOwnerCommitEvidence()); err != nil {
			return nil, err
		}
		return stored.Evidence.Receipt, nil
	default:
		return nil, status.Error(codes.InvalidArgument, "original owner reference required")
	}
}

func (s *Server) ReadWalletActionReceipt(ctx context.Context, r *api.GetReceiptByReferenceRequest) (*api.WalletActionReceiptEvidence, error) {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || !peerMayReadKind(peer, api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION) {
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
	if err := s.authorizeStoredReceipt(ctx, r, stored.WorkspaceID, stored.TenantID, stored.Evidence.GetOwnerCommitEvidence()); err != nil {
		return nil, err
	}
	return stored.Evidence, nil
}

func (s *Server) ReadLocalNoChargeReceipt(ctx context.Context, r *api.GetReceiptByReferenceRequest) (*api.LocalNoChargeReceiptEvidence, error) {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || !peerMayReadKind(peer, api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE) {
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
	if err := s.authorizeStoredReceipt(ctx, r, stored.WorkspaceID, stored.TenantID, stored.Evidence.GetOwnerCommitEvidence()); err != nil {
		return nil, err
	}
	return stored.Evidence, nil
}

// ReadGatewayWalletAdjustmentReceipt returns the typed evidence of one
// administrator balance change the Gateway owner recorded. It is read by the
// Gateway owner's own wallet operation id, never fabricated.
func (s *Server) ReadGatewayWalletAdjustmentReceipt(ctx context.Context, r *api.GetReceiptByReferenceRequest) (*api.GatewayWalletAdjustmentReceiptEvidence, error) {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || !peerMayReadKind(peer, api.ReceiptKindEnum_RECEIPT_KIND_ENUM_GATEWAY_WALLET_ADJUSTMENT) {
		return nil, status.Error(codes.Unauthenticated, "verified Gateway owner peer required")
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	if r.GetOwner() != "gateway" || strings.TrimSpace(r.GetOwnerEvidenceReference()) == "" {
		return nil, status.Error(codes.InvalidArgument, "original Gateway owner reference required")
	}
	stored, err := s.store.ReadGatewayWalletAdjustmentReceipt(ctx, r.OwnerEvidenceReference)
	if err != nil {
		return nil, receiptError(err)
	}
	if err := s.authorizeTenantReceipt(ctx, r.Context, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETRECEIPT, stored.TenantID, stored.Evidence.GetWalletOperation().GetActorId()); err != nil {
		return nil, err
	}
	return stored.Evidence, nil
}

// authorizeStoredReceipt admits a stored receipt read under the Workspace that owns
// it and the actor that committed it.
//
// The authority is the immutable OwnerCommitEvidence the evidence owner accepted and
// Ledger re-read at write time — never the stored receipt's own provenance block.
// That block is caller-owned and privacy-mutable: a privacy redaction clears
// receipt.Actor, and Authorizer treats an empty scope actor as "no actor fence"
// rather than "refuse", so binding reads to it silently widens every redacted
// receipt to any actor that clears the tenant and workspace fences. The commit
// lives inside the evidence blob, which redaction does not touch, so the fence
// survives. Tenant and Workspace identity still come from the stored row, because
// neither is privacy-mutable and both are what scope the read.
//
// An evidence blob that names no committing actor is refused rather than read
// unrestricted: fail closed, so a missing actor can never degrade into no fence.
func (s *Server) authorizeStoredReceipt(ctx context.Context, r *api.GetReceiptByReferenceRequest, workspaceID, tenantID string, commit *api.OwnerCommitEvidence) error {
	actor := commit.GetActorId()
	if actor == "" {
		return status.Error(codes.FailedPrecondition, "stored receipt evidence names no committing actor")
	}
	return s.authorizeReceipt(ctx, r.Context, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETRECEIPT, workspaceID, tenantID, actor)
}

func (s *Server) authorizeReceipt(ctx context.Context, call *api.CallContext, action api.AuthorizationActionEnum, workspaceID, tenantID, actorID string) error {
	resource := &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String(workspaceID)}
	return s.Authorizer.Authorize(ctx, call, action, resource, ownerservice.ResourceScope{TenantID: tenantID, ActorID: actorID})
}

// authorizeTenantReceipt admits a tenant-scoped receipt under the Tenant that
// owns it and the operator that ordered it.
//
// An administrator balance change names a Tenant, never a Workspace, so the
// resource is the Tenant itself. The committing actor is the operator the
// Gateway owner's own confirmed wallet operation carries, which the Ledger
// re-read from that owner before it stored anything; it is not the stored
// receipt's own privacy-mutable provenance block. An evidence blob that names
// no operator is refused rather than read unrestricted: fail closed, so a
// missing actor can never degrade into no fence.
func (s *Server) authorizeTenantReceipt(ctx context.Context, call *api.CallContext, action api.AuthorizationActionEnum, tenantID, actorID string) error {
	if strings.TrimSpace(actorID) == "" {
		return status.Error(codes.FailedPrecondition, "stored receipt evidence names no committing actor")
	}
	resource := &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: proto.String(tenantID)}
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
