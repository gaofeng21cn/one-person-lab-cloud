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
	if input.SuspensionReason == "" {
		if input.MissingResourceType != "" || input.MissingResourceID != "" {
			return time.Time{}, ErrWorkspaceRuntimePowerInputInvalid
		}
	} else if input.SuspensionReason != contracts.WorkspaceRuntimeSuspensionProviderResourceAbsent || input.DesiredState != "suspended" ||
		(input.MissingResourceType != "compute" && input.MissingResourceType != "storage") ||
		input.MissingResourceID == "" || input.MissingResourceID != strings.TrimSpace(input.MissingResourceID) {
		return time.Time{}, ErrWorkspaceRuntimePowerInputInvalid
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
		if mutate && (input.DesiredState == "running" && !period.After(s.now()) || input.DesiredState == "suspended" && period.After(s.now()) && input.SuspensionReason == "") {
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
		var absence ProviderFact
		if input.SuspensionReason == contracts.WorkspaceRuntimeSuspensionProviderResourceAbsent {
			absence, err = s.workspaceRuntimeMissingResource(ctx, input)
			if err != nil {
				return err
			}
		}
		now := s.now()
		op := newOperation(workspaceRuntimePowerAction, "workspace_runtime_power", input.WorkspaceID, input.AccountID, input.WorkspaceID, input.IdempotencyKey, hashInput(input), now)
		op.ID = "fop_runtime_power_" + stableSuffix(input.IdempotencyKey)
		op.OperationID = input.IdempotencyKey
		op.Status = "started"
		op.CreatedAt = now
		op.RedactedProviderPayload = map[string]any{"power": input}
		if input.SuspensionReason != "" {
			op.RedactedProviderPayload["missingResource"] = absence
		}
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
		if input.SuspensionReason != "" {
			stored.RedactedProviderPayload["missingResource"] = absence
		}
		return s.runtimeOperations.SaveRuntime(ctx, stored)
	})
	return result, err
}

func (s *Service) workspaceRuntimeMissingResource(ctx context.Context, input WorkspaceRuntimePowerInput) (ProviderFact, error) {
	parent, found, err := s.resourceOperations.LatestResourceOperation(ctx, "workspace_launch_stage", input.RuntimeOperationID)
	if err != nil {
		return ProviderFact{}, err
	}
	binding, bindingOK := decodeLaunchStageBinding(parent)
	record, recordOK := decodeWorkspaceLaunchStageRecord(parent)
	if !found || !bindingOK || !recordOK || parent.Status != "succeeded" || parent.Action != "ensure_runtime" ||
		parent.ID != input.RuntimeOperationID || parent.OperationID != input.RuntimeOperationID || parent.AccountID != input.AccountID || parent.WorkspaceID != input.WorkspaceID ||
		binding.Stage != "runtime" || binding.Action != "ensure_runtime" || binding.FabricOperationID != input.RuntimeOperationID || binding.AccountID != input.AccountID || binding.WorkspaceID != input.WorkspaceID ||
		record.Resources.RuntimeID != input.RuntimeID || record.Resources.RuntimeBindingRef != input.RuntimeOperationID {
		return ProviderFact{}, ErrWorkspaceRuntimePowerConflict
	}
	expectedID := record.Resources.ComputeAllocationID
	if input.MissingResourceType == "storage" {
		expectedID = record.Resources.StorageID
	}
	if expectedID == "" || expectedID != input.MissingResourceID {
		return ProviderFact{}, ErrWorkspaceRuntimePowerConflict
	}
	started := s.now()
	fact := s.providerFact(ctx, ProviderFactInput{AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, ResourceType: input.MissingResourceType, ResourceID: input.MissingResourceID})
	observedAt, timeErr := time.Parse(time.RFC3339Nano, fact.Facts.LastReadAt)
	if !fact.Available || fact.ErrorCode != "" || fact.Observation == nil || !fact.Observation.Available || fact.Observation.State != contracts.ResourceObservedAbsent ||
		timeErr != nil || observedAt.Before(started) || observedAt.After(s.now()) {
		return fact, ErrWorkspaceRuntimePowerConflict
	}
	switch strings.ToLower(strings.TrimSpace(fact.Facts.Status)) {
	case "external_deleted", "deleted", "missing", "not_found":
		return fact, nil
	default:
		return fact, ErrWorkspaceRuntimePowerConflict
	}
}

func validWorkspaceRuntimePowerResult(result WorkspaceRuntimePowerResult, input WorkspaceRuntimePowerInput) bool {
	return result.SchemaVersion == 1 && result.Binding == input && (result.State == "running" || result.State == "suspended" || result.State == "pending" || result.State == "absent")
}
