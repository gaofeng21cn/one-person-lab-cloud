package fabric

import (
	"context"
	"encoding/json"
	"errors"
	contracts "opl-cloud/packages/contracts/go"
	"strings"
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
		container, exists, err := p.inspectContainer(ctx, name)
		if err != nil {
			return WorkspaceApplicationRuntimeLifecycleResult{}, err
		}
		if exists && !container.State.Running && container.State.Status == "exited" {
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
		container, exists, err := p.inspectContainer(ctx, name)
		if err != nil {
			return WorkspaceApplicationRuntimeLifecycleResult{}, err
		}
		if !exists {
			continue
		}
		switch desired {
		case "running":
			if !container.State.Running && applicationComponentPrerequisitesReady(input.Revision, component.Name, states) {
				if _, err = p.runner.Run(ctx, nil, "container", "start", name); err != nil {
					return WorkspaceApplicationRuntimeLifecycleResult{}, err
				}
			}
			state, _, err := p.readWorkspaceApplicationComponent(ctx, input, component)
			if err != nil {
				return WorkspaceApplicationRuntimeLifecycleResult{}, err
			}
			states[component.Name] = state
		case "suspended", "absent":
			if container.State.Running {
				if _, err = p.runner.Run(ctx, nil, "container", "stop", name); err != nil {
					return WorkspaceApplicationRuntimeLifecycleResult{}, err
				}
			}
			if desired == "absent" {
				if _, err = p.runner.Run(ctx, nil, "container", "rm", name); err != nil {
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
	references, err := p.runner.Run(ctx, nil, "container", "ls", "--all", "--filter", "ancestor="+imageRef, "--format", "{{.ID}}")
	if err != nil {
		return result, err
	}
	if len(strings.TrimSpace(string(references))) > 0 {
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
	if _, err = p.runner.Run(ctx, nil, "image", "rm", imageRef); err != nil {
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
	raw, err := p.runner.Run(ctx, nil, "image", "inspect", imageRef)
	if err != nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.TrimSpace(line) == "Error response from daemon: No such image: "+imageRef {
				return false, nil
			}
		}
		return false, err
	}
	var images []dockerApplicationImageInspect
	if err = json.Unmarshal(raw, &images); err != nil || len(images) != 1 || images[0].ID == "" {
		return false, errors.New("workspace_application_image_inventory_invalid")
	}
	for _, ref := range images[0].RepoDigests {
		if ref == imageRef {
			return true, nil
		}
	}
	return false, errors.New("workspace_application_image_identity_mismatch")
}

// dockerApplicationImageInspect is the Docker API readback at this adapter.
type dockerApplicationImageInspect struct {
	ID          string   `json:"Id"`
	RepoDigests []string `json:"RepoDigests"`
}
