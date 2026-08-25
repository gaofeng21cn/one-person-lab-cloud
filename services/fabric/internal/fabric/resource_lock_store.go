package fabric

import "context"

// ResourceLockStore serializes mutations that target the same resource or
// provider pool. Callers that only need mutation fencing must not depend on the
// operation journal's read and write surface.
type ResourceLockStore interface {
	WithPoolLock(ctx context.Context, lockKey string, fn func(context.Context) error) error
}

var _ ResourceLockStore = (*MemoryOperationStore)(nil)
var _ ResourceLockStore = (*PostgresOperationStore)(nil)
