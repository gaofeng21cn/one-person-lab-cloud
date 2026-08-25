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
