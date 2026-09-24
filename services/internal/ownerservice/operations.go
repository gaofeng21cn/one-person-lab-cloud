package ownerservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

// checkCadence is the poll interval this owner reports for a non-terminal
// Operation while no worker holds a lease on it. The contract requires
// pollAfterSeconds on every non-terminal read and the Console polls at exactly
// that value, so the owner must name one cadence instead of omitting the field.
const checkCadence = 5 * time.Second

// Operations implements the contract's OwnerOperations group over this owner's own
// operations table. Every Cloud owner exposes it, and the Console BFF is the
// current caller: the browser reads one Operation by the owner that wrote it.
//
// The read is owner-local and typed. An operation owned by another service is
// simply absent here rather than resolved or guessed.
type Operations struct {
	api.UnimplementedOwnerOperationsServer

	owner      Owner
	store      *ownerstore.Store
	authorizer *Authorizer
}

// NewOperations binds the OwnerOperations group to one owner's own store.
func NewOperations(owner Owner, store *ownerstore.Store, authorizer *Authorizer) (*Operations, error) {
	if !owner.Valid() {
		return nil, fmt.Errorf("%q is not a Cloud owner", owner)
	}
	if store == nil {
		return nil, errors.New("ownerstore is required to serve owner operations")
	}
	return &Operations{owner: owner, store: store, authorizer: authorizer}, nil
}

// Register installs the OwnerOperations group on this owner's gRPC server. It is
// infrastructure every owner serves over its own store, not one owner's domain
// product surface, so it does not satisfy a required product group.
func (o *Operations) Register(server *Server) error {
	return server.Register(func(registrar *grpc.Server) {
		api.RegisterOwnerOperationsServer(registrar, o)
	})
}

// Read returns one Operation this owner wrote. An unknown id is NOT_FOUND; a
// database that cannot answer is UNAVAILABLE, never an empty success.
func (o *Operations) Read(ctx context.Context, request *api.OwnerOperationRequest) (*api.Operation, error) {
	operationID := strings.TrimSpace(request.GetOperationId())
	if operationID == "" {
		return nil, status.Error(codes.InvalidArgument, "operation id is required")
	}
	if err := ValidateCallContext(ctx, request.GetContext()); err != nil {
		return nil, err
	}
	record, err := o.store.ReadOperation(ctx, operationID)
	if errors.Is(err, ownerstore.ErrOperationNotFound) {
		return nil, status.Errorf(codes.NotFound, "%s has no operation %s", o.owner, operationID)
	}
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "read %s operation: %v", o.owner, err)
	}
	if err := o.authorize(ctx, request.GetContext(), record, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATION); err != nil {
		return nil, err
	}
	return o.toContract(record)
}

// Reconcile re-reads this owner's own state for one operation. A terminal
// operation is returned unchanged and an unknown external outcome stays
// non-terminal; this call never invents a success.
func (o *Operations) Reconcile(ctx context.Context, request *api.ReconcileOperationRpcRequest) (*api.Operation, error) {
	operationID := strings.TrimSpace(request.GetOperationId())
	if operationID == "" {
		return nil, status.Error(codes.InvalidArgument, "operation id is required")
	}
	if err := o.requireOwnedOperationOwner(request.GetOwner()); err != nil {
		return nil, err
	}
	if err := ValidateCallContext(ctx, request.GetContext()); err != nil {
		return nil, err
	}
	record, err := o.store.ReadOperation(ctx, operationID)
	if errors.Is(err, ownerstore.ErrOperationNotFound) {
		return nil, status.Error(codes.NotFound, "operation not found")
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, "operation read unavailable")
	}
	if err := o.authorize(ctx, request.GetContext(), record, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RECONCILEOPERATION); err != nil {
		return nil, err
	}
	result, err := o.store.ReconcileOperation(ctx, operationID)
	if errors.Is(err, ownerstore.ErrOperationNotFound) {
		return nil, status.Errorf(codes.NotFound, "%s has no operation %s", o.owner, operationID)
	}
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "reconcile %s operation: %v", o.owner, err)
	}
	return o.toContract(result.Operation)
}

// requireOwnedOperationOwner refuses a reconcile addressed to an owner other than
// this process's own identity. The caller routes by owner; a mismatch is a client
// error here rather than a read of another owner's state.
func (o *Operations) requireOwnedOperationOwner(requested api.OperationOwnerEnum) error {
	if requested == api.OperationOwnerEnum_OPERATION_OWNER_ENUM_UNSPECIFIED {
		return status.Error(codes.InvalidArgument, "operation owner is required")
	}
	if requested.String() != "OPERATION_OWNER_ENUM_"+strings.ToUpper(o.owner.String()) {
		return status.Errorf(codes.InvalidArgument, "operation owner %s is not %s", requested, o.owner)
	}
	return nil
}

// toContract converts one owner-local row into the typed contract Operation. Text
// stored by an owner is resolved through the contract's own enum vocabulary, so an
// unknown value is an error instead of a silent zero enum.
func (o *Operations) toContract(record ownerstore.Operation) (*api.Operation, error) {
	kind, err := operationKind(record.Kind)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%s operation %s: %v", o.owner, record.ID, err)
	}
	stage, err := operationStage(record.Stage)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%s operation %s: %v", o.owner, record.ID, err)
	}
	operationStatus, err := operationStatus(record.Status)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%s operation %s: %v", o.owner, record.ID, err)
	}
	operation := &api.Operation{
		OperationId: record.ID,
		Owner:       operationOwner(o.owner),
		Kind:        kind,
		ResourceId:  record.ResourceID,
		Status:      operationStatus,
		Stage:       stage,
		RequestId:   record.RequestID,
		CreatedAt:   timestamppb.New(record.CreatedAt.UTC()),
		UpdatedAt:   timestamppb.New(record.UpdatedAt.UTC()),
	}
	if record.Observation != "" {
		observation, err := operationObservation(record.Observation)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "%s operation %s: %v", o.owner, record.ID, err)
		}
		operation.ObservationResult = &observation
	}
	if record.ErrorCode != "" {
		code, err := operationErrorCode(record.ErrorCode)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "%s operation %s: %v", o.owner, record.ID, err)
		}
		operation.ErrorCode = &code
	}
	if !record.Terminal() {
		pollAfter := int32(checkCadence / time.Second)
		operation.PollAfterSeconds = &pollAfter
	}
	return operation, nil
}

// operationOwner maps one Cloud owner to the contract's OperationOwner enum.
func operationOwner(owner Owner) api.OperationOwnerEnum {
	value, ok := api.OperationOwnerEnum_value["OPERATION_OWNER_ENUM_"+strings.ToUpper(owner.String())]
	if !ok {
		return api.OperationOwnerEnum_OPERATION_OWNER_ENUM_UNSPECIFIED
	}
	return api.OperationOwnerEnum(value)
}

func operationKind(value string) (api.OperationKindEnum, error) {
	number, ok := api.OperationKindEnum_value["OPERATION_KIND_ENUM_"+strings.ToUpper(strings.TrimSpace(value))]
	if !ok {
		return 0, fmt.Errorf("kind %q is not an accepted operation kind", value)
	}
	return api.OperationKindEnum(number), nil
}

func operationStage(value string) (api.OperationStageEnum, error) {
	number, ok := api.OperationStageEnum_value["OPERATION_STAGE_ENUM_"+strings.ToUpper(strings.TrimSpace(value))]
	if !ok {
		return 0, fmt.Errorf("stage %q is not an accepted operation stage", value)
	}
	return api.OperationStageEnum(number), nil
}

func operationStatus(value string) (api.OperationStatusEnum, error) {
	number, ok := api.OperationStatusEnum_value["OPERATION_STATUS_ENUM_"+strings.ToUpper(strings.TrimSpace(value))]
	if !ok {
		return 0, fmt.Errorf("status %q is not an accepted operation status", value)
	}
	return api.OperationStatusEnum(number), nil
}

func operationObservation(value string) (api.OperationObservationResultEnum, error) {
	number, ok := api.OperationObservationResultEnum_value["OPERATION_OBSERVATION_RESULT_ENUM_"+strings.ToUpper(strings.TrimSpace(value))]
	if !ok {
		return 0, fmt.Errorf("observation %q is not an accepted observation result", value)
	}
	return api.OperationObservationResultEnum(number), nil
}

func operationErrorCode(value string) (api.ErrorCodeEnum, error) {
	number, ok := api.ErrorCodeEnum_value["ERROR_CODE_ENUM_"+strings.ToUpper(strings.TrimSpace(value))]
	if !ok {
		return 0, fmt.Errorf("error code %q is not an accepted error code", value)
	}
	return api.ErrorCodeEnum(number), nil
}

func (o *Operations) authorize(ctx context.Context, call *api.CallContext, record ownerstore.Operation, action api.AuthorizationActionEnum) error {
	return o.authorizer.Authorize(ctx, call, action, &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_OPERATION, Id: &record.ID}, ResourceScope{TenantID: record.TenantID, ActorID: record.ActorID})
}
