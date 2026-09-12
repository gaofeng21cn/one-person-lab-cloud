package fabric

import (
	"context"
	"fmt"
	"os"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/fabric/internal/protectedresource"
)

// EnsureWorkspaceApplicationRuntime applies one manifest per admitted
// revision on the workspace's TKE namespace: a Deployment and Service per
// declared component (main plus dependencies) sharing the workspace's CBS PVC
// through per-component subPaths, and one NetworkPolicy scoping the
// application. Apply is idempotent by resource name; image drift against a
// live deployment is a conflict. Not-yet-available deployments return the
// observation with ErrWorkspaceLaunchPending so the engine claim stays
// started and the next replay resolves by readback.
func (p *TencentProvider) EnsureWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput, compute ComputeAllocation, volume StorageVolume) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	if err := p.validateInstallationConfig(); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if compute.ID == "" || volume.ID == "" || input.WorkspaceID == "" || input.WorkspaceID != compute.WorkspaceID {
		return contracts.WorkspaceApplicationRuntimeObservation{}, fmt.Errorf("workspace_application_runtime_resource_mismatch")
	}
	if err := contracts.ValidateWorkspaceApplicationRevision(input.Revision); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	existing, err := p.readWorkspaceApplicationDeployments(ctx, input)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(input.Revision) {
		if deployment, ok := existing[workspaceApplicationComponentResourceName(input, component.Name)]; ok &&
			workspaceApplicationDeploymentImage(deployment) != component.Image {
			return contracts.WorkspaceApplicationRuntimeObservation{}, fmt.Errorf("tencent_application_component_conflict")
		}
	}
	if _, err := p.callKubectl(ctx, []string{"apply", "-f", "-"}, workspaceApplicationManifest(input, compute, volume), protectedresource.Target{
		PackageID: compute.PackageID, NodePoolID: compute.NodePoolID, MachineID: compute.MachineName, NodeName: compute.NodeName,
		CVMID: firstNonEmpty(compute.InstanceID, compute.CVMInstanceID),
	}); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	live, err := p.readWorkspaceApplicationDeployments(ctx, input)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	observation := workspaceApplicationObservationFromDeployments(input, live)
	if observation.Status != "ready" {
		return observation, ErrWorkspaceLaunchPending
	}
	return observation, nil
}

// ReadWorkspaceApplicationRuntime observes the declared components without
// mutating anything: deployments missing from the namespace are absent.
func (p *TencentProvider) ReadWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	if err := p.validateInstallationConfig(); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	live, err := p.readWorkspaceApplicationDeployments(ctx, input)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	return workspaceApplicationObservationFromDeployments(input, live), nil
}

func (p *TencentProvider) readWorkspaceApplicationDeployments(ctx context.Context, input WorkspaceApplicationRuntimeInput) (map[string]map[string]any, error) {
	selector := "oplcloud.cn/workspace-id=" + k8sCostLabelValue(input.WorkspaceID) +
		",oplcloud.cn/runtime-id=" + k8sCostLabelValue(workspaceApplicationRuntimeID(input.WorkspaceID))
	raw, err := p.callKubectl(ctx, []string{"get", "deployments", "-l", selector, "-o", "json"}, nil, protectedresource.Target{})
	if err != nil {
		return nil, err
	}
	items, err := strictKubectlItems(raw)
	if err != nil {
		return nil, err
	}
	deployments := map[string]map[string]any{}
	for _, item := range items {
		deployment, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := stringValue(nested(deployment, "metadata", "name"))
		if name == "" {
			continue
		}
		deployments[name] = deployment
	}
	return deployments, nil
}

func workspaceApplicationObservationFromDeployments(input WorkspaceApplicationRuntimeInput, deployments map[string]map[string]any) contracts.WorkspaceApplicationRuntimeObservation {
	observed := make([]contracts.WorkspaceApplicationRuntimeComponentState, 0, len(input.Revision.Dependencies)+1)
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(input.Revision) {
		state, lastError := "absent", ""
		if deployment, found := deployments[workspaceApplicationComponentResourceName(input, component.Name)]; found {
			state, lastError = "pending", ""
			if number(nested(deployment, "status", "readyReplicas")) >= 1 {
				state = "ready"
			}
		}
		observed = append(observed, contracts.WorkspaceApplicationRuntimeComponentState{
			Name: component.Name, Role: component.Role, Image: component.Image,
			State: state, LastError: lastError,
			Ports: workspaceApplicationComponentPorts(input.Revision, component.Name),
		})
	}
	observation := contracts.WorkspaceApplicationRuntimeObservation{
		SchemaVersion: 1, WorkspaceID: input.WorkspaceID, RuntimeID: workspaceApplicationRuntimeID(input.WorkspaceID),
		Status: contracts.WorkspaceApplicationRuntimeOverallStatus(observed), Components: observed,
	}
	if observation.Status == "ready" && input.Revision.ExposurePolicy != "cloud_private" && len(input.Revision.Ports) > 0 {
		observation.EntryURL = fmt.Sprintf("https://%s/", workspaceApplicationIngressHost(input))
	}
	return observation
}

func workspaceApplicationComponentPorts(revision contracts.WorkspaceApplicationRevision, componentName string) []int {
	if componentName != contracts.WorkspaceApplicationComponentMain {
		return nil
	}
	ports := make([]int, 0, len(revision.Ports))
	for _, port := range revision.Ports {
		ports = append(ports, port.Port)
	}
	return ports
}

func workspaceApplicationComponentResourceName(input WorkspaceApplicationRuntimeInput, componentName string) string {
	return k8sName(input.ComputeID + "-" + componentName)
}

func workspaceApplicationDeploymentImage(deployment map[string]any) string {
	containers, _ := nested(deployment, "spec", "template", "spec", "containers").([]any)
	if len(containers) == 0 {
		return ""
	}
	if container, ok := containers[0].(map[string]any); ok {
		return stringValue(container["image"])
	}
	return ""
}

// workspaceApplicationManifest renders one List document: a Deployment and a
// Service per declared component plus one NetworkPolicy scoping the whole
// application. Persistent component mounts share the workspace CBS PVC through
// per-component subPaths; scratch mounts are memory-backed emptyDirs.
func workspaceApplicationManifest(input WorkspaceApplicationRuntimeInput, compute ComputeAllocation, volume StorageVolume) []byte {
	components := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)
	tags := oplCostTags(compute.AccountID, input.WorkspaceID, workspaceApplicationRuntimeID(input.WorkspaceID), input.RuntimeOperationID)
	pvcName := storagePVCName(volume)
	items := make([]any, 0, len(components)*2+1)
	for _, component := range components {
		items = append(items,
			workspaceApplicationComponentService(input, compute, volume.ID, component, tags),
			workspaceApplicationComponentDeployment(input, compute, volume, component, tags, pvcName),
		)
	}
	items = append(items, workspaceApplicationNetworkPolicy(input, tags))
	if ingress := workspaceApplicationIngress(input, compute, tags); ingress != nil {
		items = append(items, ingress)
	}
	return mustJSON(map[string]any{"apiVersion": "v1", "kind": "List", "items": items})
}

func workspaceApplicationIdentityLabels(input WorkspaceApplicationRuntimeInput, compute ComputeAllocation, volumeID string, component contracts.WorkspaceApplicationRuntimeComponentState, tags map[string]string) map[string]string {
	return mergeStringMaps(map[string]string{
		"oplcloud.cn/account-id":            compute.AccountID,
		"oplcloud.cn/workspace-id":          k8sCostLabelValue(input.WorkspaceID),
		"oplcloud.cn/compute-allocation-id": k8sCostLabelValue(compute.ID),
		"oplcloud.cn/storage-id":            k8sCostLabelValue(volumeID),
		"oplcloud.cn/runtime-id":            k8sCostLabelValue(workspaceApplicationRuntimeID(input.WorkspaceID)),
		"oplcloud.cn/runtime-operation-id":  k8sCostLabelValue(input.RuntimeOperationID),
		"oplcloud.cn/component-name":        k8sCostLabelValue(component.Name),
		"oplcloud.cn/component-role":        k8sCostLabelValue(component.Role),
		"app.kubernetes.io/name":            "opl-workspace-application",
		"app.kubernetes.io/instance":        workspaceApplicationComponentResourceName(input, component.Name),
	}, k8sCostLabels(tags))
}

func workspaceApplicationComponentSelector(input WorkspaceApplicationRuntimeInput, componentName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":            "opl-workspace-application",
		"app.kubernetes.io/instance":        workspaceApplicationComponentResourceName(input, componentName),
		"oplcloud.cn/compute-allocation-id": k8sCostLabelValue(input.ComputeID),
	}
}

func workspaceApplicationComponentDeployment(
	input WorkspaceApplicationRuntimeInput,
	compute ComputeAllocation,
	volume StorageVolume,
	component contracts.WorkspaceApplicationRuntimeComponentState,
	tags map[string]string,
	pvcName string,
) map[string]any {
	labels := workspaceApplicationIdentityLabels(input, compute, volume.ID, component, tags)
	selector := workspaceApplicationComponentSelector(input, component.Name)
	ports := []any{}
	volumeMounts := []any{}
	volumes := []any{}
	if component.Role == contracts.WorkspaceApplicationComponentMain {
		for _, port := range input.Revision.Ports {
			ports = append(ports, map[string]any{"name": port.Name, "containerPort": port.Port, "protocol": port.Protocol})
		}
	}
	for _, mount := range input.Revision.PersistentMounts {
		volumeMounts = append(volumeMounts, map[string]any{"name": "workspace-data", "mountPath": mount.MountPath, "subPath": component.Name + "/" + mount.Name})
	}
	for index, mount := range input.Revision.ScratchMounts {
		volumeName := fmt.Sprintf("scratch-%d", index)
		volumes = append(volumes, map[string]any{"name": volumeName, "emptyDir": map[string]any{"medium": "Memory"}})
		volumeMounts = append(volumeMounts, map[string]any{"name": volumeName, "mountPath": mount.MountPath})
	}
	volumes = append(volumes, map[string]any{"name": "workspace-data", "persistentVolumeClaim": map[string]any{"claimName": pvcName}})
	var readinessProbe any
	if component.Role == contracts.WorkspaceApplicationComponentMain && len(input.Revision.HealthChecks) > 0 {
		check := input.Revision.HealthChecks[0]
		readinessProbe = map[string]any{"httpGet": map[string]any{"path": check.Path, "port": check.Port}, "initialDelaySeconds": check.InitialDelaySeconds, "periodSeconds": 10}
	}
	container := map[string]any{"name": "app", "image": component.Image, "imagePullPolicy": "IfNotPresent"}
	if len(ports) > 0 {
		container["ports"] = ports
	}
	if readinessProbe != nil {
		container["readinessProbe"] = readinessProbe
	}
	if len(volumeMounts) > 0 {
		container["volumeMounts"] = volumeMounts
	}
	deployment := map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{
		"name": workspaceApplicationComponentResourceName(input, component.Name), "labels": labels, "annotations": tags,
	}, "spec": map[string]any{"replicas": 1, "strategy": map[string]any{"type": "Recreate"}, "selector": map[string]any{"matchLabels": selector}, "template": map[string]any{
		"metadata": map[string]any{"labels": labels}, "spec": map[string]any{
			"automountServiceAccountToken": false, "dnsPolicy": "ClusterFirst",
			"securityContext":  map[string]any{"runAsNonRoot": true, "runAsUser": 10001, "runAsGroup": 10001, "fsGroup": 10001, "fsGroupChangePolicy": tencentWorkspaceFSGroupPolicy, "seccompProfile": map[string]any{"type": "RuntimeDefault"}},
			"imagePullSecrets": []any{map[string]any{"name": os.Getenv("OPL_IMAGE_PULL_SECRET_NAME")}},
			"nodeSelector":     map[string]any{"kubernetes.io/hostname": compute.NodeName},
			"tolerations":      workspaceNodeTolerations(compute.PackageID),
			"containers":       []any{container},
			"volumes":          volumes,
		},
	}}}
	return deployment
}

func workspaceApplicationComponentService(
	input WorkspaceApplicationRuntimeInput,
	compute ComputeAllocation,
	volumeID string,
	component contracts.WorkspaceApplicationRuntimeComponentState,
	tags map[string]string,
) map[string]any {
	labels := workspaceApplicationIdentityLabels(input, compute, volumeID, component, tags)
	service := map[string]any{"apiVersion": "v1", "kind": "Service", "metadata": map[string]any{
		"name": workspaceApplicationComponentResourceName(input, component.Name), "labels": labels, "annotations": tags,
	}, "spec": map[string]any{"type": "ClusterIP", "selector": workspaceApplicationComponentSelector(input, component.Name)}}
	if component.Role == contracts.WorkspaceApplicationComponentMain {
		ports := []any{}
		for _, port := range input.Revision.Ports {
			ports = append(ports, map[string]any{"name": port.Name, "port": port.Port, "targetPort": port.Name, "protocol": port.Protocol})
		}
		service["spec"].(map[string]any)["ports"] = ports
	}
	return service
}

func workspaceApplicationNetworkPolicy(input WorkspaceApplicationRuntimeInput, tags map[string]string) map[string]any {
	workspaceSelector := map[string]any{"matchLabels": map[string]any{
		"oplcloud.cn/workspace-id": k8sCostLabelValue(input.WorkspaceID),
		"oplcloud.cn/runtime-id":   k8sCostLabelValue(workspaceApplicationRuntimeID(input.WorkspaceID)),
	}}
	ports := []any{}
	for _, port := range input.Revision.Ports {
		ports = append(ports, map[string]any{"protocol": "TCP", "port": port.Port})
	}
	ingress := []any{map[string]any{"from": []any{map[string]any{"podSelector": map[string]any{"matchLabels": map[string]any{"app.kubernetes.io/name": "opl-cloud", "app.kubernetes.io/component": "control-plane"}}}}}}
	if len(ports) > 0 {
		ingress[0].(map[string]any)["ports"] = ports
	}
	ingress = append(ingress, map[string]any{"from": []any{map[string]any{"podSelector": workspaceSelector["matchLabels"]}}})
	return map[string]any{"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy", "metadata": map[string]any{
		"name": k8sName(input.ComputeID + "-application"), "annotations": tags,
	}, "spec": map[string]any{"podSelector": workspaceSelector, "policyTypes": []any{"Ingress", "Egress"}, "ingress": ingress, "egress": workspaceEgressRules()}}
}

// workspaceApplicationIngressHost derives the dedicated subdomain origin of
// one application deployment. Apps keep their own root path and cookies, so
// they never share the workspace domain's cookie scope; wildcard DNS and
// certificate coverage for this subdomain are installation prerequisites.
func workspaceApplicationIngressHost(input WorkspaceApplicationRuntimeInput) string {
	return fmt.Sprintf("%s.%s", k8sName(input.ComputeID+"-"+input.Revision.ApplicationID), workspaceDomain())
}

// workspaceApplicationIngress renders the public entry of one application
// deployment. cloud-private applications and applications without declared
// web ports stay cluster-internal: no Ingress is created for them.
func workspaceApplicationIngress(input WorkspaceApplicationRuntimeInput, compute ComputeAllocation, tags map[string]string) map[string]any {
	if input.Revision.ExposurePolicy == "cloud_private" || len(input.Revision.Ports) == 0 {
		return nil
	}
	runtimeID := workspaceApplicationRuntimeID(input.WorkspaceID)
	labels := mergeStringMaps(map[string]string{
		"oplcloud.cn/account-id":            compute.AccountID,
		"oplcloud.cn/workspace-id":          k8sCostLabelValue(input.WorkspaceID),
		"oplcloud.cn/runtime-id":            k8sCostLabelValue(runtimeID),
		"oplcloud.cn/compute-allocation-id": k8sCostLabelValue(compute.ID),
		"app.kubernetes.io/name":            "opl-workspace-application",
		"app.kubernetes.io/instance":        workspaceApplicationComponentResourceName(input, contracts.WorkspaceApplicationComponentMain),
	}, k8sCostLabels(tags))
	paths := []any{}
	for _, port := range input.Revision.Ports {
		paths = append(paths, map[string]any{
			"pathType": "Prefix",
			"backend":  map[string]any{"service": map[string]any{"name": workspaceApplicationComponentResourceName(input, contracts.WorkspaceApplicationComponentMain), "port": map[string]any{"number": port.Port}}},
		})
	}
	return map[string]any{"apiVersion": "networking.k8s.io/v1", "kind": "Ingress", "metadata": map[string]any{
		"name": k8sName(input.ComputeID + "-" + input.Revision.ApplicationID + "-entry"), "labels": labels, "annotations": tags,
	}, "spec": map[string]any{"rules": []any{map[string]any{
		"host": workspaceApplicationIngressHost(input),
		"http": map[string]any{"paths": paths},
	}}}}
}
