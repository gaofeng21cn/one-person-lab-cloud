// Package transport exposes this deployment unit's internal typed gRPC surface.
//
// The ports below are the v2.26 internal contract: OwnerOperations.Read and
// Reconcile, OwnerCommitReadback.ReadOwnerCommit, and DomainInbox.Deliver. A
// proto service group is an API group, not another process.
//
// This unit carries two data owners — CloudIdentity (`tenant`) and Gateway
// Integration (`gateway`) — so every request that names an owner is routed to
// that owner's store and touches no other. The two stores are separate databases
// behind separate pools: an operation id is resolved inside the addressed owner
// only, an inbound delivery is acknowledged in the addressed owner's Inbox only,
// and a failure in the other owner's database never changes the answer for the
// addressed owner.
package transport

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	v226 "opl-cloud/packages/contracts/go/v226"
	gatewaystore "opl-cloud/services/gateway-integration/internal/gateway/store"
	tenantstore "opl-cloud/services/gateway-integration/internal/tenant/store"
)

// noCommittedEvidenceMessage is the refusal returned when an owner has no
// committed aggregate evidence for an operation. A caller must never sign a claim
// or a grant from a placeholder digest or a fabricated version.
const noCommittedEvidenceMessage = "no committed aggregate evidence for this operation; refusing instead of returning a placeholder digest or version"

// tenantOperations is the CloudIdentity owner surface this unit reads. The
// concrete implementation is the `tenant` store over the `opl_tenant` database.
type tenantOperations interface {
	ReadOperation(ctx context.Context, operationID string) (tenantstore.Operation, error)
	ReconcileOperation(ctx context.Context, operationID string) (tenantstore.ReconcileResult, error)
	ReadCommittedEvidence(ctx context.Context, operationID, resourceID string) (tenantstore.CommittedEvidence, error)
}

// gatewayOperations is the Gateway Integration owner surface this unit reads.
// The concrete implementation is the `gateway` store over the `opl_gateway`
// database.
type gatewayOperations interface {
	ReadOperation(ctx context.Context, operationID string) (gatewaystore.Operation, error)
	ReconcileOperation(ctx context.Context, operationID string) (gatewaystore.ReconcileResult, error)
	ReadCommittedEvidence(ctx context.Context, operationID, resourceID string) (gatewaystore.CommittedEvidence, error)
}

// Server implements this deployment unit's internal gRPC services over the two
// owner stores it serves.
type Server struct {
	v226.UnimplementedOwnerOperationsServer
	v226.UnimplementedOwnerCommitReadbackServer
	v226.UnimplementedDomainInboxServer

	tenant  tenantOperations
	gateway gatewayOperations
}

// NewServer returns the internal server for the CloudIdentity and Gateway
// Integration owners. Each store keeps its own connection pool; this server
// never opens a transaction across them.
func NewServer(tenant tenantOperations, gateway gatewayOperations) *Server {
	return &Server{tenant: tenant, gateway: gateway}
}

// Read returns the addressed owner's operation.
//
// v2.26 fixes the operation owner in the request because an operation id is
// unique only inside its owning context. This unit therefore resolves the request
// against exactly one owner's records: an owner it does not serve is refused
// before either store is touched, and the other owner's database is never
// consulted, so its availability cannot change the answer here.
func (s *Server) Read(ctx context.Context, request *v226.OwnerOperationRequest) (*v226.Operation, error) {
	route, ok := routeFromOperationOwner(request.GetOwner())
	if !ok {
		return nil, unservedOwnerError(request.GetOwner())
	}
	operationID := request.GetOperationId()
	if operationID == "" {
		return nil, status.Error(codes.InvalidArgument, "operation id is required")
	}

	if route == routeTenant {
		operation, err := s.tenant.ReadOperation(ctx, operationID)
		if errors.Is(err, tenantstore.ErrOperationNotFound) {
			return nil, status.Error(codes.NotFound, "operation not found")
		}
		if err != nil {
			return nil, status.Error(codes.Internal, "read operation failed")
		}
		return tenantOperationToProto(operation), nil
	}
	operation, err := s.gateway.ReadOperation(ctx, operationID)
	if errors.Is(err, gatewaystore.ErrOperationNotFound) {
		return nil, status.Error(codes.NotFound, "operation not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "read operation failed")
	}
	return gatewayOperationToProto(operation), nil
}

// Reconcile re-reads the addressed owner's state for an operation. A
// non-terminal operation whose external result is unknown stays non-terminal:
// the owner does not finalize on a guess, and the response keeps the unknown
// observation. Like Read, the request names the owner, so only that owner's store
// is consulted.
func (s *Server) Reconcile(ctx context.Context, request *v226.ReconcileOperationRpcRequest) (*v226.Operation, error) {
	route, ok := routeFromOperationOwner(request.GetOwner())
	if !ok {
		return nil, unservedOwnerError(request.GetOwner())
	}
	operationID := request.GetOperationId()
	if operationID == "" {
		return nil, status.Error(codes.InvalidArgument, "operation id is required")
	}

	if route == routeTenant {
		result, err := s.tenant.ReconcileOperation(ctx, operationID)
		if errors.Is(err, tenantstore.ErrOperationNotFound) {
			return nil, status.Error(codes.NotFound, "operation not found")
		}
		if err != nil {
			return nil, status.Error(codes.Internal, "reconcile operation failed")
		}
		return tenantOperationToProto(result.Operation), nil
	}
	result, err := s.gateway.ReconcileOperation(ctx, operationID)
	if errors.Is(err, gatewaystore.ErrOperationNotFound) {
		return nil, status.Error(codes.NotFound, "operation not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "reconcile operation failed")
	}
	return gatewayOperationToProto(result.Operation), nil
}

// ReadOwnerCommit returns the addressed owner's committed evidence for an
// operation. The owner answers from its own records; a caller-supplied claim is
// never accepted as proof. When the owner has no committed aggregate evidence for
// the operation it refuses with FAILED_PRECONDITION instead of returning an empty
// digest or a fabricated version.
func (s *Server) ReadOwnerCommit(ctx context.Context, request *v226.ReadOwnerCommitRequest) (*v226.OwnerCommitEvidence, error) {
	route, ok := routeFromOwner(request.GetOwner())
	if !ok {
		return nil, unservedOwnerError(request.GetOwner())
	}
	operationID := request.GetOperationId()
	if operationID == "" {
		return nil, status.Error(codes.InvalidArgument, "operation id is required")
	}
	resourceID := request.GetResourceId()

	if route == routeTenant {
		evidence, err := s.tenant.ReadCommittedEvidence(ctx, operationID, resourceID)
		switch {
		case errors.Is(err, tenantstore.ErrOperationNotFound):
			return nil, status.Error(codes.NotFound, "operation not found")
		case errors.Is(err, tenantstore.ErrInvalidOperationInput):
			return nil, status.Error(codes.InvalidArgument, "resource id does not match the operation")
		case errors.Is(err, tenantstore.ErrNoCommittedEvidence):
			return nil, status.Error(codes.FailedPrecondition, noCommittedEvidenceMessage)
		case err != nil:
			return nil, status.Error(codes.Internal, "read owner commit failed")
		}
		return &v226.OwnerCommitEvidence{
			Owner:               v226.OwnerEnum_OWNER_ENUM_TENANT,
			OperationId:         evidence.OperationID,
			ResourceId:          evidence.ResourceID,
			AcceptedInputDigest: evidence.AcceptedInputDigest,
			CommittedVersion:    evidence.CommittedVersion,
			AcceptedAt:          timestamppb.New(evidence.AcceptedAt.UTC()),
		}, nil
	}

	evidence, err := s.gateway.ReadCommittedEvidence(ctx, operationID, resourceID)
	switch {
	case errors.Is(err, gatewaystore.ErrOperationNotFound):
		return nil, status.Error(codes.NotFound, "operation not found")
	case errors.Is(err, gatewaystore.ErrInvalidOperationInput):
		return nil, status.Error(codes.InvalidArgument, "resource id does not match the operation")
	case errors.Is(err, gatewaystore.ErrNoCommittedEvidence):
		return nil, status.Error(codes.FailedPrecondition, noCommittedEvidenceMessage)
	case err != nil:
		return nil, status.Error(codes.Internal, "read owner commit failed")
	}
	return &v226.OwnerCommitEvidence{
		Owner:               v226.OwnerEnum_OWNER_ENUM_GATEWAY,
		OperationId:         evidence.OperationID,
		ResourceId:          evidence.ResourceID,
		AcceptedInputDigest: evidence.AcceptedInputDigest,
		CommittedVersion:    evidence.CommittedVersion,
		AcceptedAt:          timestamppb.New(evidence.AcceptedAt.UTC()),
	}, nil
}

// Deliver receives an inbound event for one logical Inbox.
//
// Delivery targets the owner named by `consumer_owner`: the two owners share a
// process but keep separate databases, separate Inbox transactions and separate
// ACKs, so one owner's Inbox is never acknowledged by the other's transaction, and
// a request for an owner this unit does not serve is refused before either store is
// touched. The transport peer is the authenticated producer, and the envelope
// owner must agree with it, so a producer cannot inject another owner's event.
//
// Neither owner's command work package has an accepted inbound event type yet, so
// this refuses with UNIMPLEMENTED instead of recording an event it cannot apply or
// acknowledging one it did not apply.
func (s *Server) Deliver(ctx context.Context, request *v226.DeliverEventRequest) (*v226.InboxAck, error) {
	route, ok := routeFromOwner(request.GetConsumerOwner())
	if !ok {
		return nil, unservedOwnerError(request.GetConsumerOwner())
	}
	if request.GetEvent() == nil {
		return nil, status.Error(codes.InvalidArgument, "event envelope is required")
	}
	if request.GetAuthenticatedProducer() == "" {
		return nil, status.Error(codes.Unauthenticated, "authenticated producer is required")
	}
	if request.GetEvent().GetOwner() != request.GetAuthenticatedProducer() {
		return nil, status.Error(codes.PermissionDenied, "envelope owner does not match the authenticated producer")
	}
	return nil, status.Errorf(codes.Unimplemented,
		"%s inbox dispatch is implemented with the %s domain owner", route, route)
}

// unservedOwnerError reports an owner this deployment unit does not serve. The two
// owners it serves are CloudIdentity (`tenant`) and Gateway Integration
// (`gateway`); every other owner belongs to a different deployment unit and is
// never resolved to a local store.
func unservedOwnerError(owner fmt.Stringer) error {
	return status.Errorf(codes.InvalidArgument, "owner %s is not served by the gateway integration unit", owner)
}

func tenantOperationToProto(operation tenantstore.Operation) *v226.Operation {
	return &v226.Operation{
		OperationId:       operation.ID,
		Owner:             v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT,
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

func gatewayOperationToProto(operation gatewaystore.Operation) *v226.Operation {
	return &v226.Operation{
		OperationId:       operation.ID,
		Owner:             v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_GATEWAY,
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
