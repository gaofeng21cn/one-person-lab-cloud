package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

var workspaceLaunchStageDiagnosticErrorPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,127}$`)

type workspaceLaunchStageDiagnosticAttempt struct {
	Attempted int    `json:"attempted"`
	Confirmed int    `json:"confirmed"`
	Unknown   int    `json:"unknown"`
	Max       int    `json:"max"`
	Status    string `json:"status,omitempty"`
}

type workspaceLaunchStageDiagnostic struct {
	SchemaVersion           int                                   `json:"schemaVersion"`
	OperationIdentityDigest string                                `json:"operationIdentityDigest"`
	OperationVersion        int                                   `json:"operationVersion"`
	OperationStatus         string                                `json:"operationStatus"`
	Stage                   string                                `json:"stage"`
	State                   string                                `json:"state"`
	ErrorCode               string                                `json:"errorCode"`
	Owner                   string                                `json:"owner"`
	BlockReason             string                                `json:"blockReason"`
	Retryable               bool                                  `json:"retryable"`
	ObservedAt              string                                `json:"observedAt,omitempty"`
	Checks                  []clients.WorkspaceLaunchStageCheck   `json:"checks,omitempty"`
	Attempt                 workspaceLaunchStageDiagnosticAttempt `json:"attempt"`
	AuthoritativeRead       bool                                  `json:"authoritativeRead"`
	MutationBudget          int                                   `json:"mutationBudget"`
	AutoRecoveryEligible    bool                                  `json:"autoRecoveryEligible"`
	AutoRecoveryBlockReason string                                `json:"autoRecoveryBlockReason"`
}

type workspaceLaunchStageObserver interface {
	ObserveStage(context.Context, workspaceLaunchReconcileOperation) (workspaceLaunchStageObservation, error)
}

func observeWorkspaceLaunchStage(
	ctx context.Context,
	store workspaceLaunchReconcileStore,
	adapter workspaceLaunchStageObserver,
	operationID string,
) (workspaceLaunchStageDiagnostic, bool, error) {
	if store == nil || adapter == nil || strings.TrimSpace(operationID) == "" {
		return workspaceLaunchStageDiagnostic{}, false, errWorkspaceLaunchGrantConflict
	}
	row, found, err := store.GetRuntimeOperation(ctx, operationID)
	if err != nil || !found {
		return workspaceLaunchStageDiagnostic{}, found, err
	}
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil || operation.ID != operationID || operation.Status != contracts.StatusManualReview ||
		!workspaceLaunchReconcileStageValid(operation.Stage) || operation.Stage == contracts.StageSucceeded {
		return workspaceLaunchStageDiagnostic{}, true, errWorkspaceLaunchGrantConflict
	}
	attempt, found := operation.Attempts[operation.Stage]
	if !found {
		return workspaceLaunchStageDiagnostic{}, true, errWorkspaceLaunchGrantConflict
	}
	digest := sha256.Sum256([]byte(operation.ID))
	diagnostic := workspaceLaunchStageDiagnostic{
		SchemaVersion: 3, OperationIdentityDigest: fmt.Sprintf("sha256:%x", digest),
		OperationVersion: operation.Version, OperationStatus: string(operation.Status), Stage: string(operation.Stage),
		State: string(workspaceLaunchStageUnknown), ErrorCode: "none", Owner: workspaceLaunchStageOwner(operation.Stage),
		BlockReason: "stage_observation_unknown",
		Attempt: workspaceLaunchStageDiagnosticAttempt{
			Attempted: attempt.Attempted, Confirmed: attempt.Confirmed, Unknown: attempt.Unknown,
			Max: attempt.Max, Status: attempt.Status,
		},
		AuthoritativeRead: true, MutationBudget: 0,
		AutoRecoveryEligible: false, AutoRecoveryBlockReason: "stage_observation_not_recoverable",
	}
	startedAt := time.Now()
	observation, readErr := adapter.ObserveStage(ctx, operation)
	outcome, errorCode := "success", "none"
	if readErr != nil {
		outcome, errorCode = "error", workspaceLaunchStageReadErrorCode(readErr)
	}
	blockReason := workspaceLaunchObservationBlockReason(observation)
	if readErr != nil {
		blockReason = errorCode
		slog.ErrorContext(ctx, "workspace launch operator read",
			workspaceLaunchLogAttrs(operation.ID, string(operation.Stage), "operator_authoritative_read", false, outcome, string(observation.State), blockReason, errorCode, time.Since(startedAt))...)
	} else {
		slog.InfoContext(ctx, "workspace launch operator read",
			workspaceLaunchLogAttrs(operation.ID, string(operation.Stage), "operator_authoritative_read", false, outcome, string(observation.State), blockReason, errorCode, time.Since(startedAt))...)
	}
	if readErr != nil {
		diagnostic.ErrorCode = errorCode
		diagnostic.BlockReason = diagnostic.ErrorCode
		return diagnostic, true, nil
	}
	if observation.State != workspaceLaunchStageAbsent && observation.State != workspaceLaunchStagePending &&
		observation.State != workspaceLaunchStageReady && observation.State != workspaceLaunchStageOwnershipPending &&
		observation.State != workspaceLaunchStageUnknown {
		diagnostic.ErrorCode = "stage_observation_invalid"
		return diagnostic, true, nil
	}
	diagnostic.State = string(observation.State)
	if observation.Diagnostic != nil {
		diagnostic.Owner = observation.Diagnostic.Owner
		diagnostic.BlockReason = observation.Diagnostic.BlockReason
		diagnostic.Retryable = observation.Diagnostic.Retryable
		diagnostic.ObservedAt = observation.Diagnostic.ObservedAt
		diagnostic.Checks = append([]clients.WorkspaceLaunchStageCheck(nil), observation.Diagnostic.Checks...)
		if observation.Diagnostic.ErrorCode != "" {
			diagnostic.ErrorCode = observation.Diagnostic.ErrorCode
		}
	} else {
		switch observation.State {
		case workspaceLaunchStageReady:
			diagnostic.BlockReason = "none"
		case workspaceLaunchStageAbsent:
			diagnostic.BlockReason = "stage_resource_absent"
		case workspaceLaunchStagePending, workspaceLaunchStageOwnershipPending:
			diagnostic.BlockReason = "stage_provider_pending"
		}
	}
	if observation.State == workspaceLaunchStageUnknown {
		if diagnostic.ErrorCode == "none" {
			diagnostic.ErrorCode = "stage_observation_unknown"
		}
	}
	diagnostic.AutoRecoveryEligible, diagnostic.AutoRecoveryBlockReason = workspaceLaunchAutomaticRecoveryEligibility(operation, observation, time.Now().UTC())
	return diagnostic, true, nil
}

func workspaceLaunchAutomaticRecoveryEligibility(operation workspaceLaunchReconcileOperation, observation workspaceLaunchStageObservation, now time.Time) (bool, string) {
	switch observation.State {
	case workspaceLaunchStageReady:
		_, _, eligible := workspaceLaunchAutomaticFabricReadyAuthorization(operation, now)
		if eligible {
			return true, "none"
		}
		return false, "fabric_ready_ineligible"
	case workspaceLaunchStageOwnershipPending:
		_, _, eligible, reason := workspaceLaunchAutomaticComputeOwnershipAuthorization(operation, now)
		return eligible, reason
	case workspaceLaunchStageAbsent:
		_, _, eligible, reason := workspaceLaunchAutomaticStorageAbsenceAuthorization(operation, now)
		return eligible, reason
	case workspaceLaunchStagePending:
		return false, "stage_provider_pending"
	case workspaceLaunchStageUnknown:
		return false, "stage_observation_unknown"
	default:
		return false, "stage_observation_not_recoverable"
	}
}

func workspaceLaunchStageOwner(stage contracts.Stage) string {
	switch stage {
	case contracts.StageKey, contracts.StageActivation:
		return "cloud.control_plane"
	case contracts.StageDebit:
		return "cloud.sub2api"
	case contracts.StageCompute, contracts.StageStorage, contracts.StageAttachment, contracts.StageSecret, contracts.StageRuntime:
		return "fabric.tencent_tke"
	case contracts.StageReceipt:
		return "cloud.ledger"
	default:
		return "cloud.reconciler"
	}
}

func workspaceLaunchStageReadErrorCode(err error) string {
	if err == nil {
		return "none"
	}
	var fabricErr *clients.FabricHTTPError
	if errors.As(err, &fabricErr) {
		var envelope struct {
			Error string `json:"error"`
		}
		if json.Unmarshal([]byte(fabricErr.Body), &envelope) == nil {
			candidate := strings.TrimSpace(strings.SplitN(envelope.Error, ":", 2)[0])
			if workspaceLaunchStageDiagnosticErrorPattern.MatchString(candidate) {
				return candidate
			}
		}
		return fmt.Sprintf("fabric_http_%d", fabricErr.StatusCode)
	}
	for _, candidate := range []struct {
		err  error
		code string
	}{
		{errWorkspaceLaunchStageAdapterUnavailable, "stage_adapter_unavailable"},
		{errInvalidWorkspaceLaunchOperation, "stage_operation_invalid"},
	} {
		if errors.Is(err, candidate.err) {
			return candidate.code
		}
	}
	return "stage_read_failed"
}
