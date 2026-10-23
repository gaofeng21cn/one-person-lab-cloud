package fabric

import (
	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/internal/tkeapply"
)

func retainedManifest(input WorkspaceApplicationRuntimeInput, c ComputeAllocation, v StorageVolume) []byte {
	return tkeapply.Manifest(input, applicationPlacement(c, v), retainedApplicationInstallation(namespace()))
}
func retainedComponentManifest(input WorkspaceApplicationRuntimeInput, c ComputeAllocation, v StorageVolume, admitted map[string]bool) []byte {
	return tkeapply.ComponentManifest(input, applicationPlacement(c, v), retainedApplicationInstallation(namespace()), admitted)
}
func retainedComponentDeployment(input WorkspaceApplicationRuntimeInput, c ComputeAllocation, v StorageVolume, component contracts.WorkspaceApplicationRuntimeComponentState, tags map[string]string, pvc string) map[string]any {
	return tkeapply.ComponentDeployment(input, applicationPlacement(c, v), retainedApplicationInstallation(namespace()), component, tags, pvc)
}
func retainedNetworkPolicy(input WorkspaceApplicationRuntimeInput, tags map[string]string) map[string]any {
	return tkeapply.NetworkPolicy(input, retainedApplicationInstallation(namespace()), tags)
}
