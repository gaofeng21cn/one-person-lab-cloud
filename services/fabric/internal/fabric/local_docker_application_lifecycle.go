package fabric

import (
	"context"
	"errors"
	"slices"

	cerrdefs "github.com/containerd/errdefs"
	mobyclient "github.com/moby/moby/client"

	contracts "opl-cloud/packages/contracts/go"
)

func (p *LocalDockerProvider) ReadWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input WorkspaceApplicationRuntimeInput) (WorkspaceApplicationRuntimeLifecycleResult, error) {
	observation, err := p.ReadWorkspaceApplicationRuntime(ctx, input)
	if err != nil {
		return applicationLifecycleResult(observation), err
	}
	// Stopped components are suspended for lifecycle purposes; creation/readiness
	// still reports unexpected process exits as failures.
	for i, component := range observation.Components {
		name, _ := localDockerApplicationComponentNameForInput(input, component.Name)
		containerInspect, exists, err := p.inspectContainer(ctx, name)
		if err != nil {
			return WorkspaceApplicationRuntimeLifecycleResult{}, err
		}
		if exists && !containerInspect.State.Running && containerInspect.State.Status == "exited" {
			observation.Components[i].State = "suspended"
			observation.Components[i].LastError = ""
		}
	}
	observation.Status = contracts.WorkspaceApplicationRuntimeOverallStatus(observation.Components)
	if observation.Status != "ready" {
		observation.Entry = nil
	}
	return applicationLifecycleResult(observation), nil
}

func (p *LocalDockerProvider) SetWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input WorkspaceApplicationRuntimeInput, desired string) (WorkspaceApplicationRuntimeLifecycleResult, error) {
	current, err := p.ReadWorkspaceApplicationRuntimeLifecycle(ctx, input)
	if err != nil {
		return WorkspaceApplicationRuntimeLifecycleResult{}, err
	}
	order, err := contracts.WorkspaceApplicationStartupOrder(input.Revision)
	if err != nil {
		return WorkspaceApplicationRuntimeLifecycleResult{}, err
	}
	if desired != "running" && desired != "suspended" && desired != "absent" {
		return WorkspaceApplicationRuntimeLifecycleResult{}, ErrWorkspaceApplicationRuntimeInputInvalid
	}
	if desired != "running" {
		for left, right := 0, len(order)-1; left < right; left, right = left+1, right-1 {
			order[left], order[right] = order[right], order[left]
		}
	}
	states := map[string]contracts.WorkspaceApplicationRuntimeComponentState{}
	for _, component := range current.Observation.Components {
		states[component.Name] = component
	}
	for _, componentName := range order {
		component := states[componentName]
		name, _ := localDockerApplicationComponentNameForInput(input, component.Name)
		containerInspect, exists, err := p.inspectContainer(ctx, name)
		if err != nil {
			return WorkspaceApplicationRuntimeLifecycleResult{}, err
		}
		if !exists {
			continue
		}
		switch desired {
		case "running":
			if !containerInspect.State.Running && applicationComponentPrerequisitesReady(input.Revision, component.Name, states) {
				if err = p.docker.ContainerStart(ctx, name, mobyclient.ContainerStartOptions{}); err != nil {
					return WorkspaceApplicationRuntimeLifecycleResult{}, err
				}
			}
			state, _, err := p.readWorkspaceApplicationComponent(ctx, input, component)
			if err != nil {
				return WorkspaceApplicationRuntimeLifecycleResult{}, err
			}
			states[component.Name] = state
		case "suspended", "absent":
			if containerInspect.State.Running {
				if err = p.docker.ContainerStop(ctx, name, mobyclient.ContainerStopOptions{}); err != nil {
					return WorkspaceApplicationRuntimeLifecycleResult{}, err
				}
			}
			if desired == "absent" {
				if err = p.docker.ContainerRemove(ctx, name, mobyclient.ContainerRemoveOptions{}); err != nil {
					return WorkspaceApplicationRuntimeLifecycleResult{}, err
				}
			}
		default:
			return WorkspaceApplicationRuntimeLifecycleResult{}, ErrWorkspaceApplicationRuntimeInputInvalid
		}
	}
	result, err := p.ReadWorkspaceApplicationRuntimeLifecycle(ctx, input)
	if err != nil || desired != "absent" || result.State != "absent" {
		return result, err
	}
	seen := map[string]bool{}
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(input.Revision) {
		if seen[component.Image] {
			continue
		}
		seen[component.Image] = true
		retirement, err := p.retireWorkspaceApplicationImage(ctx, component.Image)
		result.ImageRetirement = append(result.ImageRetirement, retirement)
		if err != nil {
			return result, err
		}
	}
	return result, errors.Join(p.removeApplicationCredentialFiles(input), p.removeApplicationConfigFiles(input), p.removeApplicationSecretEnvFiles(input))
}

func (p *LocalDockerProvider) retireWorkspaceApplicationImage(ctx context.Context, imageRef string) (contracts.WorkspaceApplicationRuntimeImageRetirement, error) {
	result := contracts.WorkspaceApplicationRuntimeImageRetirement{Image: imageRef, State: "retained_reference"}
	if !contracts.ValidWorkspaceImageReference(imageRef) {
		return result, errors.New("workspace_application_image_retirement_identity_invalid")
	}
	if references, ok := ctx.Value(applicationRetainedImagesContextKey{}).(map[string]bool); ok && references[imageRef] {
		return result, nil
	}
	if _, retained := p.trustedWorkspaceImageReferences[imageRef]; retained || imageRef == p.applicationProbeImage {
		return result, nil
	}
	references, err := p.docker.ContainerList(ctx, mobyclient.ContainerListOptions{
		All:     true,
		Filters: make(mobyclient.Filters).Add("ancestor", imageRef),
	})
	if err != nil {
		return result, err
	}
	if len(references) > 0 {
		return result, nil
	}
	exists, err := p.applicationImagePresent(ctx, imageRef)
	if err != nil {
		return result, err
	}
	if !exists {
		result.State = "absent"
		return result, nil
	}
	if _, err = p.docker.ImageRemove(ctx, imageRef, mobyclient.ImageRemoveOptions{}); err != nil {
		return result, err
	}
	exists, err = p.applicationImagePresent(ctx, imageRef)
	if err != nil {
		return result, err
	}
	if exists {
		return result, errors.New("workspace_application_image_retirement_pending")
	}
	result.State = "removed"
	return result, nil
}

func (p *LocalDockerProvider) applicationImagePresent(ctx context.Context, imageRef string) (bool, error) {
	if !contracts.ValidWorkspaceImageReference(imageRef) {
		return false, errors.New("workspace_application_image_retirement_identity_invalid")
	}
	// Repository-filtered image lists omit digest-only images on Docker's
	// containerd store. Inspect the exact immutable reference instead.
	inspected, err := p.docker.ImageInspect(ctx, imageRef, nil)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if inspected.ID == "" {
		return false, errors.New("workspace_application_image_inventory_invalid")
	}
	if slices.Contains(inspected.RepoDigests, imageRef) {
		return true, nil
	}
	return false, errors.New("workspace_application_image_inventory_invalid")
}
