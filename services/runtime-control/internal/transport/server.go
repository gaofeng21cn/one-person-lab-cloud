// Package transport exposes the runtime control owner's internal typed gRPC surface.
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

	contracts "opl-cloud/packages/contracts/go"
	v226 "opl-cloud/packages/contracts/go/v226"
	"opl-cloud/services/runtime-control/internal/store"
)

// Server implements the runtime control owner's internal gRPC services over its own store.
type Server struct {
	v226.UnimplementedOwnerOperationsServer
	v226.UnimplementedOwnerCommitReadbackServer
	v226.UnimplementedDomainInboxServer

	store *store.Store
}

// NewServer returns the runtime control owner's internal server.
func NewServer(runtimeControlStore *store.Store) *Server {
	return &Server{store: runtimeControlStore}
}

// Read returns the owner-local operation. An operation id is unique only inside
// its owning context, so the request names the owner: a request for another owner
// is refused before this owner's store is touched, and this owner never scans
// another owner's records to answer.
func (s *Server) Read(ctx context.Context, request *v226.OwnerOperationRequest) (*v226.Operation, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	if err := requireOperationOwner(request.GetOwner()); err != nil {
		return nil, err
	}
	if request.GetOperationId() == "" {
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
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	if err := requireOperationOwner(request.GetOwner()); err != nil {
		return nil, err
	}
	if request.GetOperationId() == "" {
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

// ReadOwnerCommit returns this owner's real committed evidence for an operation.
// The owner answers from its own records; a caller-supplied claim is never
// accepted as proof. When this owner has no committed aggregate evidence yet it
// refuses instead of returning an empty digest or a fabricated version, because a
// caller must not sign a claim or a grant from evidence that does not exist.
func (s *Server) ReadOwnerCommit(ctx context.Context, request *v226.ReadOwnerCommitRequest) (*v226.OwnerCommitEvidence, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	if request.GetOwner() != v226.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL {
		return nil, status.Errorf(codes.InvalidArgument, "this endpoint serves owner %s", ownerName)
	}
	if request.GetOperationId() == "" {
		return nil, status.Error(codes.InvalidArgument, "operation id is required")
	}
	evidence, err := s.store.ReadCommittedEvidence(ctx, request.GetOperationId(), request.GetResourceId())
	switch {
	case errors.Is(err, store.ErrOperationNotFound):
		return nil, status.Error(codes.NotFound, "operation not found")
	case errors.Is(err, store.ErrInvalidOperationInput):
		return nil, status.Error(codes.InvalidArgument, "resource id does not match the operation")
	case errors.Is(err, store.ErrNoCommittedEvidence):
		return nil, status.Error(codes.FailedPrecondition,
			"no committed aggregate evidence for this operation; refusing instead of returning a placeholder digest or version")
	case err != nil:
		return nil, status.Error(codes.Internal, "read owner commit failed")
	}
	return &v226.OwnerCommitEvidence{
		Owner:               v226.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL,
		OperationId:         evidence.OperationID,
		ResourceId:          evidence.ResourceID,
		AcceptedInputDigest: evidence.AcceptedInputDigest,
		CommittedVersion:    evidence.CommittedVersion,
		AcceptedAt:          timestamppb.New(evidence.AcceptedAt.UTC()),
	}, nil
}

// Deliver records an inbound event in the consumer's own database. Delivery
// targets one logical Inbox: the request names the consumer owner, so a process
// that carries two owners still acknowledges each owner separately, and a request
// for another owner is refused before this owner's store is touched.
func (s *Server) Deliver(ctx context.Context, request *v226.DeliverEventRequest) (*v226.InboxAck, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	if err := requireConsumerOwner(request.GetConsumerOwner()); err != nil {
		return nil, err
	}
	if request.GetEvent() == nil {
		return nil, status.Error(codes.InvalidArgument, "event envelope is required")
	}
	if request.GetAuthenticatedProducer() == "" {
		return nil, status.Error(codes.Unauthenticated, "claimed producer is required")
	}
	event := request.GetEvent()
	identity, known := contracts.LookupEventIdentity(event.GetEventType(), event.GetSchemaVersion())
	if !known {
		return nil, status.Error(codes.InvalidArgument, "event type or schema version is not specified")
	}
	if identity.Owner != event.GetOwner() || event.GetOwner() != request.GetAuthenticatedProducer() {
		return nil, status.Error(codes.PermissionDenied, "event owner or claimed producer does not match the specified producer")
	}
	if !identity.Subscribed(ownerName) {
		return nil, status.Error(codes.FailedPrecondition, "this owner is not subscribed to the specified event version")
	}
	return nil, status.Error(codes.Unimplemented, "runtime control inbox dispatch is implemented with the runtime control action owner")
}

// ownerName is this owner's logical name in the v2.26 domain set.
const ownerName = "runtime_control"

// requireOperationOwner refuses a request that names another owner, before any
// store access, because an operation id is unique only inside its owner.
func requireOperationOwner(owner v226.OperationOwnerEnum) error {
	if owner != v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_RUNTIME_CONTROL {
		return status.Errorf(codes.InvalidArgument, "this endpoint serves owner %s", ownerName)
	}
	return nil
}

// requireConsumerOwner refuses a request that targets another owner's Inbox.
func requireConsumerOwner(owner v226.OwnerEnum) error {
	if owner != v226.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL {
		return status.Errorf(codes.InvalidArgument, "this endpoint serves owner %s", ownerName)
	}
	return nil
}

func toProtoOperation(operation store.Operation) *v226.Operation {
	return &v226.Operation{
		OperationId:       operation.ID,
		Owner:             v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_RUNTIME_CONTROL,
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
