package fabric

import "context"

// MachineOwnershipStore is the capability port for compute machine ownership.
// Its callers must not depend on the broader operation journal or pool-lock
// surface merely to claim, read, or release ownership.
type MachineOwnershipStore interface {
	ClaimMachine(ctx context.Context, ownership MachineOwnership) (MachineOwnership, bool, error)
	SaveMachineOwnership(ctx context.Context, ownership MachineOwnership) error
	MachineOwnership(ctx context.Context, resourceID string) (MachineOwnership, error)
}

var _ MachineOwnershipStore = (*MemoryOperationStore)(nil)
var _ MachineOwnershipStore = (*PostgresOperationStore)(nil)
