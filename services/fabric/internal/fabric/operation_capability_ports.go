package fabric

import "context"

type OperationJournalStore interface {
	Append(ctx context.Context, operation FabricOperation) error
}

type OperationHistoryStore interface {
	List(ctx context.Context) ([]FabricOperation, error)
	ListPage(ctx context.Context, cursor string, limit int) (FabricOperationPage, error)
}

type ResourceOperationStore interface {
	List(ctx context.Context) ([]FabricOperation, error)
	LatestResourceOperation(ctx context.Context, resourceKind, resourceID string) (FabricOperation, bool, error)
	SaveOperationOutcome(ctx context.Context, operation FabricOperation) error
}

type RuntimeOperationQueryStore interface {
	OperationByResourceActionIdempotency(ctx context.Context, resourceKind, resourceID, action, idempotencyKey string) (FabricOperation, bool, error)
	LatestResourceOperation(ctx context.Context, resourceKind, resourceID string) (FabricOperation, bool, error)
	List(ctx context.Context) ([]FabricOperation, error)
	WorkspaceRuntimeIdentityCandidates(ctx context.Context, workspaceID string) ([]FabricOperation, error)
}

type WorkspaceRuntimeReadStore interface {
	WorkspaceRuntimeIdentityCandidates(ctx context.Context, workspaceID string) ([]FabricOperation, error)
}

type ComputeClaimStore interface {
	OperationByActionIdempotency(ctx context.Context, action, idempotencyKey string) (FabricOperation, bool, error)
	ComputeClaimTerminalOperation(ctx context.Context, approvalID, idempotencyKey string) (FabricOperation, bool, error)
	SaveComputeClaimRecovery(ctx context.Context, current, next FabricOperation) error
}

type workspaceLaunchOperationReader interface {
	Get(ctx context.Context, id string) (FabricOperation, error)
}

type WorkspaceLaunchPreflightStore interface {
	workspaceLaunchOperationReader
	ClaimStageOperation(ctx context.Context, operation FabricOperation) (FabricOperation, bool, error)
}

type WorkspaceLaunchStageStore interface {
	workspaceLaunchOperationReader
	OperationByActionIdempotency(ctx context.Context, action, idempotencyKey string) (FabricOperation, bool, error)
	ClaimStageOperation(ctx context.Context, operation FabricOperation) (FabricOperation, bool, error)
	SaveStageOutcome(ctx context.Context, operation FabricOperation) error
	ConvergeStageReadback(ctx context.Context, expected, next FabricOperation) error
	ConvergeStageDiagnostic(ctx context.Context, expected, next FabricOperation) error
}

type operationStoreCapabilityPorts struct {
	store OperationStore
}

func (p operationStoreCapabilityPorts) Append(ctx context.Context, operation FabricOperation) error {
	return p.store.Append(ctx, operation)
}

func (p operationStoreCapabilityPorts) List(ctx context.Context) ([]FabricOperation, error) {
	return p.store.List(ctx)
}

func (p operationStoreCapabilityPorts) ListPage(ctx context.Context, cursor string, limit int) (FabricOperationPage, error) {
	return p.store.ListPage(ctx, cursor, limit)
}

func (p operationStoreCapabilityPorts) LatestResourceOperation(ctx context.Context, resourceKind, resourceID string) (FabricOperation, bool, error) {
	return p.store.LatestResourceOperation(ctx, resourceKind, resourceID)
}

func (p operationStoreCapabilityPorts) SaveOperationOutcome(ctx context.Context, operation FabricOperation) error {
	return p.store.SaveRuntime(ctx, operation)
}

func (p operationStoreCapabilityPorts) OperationByResourceActionIdempotency(ctx context.Context, resourceKind, resourceID, action, idempotencyKey string) (FabricOperation, bool, error) {
	return p.store.OperationByResourceActionIdempotency(ctx, resourceKind, resourceID, action, idempotencyKey)
}

func (p operationStoreCapabilityPorts) WorkspaceRuntimeIdentityCandidates(ctx context.Context, workspaceID string) ([]FabricOperation, error) {
	return p.store.WorkspaceRuntimeIdentityCandidates(ctx, workspaceID)
}

func (p operationStoreCapabilityPorts) OperationByActionIdempotency(ctx context.Context, action, idempotencyKey string) (FabricOperation, bool, error) {
	return p.store.OperationByActionIdempotency(ctx, action, idempotencyKey)
}

func (p operationStoreCapabilityPorts) ComputeClaimTerminalOperation(ctx context.Context, approvalID, idempotencyKey string) (FabricOperation, bool, error) {
	return p.store.ComputeClaimTerminalOperation(ctx, approvalID, idempotencyKey)
}

func (p operationStoreCapabilityPorts) SaveComputeClaimRecovery(ctx context.Context, current, next FabricOperation) error {
	return p.store.SaveComputeClaimRecovery(ctx, current, next)
}

func (p operationStoreCapabilityPorts) ClaimStageOperation(ctx context.Context, operation FabricOperation) (FabricOperation, bool, error) {
	return p.store.ClaimRuntime(ctx, operation)
}

func (p operationStoreCapabilityPorts) Get(ctx context.Context, id string) (FabricOperation, error) {
	return p.store.Get(ctx, id)
}

func (p operationStoreCapabilityPorts) SaveStageOutcome(ctx context.Context, operation FabricOperation) error {
	return p.store.SaveRuntime(ctx, operation)
}

func (p operationStoreCapabilityPorts) ConvergeStageReadback(ctx context.Context, expected, next FabricOperation) error {
	converger, ok := p.store.(runtimeReadbackConverger)
	if !ok {
		return ErrRuntimeOperationNotCurrent
	}
	return converger.ConvergeRuntimeReadback(ctx, expected, next)
}

func (p operationStoreCapabilityPorts) ConvergeStageDiagnostic(ctx context.Context, expected, next FabricOperation) error {
	converger, ok := p.store.(workspaceLaunchStageDiagnosticConverger)
	if !ok {
		return ErrRuntimeOperationNotCurrent
	}
	return converger.ConvergeWorkspaceLaunchStageDiagnostic(ctx, expected, next)
}

var _ OperationJournalStore = operationStoreCapabilityPorts{}
var _ OperationHistoryStore = operationStoreCapabilityPorts{}
var _ ResourceOperationStore = operationStoreCapabilityPorts{}
var _ RuntimeOperationQueryStore = operationStoreCapabilityPorts{}
var _ WorkspaceRuntimeReadStore = operationStoreCapabilityPorts{}
var _ ComputeClaimStore = operationStoreCapabilityPorts{}
var _ WorkspaceLaunchPreflightStore = operationStoreCapabilityPorts{}
var _ WorkspaceLaunchStageStore = operationStoreCapabilityPorts{}
