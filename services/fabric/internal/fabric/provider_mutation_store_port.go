package fabric

import "context"

// ProviderMutationStore is the capability port for durable provider child
// operations. Provider mutation callers must not depend on resource queries,
// pool leases, jobs, or machine ownership merely to journal an external write.
type ProviderMutationStore interface {
	Append(ctx context.Context, operation FabricOperation) error
	Get(ctx context.Context, id string) (FabricOperation, error)
	LatestResourceOperation(ctx context.Context, resourceKind, resourceID string) (FabricOperation, bool, error)
	SaveProviderMutationOutcome(ctx context.Context, operation FabricOperation) error
	ConvergeProviderMutationReadback(ctx context.Context, expected, next FabricOperation) error
	SaveProviderMutationReplayEpoch(ctx context.Context, expected, next FabricOperation) error
	ConvergeProviderMutationReplay(ctx context.Context, expected, next FabricOperation) error
}

type operationStoreProviderMutationPort struct {
	store OperationStore
}

func providerMutationStorePort(store OperationStore) ProviderMutationStore {
	return operationStoreProviderMutationPort{store: store}
}

func (p operationStoreProviderMutationPort) Append(ctx context.Context, operation FabricOperation) error {
	return p.store.Append(ctx, operation)
}

func (p operationStoreProviderMutationPort) Get(ctx context.Context, id string) (FabricOperation, error) {
	return p.store.Get(ctx, id)
}

func (p operationStoreProviderMutationPort) LatestResourceOperation(ctx context.Context, resourceKind, resourceID string) (FabricOperation, bool, error) {
	return p.store.LatestResourceOperation(ctx, resourceKind, resourceID)
}

func (p operationStoreProviderMutationPort) SaveProviderMutationOutcome(ctx context.Context, operation FabricOperation) error {
	return p.store.SaveRuntime(ctx, operation)
}

func (p operationStoreProviderMutationPort) ConvergeProviderMutationReadback(ctx context.Context, expected, next FabricOperation) error {
	converger, ok := p.store.(runtimeReadbackConverger)
	if !ok {
		return ErrRuntimeOperationNotCurrent
	}
	return converger.ConvergeRuntimeReadback(ctx, expected, next)
}

func (p operationStoreProviderMutationPort) SaveProviderMutationReplayEpoch(ctx context.Context, expected, next FabricOperation) error {
	replay, ok := p.store.(providerMutationReplayStore)
	if !ok {
		return ErrRuntimeOperationNotCurrent
	}
	return replay.SaveProviderMutationReplayEpoch(ctx, expected, next)
}

func (p operationStoreProviderMutationPort) ConvergeProviderMutationReplay(ctx context.Context, expected, next FabricOperation) error {
	replay, ok := p.store.(providerMutationReplayStore)
	if !ok {
		return ErrRuntimeOperationNotCurrent
	}
	return replay.ConvergeProviderMutationReplay(ctx, expected, next)
}

// providerMutationReplayStore remains optional because replay requires
// compare-and-swap semantics that not every operation journal provides.
type providerMutationReplayStore interface {
	SaveProviderMutationReplayEpoch(context.Context, FabricOperation, FabricOperation) error
	ConvergeProviderMutationReplay(context.Context, FabricOperation, FabricOperation) error
}

var _ ProviderMutationStore = operationStoreProviderMutationPort{}
