package fabric

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/fabric/internal/protectedresource"
)

// EnsureWorkspaceApplicationRuntime applies one manifest per admitted
// revision on the workspace's TKE namespace: a Deployment and Service per
// declared component (main plus dependencies), with the main component's
// declared mounts backed by the workspace CBS PVC, and NetworkPolicies scoping the
// application. Apply is idempotent by resource name; image drift against a
// live deployment is a conflict. Not-yet-available deployments return the
// observation with ErrWorkspaceLaunchPending so the engine claim stays
// started and the next replay resolves by readback.
func (p *TencentProvider) EnsureWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput, compute ComputeAllocation, volume StorageVolume) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	if err := validateWorkspaceApplicationConfiguration(input); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if err := p.validateInstallationConfig(); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if compute.ID == "" || volume.ID == "" || input.WorkspaceID == "" || input.WorkspaceID != compute.WorkspaceID {
		return contracts.WorkspaceApplicationRuntimeObservation{}, fmt.Errorf("workspace_application_runtime_resource_mismatch")
	}
	if err := contracts.ValidateWorkspaceApplicationRevision(input.Revision); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	// Kubernetes provides one native readiness probe per container. Do not
	// silently discard additional required checks from an admitted revision.
	if len(input.Revision.HealthChecks) > 1 {
		return contracts.WorkspaceApplicationRuntimeObservation{}, fmt.Errorf("tencent_application_health_checks_unsupported")
	}
	existing, err := p.readWorkspaceApplicationResources(ctx, input)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(input.Revision) {
		if deployment, ok := existing.deployments[workspaceApplicationComponentResourceName(input, component.Name)]; ok &&
			workspaceApplicationDeploymentImage(deployment) != component.Image {
			return contracts.WorkspaceApplicationRuntimeObservation{}, fmt.Errorf("tencent_application_component_conflict")
		}
	}
	if err := p.prepareApplicationSecrets(ctx, input); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if _, err := p.callKubectl(ctx, []string{"apply", "-f", "-"}, workspaceApplicationManifest(input, compute, volume), protectedresource.Target{
		PackageID: compute.PackageID, NodePoolID: compute.NodePoolID, MachineID: compute.MachineName, NodeName: compute.NodeName,
		CVMID: firstNonEmpty(compute.InstanceID, compute.CVMInstanceID),
	}); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	live, err := p.readWorkspaceApplicationResources(ctx, input)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	observation := workspaceApplicationObservation(input, live)
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
	live, err := p.readWorkspaceApplicationResources(ctx, input)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	return workspaceApplicationObservation(input, live), nil
}

type workspaceApplicationKubernetesResources struct {
	deployments map[string]map[string]any
	replicaSets map[string]map[string]any
	services    map[string]map[string]any
	ingresses   map[string]map[string]any
	pods        []map[string]any
	auxiliary   map[string]map[string]any
	storagePVC  string
}

func (p *TencentProvider) readWorkspaceApplicationResources(ctx context.Context, input WorkspaceApplicationRuntimeInput) (workspaceApplicationKubernetesResources, error) {
	resources := workspaceApplicationKubernetesResources{
		deployments: map[string]map[string]any{}, replicaSets: map[string]map[string]any{},
		services: map[string]map[string]any{}, ingresses: map[string]map[string]any{}, auxiliary: map[string]map[string]any{},
	}
	selector := "oplcloud.cn/workspace-id=" + k8sCostLabelValue(input.WorkspaceID) +
		",oplcloud.cn/runtime-id=" + k8sCostLabelValue(applicationRuntimeID(input))
	raw, err := p.callKubectl(ctx, []string{"get", "deployment,replicaset,pod,service,ingress,networkpolicy,secret", "-l", selector, "-o", "json"}, nil, protectedresource.Target{})
	if err != nil {
		return resources, err
	}
	items, err := strictKubectlItems(raw)
	if err != nil {
		return resources, err
	}
	seen := map[string]bool{}
	for _, item := range items {
		object, ok := item.(map[string]any)
		name, uid := stringValue(nested(object, "metadata", "name")), stringValue(nested(object, "metadata", "uid"))
		if !ok || name == "" || uid == "" || seen[uid] || input.AccountID == "" ||
			stringValue(nested(object, "metadata", "labels", "oplcloud.cn/account-id")) != k8sCostLabelValue(input.AccountID) ||
			stringValue(nested(object, "metadata", "labels", "oplcloud.cn/workspace-id")) != k8sCostLabelValue(input.WorkspaceID) ||
			stringValue(nested(object, "metadata", "labels", "oplcloud.cn/runtime-id")) != k8sCostLabelValue(applicationRuntimeID(input)) {
			return resources, fmt.Errorf("tencent_application_runtime_readback_invalid")
		}
		seen[uid] = true
		switch object["kind"] {
		case "Deployment":
			resources.deployments[name] = object
		case "ReplicaSet":
			resources.replicaSets[uid] = object
		case "Pod":
			resources.pods = append(resources.pods, object)
		case "Service":
			resources.services[name] = object
		case "Ingress":
			resources.ingresses[name] = object
		case "NetworkPolicy", "Secret":
			resources.auxiliary[stringValue(object["kind"])+":"+name] = object
		default:
			return resources, fmt.Errorf("tencent_application_runtime_readback_invalid")
		}
	}
	if input.SchemaVersion == 0 {
		// Original application policies have owner annotations but no selector
		// labels. Read the exact original names so absence cannot omit them.
		for _, component := range []string{"network", "entry-network"} {
			name := workspaceApplicationComponentResourceName(input, component)
			raw, err := p.callKubectl(ctx, []string{"get", "networkpolicy/" + name, "--ignore-not-found", "-o", "json"}, nil, protectedresource.Target{})
			if err != nil {
				return resources, err
			}
			if len(bytes.TrimSpace(raw)) == 0 {
				continue
			}
			var policy map[string]any
			if json.Unmarshal(raw, &policy) != nil || policy["kind"] != "NetworkPolicy" || stringValue(nested(policy, "metadata", "name")) != name || stringValue(nested(policy, "metadata", "annotations", "opl_account_id")) != input.AccountID || stringValue(nested(policy, "metadata", "annotations", "opl_workspace_id")) != input.WorkspaceID || stringValue(nested(policy, "metadata", "annotations", "opl_resource_id")) != applicationRuntimeID(input) {
				return resources, ErrLaunchStageBindingConflict
			}
			resources.auxiliary["NetworkPolicy:"+name] = policy
		}
	}
	if input.SchemaVersion == 2 && len(input.Revision.PersistentMounts) > 0 && resources.deployments[workspaceApplicationComponentResourceName(input, contracts.WorkspaceApplicationComponentMain)] != nil {
		// Resolve the current storage owner's PVC. Its provider name is not
		// derivable from a logical StorageID and must not be guessed.
		raw, err := p.callKubectl(ctx, []string{"get", "pvc", "-l", "oplcloud.cn/storage-id=" + k8sCostLabelValue(input.VolumeID), "-o", "json"}, nil, protectedresource.Target{})
		if err != nil {
			return resources, err
		}
		items, err := strictKubectlItems(raw)
		if err != nil {
			return resources, err
		}
		if len(items) != 1 {
			return resources, ErrLaunchStageBindingConflict
		}
		pvc, ok := items[0].(map[string]any)
		if !ok || pvc["kind"] != "PersistentVolumeClaim" || stringValue(nested(pvc, "metadata", "annotations", "opl_account_id")) != input.AccountID || stringValue(nested(pvc, "metadata", "annotations", "opl_workspace_id")) != input.WorkspaceID || stringValue(nested(pvc, "metadata", "annotations", "opl_resource_id")) != input.VolumeID || stringValue(nested(pvc, "metadata", "labels", "oplcloud.cn/storage-id")) != k8sCostLabelValue(input.VolumeID) {
			return resources, ErrLaunchStageBindingConflict
		}
		resources.storagePVC = stringValue(nested(pvc, "metadata", "name"))
		if resources.storagePVC == "" {
			return resources, ErrLaunchStageBindingConflict
		}
	}
	return resources, nil
}

func workspaceApplicationObservation(input WorkspaceApplicationRuntimeInput, resources workspaceApplicationKubernetesResources) contracts.WorkspaceApplicationRuntimeObservation {
	_, hasEntry := contracts.WorkspaceApplicationEntryPort(input.Revision)
	entryRequired := hasEntry && input.Revision.ExposurePolicy != "cloud_private"
	entryURL := workspaceApplicationEntryURL(input, resources)
	observed := make([]contracts.WorkspaceApplicationRuntimeComponentState, 0, len(input.Revision.Dependencies)+1)
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(input.Revision) {
		state, lastError := "absent", ""
		if deployment, found := resources.deployments[workspaceApplicationComponentResourceName(input, component.Name)]; found {
			state, lastError = workspaceApplicationComponentStatus(input, component, deployment, resources)
		}
		if component.Role == contracts.WorkspaceApplicationComponentMain && state == "ready" && entryRequired && entryURL == "" {
			state, lastError = "pending", "tencent_application_entry_pending"
		}
		observed = append(observed, contracts.WorkspaceApplicationRuntimeComponentState{
			Name: component.Name, Role: component.Role, Image: component.Image,
			State: state, LastError: lastError,
			Ports: workspaceApplicationComponentPorts(input.Revision, component.Name),
		})
	}
	observation := contracts.WorkspaceApplicationRuntimeObservation{
		SchemaVersion: 1, WorkspaceID: input.WorkspaceID, RuntimeID: applicationRuntimeID(input),
		Status: contracts.WorkspaceApplicationRuntimeOverallStatus(observed), Components: observed,
	}
	if observation.Status == "ready" {
		observation.EntryURL = entryURL
	}
	return observation
}

func workspaceApplicationComponentStatus(input WorkspaceApplicationRuntimeInput, component contracts.WorkspaceApplicationRuntimeComponentState, deployment map[string]any, resources workspaceApplicationKubernetesResources) (string, string) {
	if component.Role == contracts.WorkspaceApplicationComponentMain && len(input.Revision.HealthChecks) > 1 {
		return "failed", "tencent_application_health_checks_unsupported"
	}
	if !verifyTencentApplicationConfiguration(input, component, deployment, resources.storagePVC) {
		return "failed", "tencent_application_configuration_mismatch"
	}
	if workspaceApplicationDeploymentImage(deployment) != component.Image ||
		stringValue(firstContainerField(deployment, "name")) != "app" ||
		stringValue(nested(deployment, "metadata", "labels", "oplcloud.cn/runtime-operation-id")) != k8sCostLabelValue(input.RuntimeOperationID) {
		return "failed", "tencent_application_component_identity_mismatch"
	}
	if component.Role == contracts.WorkspaceApplicationComponentMain && len(input.Revision.HealthChecks) > 0 {
		check := input.Revision.HealthChecks[0]
		probe, _ := firstContainerField(deployment, "readinessProbe").(map[string]any)
		if stringValue(nested(probe, "httpGet", "path")) != check.Path || number(nested(probe, "httpGet", "port")) != float64(check.Port) {
			return "failed", "tencent_application_health_check_mismatch"
		}
	}
	generation := number(nested(deployment, "metadata", "generation"))
	if nested(deployment, "metadata", "deletionTimestamp") != nil || generation <= 0 ||
		number(nested(deployment, "status", "observedGeneration")) < generation ||
		number(nested(deployment, "spec", "replicas")) != 1 || number(nested(deployment, "status", "updatedReplicas")) != 1 ||
		number(nested(deployment, "status", "readyReplicas")) != 1 || number(nested(deployment, "status", "availableReplicas")) != 1 {
		return "pending", ""
	}
	ready := 0
	for _, pod := range resources.pods {
		replicaSet := resources.replicaSets[controllerUID(pod, "ReplicaSet")]
		if replicaSet == nil || controllerUID(replicaSet, "Deployment") != stringValue(nested(deployment, "metadata", "uid")) ||
			!runtimeTemplateMatches(deployment, replicaSet) || nested(replicaSet, "metadata", "deletionTimestamp") != nil || nested(pod, "metadata", "deletionTimestamp") != nil {
			continue
		}
		if stringValue(nested(pod, "status", "phase")) == "Failed" {
			return "failed", "tencent_application_pod_failed"
		}
		if stringValue(nested(pod, "status", "phase")) != "Running" || conditionStatuses(nested(pod, "status", "conditions"))["Ready"] != "True" {
			return "pending", ""
		}
		containers, _ := nested(pod, "spec", "containers").([]any)
		var container map[string]any
		if len(containers) == 1 {
			container, _ = containers[0].(map[string]any)
		}
		if stringValue(container["name"]) != "app" || stringValue(container["image"]) != component.Image ||
			!podImageIDsMatch([]any{pod}, "oplcloud.cn/workspace-id", k8sCostLabelValue(input.WorkspaceID), "app", component.Image) {
			return "failed", "tencent_application_component_image_unverified"
		}
		ready++
	}
	if ready != 1 {
		return "pending", ""
	}
	return "ready", ""
}

// EntryURL describes a route admitted by the installation's Ingress controller.
// It does not assert public HTTP/DNS/TLS qualification, which belongs to the
// Instance owner. A desired host or ready Deployment alone is not an entry.
func workspaceApplicationEntryURL(input WorkspaceApplicationRuntimeInput, resources workspaceApplicationKubernetesResources) string {
	entryPort, hasEntry := contracts.WorkspaceApplicationEntryPort(input.Revision)
	if input.Revision.ExposurePolicy == "cloud_private" || !hasEntry {
		return ""
	}
	ingress := resources.ingresses[workspaceApplicationComponentResourceName(input, "entry")]
	class := os.Getenv("OPL_INGRESS_CLASS")
	if ingress == nil || nested(ingress, "metadata", "deletionTimestamp") != nil ||
		class != "" && stringValue(nested(ingress, "spec", "ingressClassName")) != class {
		return ""
	}
	addresses, _ := nested(ingress, "status", "loadBalancer", "ingress").([]any)
	assigned := false
	for _, value := range addresses {
		address, _ := value.(map[string]any)
		assigned = assigned || stringValue(address["ip"]) != "" || stringValue(address["hostname"]) != ""
	}
	if !assigned {
		return ""
	}
	name := workspaceApplicationComponentResourceName(input, contracts.WorkspaceApplicationComponentMain)
	service := resources.services[name]
	selector, _ := nested(service, "spec", "selector").(map[string]any)
	if service == nil || nested(service, "metadata", "deletionTimestamp") != nil ||
		!reflect.DeepEqual(selector, stringAnyMap(workspaceApplicationComponentSelector(input, contracts.WorkspaceApplicationComponentMain))) {
		return ""
	}
	servicePorts, _ := nested(service, "spec", "ports").([]any)
	portMatches := false
	for _, value := range servicePorts {
		port, _ := value.(map[string]any)
		portMatches = portMatches || stringValue(port["name"]) == entryPort.Name && number(port["port"]) == float64(entryPort.Port) &&
			stringValue(port["targetPort"]) == entryPort.Name && stringValue(port["protocol"]) == "TCP"
	}
	if !portMatches {
		return ""
	}
	host := workspaceApplicationIngressHost(input)
	rules, _ := nested(ingress, "spec", "rules").([]any)
	if len(rules) != 1 {
		return ""
	}
	rule, _ := rules[0].(map[string]any)
	paths, _ := nested(rule, "http", "paths").([]any)
	if stringValue(rule["host"]) != host || len(paths) != 1 {
		return ""
	}
	path, _ := paths[0].(map[string]any)
	if stringValue(path["path"]) != "/" || stringValue(path["pathType"]) != "Prefix" ||
		stringValue(nested(path, "backend", "service", "name")) != name ||
		number(nested(path, "backend", "service", "port", "number")) != float64(entryPort.Port) {
		return ""
	}
	return "http://" + host + "/"
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
	if input.SchemaVersion == 0 {
		switch componentName {
		case "network":
			return k8sName(input.ComputeID + "-application")
		case "entry-network":
			return k8sName(input.ComputeID + "-application-entry")
		case "entry":
			return k8sName(input.ComputeID + "-" + input.Revision.ApplicationID + "-entry")
		}
		return k8sName(input.ComputeID + "-" + componentName)
	}
	return k8sName("app-" + stableSuffix(input.RuntimeOperationID, componentName)[:32])
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
// Service per declared component plus NetworkPolicies scoping the application.
// The main component's persistent mounts use the workspace CBS PVC and its
// scratch mounts are memory-backed emptyDirs. Dependencies do not inherit them.
func workspaceApplicationManifest(input WorkspaceApplicationRuntimeInput, compute ComputeAllocation, volume StorageVolume) []byte {
	components := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)
	tags := oplCostTags(compute.AccountID, input.WorkspaceID, applicationRuntimeID(input), input.RuntimeOperationID)
	pvcName := storagePVCName(volume)
	items := make([]any, 0, len(components)*2+1)
	for _, component := range components {
		items = append(items,
			workspaceApplicationComponentService(input, compute, volume.ID, component, tags),
			workspaceApplicationComponentDeployment(input, compute, volume, component, tags, pvcName),
		)
	}
	items = append(items, workspaceApplicationNetworkPolicy(input, tags))
	if policy := workspaceApplicationPublicEntryPolicy(input, tags); policy != nil {
		items = append(items, policy)
	}
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
		"oplcloud.cn/runtime-id":            k8sCostLabelValue(applicationRuntimeID(input)),
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
	if component.Role == contracts.WorkspaceApplicationComponentDependency {
		if dependency, depErr := dependencySpecByName(input.Revision, component.Name); depErr == nil {
			for _, port := range dependency.Ports {
				ports = append(ports, map[string]any{"name": port.Name, "containerPort": port.Port, "protocol": port.Protocol})
			}
		}
	}
	if component.Role == contracts.WorkspaceApplicationComponentMain {
		for _, port := range input.Revision.Ports {
			ports = append(ports, map[string]any{"name": port.Name, "containerPort": port.Port, "protocol": port.Protocol})
		}
		for _, mount := range input.Revision.PersistentMounts {
			volumeMounts = append(volumeMounts, map[string]any{"name": "workspace-data", "mountPath": mount.MountPath, "subPath": applicationPersistentSubPath(input, mount), "readOnly": mount.ReadOnly})
		}
		for index, mount := range input.Revision.ScratchMounts {
			volumeName := fmt.Sprintf("scratch-%d", index)
			volumes = append(volumes, map[string]any{"name": volumeName, "emptyDir": map[string]any{"medium": "Memory"}})
			volumeMounts = append(volumeMounts, map[string]any{"name": volumeName, "mountPath": mount.MountPath})
		}
		targets := workspaceApplicationSecretTargets(input)
		if len(targets) > 0 {
			names := make([]string, 0, len(targets))
			for name := range targets {
				names = append(names, name)
			}
			sort.Strings(names)
			volumes = append(volumes, map[string]any{"name": "application-secrets", "secret": map[string]any{"secretName": workspaceApplicationComponentResourceName(input, "secrets"), "defaultMode": 0440}})
			for _, name := range names {
				volumeMounts = append(volumeMounts, map[string]any{"name": "application-secrets", "mountPath": targets[name], "subPath": name, "readOnly": true})
			}
		}
		if len(input.Revision.PersistentMounts) > 0 {
			volumes = append(volumes, map[string]any{"name": "workspace-data", "persistentVolumeClaim": map[string]any{"claimName": pvcName}})
		}
	}
	var readinessProbe any
	if component.Role == contracts.WorkspaceApplicationComponentMain && len(input.Revision.HealthChecks) > 0 {
		check := input.Revision.HealthChecks[0]
		readinessProbe = map[string]any{"httpGet": map[string]any{"path": check.Path, "port": check.Port}, "initialDelaySeconds": check.InitialDelaySeconds, "periodSeconds": 10}
	}
	container := map[string]any{"name": "app", "image": component.Image, "imagePullPolicy": "IfNotPresent"}
	if component.Role == contracts.WorkspaceApplicationComponentMain && len(input.Configuration.Environment) > 0 {
		container["env"] = workspaceApplicationEnvironment(input)
	}
	if component.Role == contracts.WorkspaceApplicationComponentMain && len(input.Revision.Entrypoint) > 0 {
		container["command"] = input.Revision.Entrypoint
	}
	if component.Role == contracts.WorkspaceApplicationComponentDependency {
		dependency, depErr := dependencySpecByName(input.Revision, component.Name)
		if depErr != nil {
			return map[string]any{}
		}
		if len(dependency.Command.Entrypoint) > 0 {
			container["command"] = dependency.Command.Entrypoint
		}
		if len(dependency.Command.Args) > 0 {
			container["args"] = dependency.Command.Args
		}
		if len(dependency.Command.Env) > 0 {
			names := make([]string, 0, len(dependency.Command.Env))
			for name := range dependency.Command.Env {
				names = append(names, name)
			}
			sort.Strings(names)
			env := []any{}
			for _, name := range names {
				env = append(env, map[string]any{"name": name, "value": dependency.Command.Env[name]})
			}
			container["env"] = env
		}
		for _, mount := range dependency.PersistentMounts {
			volumeMounts = append(volumeMounts, map[string]any{"name": "workspace-data", "mountPath": mount.MountPath, "subPath": applicationPersistentSubPath(input, contracts.WorkspaceApplicationMount{Name: mount.Name, MountPath: mount.MountPath, ReadOnly: mount.ReadOnly}), "readOnly": mount.ReadOnly})
		}
		for index, mount := range dependency.ScratchMounts {
			volumeName := fmt.Sprintf("dependency-scratch-%d", index)
			volumes = append(volumes, map[string]any{"name": volumeName, "emptyDir": map[string]any{"medium": "Memory"}})
			volumeMounts = append(volumeMounts, map[string]any{"name": volumeName, "mountPath": mount.MountPath})
		}
		if len(dependency.HealthChecks) > 0 {
			check := dependency.HealthChecks[0]
			if check.Type == "http" {
				readinessProbe = map[string]any{"httpGet": map[string]any{"path": check.Path, "port": check.Port}, "initialDelaySeconds": check.InitialDelaySeconds, "periodSeconds": 10}
			} else {
				readinessProbe = map[string]any{"tcpSocket": map[string]any{"port": check.Port}, "initialDelaySeconds": check.InitialDelaySeconds, "periodSeconds": 10}
			}
		}
	}
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
		"name": workspaceApplicationComponentResourceName(input, component.Name), "labels": labels, "annotations": mergeStringMaps(tags, map[string]string{"oplcloud.cn/configuration-digest": input.ConfigurationDigest}),
	}, "spec": map[string]any{"replicas": 1, "strategy": map[string]any{"type": "Recreate"}, "selector": map[string]any{"matchLabels": selector}, "template": map[string]any{
		"metadata": map[string]any{"labels": labels}, "spec": map[string]any{
			"automountServiceAccountToken": false, "dnsPolicy": "ClusterFirst",
			"securityContext":  map[string]any{"runAsNonRoot": true, "runAsUser": 10001, "runAsGroup": 10001, "fsGroup": 10001, "fsGroupChangePolicy": tencentWorkspaceFSGroupPolicy, "seccompProfile": map[string]any{"type": "RuntimeDefault"}},
			"imagePullSecrets": []any{map[string]any{"name": os.Getenv("OPL_IMAGE_PULL_SECRET_NAME")}},
			"nodeSelector":     map[string]any{"kubernetes.io/hostname": compute.NodeName},
			"tolerations":      workspaceNodeTolerations(compute.PackageID),
			"containers":       []any{container},
		},
	}}}
	if len(volumes) > 0 {
		deployment["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["volumes"] = volumes
	}
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
	if component.Role == contracts.WorkspaceApplicationComponentMain && len(input.Revision.Ports) > 0 {
		ports := []any{}
		for _, port := range input.Revision.Ports {
			ports = append(ports, map[string]any{"name": port.Name, "port": port.Port, "targetPort": port.Name, "protocol": port.Protocol})
		}
		service["spec"].(map[string]any)["ports"] = ports
	} else {
		// Components without declared service ports retain DNS discovery through
		// a headless Service; the adapter does not infer a dependency's ports.
		service["spec"].(map[string]any)["clusterIP"] = "None"
	}
	return service
}

func workspaceApplicationNetworkPolicy(input WorkspaceApplicationRuntimeInput, tags map[string]string) map[string]any {
	workspaceSelector := map[string]any{"matchLabels": map[string]any{
		"oplcloud.cn/workspace-id": k8sCostLabelValue(input.WorkspaceID),
		"oplcloud.cn/runtime-id":   k8sCostLabelValue(applicationRuntimeID(input)),
	}}
	ports := []any{}
	for _, port := range input.Revision.Ports {
		ports = append(ports, map[string]any{"protocol": port.Protocol, "port": port.Port})
	}
	ingress := []any{map[string]any{"from": []any{map[string]any{"podSelector": map[string]any{"matchLabels": map[string]any{"app.kubernetes.io/name": "opl-cloud", "app.kubernetes.io/component": "control-plane"}}}}}}
	if len(ports) > 0 {
		ingress[0].(map[string]any)["ports"] = ports
	}
	ingress = append(ingress, map[string]any{"from": []any{map[string]any{"podSelector": workspaceSelector}}})
	egress := append(workspaceEgressRules(), map[string]any{"to": []any{map[string]any{"podSelector": workspaceSelector}}})
	return map[string]any{"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy", "metadata": map[string]any{
		"name": workspaceApplicationComponentResourceName(input, "network"), "annotations": tags, "labels": mergeStringMaps(k8sCostLabels(tags), map[string]string{"oplcloud.cn/runtime-id": k8sCostLabelValue(applicationRuntimeID(input))}),
	}, "spec": map[string]any{"podSelector": workspaceSelector, "policyTypes": []any{"Ingress", "Egress"}, "ingress": ingress, "egress": egress}}
}

func workspaceApplicationPublicEntryPolicy(input WorkspaceApplicationRuntimeInput, tags map[string]string) map[string]any {
	port, hasEntry := contracts.WorkspaceApplicationEntryPort(input.Revision)
	if input.Revision.ExposurePolicy == "cloud_private" || !hasEntry {
		return nil
	}
	return map[string]any{"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy", "metadata": map[string]any{
		"name": workspaceApplicationComponentResourceName(input, "entry-network"), "annotations": tags, "labels": mergeStringMaps(k8sCostLabels(tags), map[string]string{"oplcloud.cn/runtime-id": k8sCostLabelValue(applicationRuntimeID(input))}),
	}, "spec": map[string]any{
		"podSelector": map[string]any{"matchLabels": workspaceApplicationComponentSelector(input, contracts.WorkspaceApplicationComponentMain)},
		"policyTypes": []any{"Ingress"},
		"ingress":     []any{map[string]any{"ports": []any{map[string]any{"protocol": "TCP", "port": port.Port}}}},
	}}
}

// workspaceApplicationIngressHost derives the dedicated subdomain origin of
// one application deployment. Apps keep their own root path and cookies, so
// they never share the workspace domain's cookie scope; wildcard DNS and
// certificate coverage for this subdomain are installation prerequisites.
func workspaceApplicationIngressHost(input WorkspaceApplicationRuntimeInput) string {
	if input.SchemaVersion == 0 {
		return fmt.Sprintf("%s.%s", k8sName(input.ComputeID+"-"+input.Revision.ApplicationID), workspaceDomain())
	}
	return fmt.Sprintf("%s.%s", workspaceApplicationComponentResourceName(input, "origin"), workspaceDomain())
}

// workspaceApplicationIngress renders the public entry of one application
// deployment. cloud-private applications and applications without declared
// web ports stay cluster-internal: no Ingress is created for them.
func workspaceApplicationIngress(input WorkspaceApplicationRuntimeInput, compute ComputeAllocation, tags map[string]string) map[string]any {
	port, hasEntry := contracts.WorkspaceApplicationEntryPort(input.Revision)
	if input.Revision.ExposurePolicy == "cloud_private" || !hasEntry {
		return nil
	}
	runtimeID := applicationRuntimeID(input)
	labels := mergeStringMaps(map[string]string{
		"oplcloud.cn/account-id":            compute.AccountID,
		"oplcloud.cn/workspace-id":          k8sCostLabelValue(input.WorkspaceID),
		"oplcloud.cn/runtime-id":            k8sCostLabelValue(runtimeID),
		"oplcloud.cn/compute-allocation-id": k8sCostLabelValue(compute.ID),
		"app.kubernetes.io/name":            "opl-workspace-application",
		"app.kubernetes.io/instance":        workspaceApplicationComponentResourceName(input, contracts.WorkspaceApplicationComponentMain),
	}, k8sCostLabels(tags))
	paths := []any{map[string]any{
		"path": "/", "pathType": "Prefix",
		"backend": map[string]any{"service": map[string]any{"name": workspaceApplicationComponentResourceName(input, contracts.WorkspaceApplicationComponentMain), "port": map[string]any{"number": port.Port}}},
	}}
	spec := map[string]any{"rules": []any{map[string]any{
		"host": workspaceApplicationIngressHost(input), "http": map[string]any{"paths": paths},
	}}}
	if class := os.Getenv("OPL_INGRESS_CLASS"); class != "" {
		spec["ingressClassName"] = class
	}
	return map[string]any{"apiVersion": "networking.k8s.io/v1", "kind": "Ingress", "metadata": map[string]any{
		"name": workspaceApplicationComponentResourceName(input, "entry"), "labels": labels, "annotations": tags,
	}, "spec": spec}
}
