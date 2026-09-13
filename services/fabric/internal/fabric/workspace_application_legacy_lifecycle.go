package fabric

import (
	"context"
	"errors"
	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/fabric/internal/protectedresource"
	"strconv"
)

type workspaceApplicationLegacyProvider interface {
	WorkspaceApplicationLegacyLifecycle(context.Context, WorkspaceApplicationRuntimeLifecycleInput, WorkspaceRuntime, bool) (WorkspaceApplicationRuntimeLifecycleResult, error)
}

func (s *Service) workspaceApplicationLegacyLifecycle(ctx context.Context, input WorkspaceApplicationRuntimeLifecycleInput, mutate bool) (WorkspaceApplicationRuntimeLifecycleResult, error) {
	result := WorkspaceApplicationRuntimeLifecycleResult{WorkspaceID: input.WorkspaceID, RuntimeID: input.RuntimeID, State: "pending"}
	owners, err := s.runtimeRead.operations.WorkspaceRuntimeIdentityCandidates(ctx, input.WorkspaceID)
	if err != nil {
		return result, err
	}
	var runtime WorkspaceRuntime
	if len(owners) != 1 || owners[0].AccountID != input.AccountID || !decodeOperationResource(owners[0], &runtime) || runtime.ID != input.RuntimeID || runtime.OperationID != input.RuntimeOperationID || runtime.WorkspaceID != input.WorkspaceID || runtime.ServiceName == "" {
		return result, ErrRuntimeIdempotencyConflict
	}
	provider, ok := s.runtimeProvider.(workspaceApplicationLegacyProvider)
	if !ok {
		return result, ErrWorkspaceApplicationRuntimeProviderUnsupported
	}
	latest, found, err := s.resourceOperations.LatestResourceOperation(ctx, "workspace_application_lifecycle", input.RuntimeID)
	if err != nil {
		return result, err
	}
	if found {
		var previous workspaceApplicationLifecycleRecord
		if !decodeOperationResource(latest, &previous) {
			return result, ErrRuntimeIdempotencyConflict
		}
		if mutate && previous.Input.DesiredState == "absent" && input.DesiredState != "absent" {
			return result, errors.New("workspace_application_runtime_retired")
		}
	}
	result, err = provider.WorkspaceApplicationLegacyLifecycle(ctx, input, runtime, false)
	if err != nil || !mutate {
		return result, err
	}
	operation := newOperation("set_workspace_application_runtime_lifecycle", "workspace_application_lifecycle", input.RuntimeID, input.AccountID, input.WorkspaceID, input.IdempotencyKey, hashInput(input), s.now())
	operation.ID = "fop_app_lifecycle_" + stableSuffix(input.IdempotencyKey)
	operation.OperationID = input.IdempotencyKey
	operation.Status = "started"
	operation.CreatedAt = s.now()
	operation.RedactedProviderPayload = map[string]any{"resource": workspaceApplicationLifecycleRecord{Input: input, Result: result}}
	stored, claimed, err := s.runtimeOperations.ClaimRuntime(ctx, operation)
	if err != nil {
		return result, err
	}
	if stored.RequestHash != operation.RequestHash {
		return result, ErrRuntimeIdempotencyConflict
	}
	if !claimed && found && latest.IdempotencyKey != input.IdempotencyKey {
		return result, errors.New("workspace_application_lifecycle_superseded")
	}
	if !claimed && stored.Status == "succeeded" {
		if result.State != input.DesiredState && !(result.State == "absent" && input.DesiredState == "suspended") {
			return result, errors.New("workspace_application_lifecycle_readback_drift")
		}
		var previous workspaceApplicationLifecycleRecord
		if !decodeOperationResource(stored, &previous) {
			return result, ErrRuntimeIdempotencyConflict
		}
		result.ImageRetirement = previous.Result.ImageRetirement
		return result, nil
	}
	if result.State != input.DesiredState || input.DesiredState == "absent" {
		mutationCtx := s.providerMutationContext(ctx, stored)
		if input.DesiredState == "absent" {
			references, referenceErr := s.retainedApplicationImages(ctx, input.RuntimeID)
			if referenceErr != nil {
				return result, referenceErr
			}
			mutationCtx = context.WithValue(mutationCtx, applicationRetainedImagesContextKey{}, references)
		}
		result, err = provider.WorkspaceApplicationLegacyLifecycle(mutationCtx, input, runtime, true)
	}
	previous := stored
	stored.RedactedProviderPayload = map[string]any{"resource": workspaceApplicationLifecycleRecord{Input: input, Result: result}}
	if err != nil {
		stored.Status = "failed"
		stored.ErrorCode = errorCode(err)
	} else if result.State == input.DesiredState {
		stored.Status = "succeeded"
		stored.FinishedAt = s.now()
	}
	if previous.Status == "failed" {
		if stored.Status == "succeeded" {
			return result, s.runtimeOperations.ConvergeRuntimeReadback(ctx, previous, stored)
		}
		return result, err
	}
	return result, errors.Join(err, s.runtimeOperations.SaveRuntime(ctx, stored))
}

func legacyApplicationResult(input WorkspaceApplicationRuntimeLifecycleInput, runtime WorkspaceRuntime, state string) WorkspaceApplicationRuntimeLifecycleResult {
	componentState := state
	if state == "running" {
		componentState = "ready"
	}
	return WorkspaceApplicationRuntimeLifecycleResult{RuntimeID: input.RuntimeID, WorkspaceID: input.WorkspaceID, State: state, Observation: contracts.WorkspaceApplicationRuntimeObservation{SchemaVersion: 1, RuntimeID: input.RuntimeID, WorkspaceID: input.WorkspaceID, Status: componentState, Components: []contracts.WorkspaceApplicationRuntimeComponentState{{Name: "main", Role: "main", Image: runtime.ImageID, State: componentState}}}}
}
func (p *LocalDockerProvider) WorkspaceApplicationLegacyLifecycle(ctx context.Context, input WorkspaceApplicationRuntimeLifecycleInput, runtime WorkspaceRuntime, mutate bool) (WorkspaceApplicationRuntimeLifecycleResult, error) {
	result := legacyApplicationResult(input, runtime, "pending")
	if runtime.ServiceName != localRuntimeName(input.WorkspaceID) || runtime.ID != localRuntimeID(input.WorkspaceID) {
		return result, ErrRuntimeIdempotencyConflict
	}
	container, exists, err := p.inspectContainer(ctx, runtime.ServiceName)
	if err != nil {
		return result, err
	}
	if exists {
		live, err := p.runtimeFromContainer(container)
		if err != nil || live.ID != runtime.ID || live.OperationID != runtime.OperationID || container.Config.Labels["opl.account.id"] != input.AccountID || live.ImageID != runtime.ImageID {
			return result, ErrLaunchStageBindingConflict
		}
		if mutate {
			if input.DesiredState == "running" && !container.State.Running {
				_, err = p.runner.Run(ctx, nil, "container", "start", runtime.ServiceName)
			}
			if input.DesiredState != "running" && container.State.Running {
				_, err = p.runner.Run(ctx, nil, "container", "stop", runtime.ServiceName)
			}
			if err == nil && input.DesiredState == "absent" {
				_, err = p.runner.Run(ctx, nil, "container", "rm", runtime.ServiceName)
			}
			if err != nil {
				return result, err
			}
			container, exists, err = p.inspectContainer(ctx, runtime.ServiceName)
			if err != nil {
				return result, err
			}
		}
	}
	state := "absent"
	if exists {
		state = "suspended"
		if container.State.Running {
			state = "running"
		}
	}
	result = legacyApplicationResult(input, runtime, state)
	if mutate && input.DesiredState == "absent" && state == "absent" {
		retirement, err := p.retireWorkspaceApplicationImage(ctx, runtime.ImageID)
		result.ImageRetirement = []contracts.WorkspaceApplicationRuntimeImageRetirement{retirement}
		return result, err
	}
	return result, nil
}

func (p *TencentProvider) WorkspaceApplicationLegacyLifecycle(ctx context.Context, input WorkspaceApplicationRuntimeLifecycleInput, runtime WorkspaceRuntime, mutate bool) (WorkspaceApplicationRuntimeLifecycleResult, error) {
	result := legacyApplicationResult(input, runtime, "pending")
	targets := []string{"deployment/" + runtime.ServiceName, "service/" + runtime.ServiceName, "networkpolicy/" + runtime.ServiceName, "secret/" + runtime.ServiceName + "-env"}
	read := func() (map[string]any, int, error) {
		args := append([]string{"get"}, targets...)
		args = append(args, "--ignore-not-found", "-o", "json")
		raw, err := p.callKubectl(ctx, args, nil, protectedresource.Target{})
		if err != nil {
			return nil, 0, err
		}
		items, err := strictKubectlItems(raw)
		if err != nil {
			return nil, 0, err
		}
		var deployment map[string]any
		for _, item := range items {
			object, ok := item.(map[string]any)
			if !ok || stringValue(nested(object, "metadata", "labels", "oplcloud.cn/account-id")) != k8sCostLabelValue(input.AccountID) || stringValue(nested(object, "metadata", "labels", "oplcloud.cn/workspace-id")) != k8sCostLabelValue(input.WorkspaceID) || stringValue(nested(object, "metadata", "labels", "oplcloud.cn/runtime-id")) != k8sCostLabelValue(input.RuntimeID) {
				return nil, 0, ErrLaunchStageBindingConflict
			}
			if object["kind"] == "Deployment" {
				deployment = object
			}
		}
		return deployment, len(items), nil
	}
	deployment, count, err := read()
	if err != nil {
		return result, err
	}
	if deployment != nil && workspaceApplicationDeploymentImage(deployment) != runtime.ImageID {
		return result, ErrLaunchStageBindingConflict
	}
	if mutate {
		if input.DesiredState == "absent" {
			args := append([]string{"delete"}, targets...)
			args = append(args, "--ignore-not-found=true", "--wait=true")
			_, err = p.callKubectl(ctx, args, nil, protectedresource.Target{})
		} else if deployment != nil {
			replicas := 0
			if input.DesiredState == "running" {
				replicas = 1
			}
			_, err = p.callKubectl(ctx, []string{"scale", "deployment/" + runtime.ServiceName, "--replicas=" + strconv.Itoa(replicas)}, nil, protectedresource.Target{})
		}
		if err != nil {
			return result, err
		}
		deployment, count, err = read()
		if err != nil {
			return result, err
		}
	}
	state := "pending"
	if count == 0 {
		state = "absent"
	} else if deployment != nil && number(nested(deployment, "status", "observedGeneration")) >= number(nested(deployment, "metadata", "generation")) {
		if number(nested(deployment, "spec", "replicas")) == 0 && number(nested(deployment, "status", "replicas")) == 0 {
			state = "suspended"
		} else if number(nested(deployment, "status", "readyReplicas")) == 1 {
			state = "running"
		}
	}
	result = legacyApplicationResult(input, runtime, state)
	if mutate && input.DesiredState == "absent" && state == "absent" {
		result.ImageRetirement = []contracts.WorkspaceApplicationRuntimeImageRetirement{{Image: runtime.ImageID, State: "instance_required"}}
	}
	return result, nil
}
