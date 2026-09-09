package contracts

// StageState represents the authoritative observation of the current
// workspace launch stage.
type StageState string

const (
	StageStateAbsent                      StageState = "absent"
	StageStateComputePoolQueued           StageState = "compute_pool_queued"
	StageStateComputeDispatchPending      StageState = "compute_dispatch_pending"
	StageStateOwnershipPending            StageState = "ownership_pending"
	StageStatePending                     StageState = "pending"
	StageStateReady                       StageState = "ready"
	StageStateRuntimeImageRevisionPending StageState = "runtime_image_revision_pending"
	StageStateUnknown                     StageState = "unknown"
)
