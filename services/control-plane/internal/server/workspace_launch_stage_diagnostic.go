package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

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
	Attempt                 workspaceLaunchStageDiagnosticAttempt `json:"attempt"`
	AuthoritativeRead       bool                                  `json:"authoritativeRead"`
	MutationBudget          int                                   `json:"mutationBudget"`
}

func observeWorkspaceLaunchStage(
	ctx context.Context,
	store workspaceLaunchReconcileStore,
	adapter workspaceLaunchStageAdapter,
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
	if err != nil || operation.ID != operationID || operation.Status != "manual_review" ||
		!workspaceLaunchReconcileStageValid(operation.Stage) || operation.Stage == "succeeded" {
		return workspaceLaunchStageDiagnostic{}, true, errWorkspaceLaunchGrantConflict
	}
	attempt, found := operation.Attempts[operation.Stage]
	if !found {
		return workspaceLaunchStageDiagnostic{}, true, errWorkspaceLaunchGrantConflict
	}
	digest := sha256.Sum256([]byte(operation.ID))
	diagnostic := workspaceLaunchStageDiagnostic{
		SchemaVersion: 1, OperationIdentityDigest: fmt.Sprintf("sha256:%x", digest),
		OperationVersion: operation.Version, OperationStatus: operation.Status, Stage: operation.Stage,
		State: workspaceLaunchStageUnknown, ErrorCode: "none",
		Attempt: workspaceLaunchStageDiagnosticAttempt{
			Attempted: attempt.Attempted, Confirmed: attempt.Confirmed, Unknown: attempt.Unknown,
			Max: attempt.Max, Status: attempt.Status,
		},
		AuthoritativeRead: true, MutationBudget: 0,
	}
	observation, readErr := adapter.ReadStage(ctx, operation)
	if readErr != nil {
		diagnostic.ErrorCode = workspaceLaunchStageReadErrorCode(readErr)
		return diagnostic, true, nil
	}
	if observation.State != workspaceLaunchStageAbsent && observation.State != workspaceLaunchStagePending &&
		observation.State != workspaceLaunchStageReady && observation.State != workspaceLaunchStageOwnershipPending &&
		observation.State != workspaceLaunchStageUnknown {
		diagnostic.ErrorCode = "stage_observation_invalid"
		return diagnostic, true, nil
	}
	diagnostic.State = observation.State
	if observation.State == workspaceLaunchStageUnknown {
		diagnostic.ErrorCode = "stage_observation_unknown"
	}
	return diagnostic, true, nil
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
