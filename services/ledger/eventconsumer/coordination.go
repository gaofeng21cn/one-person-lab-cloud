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
	// An authorization context is bound to its owner audience. Catalog obtains
	// its own live decision from the same session or accepted operation grant.
	catalogCall := proto.Clone(r.Context).(*api.CallContext)
	catalogCall.AuthorizationContextId = ""
	actualQuote, err := s.Catalog.ReadQuoteResourcePlan(ctx, &api.QuoteResourcePlanRequest{Context: catalogCall, QuoteId: q.Quote.Id})
	if err != nil {
		return nil, err
	}
	if !proto.Equal(actualQuote, q) {
		return nil, status.Error(codes.FailedPrecondition, "accepted Catalog quote differs from submitted evidence")
	}
	actualCommit, err := s.Workspace.ReadOwnerCommit(ctx, &api.ReadOwnerCommitRequest{Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: commit.OperationId, ResourceId: commit.ResourceId})
	if err != nil {
		return nil, err
	}
	if !proto.Equal(actualCommit, commit) {
		return nil, status.Error(codes.FailedPrecondition, "Workspace commit differs from submitted evidence")
	}
	stored, err := s.store.RecordLocalNoChargeReceipt(ctx, r)
	if err != nil {
		return nil, receiptError(err)
	}
	return stored.Evidence.Receipt, nil
}

func (s *Server) ReadReceiptByReference(ctx context.Context, r *api.GetReceiptByReferenceRequest) (*api.Receipt, error) {
	evidence, err := s.ReadLocalNoChargeReceipt(ctx, r)
	if err != nil {
		return nil, err
	}
	return evidence.Receipt, nil
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
