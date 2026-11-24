package fabric

import (
	"context"

	mobyclient "github.com/moby/moby/client"
)

func (p *LocalDockerProvider) readWorkspaceRuntimePowerContainer(ctx context.Context, input WorkspaceRuntimePowerInput) (dockerContainerInspect, WorkspaceRuntimePowerResult, error) {
	result := WorkspaceRuntimePowerResult{SchemaVersion: 1, Binding: input, State: "pending"}
	containerInspect, exists, err := p.inspectContainer(ctx, localRuntimeName(input.WorkspaceID))
	if err != nil {
		return containerInspect, result, err
	}
	if !exists {
		result.State = "absent"
		return containerInspect, result, nil
	}
	labels := localDockerLabels(input.AccountID, input.WorkspaceID, input.RuntimeID, input.RuntimeOperationID, "runtime")
	for key, want := range labels {
		if containerInspect.Config.Labels[key] != want {
			return containerInspect, result, ErrWorkspaceRuntimePowerConflict
		}
	}
	if containerInspect.ID == "" || input.RuntimeID != localRuntimeID(input.WorkspaceID) {
		return containerInspect, result, ErrWorkspaceRuntimePowerConflict
	}
	if containerInspect.State.Running {
		runtime, err := p.runtimeFromContainer(containerInspect)
		if err != nil {
			return containerInspect, result, err
		}
		if runtime.Ready {
			result.State = "running"
		}
	} else if containerInspect.State.Status == "exited" || containerInspect.State.Status == "created" {
		result.State = "suspended"
	}
	return containerInspect, result, nil
}

func (p *LocalDockerProvider) ReadWorkspaceRuntimePower(ctx context.Context, input WorkspaceRuntimePowerInput) (WorkspaceRuntimePowerResult, error) {
	_, result, err := p.readWorkspaceRuntimePowerContainer(ctx, input)
	return result, err
}

func (p *LocalDockerProvider) SetWorkspaceRuntimePower(ctx context.Context, input WorkspaceRuntimePowerInput) (WorkspaceRuntimePowerResult, error) {
	var result WorkspaceRuntimePowerResult
	err := p.withStorageQuotaLock(ctx, func() error {
		containerInspect, observed, err := p.readWorkspaceRuntimePowerContainer(ctx, input)
		result = observed
		if err != nil || result.State == "absent" || result.State == input.DesiredState {
			return err
		}
		if input.DesiredState == "running" && containerInspect.State.Running {
			return nil
		}
		if input.DesiredState == "running" {
			if err := p.docker.ContainerStart(ctx, containerInspect.ID, mobyclient.ContainerStartOptions{}); err != nil {
				return err
			}
		} else {
			if err := p.docker.ContainerStop(ctx, containerInspect.ID, mobyclient.ContainerStopOptions{}); err != nil {
				return err
			}
		}
		_, result, err = p.readWorkspaceRuntimePowerContainer(ctx, input)
		return err
	})
	return result, err
}
