// Package transport exposes this deployment unit's internal typed gRPC surface.
//
// The ports below are the v2.26 internal contract: OwnerOperations.Read and
// Reconcile, OwnerCommitReadback.ReadOwnerCommit, and DomainInbox.Deliver. A
// proto service group is an API group, not another process.
//
// This unit carries two data owners — CloudIdentity (`tenant`) and Gateway
// Integration (`gateway`) — so every request that names an owner is routed to
// that owner's store explicitly. The two stores are separate databases with
// separate connection pools: no request reads or writes the other owner's tables.
package transport

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	v226 "opl-cloud/packages/contracts/go/v226"
	gatewaystore "opl-cloud/services/gateway-integration/internal/gateway/store"
	tenantstore "opl-cloud/services/gateway-integration/internal/tenant/store"
)

// tenantOperations is the CloudIdentity owner surface this unit reads. The
// concrete implementation is the `tenant` store over the `opl_tenant` database.
type tenantOperations interface {
	ReadOperation(ctx context.Context, operationID string) (tenantstore.Operation, error)
	ReconcileOperation(ctx context.Context, operationID string) (tenantstore.ReconcileResult, error)
}

// gatewayOperations is the Gateway Integration owner surface this unit reads.
// The concrete implementation is the `gateway` store over the `opl_gateway`
// database.
type gatewayOperations interface {
	ReadOperation(ctx context.Context, operationID string) (gatewaystore.Operation, error)
	ReconcileOperation(ctx context.Context, operationID string) (gatewaystore.ReconcileResult, error)
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

// Read returns the owner-local operation.
//
// The v2.26 OwnerOperationRequest carries no owner while this unit owns two
// operation tables, so the owning store is resolved from the records that
// actually hold the id: an id held by neither owner is NOT_FOUND, and an id held
// by both owners is rejected instead of picking one, because the request cannot
// say which owner's operation was meant.
func (s *Server) Read(ctx context.Context, request *v226.OwnerOperationRequest) (*v226.Operation, error) {
	if request == nil || request.GetOperationId() == "" {
		return nil, status.Error(codes.InvalidArgument, "operation id is required")
	}
	operationID := request.GetOperationId()

	tenantOperation, tenantErr := s.tenant.ReadOperation(ctx, operationID)
	gatewayOperation, gatewayErr := s.gateway.ReadOperation(ctx, operationID)
	if tenantErr != nil && !errors.Is(tenantErr, tenantstore.ErrOperationNotFound) {
		return nil, status.Error(codes.Internal, "read operation failed")
	}
	if gatewayErr != nil && !errors.Is(gatewayErr, gatewaystore.ErrOperationNotFound) {
		return nil, status.Error(codes.Internal, "read operation failed")
	}
	tenantFound := tenantErr == nil
	gatewayFound := gatewayErr == nil
	switch {
	case tenantFound && gatewayFound:
		return nil, status.Error(codes.InvalidArgument, "operation id is ambiguous between the tenant and gateway owners")
	case tenantFound:
		return tenantOperationToProto(tenantOperation), nil
	case gatewayFound:
		return gatewayOperationToProto(gatewayOperation), nil
	default:
		return nil, status.Error(codes.NotFound, "operation not found")
	}
}

// Reconcile re-reads the requested owner's state for an operation. A
// non-terminal operation whose external result is unknown stays non-terminal:
// the owner does not finalize on a guess, and the response keeps the unknown
// observation.
func (s *Server) Reconcile(ctx context.Context, request *v226.ReconcileOperationRpcRequest) (*v226.Operation, error) {
	if request == nil || request.GetOperationId() == "" {
		return nil, status.Error(codes.InvalidArgument, "operation id is required")
	}
	owner, ok := routeFromOperationOwner(request.GetOwner())
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "owner %s is not served by the gateway integration unit", request.GetOwner())
	}
	operationID := request.GetOperationId()

	if owner == routeTenant {
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

// ReadOwnerCommit returns the requested owner's committed evidence for an
// operation. The owner answers from its own records; a caller-supplied claim is
// never accepted as proof.
func (s *Server) ReadOwnerCommit(ctx context.Context, request *v226.ReadOwnerCommitRequest) (*v226.OwnerCommitEvidence, error) {
	if request == nil || request.GetOperationId() == "" {
		return nil, status.Error(codes.InvalidArgument, "operation id is required")
	}
	owner, ok := routeFromOwner(request.GetOwner())
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "owner %s is not served by the gateway integration unit", request.GetOwner())
	}
	operationID := request.GetOperationId()

	if owner == routeTenant {
		operation, err := s.tenant.ReadOperation(ctx, operationID)
		if errors.Is(err, tenantstore.ErrOperationNotFound) {
			return nil, status.Error(codes.NotFound, "operation not found")
		}
		if err != nil {
			return nil, status.Error(codes.Internal, "read owner commit failed")
		}
		if request.GetResourceId() != "" && request.GetResourceId() != operation.ResourceID {
			return nil, status.Error(codes.InvalidArgument, "resource id does not match the operation")
		}
		return &v226.OwnerCommitEvidence{
			Owner:            v226.OwnerEnum_OWNER_ENUM_TENANT,
			OperationId:      operation.ID,
			ResourceId:       operation.ResourceID,
			CommittedVersion: 0,
			AcceptedAt:       timestamppb.New(operation.CreatedAt.UTC()),
		}, nil
	}
	operation, err := s.gateway.ReadOperation(ctx, operationID)
	if errors.Is(err, gatewaystore.ErrOperationNotFound) {
		return nil, status.Error(codes.NotFound, "operation not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "read owner commit failed")
	}
	if request.GetResourceId() != "" && request.GetResourceId() != operation.ResourceID {
		return nil, status.Error(codes.InvalidArgument, "resource id does not match the operation")
	}
	return &v226.OwnerCommitEvidence{
		Owner:            v226.OwnerEnum_OWNER_ENUM_GATEWAY,
		OperationId:      operation.ID,
		ResourceId:       operation.ResourceID,
		CommittedVersion: 0,
		AcceptedAt:       timestamppb.New(operation.CreatedAt.UTC()),
	}, nil
}

// Deliver receives an inbound event for a consumer owner. This unit has no
// accepted inbound event type yet — the tenant and gateway command owners that
// would apply one are W03/W04 — so it refuses with UNIMPLEMENTED instead of
// recording an event it cannot apply or acknowledging one it did not apply.
func (s *Server) Deliver(ctx context.Context, request *v226.DeliverEventRequest) (*v226.InboxAck, error) {
	if request == nil || request.GetEvent() == nil {
		return nil, status.Error(codes.InvalidArgument, "event envelope is required")
	}
	if request.GetAuthenticatedProducer() == "" {
		return nil, status.Error(codes.Unauthenticated, "authenticated producer is required")
	}
	return nil, status.Error(codes.Unimplemented, "gateway integration inbox dispatch is implemented with the tenant and gateway command owners")
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
