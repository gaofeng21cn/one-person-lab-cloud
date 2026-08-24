package contracts

import (
	"fmt"
	"slices"
)

type Stage string

const (
	StageKey                     Stage = "key"
	StageDebit                   Stage = "debit"
	StageEnsureComputeAllocation Stage = "ensure_compute_allocation"
	StageStorage                 Stage = "storage"
	StageAttachment              Stage = "attachment"
	StageSecret                  Stage = "secret"
	StageRuntime                 Stage = "runtime"
	StageActivation              Stage = "activation"
	StageReceipt                 Stage = "receipt"
	StageSucceeded               Stage = "succeeded"
)

type LaunchStatus string

const (
	LaunchStatusPending      LaunchStatus = "pending"
	LaunchStatusManualReview LaunchStatus = "manual_review"
	LaunchStatusSucceeded    LaunchStatus = "succeeded"
	LaunchStatusFailed       LaunchStatus = "failed"
	LaunchStatusRefunded     LaunchStatus = "refunded"
)

type ErrorCode string

const (
	ErrorCodeMonthlyBalanceInsufficient ErrorCode = "monthly_balance_insufficient"
	ErrorCodeWorkspaceLaunchNotFound    ErrorCode = "workspace_launch_not_found"
	ErrorCodeStatePersistFailed         ErrorCode = "state_persist_failed"
)

var stages = []Stage{
	StageKey,
	StageDebit,
	StageEnsureComputeAllocation,
	StageStorage,
	StageAttachment,
	StageSecret,
	StageRuntime,
	StageActivation,
	StageReceipt,
	StageSucceeded,
}

var launchStatuses = []LaunchStatus{
	LaunchStatusPending,
	LaunchStatusManualReview,
	LaunchStatusSucceeded,
	LaunchStatusFailed,
	LaunchStatusRefunded,
}

var errorCodes = []ErrorCode{
	ErrorCodeMonthlyBalanceInsufficient,
	ErrorCodeWorkspaceLaunchNotFound,
	ErrorCodeStatePersistFailed,
}

func (value Stage) Valid() bool {
	return slices.Contains(stages, value)
}

func (value LaunchStatus) Valid() bool {
	return slices.Contains(launchStatuses, value)
}

func (value ErrorCode) Valid() bool {
	return slices.Contains(errorCodes, value)
}

func (value Stage) Validate() error {
	if value.Valid() {
		return nil
	}
	return fmt.Errorf("invalid stage %q", string(value))
}

func (value LaunchStatus) Validate() error {
	if value.Valid() {
		return nil
	}
	return fmt.Errorf("invalid launch status %q", string(value))
}

func (value ErrorCode) Validate() error {
	if value.Valid() {
		return nil
	}
	return fmt.Errorf("invalid error code %q", string(value))
}

type LaunchStageState struct {
	Stage   Stage        `json:"stage"`
	Status  LaunchStatus `json:"status"`
	Code    ErrorCode    `json:"errorCode,omitempty"`
	Message string       `json:"message,omitempty"`
}

type DeviceUploadAttachmentEvidence struct {
	FileChooserObserved         bool `json:"fileChooserObserved"`
	UploadResponseOK            bool `json:"uploadResponseOk"`
	UploadResponseStatus        int  `json:"uploadResponseStatus"`
	PendingAttachmentTagVisible bool `json:"pendingAttachmentTagObserved"`
}

func (evidence DeviceUploadAttachmentEvidence) Uploaded() error {
	if !evidence.FileChooserObserved {
		return fmt.Errorf("device file chooser not observed")
	}
	if !evidence.UploadResponseOK || evidence.UploadResponseStatus != 200 {
		return fmt.Errorf("device upload response not accepted")
	}
	if !evidence.PendingAttachmentTagVisible {
		return fmt.Errorf("pending attachment not attached to conversation")
	}
	return nil
}
