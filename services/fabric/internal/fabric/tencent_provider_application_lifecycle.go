package fabric

import (
	"context"
	"errors"
	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/fabric/internal/protectedresource"
	"strconv"
)

func (p *TencentProvider) ReadWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input WorkspaceApplicationRuntimeInput) (WorkspaceApplicationRuntimeLifecycleResult, error) {
	resources, err := p.readWorkspaceApplicationResources(ctx, input)
	if err != nil {
		return WorkspaceApplicationRuntimeLifecycleResult{}, err
	}
	observation := workspaceApplicationObservation(input, resources)
	for i, component := range observation.Components {
		deployment, exists := resources.deployments[workspaceApplicationComponentResourceName(input, component.Name)]
		if !exists {
			continue
		}
		if workspaceApplicationDeploymentImage(deployment) != component.Image || stringValue(nested(deployment, "metadata", "labels", "oplcloud.cn/runtime-operation-id")) != k8sCostLabelValue(input.RuntimeOperationID) {
			return WorkspaceApplicationRuntimeLifecycleResult{}, ErrLaunchStageBindingConflict
		}
		if !verifyTencentApplicationConfiguration(input, component, deployment, resources.storagePVC) {
			return WorkspaceApplicationRuntimeLifecycleResult{}, ErrLaunchStageBindingConflict
		}
		if number(nested(deployment, "spec", "replicas")) == 0 && number(nested(deployment, "status", "replicas")) == 0 && number(nested(deployment, "status", "observedGeneration")) >= number(nested(deployment, "metadata", "generation")) {
			hasPod := false
			for _, pod := range resources.pods {
				rs := resources.replicaSets[controllerUID(pod, "ReplicaSet")]
				if controllerUID(rs, "Deployment") == stringValue(nested(deployment, "metadata", "uid")) {
					hasPod = true
				}
			}
			if !hasPod {
				observation.Components[i].State = "suspended"
				observation.Components[i].LastError = ""
			}
		}
	}
	observation.Status = contracts.WorkspaceApplicationRuntimeOverallStatus(observation.Components)
	if observation.Status != "ready" {
		observation.Entry = nil
	}
	result := applicationLifecycleResult(observation)
	if result.State == "absent" && (len(resources.services) > 0 || len(resources.replicaSets) > 0 || len(resources.pods) > 0 || len(resources.auxiliary) > 0) {
		result.State = "pending"
		result.Observation.Status = "pending"
		for i := range result.Observation.Components {
			result.Observation.Components[i].State = "pending"
		}
	}
	return result, nil
}

func (p *TencentProvider) SetWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input WorkspaceApplicationRuntimeInput, desired string) (WorkspaceApplicationRuntimeLifecycleResult, error) {
	if _, err := p.ReadWorkspaceApplicationRuntimeLifecycle(ctx, input); err != nil {
		return WorkspaceApplicationRuntimeLifecycleResult{}, err
	}
	order, err := contracts.WorkspaceApplicationStartupOrder(input.Revision)
	if err != nil {
		return WorkspaceApplicationRuntimeLifecycleResult{}, err
	}
	if desired != "running" {
		for left, right := 0, len(order)-1; left < right; left, right = left+1, right-1 {
			order[left], order[right] = order[right], order[left]
		}
	}
	if desired == "absent" {
		targets := []string{"delete"}
		for _, componentName := range order {
			name := workspaceApplicationComponentResourceName(input, componentName)
			targets = append(targets, "deployment/"+name, "service/"+name)
		}
		if len(input.Configuration.Files) > 0 {
			if _, err := p.readApplicationConfigObject(ctx, input); err != nil && !errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
				return WorkspaceApplicationRuntimeLifecycleResult{}, err
			}
			targets = append(targets, "configmap/"+workspaceApplicationComponentResourceName(input, "config"))
		}
		targets = append(targets, "networkpolicy/"+workspaceApplicationComponentResourceName(input, "network"), "secret/"+workspaceApplicationComponentResourceName(input, "secrets"), "--ignore-not-found=true", "--wait=false")
		if _, err := p.callKubectl(ctx, targets, nil, protectedresource.Target{}); err != nil {
			return WorkspaceApplicationRuntimeLifecycleResult{}, err
		}
	} else {
		replicas := 0
		if desired == "running" {
			replicas = 1
		} else if desired != "suspended" {
			return WorkspaceApplicationRuntimeLifecycleResult{}, ErrWorkspaceApplicationRuntimeInputInvalid
		}
		resources, err := p.readWorkspaceApplicationResources(ctx, input)
		if err != nil {
			return WorkspaceApplicationRuntimeLifecycleResult{}, err
		}
		for _, componentName := range order {
			name := workspaceApplicationComponentResourceName(input, componentName)
			deployment, exists := resources.deployments[name]
			if !exists || number(nested(deployment, "spec", "replicas")) == float64(replicas) {
				continue
			}
			if desired == "running" && !workspaceApplicationDependenciesReady(input, componentName, resources) {
				continue
			}
			if _, err := p.callKubectl(ctx, []string{"scale", "deployment/" + name, "--replicas=" + strconv.Itoa(replicas)}, nil, protectedresource.Target{}); err != nil {
				return WorkspaceApplicationRuntimeLifecycleResult{}, err
			}
		}
	}
	result, err := p.ReadWorkspaceApplicationRuntimeLifecycle(ctx, input)
	if desired == "absent" && result.State == "absent" {
		for _, component := range contracts.WorkspaceApplicationRuntimeComponents(input.Revision) {
			result.ImageRetirement = append(result.ImageRetirement, contracts.WorkspaceApplicationRuntimeImageRetirement{Image: component.Image, State: "instance_required"})
		}
	}
	return result, err
}
