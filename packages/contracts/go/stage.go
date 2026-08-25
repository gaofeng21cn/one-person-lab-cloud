package contracts

// Stage represents a workspace launch lifecycle stage.
type Stage string

const (
	StageKey       Stage = "key"
	StageDebit     Stage = "debit"
	StageCompute   Stage = "ensure_compute_allocation"
	StageStorage   Stage = "storage"
	StageAttachment Stage = "attachment"
	StageSecret    Stage = "secret"
	StageRuntime   Stage = "runtime"
	StageActivation Stage = "activation"
	StageReceipt   Stage = "receipt"
	StageSucceeded Stage = "succeeded"
)

func AllLaunchStages() []Stage {
	return []Stage{
		StageKey, StageDebit, StageCompute, StageStorage, StageAttachment,
		StageSecret, StageRuntime, StageActivation, StageReceipt, StageSucceeded,
	}
}
