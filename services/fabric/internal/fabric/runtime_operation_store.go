package fabric

import (
	"context"
	"time"
)

// RuntimeOperationStore is the capability port for Runtime operation claims,
// durable outcomes, and readback convergence. Runtime workflows must not
// depend on unrelated operation queries, pool locks, or provider replay state.
type RuntimeOperationStore interface {
	ClaimRuntime(ctx context.Context, operation FabricOperation) (FabricOperation, bool, error)
	SaveRuntime(ctx context.Context, operation FabricOperation) error
	ConvergeRuntimeReadback(ctx context.Context, expected, next FabricOperation) error
	ReopenWorkspaceRuntimeDelete(context.Context, FabricOperation, time.Time) (FabricOperation, error)
}

var _ RuntimeOperationStore = (*MemoryOperationStore)(nil)
var _ RuntimeOperationStore = (*PostgresOperationStore)(nil)
