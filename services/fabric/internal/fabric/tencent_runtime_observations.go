package fabric

import (
	"context"
	"fmt"
	"reflect"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/fabric/internal/protectedresource"
)

func controllerUID(resource map[string]any, kind string) string {
	owners, _ := nested(resource, "metadata", "ownerReferences").([]any)
	uid := ""
	for _, value := range owners {
		owner, _ := value.(map[string]any)
		if owner["controller"] != true {
			continue
		}
		if uid != "" || stringValue(owner["kind"]) != kind || stringValue(owner["uid"]) == "" {
			return ""
		}
		uid = stringValue(owner["uid"])
	}
	return uid
}

func runtimeTemplateMatches(deployment, replicaSet map[string]any) bool {
	clone := func(resource map[string]any) map[string]any {
		template, _ := nested(resource, "spec", "template").(map[string]any)
		result := map[string]any{}
		for key, value := range template {
			result[key] = value
		}
		metadata, _ := result["metadata"].(map[string]any)
		copied := map[string]any{}
		for key, value := range metadata {
			copied[key] = value
		}
		labels, _ := copied["labels"].(map[string]any)
		labelCopy := map[string]any{}
		for key, value := range labels {
			if key != "pod-template-hash" {
				labelCopy[key] = value
			}
		}
		copied["labels"] = labelCopy
		result["metadata"] = copied
		return result
	}
	return nested(deployment, "spec", "template") != nil && reflect.DeepEqual(clone(deployment), clone(replicaSet))
}

func (p *TencentProvider) readRuntimeObservations(ctx context.Context) ([]runtimeProviderObservation, error) {
	raw, err := p.callKubectl(ctx, []string{"get", "deployment,replicaset,pod", "-l", "oplcloud.cn/workspace-id", "-o", "json"}, nil, protectedresource.Target{})
	if err != nil {
		return nil, err
	}
	items, err := strictKubectlItems(raw)
	if err != nil {
		return nil, fmt.Errorf("workspace_runtime_summary_response_invalid")
	}
	deployments := []map[string]any{}
	deploymentUIDs := map[string]bool{}
	replicaSets := map[string]map[string]any{}
	pods := []map[string]any{}
	objectUIDs := map[string]bool{}
	for _, value := range items {
		object, ok := value.(map[string]any)
		uid := stringValue(nested(object, "metadata", "uid"))
		if !ok || uid == "" || objectUIDs[uid] || stringValue(nested(object, "metadata", "name")) == "" {
			return nil, fmt.Errorf("runtime_observations_object_invalid")
		}
		objectUIDs[uid] = true
		switch stringValue(object["kind"]) {
		case "Deployment":
			deployments = append(deployments, object)
			deploymentUIDs[uid] = true
		case "ReplicaSet":
			replicaSets[uid] = object
		case "Pod":
			pods = append(pods, object)
		default:
			return nil, fmt.Errorf("runtime_observations_object_invalid")
		}
	}
	for _, pod := range pods {
		phase := stringValue(nested(pod, "status", "phase"))
		if phase == "Succeeded" || phase == "Failed" {
			continue
		}
		rs := replicaSets[controllerUID(pod, "ReplicaSet")]
		if rs == nil || !deploymentUIDs[controllerUID(rs, "Deployment")] {
			return nil, fmt.Errorf("runtime_observations_orphan_pod")
		}
	}
	for _, rs := range replicaSets {
		if !deploymentUIDs[controllerUID(rs, "Deployment")] && number(nested(rs, "spec", "replicas")) > 0 {
			return nil, fmt.Errorf("runtime_observations_orphan_replicaset")
		}
	}
	result := make([]runtimeProviderObservation, 0, len(deployments))
	for _, deployment := range deployments {
		uid := stringValue(nested(deployment, "metadata", "uid"))
		observation := runtimeProviderObservation{RuntimeObservation: contracts.RuntimeObservation{
			ObjectRef:    "runtime:" + stableSuffix("tencent-tke", p.namespace, uid),
			AccountID:    stringValue(nested(deployment, "metadata", "annotations", "opl_account_id")),
			WorkspaceID:  firstNonEmpty(stringValue(nested(deployment, "metadata", "annotations", "opl_workspace_id")), stringValue(nested(deployment, "metadata", "labels", "oplcloud.cn/workspace-id"))),
			RuntimeID:    stringValue(nested(deployment, "metadata", "labels", "oplcloud.cn/runtime-id")),
			DesiredState: contracts.ResourceObservedUnknown, ObservedState: contracts.ResourceObservedUnknown,
		}, serviceName: stringValue(nested(deployment, "metadata", "name")), operationID: stringValue(nested(deployment, "metadata", "annotations", "opl_operation_id"))}
		observation.bindingValid = observation.AccountID != "" && observation.WorkspaceID != "" && observation.RuntimeID != "" && observation.operationID != ""
		for key, want := range oplCostTags(observation.AccountID, observation.WorkspaceID, observation.RuntimeID, observation.operationID) {
			if stringValue(nested(deployment, "metadata", "annotations", key)) != want {
				observation.bindingValid = false
			}
		}
		for key, want := range k8sCostLabels(oplCostTags(observation.AccountID, observation.WorkspaceID, observation.RuntimeID, observation.operationID)) {
			if stringValue(nested(deployment, "metadata", "labels", key)) != want {
				observation.bindingValid = false
			}
		}
		if stringValue(nested(deployment, "metadata", "labels", "oplcloud.cn/runtime-operation-id")) != k8sCostLabelValue(observation.operationID) {
			observation.bindingValid = false
		}
		replicas, validReplicas := nested(deployment, "spec", "replicas").(float64)
		if !validReplicas || replicas != 0 && replicas != 1 {
			observation.ReasonCode = "runtime_desired_replicas_invalid"
			result = append(result, observation)
			continue
		}
		observation.DesiredState = contracts.ResourceObservedRunning
		if replicas == 0 {
			observation.DesiredState = contracts.ResourceObservedSuspended
		}
		generation := number(nested(deployment, "metadata", "generation"))
		current := generation > 0 && number(nested(deployment, "status", "observedGeneration")) >= generation
		currentReady, active, wrongOwner := 0, 0, false
		for _, pod := range pods {
			if stringValue(nested(pod, "metadata", "labels", "oplcloud.cn/workspace-id")) != k8sCostLabelValue(observation.WorkspaceID) {
				continue
			}
			rs := replicaSets[controllerUID(pod, "ReplicaSet")]
			if rs == nil || controllerUID(rs, "Deployment") != uid {
				wrongOwner = true
				continue
			}
			phase := stringValue(nested(pod, "status", "phase"))
			if phase != "Succeeded" && phase != "Failed" {
				active++
			}
			if phase == "Running" && conditionStatuses(nested(pod, "status", "conditions"))["Ready"] == "True" &&
				nested(pod, "metadata", "deletionTimestamp") == nil && runtimeTemplateMatches(deployment, rs) {
				currentReady++
			}
		}
		observation.ObservedState = contracts.ResourceObservedPending
		switch {
		case wrongOwner:
			observation.ReasonCode = "runtime_pod_owner_conflict"
		case !current:
			observation.ReasonCode = "runtime_generation_pending"
		case replicas == 0 && active == 0:
			observation.ObservedState = contracts.ResourceObservedSuspended
		case replicas == 1 && currentReady == 1 && number(nested(deployment, "status", "updatedReplicas")) == 1 && number(nested(deployment, "status", "readyReplicas")) == 1 && number(nested(deployment, "status", "availableReplicas")) == 1:
			observation.ObservedState = contracts.ResourceObservedRunning
			observation.legacyReady = true
		default:
			observation.ReasonCode = "runtime_workload_pending"
		}
		result = append(result, observation)
	}
	return result, nil
}
