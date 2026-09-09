package fabric

import (
	"context"
	"errors"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

type WorkspaceRuntimePowerInput = contracts.WorkspaceRuntimePowerInput
type WorkspaceRuntimePowerResult = contracts.WorkspaceRuntimePowerResult

var ErrWorkspaceRuntimePowerInputInvalid = errors.New("workspace_runtime_power_input_invalid")
var ErrWorkspaceRuntimePowerConflict = errors.New("workspace_runtime_power_conflict")
var ErrWorkspaceRuntimePowerUnavailable = errors.New("workspace_runtime_power_unavailable")

type workspaceRuntimePowerProvider interface {
	SetWorkspaceRuntimePower(context.Context, WorkspaceRuntimePowerInput) (WorkspaceRuntimePowerResult, error)
	ReadWorkspaceRuntimePower(context.Context, WorkspaceRuntimePowerInput) (WorkspaceRuntimePowerResult, error)
}

const workspaceRuntimePowerAction = "set_workspace_runtime_power"

func workspaceRuntimePowerInputPeriod(input WorkspaceRuntimePowerInput) (time.Time, error) {
	if input.SchemaVersion != 1 || input.DesiredState != "running" && input.DesiredState != "suspended" {
		return time.Time{}, ErrWorkspaceRuntimePowerInputInvalid
	}
	for _, value := range []string{input.AccountID, input.WorkspaceID, input.RuntimeID, input.RuntimeOperationID, input.PaidThrough, input.IdempotencyKey} {
		if value == "" || value != strings.TrimSpace(value) {
			return time.Time{}, ErrWorkspaceRuntimePowerInputInvalid
		}
	}
	period, err := time.Parse(time.RFC3339Nano, input.PaidThrough)
	if err != nil {
		return time.Time{}, ErrWorkspaceRuntimePowerInputInvalid
	}
	return period, nil
}

func (s *Service) ReadWorkspaceRuntimePower(ctx context.Context, input WorkspaceRuntimePowerInput) (WorkspaceRuntimePowerResult, error) {
	return s.workspaceRuntimePower(ctx, input, false)
}

func (s *Service) SetWorkspaceRuntimePower(ctx context.Context, input WorkspaceRuntimePowerInput) (WorkspaceRuntimePowerResult, error) {
	return s.workspaceRuntimePower(ctx, input, true)
}

func (s *Service) workspaceRuntimePower(ctx context.Context, input WorkspaceRuntimePowerInput, mutate bool) (WorkspaceRuntimePowerResult, error) {
	result := WorkspaceRuntimePowerResult{SchemaVersion: 1, Binding: input, State: "pending"}
	period, err := workspaceRuntimePowerInputPeriod(input)
	if err != nil {
		return result, err
	}
	provider, ok := s.runtimeProvider.(workspaceRuntimePowerProvider)
	if !ok {
		return result, ErrWorkspaceRuntimePowerUnavailable
	}
	err = s.resourceLocks.WithPoolLock(ctx, workspaceRuntimeLockKey(input.WorkspaceID), func(ctx context.Context) error {
		if mutate && (input.DesiredState == "running" && !period.After(s.now()) || input.DesiredState == "suspended" && period.After(s.now())) {
			return ErrWorkspaceRuntimePowerConflict
		}
		owners, err := s.runtimeRead.operations.WorkspaceRuntimeIdentityCandidates(ctx, input.WorkspaceID)
		if err != nil {
			return err
		}
		var runtime WorkspaceRuntime
		if len(owners) != 1 || owners[0].AccountID != input.AccountID || !decodeOperationResource(owners[0], &runtime) || runtime.ID != input.RuntimeID || runtime.OperationID != input.RuntimeOperationID || runtime.WorkspaceID != input.WorkspaceID {
			return ErrWorkspaceRuntimePowerConflict
		}
		deletion, found, err := s.resourceOperations.LatestResourceOperation(ctx, "workspace_runtime", input.WorkspaceID)
		if err != nil {
			return err
		}
		if found && deletion.Action == "destroy_workspace_runtime" {
			return ErrWorkspaceRuntimePowerConflict
		}
		latest, found, err := s.resourceOperations.LatestResourceOperation(ctx, "workspace_runtime_power", input.WorkspaceID)
		if err != nil {
			return err
		}
		if found {
			var previous WorkspaceRuntimePowerInput
			if !decodeWorkspaceLaunchCloseoutPayload(latest.RedactedProviderPayload["power"], &previous) || latest.Action != workspaceRuntimePowerAction || latest.RequestHash != hashInput(previous) {
				return ErrWorkspaceRuntimePowerConflict
			}
			previousPeriod, err := workspaceRuntimePowerInputPeriod(previous)
			if err != nil || previous.AccountID != input.AccountID || previous.RuntimeID != input.RuntimeID || previous.RuntimeOperationID != input.RuntimeOperationID || period.Before(previousPeriod) {
				return ErrWorkspaceRuntimePowerConflict
			}
		}
		result, err = provider.ReadWorkspaceRuntimePower(ctx, input)
		if err != nil {
			return err
		}
		if !validWorkspaceRuntimePowerResult(result, input) {
			return ErrWorkspaceRuntimePowerConflict
		}
		if !mutate {
			return nil
		}
		now := s.now()
		op := newOperation(workspaceRuntimePowerAction, "workspace_runtime_power", input.WorkspaceID, input.AccountID, input.WorkspaceID, input.IdempotencyKey, hashInput(input), now)
		op.ID = "fop_runtime_power_" + stableSuffix(input.IdempotencyKey)
		op.OperationID = input.IdempotencyKey
		op.Status = "started"
		op.CreatedAt = now
		op.RedactedProviderPayload = map[string]any{"power": input}
		stored, _, err := s.runtimeOperations.ClaimRuntime(ctx, op)
		if err != nil {
			return err
		}
		if stored.RequestHash != op.RequestHash {
			return ErrRuntimeIdempotencyConflict
		}
		if result.State != input.DesiredState && result.State != "absent" {
			if stored.Status == "succeeded" {
				return ErrWorkspaceRuntimePowerConflict
			}
			result, err = provider.SetWorkspaceRuntimePower(ctx, input)
			if err != nil {
				return err
			}
			if !validWorkspaceRuntimePowerResult(result, input) {
				return ErrWorkspaceRuntimePowerConflict
			}
		}
		if result.State != input.DesiredState && result.State != "absent" {
			return nil
		}
		if stored.Status == "succeeded" {
			return nil
		}
		stored.Status, stored.FinishedAt = "succeeded", s.now()
		stored.RedactedProviderPayload = map[string]any{"power": input, "result": result}
		return s.runtimeOperations.SaveRuntime(ctx, stored)
	})
	return result, err
}

func validWorkspaceRuntimePowerResult(result WorkspaceRuntimePowerResult, input WorkspaceRuntimePowerInput) bool {
	return result.SchemaVersion == 1 && result.Binding == input && (result.State == "running" || result.State == "suspended" || result.State == "pending" || result.State == "absent")
}
