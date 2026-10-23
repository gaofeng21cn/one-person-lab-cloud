package fabric

import (
	"context"
	"errors"
	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/internal/tkeapply"
	"os"
)

// The retained owner keeps its operation journal, credential provenance and
// placement policy. Kubernetes apply/readback/config/lifecycle are shared with
// Serve; this adapter adds no execution algorithm or cross-owner state.
func (p *TencentProvider) applicationExecutor() *tkeapply.Executor {
	return &tkeapply.Executor{Kubectl: p.kubectl, RunKubectl: p.callKubectl, Guard: p.guard,
		CredentialToken: tencentApplicationCredentialToken, Installation: retainedApplicationInstallation(p.namespace)}
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
