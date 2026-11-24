package tkeapply

import (
	"context"
	"errors"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/internal/protectedresource"
)

func (p *Executor) ReadWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input WorkspaceApplicationRuntimeInput) (WorkspaceApplicationRuntimeLifecycleResult, error) {
	if err := p.clusterAccessError(); err != nil {
		return WorkspaceApplicationRuntimeLifecycleResult{}, err
	}
	resources, err := p.readWorkspaceApplicationResources(ctx, input)
	if err != nil {
		return WorkspaceApplicationRuntimeLifecycleResult{}, err
	}
	observation := workspaceApplicationObservation(input, resources)
	for i, component := range observation.Components {
		deployment, exists := resources.deployments[ComponentResourceName(input, component.Name)]
		if !exists {
			continue
		}
		if typedDeploymentImage(deployment) != component.Image ||
			deployment.Labels["oplcloud.cn/runtime-operation-id"] != k8sCostLabelValue(input.RuntimeOperationID) {
			return WorkspaceApplicationRuntimeLifecycleResult{}, ErrLaunchStageBindingConflict
		}
		if !verifyTypedConfiguration(input, component, deployment, resources.storagePVC, resources.imageArgs) {
			return WorkspaceApplicationRuntimeLifecycleResult{}, ErrLaunchStageBindingConflict
		}
		if deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == 0 &&
			deployment.Status.Replicas == 0 &&
			deployment.Status.ObservedGeneration >= deployment.Generation {
			hasPod := false
			for _, pod := range resources.pods {
				rs := resources.replicaSets[typedControllerUID(pod.OwnerReferences, "ReplicaSet")]
				if rs != nil && typedControllerUID(rs.OwnerReferences, "Deployment") == deployment.UID {
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
	if result.State == "absent" && (len(resources.services) > 0 || len(resources.replicaSets) > 0 || len(resources.pods) > 0 || resources.auxiliaryCount() > 0) {
		result.State = "pending"
		result.Observation.Status = "pending"
		for i := range result.Observation.Components {
			result.Observation.Components[i].State = "pending"
		}
	}
	return result, nil
}

func (p *Executor) SetWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input WorkspaceApplicationRuntimeInput, desired string) (WorkspaceApplicationRuntimeLifecycleResult, error) {
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
		refs := make([]ResourceRef, 0, len(order)*2+3)
		for _, componentName := range order {
			name := ComponentResourceName(input, componentName)
			refs = append(refs,
				ResourceRef{Kind: "deployment", Name: name},
				ResourceRef{Kind: "service", Name: name},
			)
		}
		if len(input.Configuration.Files) > 0 {
			if _, err := p.readApplicationConfigObject(ctx, input); err != nil && !errors.Is(err, ErrResourceAbsent) {
				return WorkspaceApplicationRuntimeLifecycleResult{}, err
			}
			refs = append(refs, ResourceRef{Kind: "configmap", Name: ComponentResourceName(input, "config")})
		}
		refs = append(refs,
			ResourceRef{Kind: "networkpolicy", Name: ComponentResourceName(input, "network")},
			ResourceRef{Kind: "secret", Name: ComponentResourceName(input, "secrets")},
		)
		if err := p.deleteResources(ctx, refs, protectedresource.Target{}); err != nil {
			return WorkspaceApplicationRuntimeLifecycleResult{}, err
		}
	} else {
		var replicas int32
		if desired == "running" {
			replicas = 1
		} else if desired != "suspended" {
			return WorkspaceApplicationRuntimeLifecycleResult{}, ErrInputInvalid
		}
		resources, err := p.readWorkspaceApplicationResources(ctx, input)
		if err != nil {
			return WorkspaceApplicationRuntimeLifecycleResult{}, err
		}
		for _, componentName := range order {
			name := ComponentResourceName(input, componentName)
			deployment, exists := resources.deployments[name]
			if !exists || (deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == replicas) {
				continue
			}
			if desired == "running" && !workspaceApplicationDependenciesReady(input, componentName, resources) {
				continue
			}
			if err := p.scaleDeployment(ctx, name, replicas, protectedresource.Target{}); err != nil {
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
