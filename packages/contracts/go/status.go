package contracts

// LaunchStatus represents the overall status of a workspace launch operation.
type LaunchStatus string

const (
	StatusPending      LaunchStatus = "pending"
	StatusManualReview LaunchStatus = "manual_review"
	StatusRunning      LaunchStatus = "running"
	StatusUnready      LaunchStatus = "unready"
	StatusFailed       LaunchStatus = "failed"
	StatusRefunded     LaunchStatus = "refunded"
	StatusSucceeded    LaunchStatus = "succeeded"
)

// ErrorCode represents a canonical error code shared across services.
type ErrorCode string

const (
	ErrBalanceInsufficient  ErrorCode = "monthly_balance_insufficient"
	ErrWorkspaceNotFound    ErrorCode = "workspace_launch_not_found"
	ErrStatePersistFailed   ErrorCode = "state_persist_failed"
	ErrStateReadFailed      ErrorCode = "state_read_failed"
	ErrIdempotencyConflict  ErrorCode = "idempotency_conflict"
	ErrRuntimeNotReady      ErrorCode = "workspace_runtime_not_ready"
)

// Stage represents a workspace launch lifecycle stage.
type Stage string

const (
	StageKey        Stage = "key"
	StageDebit      Stage = "debit"
	StageCompute    Stage = "ensure_compute_allocation"
	StageStorage    Stage = "storage"
	StageAttachment Stage = "attachment"
	StageSecret     Stage = "secret"
	StageRuntime    Stage = "runtime"
	StageActivation Stage = "activation"
	StageReceipt    Stage = "receipt"
	StageSucceeded  Stage = "succeeded"
)

func AllLaunchStages() []Stage {
	return []Stage{
		StageKey, StageDebit, StageCompute, StageStorage, StageAttachment,
		StageSecret, StageRuntime, StageActivation, StageReceipt, StageSucceeded,
	}
}

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

// BlockReason represents why a runtime stage is blocked (from Fabric diagnostics).
type BlockReason string

const (
	BlockNone                    BlockReason = "none"
	BlockImageNotReady           BlockReason = "runtime_image_not_ready"
	BlockStorageNotReady         BlockReason = "runtime_storage_not_ready"
	BlockDeploymentNotReady      BlockReason = "runtime_deployment_not_ready"
	BlockNetworkNotReady         BlockReason = "runtime_network_not_ready"
	BlockCredentialsNotReady     BlockReason = "runtime_credentials_not_ready"
	BlockIsolationNotReady       BlockReason = "runtime_isolation_not_ready"
	BlockAuthoritativeReadFailed BlockReason = "runtime_authoritative_read_failed"
)
