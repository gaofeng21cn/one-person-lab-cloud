package fabric

import "context"

type JobStore interface {
	Append(ctx context.Context, operation FabricOperation) error
	OperationByActionIdempotency(ctx context.Context, action, idempotencyKey string) (FabricOperation, bool, error)
	OperationByResourceActionIdempotency(ctx context.Context, resourceKind, resourceID, action, idempotencyKey string) (FabricOperation, bool, error)
	LatestResourceOperation(ctx context.Context, resourceKind, resourceID string) (FabricOperation, bool, error)
	SaveJobHeartbeat(ctx context.Context, operation FabricOperation) (FabricOperation, error)
}

var _ JobStore = (*MemoryOperationStore)(nil)
var _ JobStore = (*PostgresOperationStore)(nil)
