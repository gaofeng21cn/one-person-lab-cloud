package fabric

import "context"

// RuntimeOperationStore is the capability port for Runtime operation claims,
// durable outcomes, and readback convergence. Runtime workflows must not
// depend on unrelated operation queries, pool locks, or provider replay state.
type RuntimeOperationStore interface {
	ClaimRuntime(ctx context.Context, operation FabricOperation) (FabricOperation, bool, error)
	SaveRuntime(ctx context.Context, operation FabricOperation) error
	ConvergeRuntimeReadback(ctx context.Context, expected, next FabricOperation) error
}

var _ RuntimeOperationStore = (*MemoryOperationStore)(nil)
var _ RuntimeOperationStore = (*PostgresOperationStore)(nil)

// operationStoreRuntimePort keeps the readback capability optional for legacy
// OperationStore decorators that only override unrelated query methods.
type operationStoreRuntimePort struct {
	store OperationStore
}

func (s operationStoreRuntimePort) ClaimRuntime(ctx context.Context, operation FabricOperation) (FabricOperation, bool, error) {
	return s.store.ClaimRuntime(ctx, operation)
}

func (s operationStoreRuntimePort) SaveRuntime(ctx context.Context, operation FabricOperation) error {
	return s.store.SaveRuntime(ctx, operation)
}

func (s operationStoreRuntimePort) ConvergeRuntimeReadback(ctx context.Context, expected, next FabricOperation) error {
	converger, ok := s.store.(runtimeReadbackConverger)
	if !ok {
		return ErrRuntimeOperationNotCurrent
	}
	return converger.ConvergeRuntimeReadback(ctx, expected, next)
}

func runtimeOperationPort(store OperationStore) RuntimeOperationStore {
	if runtime, ok := store.(RuntimeOperationStore); ok {
		return runtime
	}
	return operationStoreRuntimePort{store: store}
}
