package fabric

import (
	"context"
	"fmt"
	"strconv"

	"opl-cloud/services/fabric/internal/protectedresource"
)

func (p *TencentProvider) readWorkspaceRuntimePowerDeployment(ctx context.Context, input WorkspaceRuntimePowerInput) (map[string]any, WorkspaceRuntimePowerResult, error) {
	result := WorkspaceRuntimePowerResult{SchemaVersion: 1, Binding: input, State: "pending"}
	raw, err := p.callKubectl(ctx, []string{"get", "deployment", "-l", "oplcloud.cn/workspace-id=" + input.WorkspaceID, "-o", "json"}, nil, protectedresource.Target{})
	if err != nil {
		return nil, result, err
	}
	items, err := strictKubectlItems(raw)
	if err != nil {
		return nil, result, err
	}
	if len(items) == 0 {
		result.State = "absent"
		return nil, result, nil
	}
	if len(items) != 1 {
		return nil, result, ErrWorkspaceRuntimePowerConflict
	}
	deployment, ok := items[0].(map[string]any)
	if !ok || stringValue(deployment["kind"]) != "Deployment" || stringValue(nested(deployment, "metadata", "name")) == "" {
		return nil, result, ErrWorkspaceRuntimePowerConflict
	}
	for key, want := range k8sCostLabels(oplCostTags(input.AccountID, input.WorkspaceID, input.RuntimeID, input.RuntimeOperationID)) {
		if want != "" && stringValue(nested(deployment, "metadata", "labels", key)) != want {
			return nil, result, ErrWorkspaceRuntimePowerConflict
		}
	}
	if stringValue(nested(deployment, "metadata", "labels", "oplcloud.cn/runtime-id")) != input.RuntimeID || stringValue(nested(deployment, "metadata", "annotations", "opl_operation_id")) != input.RuntimeOperationID {
		return nil, result, ErrWorkspaceRuntimePowerConflict
	}
	replicas, replicaOK := nested(deployment, "spec", "replicas").(float64)
	if !replicaOK || replicas != 0 && replicas != 1 || stringValue(nested(deployment, "metadata", "resourceVersion")) == "" {
		return nil, result, ErrWorkspaceRuntimePowerConflict
	}
	if replicas == 0 {
		podsRaw, err := p.callKubectl(ctx, []string{"get", "pod", "-l", "oplcloud.cn/workspace-id=" + input.WorkspaceID, "-o", "json"}, nil, protectedresource.Target{})
		if err != nil {
			return deployment, result, err
		}
		pods, err := strictKubectlItems(podsRaw)
		if err != nil {
			return deployment, result, err
		}
		if len(pods) == 0 {
			result.State = "suspended"
		}
	} else if generation := number(nested(deployment, "metadata", "generation")); generation > 0 &&
		number(nested(deployment, "status", "observedGeneration")) >= generation &&
		number(nested(deployment, "status", "readyReplicas")) == 1 && number(nested(deployment, "status", "availableReplicas")) == 1 {
		result.State = "running"
	}
	return deployment, result, nil
}

func (p *TencentProvider) ReadWorkspaceRuntimePower(ctx context.Context, input WorkspaceRuntimePowerInput) (WorkspaceRuntimePowerResult, error) {
	_, result, err := p.readWorkspaceRuntimePowerDeployment(ctx, input)
	return result, err
}

func (p *TencentProvider) SetWorkspaceRuntimePower(ctx context.Context, input WorkspaceRuntimePowerInput) (WorkspaceRuntimePowerResult, error) {
	deployment, result, err := p.readWorkspaceRuntimePowerDeployment(ctx, input)
	if err != nil || result.State == "absent" || result.State == input.DesiredState {
		return result, err
	}
	target := 0
	if input.DesiredState == "running" {
		target = 1
	}
	current := int(number(nested(deployment, "spec", "replicas")))
	if current != target {
		_, err = p.callKubectl(ctx, []string{"scale", "deployment/" + stringValue(nested(deployment, "metadata", "name")), "--replicas=" + strconv.Itoa(target), fmt.Sprintf("--current-replicas=%d", current), "--resource-version=" + stringValue(nested(deployment, "metadata", "resourceVersion"))}, nil, protectedresource.Target{})
		if err != nil {
			return result, err
		}
	}
	return p.ReadWorkspaceRuntimePower(ctx, input)
}
