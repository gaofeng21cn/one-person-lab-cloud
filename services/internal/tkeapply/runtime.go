package tkeapply

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	applyappsv1 "k8s.io/client-go/applyconfigurations/apps/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	applymetav1 "k8s.io/client-go/applyconfigurations/meta/v1"
	applynetworkingv1 "k8s.io/client-go/applyconfigurations/networking/v1"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/internal/protectedresource"
)

// EnsureWorkspaceApplicationRuntime applies one typed Server-Side Apply bundle
// per admitted revision on the workspace's TKE namespace: a Deployment and
// Service per declared component (main plus dependencies), with the main
// component's declared mounts backed by the workspace CBS PVC, and
// NetworkPolicies scoping the application. Apply is idempotent by resource
// name; image drift against a live deployment is a conflict. Not-yet-available
// deployments return the observation with ErrWorkspaceLaunchPending so the
// engine claim stays started and the next replay resolves by readback.
func (p *Executor) EnsureWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput, placement Placement) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	if err := validateWorkspaceApplicationConfiguration(input); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if err := p.applyConfigError(); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	// The workload is scheduled onto the placement the resources owner confirmed:
	// the exact node, the prepaid package that owns it and the prepaid storage
	// claim. A placement without them cannot be executed at all, so it is refused as
	// unconfirmed before any other comparison, and the application is never applied
	// to a guessed target.
	if placement.ComputeNodeName == "" || placement.ComputePackageID == "" || placement.StoragePVCName == "" {
		return contracts.WorkspaceApplicationRuntimeObservation{}, ErrPlacementUnconfirmed
	}
	// A confirmed placement that does not name this delivery's own compute and
	// prepaid storage describes another workload, which this executor may not touch.
	if placement.ComputeID == "" || placement.StorageVolumeID == "" || input.WorkspaceID == "" || input.WorkspaceID != placement.WorkspaceID {
		return contracts.WorkspaceApplicationRuntimeObservation{}, fmt.Errorf("workspace_application_runtime_resource_mismatch")
	}
	if err := contracts.ValidateWorkspaceApplicationRevision(input.Revision); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if err := ValidateCapabilities(input.Revision); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	existing, err := p.readWorkspaceApplicationResources(ctx, input)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if existing.deployments[ComponentResourceName(input, "main")] == nil {
		existing.imageArgs, err = p.mainImageArgs(ctx, input)
		if err != nil {
			return contracts.WorkspaceApplicationRuntimeObservation{}, err
		}
	}
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(input.Revision) {
		if deployment, ok := existing.deployments[ComponentResourceName(input, component.Name)]; ok &&
			typedDeploymentImage(deployment) != component.Image {
			return contracts.WorkspaceApplicationRuntimeObservation{}, fmt.Errorf("tencent_application_component_conflict")
		}
	}
	if err := p.prepareApplicationSecrets(ctx, input); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if err := p.prepareApplicationConfigFiles(ctx, input); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	admitted := map[string]bool{}
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(input.Revision) {
		_, exists := existing.deployments[ComponentResourceName(input, component.Name)]
		admitted[component.Name] = exists || workspaceApplicationDependenciesReady(input, component.Name, existing)
	}
	bundle := BuildApplyBundle(input, placement, p.Installation, admitted, existing.imageArgs)
	if err := p.applyBundle(ctx, bundle, protectedresource.Target{
		PackageID: placement.ComputePackageID, NodePoolID: placement.ComputeNodePoolID, MachineID: placement.ComputeMachineName, NodeName: placement.ComputeNodeName,
		CVMID: placement.ComputeInstanceID,
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
func (p *Executor) ReadWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	if err := p.clusterAccessError(); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	live, err := p.readWorkspaceApplicationResources(ctx, input)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	return workspaceApplicationObservation(input, live), nil
}

type workspaceApplicationKubernetesResources struct {
	deployments     map[string]*appsv1.Deployment
	replicaSets     map[types.UID]*appsv1.ReplicaSet
	services        map[string]*corev1.Service
	pods            []*corev1.Pod
	networkPolicies map[string]*networkingv1.NetworkPolicy
	secrets         map[string]*corev1.Secret
	configMaps      map[string]*corev1.ConfigMap
	storagePVC      string
	imageArgs       []string
}

func (r workspaceApplicationKubernetesResources) auxiliaryCount() int {
	return len(r.networkPolicies) + len(r.secrets) + len(r.configMaps)
}

func (p *Executor) readWorkspaceApplicationResources(ctx context.Context, input WorkspaceApplicationRuntimeInput) (workspaceApplicationKubernetesResources, error) {
	resources := workspaceApplicationKubernetesResources{
		deployments:     map[string]*appsv1.Deployment{},
		replicaSets:     map[types.UID]*appsv1.ReplicaSet{},
		services:        map[string]*corev1.Service{},
		networkPolicies: map[string]*networkingv1.NetworkPolicy{},
		secrets:         map[string]*corev1.Secret{},
		configMaps:      map[string]*corev1.ConfigMap{},
	}
	selector := "oplcloud.cn/workspace-id=" + k8sCostLabelValue(input.WorkspaceID) +
		",oplcloud.cn/runtime-id=" + k8sCostLabelValue(applicationRuntimeID(input))
	listed, err := p.listApplicationResources(ctx, selector)
	if err != nil {
		return resources, err
	}
	seen := map[types.UID]bool{}
	wantAccount := k8sCostLabelValue(input.AccountID)
	wantWorkspace := k8sCostLabelValue(input.WorkspaceID)
	wantRuntime := k8sCostLabelValue(applicationRuntimeID(input))
	validateMeta := func(name string, uid types.UID, labels map[string]string) bool {
		if name == "" || uid == "" || seen[uid] || input.AccountID == "" ||
			labels["oplcloud.cn/account-id"] != wantAccount ||
			labels["oplcloud.cn/workspace-id"] != wantWorkspace ||
			labels["oplcloud.cn/runtime-id"] != wantRuntime {
			return false
		}
		seen[uid] = true
		return true
	}
	for i := range listed.Deployments {
		item := listed.Deployments[i].DeepCopy()
		if !validateMeta(item.Name, item.UID, item.Labels) {
			return resources, fmt.Errorf("tencent_application_runtime_readback_invalid")
		}
		resources.deployments[item.Name] = item
	}
	for i := range listed.ReplicaSets {
		item := listed.ReplicaSets[i].DeepCopy()
		if !validateMeta(item.Name, item.UID, item.Labels) {
			return resources, fmt.Errorf("tencent_application_runtime_readback_invalid")
		}
		resources.replicaSets[item.UID] = item
	}
	for i := range listed.Pods {
		item := listed.Pods[i].DeepCopy()
		if !validateMeta(item.Name, item.UID, item.Labels) {
			return resources, fmt.Errorf("tencent_application_runtime_readback_invalid")
		}
		resources.pods = append(resources.pods, item)
	}
	for i := range listed.Services {
		item := listed.Services[i].DeepCopy()
		if !validateMeta(item.Name, item.UID, item.Labels) {
			return resources, fmt.Errorf("tencent_application_runtime_readback_invalid")
		}
		resources.services[item.Name] = item
	}
	for i := range listed.NetworkPolicies {
		item := listed.NetworkPolicies[i].DeepCopy()
		if !validateMeta(item.Name, item.UID, item.Labels) {
			return resources, fmt.Errorf("tencent_application_runtime_readback_invalid")
		}
		resources.networkPolicies[item.Name] = item
	}
	for i := range listed.Secrets {
		item := listed.Secrets[i].DeepCopy()
		if !validateMeta(item.Name, item.UID, item.Labels) {
			return resources, fmt.Errorf("tencent_application_runtime_readback_invalid")
		}
		resources.secrets[item.Name] = item
	}
	for i := range listed.ConfigMaps {
		item := listed.ConfigMaps[i].DeepCopy()
		if !validateMeta(item.Name, item.UID, item.Labels) {
			return resources, fmt.Errorf("tencent_application_runtime_readback_invalid")
		}
		resources.configMaps[item.Name] = item
	}
	if resources.deployments[ComponentResourceName(input, "main")] != nil {
		resources.imageArgs, err = p.mainImageArgs(ctx, input)
		if err != nil {
			return resources, err
		}
	}
	if len(input.Configuration.Files) > 0 {
		config := resources.configMaps[ComponentResourceName(input, "config")]
		if (config != nil && !verifyTypedConfigMap(input, config)) || (config == nil && len(resources.deployments) > 0) {
			return resources, ErrLaunchStageBindingConflict
		}
	}
	if input.SchemaVersion == 0 {
		// Original application policies have owner annotations but no selector
		// labels. Read the exact original names so absence cannot omit them.
		for _, component := range []string{"network", "entry-network"} {
			name := ComponentResourceName(input, component)
			policy, err := p.getNetworkPolicy(ctx, name)
			if err != nil {
				return resources, err
			}
			if policy == nil {
				continue
			}
			if (policy.Kind != "" && policy.Kind != "NetworkPolicy") ||
				policy.Name != name ||
				policy.Annotations["opl_account_id"] != input.AccountID ||
				policy.Annotations["opl_workspace_id"] != input.WorkspaceID ||
				policy.Annotations["opl_resource_id"] != applicationRuntimeID(input) {
				return resources, ErrLaunchStageBindingConflict
			}
			resources.networkPolicies[name] = policy
		}
	}
	requiresPVC := len(input.Revision.PersistentMounts) > 0
	for _, dependency := range input.Revision.Dependencies {
		requiresPVC = requiresPVC || len(dependency.PersistentMounts) > 0
	}
	if input.SchemaVersion == 2 && requiresPVC && len(resources.deployments) > 0 {
		// Resolve the current storage owner's PVC. Its provider name is not
		// derivable from a logical StorageID and must not be guessed.
		pvcs, err := p.listPVCs(ctx, "oplcloud.cn/storage-id="+k8sCostLabelValue(input.VolumeID))
		if err != nil {
			return resources, err
		}
		if len(pvcs) != 1 {
			return resources, ErrLaunchStageBindingConflict
		}
		pvc := pvcs[0]
		if (pvc.Kind != "" && pvc.Kind != "PersistentVolumeClaim") ||
			pvc.Annotations["opl_account_id"] != input.AccountID ||
			pvc.Annotations["opl_workspace_id"] != input.WorkspaceID ||
			pvc.Annotations["opl_resource_id"] != input.VolumeID ||
			pvc.Labels["oplcloud.cn/storage-id"] != k8sCostLabelValue(input.VolumeID) ||
			pvc.Name == "" {
			return resources, ErrLaunchStageBindingConflict
		}
		resources.storagePVC = pvc.Name
	}
	return resources, nil
}

func workspaceApplicationObservation(input WorkspaceApplicationRuntimeInput, resources workspaceApplicationKubernetesResources) contracts.WorkspaceApplicationRuntimeObservation {
	observed := make([]contracts.WorkspaceApplicationRuntimeComponentState, 0, len(input.Revision.Dependencies)+1)
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(input.Revision) {
		state, lastError := "absent", ""
		if deployment, found := resources.deployments[ComponentResourceName(input, component.Name)]; found {
			state, lastError = workspaceApplicationComponentStatus(input, component, deployment, resources)
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
	// The installation serves applications through its own gateway, so the
	// provider reports the destination that gateway proxies to and never a
	// user-facing URL of its own.
	if observation.Status == "ready" {
		if entryPort, published := contracts.WorkspaceApplicationEntryPort(input.Revision); published && input.Revision.ExposurePolicy != "cloud_private" {
			observation.Entry = &contracts.WorkspaceApplicationEntry{
				ServiceName: ComponentResourceName(input, contracts.WorkspaceApplicationComponentMain),
				Port:        entryPort.Port,
			}
		}
	}
	return observation
}

func workspaceApplicationComponentStatus(input WorkspaceApplicationRuntimeInput, component contracts.WorkspaceApplicationRuntimeComponentState, deployment *appsv1.Deployment, resources workspaceApplicationKubernetesResources) (string, string) {
	if component.Role == contracts.WorkspaceApplicationComponentMain && len(input.Revision.HealthChecks) > 1 {
		return "failed", "tencent_application_health_checks_unsupported"
	}
	if !verifyTypedConfiguration(input, component, deployment, resources.storagePVC, resources.imageArgs) {
		return "failed", "tencent_application_configuration_mismatch"
	}
	var firstContainer *corev1.Container
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		firstContainer = &deployment.Spec.Template.Spec.Containers[0]
	}
	if typedDeploymentImage(deployment) != component.Image ||
		firstContainer == nil || firstContainer.Name != "app" ||
		deployment.Labels["oplcloud.cn/runtime-operation-id"] != k8sCostLabelValue(input.RuntimeOperationID) {
		return "failed", "tencent_application_component_identity_mismatch"
	}
	if component.Role == contracts.WorkspaceApplicationComponentMain && len(input.Revision.HealthChecks) > 0 {
		check := input.Revision.HealthChecks[0]
		probe := firstContainer.ReadinessProbe
		if probe == nil || probe.HTTPGet == nil || probe.HTTPGet.Path != check.Path || probe.HTTPGet.Port.IntValue() != check.Port {
			return "failed", "tencent_application_health_check_mismatch"
		}
	}
	if deployment.DeletionTimestamp != nil || deployment.Generation <= 0 ||
		deployment.Status.ObservedGeneration < deployment.Generation ||
		deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 ||
		deployment.Status.UpdatedReplicas != 1 ||
		deployment.Status.ReadyReplicas != 1 ||
		deployment.Status.AvailableReplicas != 1 {
		return "pending", ""
	}
	ready := 0
	for _, pod := range resources.pods {
		replicaSet := resources.replicaSets[typedControllerUID(pod.OwnerReferences, "ReplicaSet")]
		if replicaSet == nil ||
			typedControllerUID(replicaSet.OwnerReferences, "Deployment") != deployment.UID ||
			!typedRuntimeTemplateMatches(deployment, replicaSet) ||
			replicaSet.DeletionTimestamp != nil ||
			pod.DeletionTimestamp != nil {
			continue
		}
		if pod.Status.Phase == corev1.PodFailed {
			return "failed", "tencent_application_pod_failed"
		}
		if !typedPodReady(pod) {
			return "pending", ""
		}
		if len(pod.Spec.Containers) != 1 ||
			pod.Spec.Containers[0].Name != "app" ||
			pod.Spec.Containers[0].Image != component.Image ||
			!typedPodImageIDsMatch([]*corev1.Pod{pod}, "oplcloud.cn/workspace-id", k8sCostLabelValue(input.WorkspaceID), "app", component.Image) {
			return "failed", "tencent_application_component_image_unverified"
		}
		if input.SchemaVersion != 0 && !verifyTypedApplicationExecution(input, component, pod.Spec.SecurityContext) {
			return "failed", "tencent_application_execution_mismatch"
		}
		ready++
	}
	if ready != 1 {
		return "pending", ""
	}
	return "ready", ""
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

func ComponentResourceName(input WorkspaceApplicationRuntimeInput, componentName string) string {
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

func typedDeploymentImage(deployment *appsv1.Deployment) string {
	if deployment == nil || len(deployment.Spec.Template.Spec.Containers) == 0 {
		return ""
	}
	return deployment.Spec.Template.Spec.Containers[0].Image
}

// DeploymentImage extracts the primary container image from a serialized
// Deployment object map.
func DeploymentImage(deployment map[string]any) string {
	var typed appsv1.Deployment
	if json.Unmarshal(mustJSON(deployment), &typed) != nil {
		return ""
	}
	return typedDeploymentImage(&typed)
}

// BuildApplyBundle constructs the typed Server-Side Apply configurations for
// every admitted component plus the scoping NetworkPolicy.
func BuildApplyBundle(input WorkspaceApplicationRuntimeInput, placement Placement, installation Installation, admitted map[string]bool, imageArgs ...[]string) *ApplyBundle {
	components := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)
	tags := oplCostTags(placement.AccountID, input.WorkspaceID, applicationRuntimeID(input), input.RuntimeOperationID)
	pvcName := placement.StoragePVCName
	bundle := &ApplyBundle{
		Components: make([]ComponentApplyItem, 0, len(components)),
	}
	for _, component := range components {
		if admitted != nil && !admitted[component.Name] {
			continue
		}
		bundle.Components = append(bundle.Components, ComponentApplyItem{
			Service:    BuildComponentService(input, placement, component, tags),
			Deployment: BuildComponentDeployment(input, placement, installation, component, tags, pvcName, imageArgs...),
		})
	}
	bundle.NetworkPolicies = append(bundle.NetworkPolicies, BuildNetworkPolicy(input, installation, tags))
	return bundle
}

// Manifest renders one List document: a Deployment and a
// Service per declared component plus NetworkPolicies scoping the application.
// The main component's persistent mounts use the workspace CBS PVC and its
// scratch mounts are memory-backed emptyDirs. Dependencies do not inherit them.
func Manifest(input WorkspaceApplicationRuntimeInput, placement Placement, installation Installation) []byte {
	return ComponentManifest(input, placement, installation, nil)
}

// ComponentManifest renders the admitted components' Server-Side Apply bundle
// as a v1 List JSON document. A replay admits only components whose declared
// predecessors are ready.
func ComponentManifest(input WorkspaceApplicationRuntimeInput, placement Placement, installation Installation, admitted map[string]bool, imageArgs ...[]string) []byte {
	return BuildApplyBundle(input, placement, installation, admitted, imageArgs...).ListJSON()
}

func workspaceApplicationDependenciesReady(input WorkspaceApplicationRuntimeInput, name string, resources workspaceApplicationKubernetesResources) bool {
	for _, dependencyName := range contracts.WorkspaceApplicationComponentDependencies(input.Revision, name) {
		deployment := resources.deployments[ComponentResourceName(input, dependencyName)]
		if deployment == nil {
			return false
		}
		dependency, err := dependencySpecByName(input.Revision, dependencyName)
		if err != nil {
			return false
		}
		component := contracts.WorkspaceApplicationRuntimeComponentState{Name: dependencyName, Role: contracts.WorkspaceApplicationComponentDependency, Image: dependency.Image}
		if state, _ := workspaceApplicationComponentStatus(input, component, deployment, resources); state != "ready" {
			return false
		}
	}
	return true
}

func workspaceApplicationIdentityLabels(input WorkspaceApplicationRuntimeInput, placement Placement, component contracts.WorkspaceApplicationRuntimeComponentState, tags map[string]string) map[string]string {
	return mergeStringMaps(map[string]string{
		"oplcloud.cn/account-id":            placement.AccountID,
		"oplcloud.cn/workspace-id":          k8sCostLabelValue(input.WorkspaceID),
		"oplcloud.cn/compute-allocation-id": k8sCostLabelValue(placement.ComputeID),
		"oplcloud.cn/storage-id":            k8sCostLabelValue(placement.StorageVolumeID),
		"oplcloud.cn/runtime-id":            k8sCostLabelValue(applicationRuntimeID(input)),
		"oplcloud.cn/runtime-operation-id":  k8sCostLabelValue(input.RuntimeOperationID),
		"oplcloud.cn/component-name":        k8sCostLabelValue(component.Name),
		"oplcloud.cn/component-role":        k8sCostLabelValue(component.Role),
		"app.kubernetes.io/name":            "opl-workspace-application",
		"app.kubernetes.io/instance":        ComponentResourceName(input, component.Name),
	}, k8sCostLabels(tags))
}

func workspaceApplicationComponentSelector(input WorkspaceApplicationRuntimeInput, componentName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":            "opl-workspace-application",
		"app.kubernetes.io/instance":        ComponentResourceName(input, componentName),
		"oplcloud.cn/compute-allocation-id": k8sCostLabelValue(input.ComputeID),
	}
}

// BuildComponentDeployment constructs the strongly-typed Server-Side Apply
// configuration for one component Deployment.
func BuildComponentDeployment(
	input WorkspaceApplicationRuntimeInput,
	placement Placement,
	installation Installation,
	component contracts.WorkspaceApplicationRuntimeComponentState,
	tags map[string]string,
	pvcName string,
	imageArgs ...[]string,
) *applyappsv1.DeploymentApplyConfiguration {
	labels := workspaceApplicationIdentityLabels(input, placement, component, tags)
	selector := workspaceApplicationComponentSelector(input, component.Name)
	var ports []*applycorev1.ContainerPortApplyConfiguration
	var volumeMounts []*applycorev1.VolumeMountApplyConfiguration
	var volumes []*applycorev1.VolumeApplyConfiguration
	var readinessProbe *applycorev1.ProbeApplyConfiguration

	container := applycorev1.Container().
		WithName("app").
		WithImage(component.Image).
		WithImagePullPolicy(corev1.PullIfNotPresent)

	if environment := workspaceApplicationComponentEnvironment(input, component); len(environment) > 0 {
		container.WithEnv(envVarsToApply(environment)...)
	}

	if component.Role == contracts.WorkspaceApplicationComponentMain {
		for _, port := range input.Revision.Ports {
			ports = append(ports, applycorev1.ContainerPort().
				WithName(port.Name).
				WithContainerPort(int32(port.Port)).
				WithProtocol(corev1.Protocol(port.Protocol)))
		}
		for _, mount := range input.Revision.PersistentMounts {
			volumeMounts = append(volumeMounts, applycorev1.VolumeMount().
				WithName("workspace-data").
				WithMountPath(mount.MountPath).
				WithSubPath(applicationPersistentSubPath(input, mount)).
				WithReadOnly(mount.ReadOnly))
		}
		for index, mount := range input.Revision.ScratchMounts {
			volumeName := fmt.Sprintf("scratch-%d", index)
			volumes = append(volumes, applycorev1.Volume().
				WithName(volumeName).
				WithEmptyDir(workspaceApplicationScratchVolumeApply(mount)))
			volumeMounts = append(volumeMounts, applycorev1.VolumeMount().
				WithName(volumeName).
				WithMountPath(mount.MountPath))
		}
		if len(input.Revision.PersistentMounts) > 0 {
			volumes = append(volumes, applycorev1.Volume().
				WithName("workspace-data").
				WithPersistentVolumeClaim(applycorev1.PersistentVolumeClaimVolumeSource().WithClaimName(pvcName)))
		}
		if len(input.Revision.HealthChecks) > 0 {
			check := input.Revision.HealthChecks[0]
			readinessProbe = applycorev1.Probe().
				WithHTTPGet(applycorev1.HTTPGetAction().
					WithPath(check.Path).
					WithPort(intstr.FromInt32(int32(check.Port)))).
				WithInitialDelaySeconds(int32(check.InitialDelaySeconds)).
				WithPeriodSeconds(10)
		}
		if len(input.Revision.Entrypoint) > 0 {
			container.WithCommand(input.Revision.Entrypoint...)
			if len(imageArgs) > 0 && len(imageArgs[0]) > 0 {
				container.WithArgs(imageArgs[0]...)
			}
		}
	}

	if component.Role == contracts.WorkspaceApplicationComponentDependency {
		dependency, depErr := dependencySpecByName(input.Revision, component.Name)
		if depErr != nil {
			return nil
		}
		for _, port := range dependency.Ports {
			ports = append(ports, applycorev1.ContainerPort().
				WithName(port.Name).
				WithContainerPort(int32(port.Port)).
				WithProtocol(corev1.Protocol(port.Protocol)))
		}
		if len(dependency.Command.Entrypoint) > 0 {
			container.WithCommand(dependency.Command.Entrypoint...)
		}
		if len(dependency.Command.Args) > 0 {
			container.WithArgs(dependency.Command.Args...)
		}
		for _, mount := range dependency.PersistentMounts {
			volumeMounts = append(volumeMounts, applycorev1.VolumeMount().
				WithName("workspace-data").
				WithMountPath(mount.MountPath).
				WithSubPath(applicationPersistentSubPath(input, contracts.WorkspaceApplicationMount{Name: mount.Name, MountPath: mount.MountPath, ReadOnly: mount.ReadOnly})).
				WithReadOnly(mount.ReadOnly))
		}
		if len(dependency.PersistentMounts) > 0 {
			volumes = append(volumes, applycorev1.Volume().
				WithName("workspace-data").
				WithPersistentVolumeClaim(applycorev1.PersistentVolumeClaimVolumeSource().WithClaimName(pvcName)))
		}
		for index, mount := range dependency.ScratchMounts {
			volumeName := fmt.Sprintf("dependency-scratch-%d", index)
			volumes = append(volumes, applycorev1.Volume().
				WithName(volumeName).
				WithEmptyDir(workspaceApplicationScratchVolumeApply(mount)))
			volumeMounts = append(volumeMounts, applycorev1.VolumeMount().
				WithName(volumeName).
				WithMountPath(mount.MountPath))
		}
		if len(dependency.HealthChecks) > 0 {
			check := dependency.HealthChecks[0]
			probe := applycorev1.Probe().
				WithInitialDelaySeconds(int32(check.InitialDelaySeconds)).
				WithPeriodSeconds(10)
			switch check.Type {
			case "http":
				probe.WithHTTPGet(applycorev1.HTTPGetAction().
					WithPath(check.Path).
					WithPort(intstr.FromInt32(int32(check.Port))))
			case "exec":
				probe.WithExec(applycorev1.ExecAction().WithCommand(check.Command...))
			default:
				probe.WithTCPSocket(applycorev1.TCPSocketAction().WithPort(intstr.FromInt32(int32(check.Port))))
			}
			readinessProbe = probe
		}
	}

	targets := workspaceApplicationComponentSecretTargets(input, component)
	if len(targets) > 0 {
		names := make([]string, 0, len(targets))
		for name := range targets {
			names = append(names, name)
		}
		sort.Strings(names)
		volumes = append(volumes, applycorev1.Volume().
			WithName("application-secrets").
			WithSecret(applycorev1.SecretVolumeSource().
				WithSecretName(ComponentResourceName(input, "secrets")).
				WithDefaultMode(0440)))
		for _, name := range names {
			volumeMounts = append(volumeMounts, applycorev1.VolumeMount().
				WithName("application-secrets").
				WithMountPath(targets[name]).
				WithSubPath(name).
				WithReadOnly(true))
		}
	}

	configInputs := workspaceApplicationComponentConfigInputs(input, component)
	if len(configInputs) > 0 {
		volumes = append(volumes, applycorev1.Volume().
			WithName("application-config").
			WithConfigMap(applycorev1.ConfigMapVolumeSource().
				WithName(ComponentResourceName(input, "config")).
				WithDefaultMode(0444)))
		for _, config := range configInputs {
			volumeMounts = append(volumeMounts, applycorev1.VolumeMount().
				WithName("application-config").
				WithMountPath(config.Target).
				WithSubPath(config.Name).
				WithReadOnly(true))
		}
	}

	// The declared envelope becomes the container's requests and limits. An
	// application that declares nothing keeps the previous BestEffort shape
	// rather than receiving invented bounds.
	envelope := input.Revision.Compute
	if component.Role == contracts.WorkspaceApplicationComponentDependency {
		if dependency, depErr := dependencySpecByName(input.Revision, component.Name); depErr == nil {
			envelope = dependency.Compute
		}
	}
	if resources := workspaceApplicationComponentResourcesApply(envelope); resources != nil {
		container.WithResources(resources)
	}
	if len(ports) > 0 {
		container.WithPorts(ports...)
	}
	if readinessProbe != nil {
		container.WithReadinessProbe(readinessProbe)
	}
	if len(volumeMounts) > 0 {
		container.WithVolumeMounts(volumeMounts...)
	}

	podSpec := applycorev1.PodSpec().
		WithAutomountServiceAccountToken(false).
		WithDNSPolicy(corev1.DNSClusterFirst).
		WithSecurityContext(buildSecurityContextApply(input, component)).
		WithImagePullSecrets(applycorev1.LocalObjectReference().WithName(installation.ImagePullSecretName)).
		WithNodeSelector(map[string]string{"kubernetes.io/hostname": placement.ComputeNodeName}).
		WithTolerations(workspaceNodeTolerations(placement.ComputePackageID)...).
		WithContainers(container)
	if len(volumes) > 0 {
		podSpec.WithVolumes(volumes...)
	}

	return applyappsv1.Deployment(ComponentResourceName(input, component.Name), "").
		WithLabels(labels).
		WithAnnotations(mergeStringMaps(tags, map[string]string{"oplcloud.cn/configuration-digest": input.ConfigurationDigest})).
		WithSpec(applyappsv1.DeploymentSpec().
			WithReplicas(1).
			WithStrategy(applyappsv1.DeploymentStrategy().WithType(appsv1.RecreateDeploymentStrategyType)).
			WithSelector(applymetav1.LabelSelector().WithMatchLabels(selector)).
			WithTemplate(applycorev1.PodTemplateSpec().
				WithLabels(labels).
				WithSpec(podSpec)))
}

// ComponentDeployment renders one component Deployment as a JSON-compatible map
// for callers inspecting serialized manifests.
func ComponentDeployment(
	input WorkspaceApplicationRuntimeInput,
	placement Placement,
	installation Installation,
	component contracts.WorkspaceApplicationRuntimeComponentState,
	tags map[string]string,
	pvcName string,
	imageArgs ...[]string,
) map[string]any {
	dep := BuildComponentDeployment(input, placement, installation, component, tags, pvcName, imageArgs...)
	if dep == nil {
		return map[string]any{}
	}
	return toMap(dep)
}

// BuildComponentService constructs the strongly-typed Server-Side Apply
// configuration for one component Service.
func BuildComponentService(
	input WorkspaceApplicationRuntimeInput,
	placement Placement,
	component contracts.WorkspaceApplicationRuntimeComponentState,
	tags map[string]string,
) *applycorev1.ServiceApplyConfiguration {
	labels := workspaceApplicationIdentityLabels(input, placement, component, tags)
	spec := applycorev1.ServiceSpec().
		WithType(corev1.ServiceTypeClusterIP).
		WithSelector(workspaceApplicationComponentSelector(input, component.Name))
	if component.Role == contracts.WorkspaceApplicationComponentMain && len(input.Revision.Ports) > 0 {
		ports := make([]*applycorev1.ServicePortApplyConfiguration, 0, len(input.Revision.Ports))
		for _, port := range input.Revision.Ports {
			ports = append(ports, applycorev1.ServicePort().
				WithName(port.Name).
				WithPort(int32(port.Port)).
				WithTargetPort(intstr.FromString(port.Name)).
				WithProtocol(corev1.Protocol(port.Protocol)))
		}
		spec.WithPorts(ports...)
	} else {
		// Components without declared service ports retain DNS discovery through
		// a headless Service; the adapter does not infer a dependency's ports.
		spec.WithClusterIP(corev1.ClusterIPNone)
	}
	return applycorev1.Service(ComponentResourceName(input, component.Name), "").
		WithLabels(labels).
		WithAnnotations(tags).
		WithSpec(spec)
}

// BuildNetworkPolicy constructs the strongly-typed Server-Side Apply
// configuration scoping who may reach the application. Its ingress names
// exactly two peers: the installation-declared pods that serve the Workspace
// application entry (the Serve access data plane, whose labels only the
// installation knows) and the application's own sibling components.
func BuildNetworkPolicy(input WorkspaceApplicationRuntimeInput, installation Installation, tags map[string]string) *applynetworkingv1.NetworkPolicyApplyConfiguration {
	workspaceLabels := map[string]string{
		"oplcloud.cn/workspace-id": k8sCostLabelValue(input.WorkspaceID),
		"oplcloud.cn/runtime-id":   k8sCostLabelValue(applicationRuntimeID(input)),
	}
	workspaceSelector := applymetav1.LabelSelector().WithMatchLabels(workspaceLabels)
	ports := make([]*applynetworkingv1.NetworkPolicyPortApplyConfiguration, 0, len(input.Revision.Ports))
	for _, port := range input.Revision.Ports {
		ports = append(ports, applynetworkingv1.NetworkPolicyPort().
			WithProtocol(corev1.Protocol(port.Protocol)).
			WithPort(intstr.FromInt32(int32(port.Port))))
	}
	ingress := make([]*applynetworkingv1.NetworkPolicyIngressRuleApplyConfiguration, 0, len(installation.ApplicationIngressPeers)+1)
	for _, peer := range installation.ApplicationIngressPeers {
		rule := applynetworkingv1.NetworkPolicyIngressRule().
			WithFrom(applynetworkingv1.NetworkPolicyPeer().
				WithPodSelector(applymetav1.LabelSelector().WithMatchLabels(peer)))
		ingress = append(ingress, rule)
	}
	if len(ports) > 0 && len(ingress) > 0 {
		ingress[0].WithPorts(ports...)
	}
	ingress = append(ingress, applynetworkingv1.NetworkPolicyIngressRule().
		WithFrom(applynetworkingv1.NetworkPolicyPeer().WithPodSelector(workspaceSelector)))
	egress := append(workspaceEgressRules(),
		applynetworkingv1.NetworkPolicyEgressRule().
			WithTo(applynetworkingv1.NetworkPolicyPeer().WithPodSelector(workspaceSelector)))

	return applynetworkingv1.NetworkPolicy(ComponentResourceName(input, "network"), "").
		WithAnnotations(tags).
		WithLabels(mergeStringMaps(k8sCostLabels(tags), map[string]string{"oplcloud.cn/runtime-id": k8sCostLabelValue(applicationRuntimeID(input))})).
		WithSpec(applynetworkingv1.NetworkPolicySpec().
			WithPodSelector(workspaceSelector).
			WithPolicyTypes(networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress).
			WithIngress(ingress...).
			WithEgress(egress...))
}

// NetworkPolicy renders the application NetworkPolicy as a JSON-compatible map
// for callers inspecting serialized manifests.
func NetworkPolicy(input WorkspaceApplicationRuntimeInput, installation Installation, tags map[string]string) map[string]any {
	return toMap(BuildNetworkPolicy(input, installation, tags))
}

// workspaceApplicationComponentResourcesApply translates one declared envelope
// into a container's requests and limits. Scheduling and the target-node
// feasibility check depend on the requests; the limits bound one component so a
// runaway process cannot consume its siblings on the shared workspace node.
func workspaceApplicationComponentResourcesApply(compute contracts.WorkspaceApplicationCompute) *applycorev1.ResourceRequirementsApplyConfiguration {
	requests, limits := corev1.ResourceList{}, corev1.ResourceList{}
	if compute.CPURequestMilli > 0 {
		requests[corev1.ResourceCPU] = resource.MustParse(fmt.Sprintf("%dm", compute.CPURequestMilli))
	}
	if compute.MemoryRequestBytes > 0 {
		requests[corev1.ResourceMemory] = resource.MustParse(strconv.FormatInt(compute.MemoryRequestBytes, 10))
	}
	if compute.CPULimitMilli > 0 {
		limits[corev1.ResourceCPU] = resource.MustParse(fmt.Sprintf("%dm", compute.CPULimitMilli))
	}
	if compute.MemoryLimitBytes > 0 {
		limits[corev1.ResourceMemory] = resource.MustParse(strconv.FormatInt(compute.MemoryLimitBytes, 10))
	}
	if len(requests) == 0 && len(limits) == 0 {
		return nil
	}
	resources := applycorev1.ResourceRequirements()
	if len(requests) > 0 {
		resources.WithRequests(requests)
	}
	if len(limits) > 0 {
		resources.WithLimits(limits)
	}
	return resources
}
