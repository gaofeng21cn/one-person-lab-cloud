package fabric

import "context"

func (p *LocalDockerProvider) readWorkspaceRuntimePowerContainer(ctx context.Context, input WorkspaceRuntimePowerInput) (dockerContainerInspect, WorkspaceRuntimePowerResult, error) {
	result := WorkspaceRuntimePowerResult{SchemaVersion: 1, Binding: input, State: "pending"}
	container, exists, err := p.inspectContainer(ctx, localRuntimeName(input.WorkspaceID))
	if err != nil {
		return container, result, err
	}
	if !exists {
		result.State = "absent"
		return container, result, nil
	}
	labels := localDockerLabels(input.AccountID, input.WorkspaceID, input.RuntimeID, input.RuntimeOperationID, "runtime")
	for key, want := range labels {
		if container.Config.Labels[key] != want {
			return container, result, ErrWorkspaceRuntimePowerConflict
		}
	}
	if container.ID == "" || input.RuntimeID != localRuntimeID(input.WorkspaceID) {
		return container, result, ErrWorkspaceRuntimePowerConflict
	}
	if container.State.Running {
		runtime, err := p.runtimeFromContainer(container)
		if err != nil {
			return container, result, err
		}
		if runtime.Ready {
			result.State = "running"
		}
	} else if container.State.Status == "exited" || container.State.Status == "created" {
		result.State = "suspended"
	}
	return container, result, nil
}

func (p *LocalDockerProvider) ReadWorkspaceRuntimePower(ctx context.Context, input WorkspaceRuntimePowerInput) (WorkspaceRuntimePowerResult, error) {
	_, result, err := p.readWorkspaceRuntimePowerContainer(ctx, input)
	return result, err
}

func (p *LocalDockerProvider) SetWorkspaceRuntimePower(ctx context.Context, input WorkspaceRuntimePowerInput) (WorkspaceRuntimePowerResult, error) {
	var result WorkspaceRuntimePowerResult
	err := p.withStorageQuotaLock(ctx, func() error {
		container, observed, err := p.readWorkspaceRuntimePowerContainer(ctx, input)
		result = observed
		if err != nil || result.State == "absent" || result.State == input.DesiredState {
			return err
		}
		if input.DesiredState == "running" && container.State.Running {
			return nil
		}
		action := "stop"
		if input.DesiredState == "running" {
			action = "start"
		}
		if _, err := p.runner.Run(ctx, nil, "container", action, container.ID); err != nil {
			return err
		}
		_, result, err = p.readWorkspaceRuntimePowerContainer(ctx, input)
		return err
	})
	return result, err
}
