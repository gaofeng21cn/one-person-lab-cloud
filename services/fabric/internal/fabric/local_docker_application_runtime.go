package fabric

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
)

var localDockerApplicationComponentNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// EnsureWorkspaceApplicationRuntime materialises every declared component of
// one admitted revision as a deterministic, idempotent container on the
// workspace's existing compute network. Persistent mounts bind under the
// workspace's CBS-backed storage directories; the declared ports are recorded
// in the observation — host publishing belongs to the access slice.
func (p *LocalDockerProvider) EnsureWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput, compute ComputeAllocation, volume StorageVolume) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	if err := p.validateLocalDockerApplicationRuntimeIdentity(input, compute); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	storagePaths, err := p.readStorageDirectories(volume)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	network := localDockerName("opl-compute", compute.ID)
	runtimeID := workspaceApplicationRuntimeID(input.WorkspaceID)
	components := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)
	observed := make([]contracts.WorkspaceApplicationRuntimeComponentState, 0, len(components))
	entryURL := ""
	ensureErr := error(nil)
	for _, component := range components {
		state, componentErr := p.ensureWorkspaceApplicationComponent(ctx, input, compute, network, storagePaths, component)
		observed = append(observed, state)
		if componentErr == nil && component.Role == contracts.WorkspaceApplicationComponentMain && entryURL == "" {
			if url, urlErr := p.localDockerApplicationEntryURL(ctx, input, component); urlErr == nil {
				entryURL = url
			}
		}
		if componentErr != nil && ensureErr == nil {
			ensureErr = componentErr
		}
		if componentErr != nil {
			// Remaining declared components stay absent: the observation must
			// always cover exactly the declared set.
			for _, remaining := range components[len(observed):] {
				observed = append(observed, contracts.WorkspaceApplicationRuntimeComponentState{
					Name: remaining.Name, Role: remaining.Role, Image: remaining.Image, State: "absent",
				})
			}
			break
		}
	}
	observation := contracts.WorkspaceApplicationRuntimeObservation{
		SchemaVersion: 1, WorkspaceID: input.WorkspaceID, RuntimeID: runtimeID,
		Status: contracts.WorkspaceApplicationRuntimeOverallStatus(observed), Components: observed,
	}
	if ensureErr == nil && input.Revision.ExposurePolicy != "cloud_private" {
		observation.EntryURL = entryURL
	}
	return observation, ensureErr
}

// localDockerApplicationEntryURL reads back the host port docker assigned to
// the main component's first declared port and forms the local entry URL.
func (p *LocalDockerProvider) localDockerApplicationEntryURL(ctx context.Context, input WorkspaceApplicationRuntimeInput, component contracts.WorkspaceApplicationRuntimeComponentState) (string, error) {
	if len(input.Revision.Ports) == 0 {
		return "", nil
	}
	name, nameErr := localDockerApplicationComponentName(input.WorkspaceID, component.Name)
	if nameErr != nil {
		return "", nameErr
	}
	container, exists, inspectErr := p.inspectContainer(ctx, name)
	if inspectErr != nil || !exists {
		return "", firstNonNil(inspectErr, fmt.Errorf("local_docker_application_component_readback_missing"))
	}
	bindings := container.NetworkSettings.Ports[strconv.Itoa(input.Revision.Ports[0].Port)+"/tcp"]
	if len(bindings) == 0 || bindings[0].HostPort == "" {
		return "", nil
	}
	return "http://" + net.JoinHostPort(p.publishHost, bindings[0].HostPort) + "/", nil
}

// ReadWorkspaceApplicationRuntime observes the declared components without
// mutating anything: absent containers are reported as absent.
func (p *LocalDockerProvider) ReadWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	runtimeID := workspaceApplicationRuntimeID(input.WorkspaceID)
	components := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)
	observed := make([]contracts.WorkspaceApplicationRuntimeComponentState, 0, len(components))
	for _, component := range components {
		name, nameErr := localDockerApplicationComponentName(input.WorkspaceID, component.Name)
		if nameErr != nil {
			return contracts.WorkspaceApplicationRuntimeObservation{}, nameErr
		}
		container, exists, inspectErr := p.inspectContainer(ctx, name)
		if inspectErr != nil {
			return contracts.WorkspaceApplicationRuntimeObservation{}, inspectErr
		}
		if !exists {
			observed = append(observed, contracts.WorkspaceApplicationRuntimeComponentState{
				Name: component.Name, Role: component.Role, Image: component.Image, State: "absent",
			})
			continue
		}
		observed = append(observed, localDockerApplicationComponentState(component, container))
	}
	return contracts.WorkspaceApplicationRuntimeObservation{
		SchemaVersion: 1, WorkspaceID: input.WorkspaceID, RuntimeID: runtimeID,
		Status: contracts.WorkspaceApplicationRuntimeOverallStatus(observed), Components: observed,
	}, nil
}

func (p *LocalDockerProvider) validateLocalDockerApplicationRuntimeIdentity(input WorkspaceApplicationRuntimeInput, compute ComputeAllocation) error {
	if compute.ID == "" || compute.AccountID == "" || input.WorkspaceID == "" || input.WorkspaceID != compute.WorkspaceID {
		return fmt.Errorf("workspace_application_runtime_resource_mismatch")
	}
	return nil
}

func (p *LocalDockerProvider) ensureWorkspaceApplicationComponent(
	ctx context.Context,
	input WorkspaceApplicationRuntimeInput,
	compute ComputeAllocation,
	network string,
	storagePaths localDockerStoragePaths,
	component contracts.WorkspaceApplicationRuntimeComponentState,
) (contracts.WorkspaceApplicationRuntimeComponentState, error) {
	name, nameErr := localDockerApplicationComponentName(input.WorkspaceID, component.Name)
	if nameErr != nil {
		return localDockerApplicationComponentState(component, dockerContainerInspect{}), nameErr
	}
	labels := map[string]string{
		"opl.fabric.provider": "local-docker", "opl.fabric.kind": "application_runtime",
		"opl.account.id": compute.AccountID, "opl.workspace.id": input.WorkspaceID,
		"opl.runtime.id":     workspaceApplicationRuntimeID(input.WorkspaceID),
		"opl.component.name": component.Name, "opl.component.role": component.Role,
		"opl.image.ref": component.Image, "opl.configuration.digest": input.ConfigurationDigest,
	}
	container, exists, inspectErr := p.inspectContainer(ctx, name)
	if inspectErr != nil {
		return localDockerApplicationComponentState(component, dockerContainerInspect{}), inspectErr
	}
	if exists {
		if !exactDockerLabels(container.Config.Labels, labels) || container.Config.Image != component.Image {
			return localDockerApplicationComponentState(component, container), fmt.Errorf("local_docker_application_component_conflict")
		}
		return localDockerApplicationComponentState(component, container), nil
	}
	args := append([]string{"run", "-d", "--name", name}, dockerLabelArgs(labels)...)
	args = append(args, "--network", network)
	if component.Role == contracts.WorkspaceApplicationComponentMain {
		for _, port := range input.Revision.Ports {
			args = append(args, "-p", p.publishHost+"::"+strconv.Itoa(port.Port))
		}
	}
	for _, mount := range input.Revision.PersistentMounts {
		source := filepath.Join(storagePaths.Data, mount.Name)
		if err := os.MkdirAll(source, 0755); err != nil {
			return localDockerApplicationComponentState(component, dockerContainerInspect{}), err
		}
		args = append(args, "--mount", "type=bind,source="+source+",target="+mount.MountPath+",bind-propagation=rprivate")
	}
	for _, mount := range input.Revision.ScratchMounts {
		args = append(args, "--mount", "type=tmpfs,target="+mount.MountPath+",tmpfs-mode=0755")
	}
	if len(input.Revision.Entrypoint) > 0 {
		args = append(args, "--entrypoint", input.Revision.Entrypoint[0])
	}
	args = append(args, component.Image)
	if len(input.Revision.Entrypoint) > 1 {
		args = append(args, input.Revision.Entrypoint[1:]...)
	}
	if _, err := p.runner.Run(ctx, nil, args...); err != nil {
		return localDockerApplicationComponentState(component, dockerContainerInspect{}), err
	}
	created, exists, inspectErr := p.inspectContainer(ctx, name)
	if inspectErr != nil || !exists {
		return localDockerApplicationComponentState(component, dockerContainerInspect{}), firstNonNil(inspectErr, fmt.Errorf("local_docker_application_component_readback_missing"))
	}
	return localDockerApplicationComponentState(component, created), nil
}

func localDockerApplicationComponentState(component contracts.WorkspaceApplicationRuntimeComponentState, container dockerContainerInspect) contracts.WorkspaceApplicationRuntimeComponentState {
	state, lastError := "pending", ""
	switch {
	case container.State.Running && (container.State.Health == nil || container.State.Health.Status == "healthy"):
		state = "ready"
	case container.State.Running:
		state = "pending"
	case container.State.Status == "exited" || container.State.Status == "dead":
		state, lastError = "failed", "container "+container.State.Status
	}
	observed := contracts.WorkspaceApplicationRuntimeComponentState{
		Name: component.Name, Role: component.Role, Image: component.Image,
		State: state, LastError: lastError, ReadyCheck: component.ReadyCheck,
	}
	if component.Role == contracts.WorkspaceApplicationComponentMain {
		observed.Ports = component.Ports
	}
	return observed
}

func localDockerApplicationComponentName(workspaceID, componentName string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(componentName))
	if !localDockerApplicationComponentNamePattern.MatchString(name) {
		return "", fmt.Errorf("local_docker_application_component_name_invalid")
	}
	return localDockerName("opl-app-"+name, workspaceID), nil
}
