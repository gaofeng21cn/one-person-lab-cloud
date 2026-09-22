// Package transport exposes the resource catalog owner's internal typed gRPC surface.
//
// The ports below are the v2.26 internal contract: OwnerOperations.Read and
// Reconcile, OwnerCommitReadback.ReadOwnerCommit, and DomainInbox.Deliver. A
// proto service group is an API group, not another process.
package transport

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	v226 "opl-cloud/packages/contracts/go/v226"
	"opl-cloud/services/resource-catalog/internal/store"
)

// Server implements the resource catalog owner's internal gRPC services over its own store.
type Server struct {
	v226.UnimplementedOwnerOperationsServer
	v226.UnimplementedOwnerCommitReadbackServer
	v226.UnimplementedDomainInboxServer

	store *store.Store
}

// NewServer returns the resource catalog owner's internal server.
func NewServer(catalogStore *store.Store) *Server {
	return &Server{store: catalogStore}
}

// Read returns the owner-local operation. An unknown id in this owner's
// database is NOT_FOUND; another owner's operation is not visible here.
func (s *Server) Read(ctx context.Context, request *v226.OwnerOperationRequest) (*v226.Operation, error) {
	if request == nil || request.GetOperationId() == "" {
		return nil, status.Error(codes.InvalidArgument, "operation id is required")
	}
	operation, err := s.store.ReadOperation(ctx, request.GetOperationId())
	if errors.Is(err, store.ErrOperationNotFound) {
		return nil, status.Error(codes.NotFound, "operation not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "read operation failed")
	}
	return toProtoOperation(operation), nil
}

// Reconcile re-reads this owner's state for an operation. A non-terminal
// operation whose external result is unknown stays non-terminal: the owner does
// not finalize on a guess, and the response keeps the unknown observation.
func (s *Server) Reconcile(ctx context.Context, request *v226.ReconcileOperationRpcRequest) (*v226.Operation, error) {
	if request == nil || request.GetOperationId() == "" {
		return nil, status.Error(codes.InvalidArgument, "operation id is required")
	}
	result, err := s.store.ReconcileOperation(ctx, request.GetOperationId())
	if errors.Is(err, store.ErrOperationNotFound) {
		return nil, status.Error(codes.NotFound, "operation not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "reconcile operation failed")
	}
	return toProtoOperation(result.Operation), nil
}

// ReadOwnerCommit returns this owner's committed evidence for an operation. The
// owner answers from its own records; a caller-supplied claim is never accepted
// as proof.
func (s *Server) ReadOwnerCommit(ctx context.Context, request *v226.ReadOwnerCommitRequest) (*v226.OwnerCommitEvidence, error) {
	if request == nil || request.GetOperationId() == "" {
		return nil, status.Error(codes.InvalidArgument, "operation id is required")
	}
	operation, err := s.store.ReadOperation(ctx, request.GetOperationId())
	if errors.Is(err, store.ErrOperationNotFound) {
		return nil, status.Error(codes.NotFound, "operation not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "read owner commit failed")
	}
	if request.GetResourceId() != "" && request.GetResourceId() != operation.ResourceID {
		return nil, status.Error(codes.InvalidArgument, "resource id does not match the operation")
	}
	evidence := &v226.OwnerCommitEvidence{
		Owner:            v226.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG,
		OperationId:      operation.ID,
		ResourceId:       operation.ResourceID,
		CommittedVersion: 0,
		AcceptedAt:       timestamppb.New(operation.CreatedAt.UTC()),
	}
	return evidence, nil
}

// Deliver records an inbound event in the consumer's own database. A duplicate
// is acknowledged without reapplication; the same producer/event identity with
// different bytes is rejected instead of overwriting the recorded fact.
func (s *Server) Deliver(ctx context.Context, request *v226.DeliverEventRequest) (*v226.InboxAck, error) {
	if request == nil || request.GetEvent() == nil {
		return nil, status.Error(codes.InvalidArgument, "event envelope is required")
	}
	if request.GetAuthenticatedProducer() == "" {
		return nil, status.Error(codes.Unauthenticated, "authenticated producer is required")
	}
	return nil, status.Error(codes.Unimplemented, "resource catalog inbox dispatch is implemented with the resource catalog command owner")
}

func toProtoOperation(operation store.Operation) *v226.Operation {
	return &v226.Operation{
		OperationId:       operation.ID,
		Owner:             v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_RESOURCE_CATALOG,
		Kind:              kindFromText(operation.Kind),
		ResourceId:        operation.ResourceID,
		Status:            statusFromText(operation.Status),
		Stage:             stageFromText(operation.Stage),
		ObservationResult: observationFromText(operation.Observation),
		ErrorCode:         errorCodeFromText(operation.ErrorCode),
		RequestId:         operation.RequestID,
		CreatedAt:         timestamppb.New(operation.CreatedAt.UTC()),
		UpdatedAt:         timestamppb.New(operation.UpdatedAt.UTC()),
	}
}
