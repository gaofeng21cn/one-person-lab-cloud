package contracts

// StageState represents the authoritative observation of the current
// workspace launch stage.
type StageState string

const (
	StageStateAbsent                      StageState = "absent"
	StageStateOwnershipPending            StageState = "ownership_pending"
	StageStatePending                     StageState = "pending"
	StageStateReady                       StageState = "ready"
	StageStateRuntimeImageRevisionPending StageState = "runtime_image_revision_pending"
	StageStateUnknown                     StageState = "unknown"
)
