package fabric

import (
	"context"
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
		observation.EntryURL = ""
	}
	result := applicationLifecycleResult(observation)
	if result.State == "absent" && (len(resources.services) > 0 || len(resources.ingresses) > 0 || len(resources.replicaSets) > 0 || len(resources.pods) > 0 || len(resources.auxiliary) > 0) {
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
	if desired == "absent" {
		targets := []string{"delete"}
		for _, component := range contracts.WorkspaceApplicationRuntimeComponents(input.Revision) {
			name := workspaceApplicationComponentResourceName(input, component.Name)
			targets = append(targets, "deployment/"+name, "service/"+name)
		}
		targets = append(targets, "networkpolicy/"+workspaceApplicationComponentResourceName(input, "network"), "networkpolicy/"+workspaceApplicationComponentResourceName(input, "entry-network"), "ingress/"+workspaceApplicationComponentResourceName(input, "entry"), "secret/"+workspaceApplicationComponentResourceName(input, "secrets"), "--ignore-not-found=true", "--wait=false")
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
		for _, component := range contracts.WorkspaceApplicationRuntimeComponents(input.Revision) {
			name := workspaceApplicationComponentResourceName(input, component.Name)
			if _, exists := resources.deployments[name]; !exists {
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
