package fabric

import (
	"context"
	"time"
)

// ComputePoolStore is the capability port for FIFO pool admission and lease
// fencing. It includes the initial durable claim because that write establishes
// the pool head consumed by the lease methods.
type ComputePoolStore interface {
	ClaimComputePoolRuntime(ctx context.Context, operation FabricOperation) (FabricOperation, bool, error)
	ComputePoolHead(ctx context.Context, poolKey string) (FabricOperation, bool, error)
	TryClaimComputePoolHead(ctx context.Context, operationID, poolKey, leaseOwner string, now, leaseExpiresAt time.Time) (FabricOperation, bool, error)
	ReleaseComputePoolHead(ctx context.Context, operationID, poolKey, leaseOwner string) error
}

var _ ComputePoolStore = (*MemoryOperationStore)(nil)
var _ ComputePoolStore = (*PostgresOperationStore)(nil)
