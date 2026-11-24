package fabric

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/internal/protectedresource"
	"opl-cloud/services/internal/tkeapply"
)

// The retained owner keeps its operation journal, credential provenance and
// placement policy. Kubernetes apply/readback/config/lifecycle are shared with
// Serve; this adapter adds no execution algorithm or cross-owner state.
func (p *TencentProvider) applicationExecutor() *tkeapply.Executor {
	return &tkeapply.Executor{
		ImageCommand:    p.imageCommand,
		Client:          tencentKubectlClusterAdapter{provider: p},
		Guard:           p.guard,
		CredentialToken: tencentApplicationCredentialToken,
		Installation:    retainedApplicationInstallation(p.namespace),
	}
}

type tencentKubectlClusterAdapter struct {
	provider *TencentProvider
}

func (a tencentKubectlClusterAdapter) Apply(ctx context.Context, bundle *tkeapply.ApplyBundle, target protectedresource.Target) error {
	if bundle == nil {
		return nil
	}
	_, err := a.provider.callKubectl(ctx, []string{"apply", "-f", "-"}, bundle.ListJSON(), target)
	return err
}

func (a tencentKubectlClusterAdapter) CreateSecret(ctx context.Context, secret *corev1.Secret, target protectedresource.Target) error {
	raw, err := json.Marshal(secret)
	if err != nil {
		return err
	}
	_, err = a.provider.callKubectl(ctx, []string{"create", "-f", "-"}, raw, target)
	return err
}

func (a tencentKubectlClusterAdapter) CreateConfigMap(ctx context.Context, configMap *corev1.ConfigMap, target protectedresource.Target) error {
	raw, err := json.Marshal(configMap)
	if err != nil {
		return err
	}
	_, err = a.provider.callKubectl(ctx, []string{"create", "-f", "-"}, raw, target)
	return err
}

func (a tencentKubectlClusterAdapter) ScaleDeployment(ctx context.Context, name string, replicas int32, target protectedresource.Target) error {
	_, err := a.provider.callKubectl(ctx, []string{"scale", "deployment/" + name, "--replicas=" + strconv.Itoa(int(replicas))}, nil, target)
	return err
}

func (a tencentKubectlClusterAdapter) DeleteResources(ctx context.Context, refs []tkeapply.ResourceRef, target protectedresource.Target) error {
	args := make([]string, 0, len(refs)+3)
	args = append(args, "delete")
	for _, ref := range refs {
		args = append(args, ref.Kind+"/"+ref.Name)
	}
	args = append(args, "--ignore-not-found=true", "--wait=false")
	_, err := a.provider.callKubectl(ctx, args, nil, target)
	return err
}

func (a tencentKubectlClusterAdapter) ListApplicationResources(ctx context.Context, labelSelector string) (tkeapply.ApplicationResourceList, error) {
	var out tkeapply.ApplicationResourceList
	raw, err := a.provider.callKubectl(ctx, []string{"get", "deployment,replicaset,pod,service,networkpolicy,secret,configmap", "-l", labelSelector, "-o", "json"}, nil, protectedresource.Target{})
	if err != nil {
		return out, err
	}
	items, err := strictKubectlItems(raw)
	if err != nil {
		return out, err
	}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return out, fmt.Errorf("tencent_application_runtime_readback_invalid")
		}
		itemBytes, err := json.Marshal(object)
		if err != nil {
			return out, fmt.Errorf("tencent_application_runtime_readback_invalid")
		}
		switch stringValue(object["kind"]) {
		case "Deployment":
			var typed appsv1.Deployment
			if json.Unmarshal(itemBytes, &typed) != nil {
				return out, fmt.Errorf("tencent_application_runtime_readback_invalid")
			}
			out.Deployments = append(out.Deployments, typed)
		case "ReplicaSet":
			var typed appsv1.ReplicaSet
			if json.Unmarshal(itemBytes, &typed) != nil {
				return out, fmt.Errorf("tencent_application_runtime_readback_invalid")
			}
			out.ReplicaSets = append(out.ReplicaSets, typed)
		case "Pod":
			var typed corev1.Pod
			if json.Unmarshal(itemBytes, &typed) != nil {
				return out, fmt.Errorf("tencent_application_runtime_readback_invalid")
			}
			out.Pods = append(out.Pods, typed)
		case "Service":
			var typed corev1.Service
			if json.Unmarshal(itemBytes, &typed) != nil {
				return out, fmt.Errorf("tencent_application_runtime_readback_invalid")
			}
			out.Services = append(out.Services, typed)
		case "NetworkPolicy":
			var typed networkingv1.NetworkPolicy
			if json.Unmarshal(itemBytes, &typed) != nil {
				return out, fmt.Errorf("tencent_application_runtime_readback_invalid")
			}
			out.NetworkPolicies = append(out.NetworkPolicies, typed)
		case "Secret":
			var typed corev1.Secret
			if json.Unmarshal(itemBytes, &typed) != nil {
				return out, fmt.Errorf("tencent_application_runtime_readback_invalid")
			}
			out.Secrets = append(out.Secrets, typed)
		case "ConfigMap":
			var typed corev1.ConfigMap
			if json.Unmarshal(itemBytes, &typed) != nil {
				return out, fmt.Errorf("tencent_application_runtime_readback_invalid")
			}
			out.ConfigMaps = append(out.ConfigMaps, typed)
		default:
			return out, fmt.Errorf("tencent_application_runtime_readback_invalid")
		}
	}
	return out, nil
}

func (a tencentKubectlClusterAdapter) ListPVCs(ctx context.Context, labelSelector string) ([]corev1.PersistentVolumeClaim, error) {
	raw, err := a.provider.callKubectl(ctx, []string{"get", "pvc", "-l", labelSelector, "-o", "json"}, nil, protectedresource.Target{})
	if err != nil {
		return nil, err
	}
	items, err := strictKubectlItems(raw)
	if err != nil {
		return nil, err
	}
	out := make([]corev1.PersistentVolumeClaim, 0, len(items))
	for _, item := range items {
		itemBytes, err := json.Marshal(item)
		if err != nil {
			return nil, ErrLaunchStageBindingConflict
		}
		var pvc corev1.PersistentVolumeClaim
		if json.Unmarshal(itemBytes, &pvc) != nil {
			return nil, ErrLaunchStageBindingConflict
		}
		out = append(out, pvc)
	}
	return out, nil
}

func (a tencentKubectlClusterAdapter) GetSecret(ctx context.Context, name string) (*corev1.Secret, error) {
	raw, err := a.provider.callKubectl(ctx, []string{"get", "secret/" + name, "--ignore-not-found", "-o", "json"}, nil, protectedresource.Target{})
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var secret corev1.Secret
	if json.Unmarshal(raw, &secret) != nil {
		return nil, ErrLaunchStageBindingConflict
	}
	return &secret, nil
}

func (a tencentKubectlClusterAdapter) GetConfigMap(ctx context.Context, name string) (*corev1.ConfigMap, error) {
	raw, err := a.provider.callKubectl(ctx, []string{"get", "configmap/" + name, "--ignore-not-found", "-o", "json"}, nil, protectedresource.Target{})
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var cm corev1.ConfigMap
	if json.Unmarshal(raw, &cm) != nil {
		return nil, ErrLaunchStageBindingConflict
	}
	return &cm, nil
}

func (a tencentKubectlClusterAdapter) GetNetworkPolicy(ctx context.Context, name string) (*networkingv1.NetworkPolicy, error) {
	raw, err := a.provider.callKubectl(ctx, []string{"get", "networkpolicy/" + name, "--ignore-not-found", "-o", "json"}, nil, protectedresource.Target{})
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var policy networkingv1.NetworkPolicy
	if json.Unmarshal(raw, &policy) != nil {
		return nil, ErrLaunchStageBindingConflict
	}
	return &policy, nil
}

func retainedApplicationInstallation(namespace string) tkeapply.Installation {
	return tkeapply.Installation{Namespace: namespace, ImagePullSecretName: os.Getenv("OPL_IMAGE_PULL_SECRET_NAME"),
		AdminPasswordSeed:       os.Getenv("OPL_AIONUI_ADMIN_PASSWORD_SEED"),
		ApplicationIngressPeers: []map[string]string{{"app.kubernetes.io/name": "opl-cloud", "app.kubernetes.io/component": "control-plane"}}}
}
func applicationPlacement(compute ComputeAllocation, volume StorageVolume) tkeapply.Placement {
	return tkeapply.Placement{AccountID: compute.AccountID, WorkspaceID: compute.WorkspaceID, ComputeID: compute.ID, StorageVolumeID: volume.ID,
		ComputeNodeName: compute.NodeName, ComputePackageID: compute.PackageID, ComputeNodePoolID: compute.NodePoolID, ComputeMachineName: compute.MachineName,
		ComputeInstanceID: firstNonEmpty(compute.InstanceID, compute.CVMInstanceID), StoragePVCName: StoragePVCName(volume)}
}
func applicationExecutionError(err error) error {
	switch {
	case errors.Is(err, tkeapply.ErrWorkspaceLaunchPending):
		return errors.Join(ErrWorkspaceLaunchPending, err)
	case errors.Is(err, tkeapply.ErrLaunchStageBindingConflict):
		return errors.Join(ErrLaunchStageBindingConflict, err)
	case errors.Is(err, tkeapply.ErrResourceAbsent):
		return errors.Join(ErrWorkspaceLaunchResourceAbsent, err)
	}
	return err
}
func (p *TencentProvider) EnsureWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput, compute ComputeAllocation, volume StorageVolume) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	if err := p.validateInstallationConfig(); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	result, err := p.applicationExecutor().EnsureWorkspaceApplicationRuntime(ctx, input, applicationPlacement(compute, volume))
	return result, applicationExecutionError(err)
}
func (p *TencentProvider) ReadWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	result, err := p.applicationExecutor().ReadWorkspaceApplicationRuntime(ctx, input)
	return result, applicationExecutionError(err)
}
func (p *TencentProvider) ReadWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input WorkspaceApplicationRuntimeInput) (WorkspaceApplicationRuntimeLifecycleResult, error) {
	result, err := p.applicationExecutor().ReadWorkspaceApplicationRuntimeLifecycle(ctx, input)
	return result, applicationExecutionError(err)
}
func (p *TencentProvider) SetWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input WorkspaceApplicationRuntimeInput, desired string) (WorkspaceApplicationRuntimeLifecycleResult, error) {
	result, err := p.applicationExecutor().SetWorkspaceApplicationRuntimeLifecycle(ctx, input, desired)
	return result, applicationExecutionError(err)
}
func (p *TencentProvider) ReadWorkspaceApplicationRuntimeCredentials(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeCredentials, error) {
	result, err := p.applicationExecutor().ReadWorkspaceApplicationRuntimeCredentials(ctx, input)
	return result, applicationExecutionError(err)
}
func (p *TencentProvider) readApplicationBoundSecret(ctx context.Context, input WorkspaceApplicationRuntimeInput, binding contracts.WorkspaceApplicationRuntimeSecretBinding) (string, error) {
	result, err := p.applicationExecutor().ReadBoundSecret(ctx, input, binding)
	return result, applicationExecutionError(err)
}
func (p *TencentProvider) prepareApplicationConfigFiles(ctx context.Context, input WorkspaceApplicationRuntimeInput) error {
	return applicationExecutionError(p.applicationExecutor().PrepareConfigFiles(ctx, input))
}
func tencentApplicationCredentialToken(ctx context.Context, input WorkspaceApplicationRuntimeInput) (string, error) {
	version := input.Configuration.CredentialVersion
	if version == "" {
		return "", errors.New("workspace_application_credential_version_required")
	}
	if source := input.Configuration.CredentialSourceRuntimeOperationID; source != "" {
		proven, ok := ctx.Value(applicationCredentialSourceContextKey{}).(applicationCredentialSource)
		if !ok || proven.RuntimeOperationID != source || proven.Version != version || proven.OperationKey == "" {
			return "", ErrLaunchStageBindingConflict
		}
		token := stableID(input.WorkspaceID, proven.OperationKey)[:24]
		if stableID("workspace-credential", input.WorkspaceID, token)[:16] != version {
			return "", ErrLaunchStageBindingConflict
		}
		return token, nil
	}
	return version, nil
}
