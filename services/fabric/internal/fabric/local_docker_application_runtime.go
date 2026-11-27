package fabric

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"

	contracts "opl-cloud/packages/contracts/go"
)

var localDockerApplicationComponentNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// EnsureWorkspaceApplicationRuntime materialises every declared component of
// one admitted revision as a deterministic, idempotent container on the
// workspace's existing compute network. Persistent mounts bind under the
// workspace's owned storage directories. Only the explicitly selected public
// HTTP entry is published; declared HTTP probes run in the container network.
func (p *LocalDockerProvider) EnsureWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput, compute ComputeAllocation, volume StorageVolume) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	if err := validateWorkspaceApplicationConfiguration(input); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if err := p.validateApplicationExecutions(input); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if _, _, err := p.applicationSecretFiles(input); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	for _, dependency := range input.Revision.Dependencies {
		if _, _, err := p.applicationDeclaredSecrets(input, dependency.SecretInputs); err != nil {
			return contracts.WorkspaceApplicationRuntimeObservation{}, err
		}
	}

	if err := p.validateLocalDockerApplicationRuntimeIdentity(input, compute); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if err := p.validateLocalDockerApplicationProbe(input.Revision); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if err := p.applicationCredentialFiles(input, true); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if err := p.applicationConfigFiles(input, true); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	storagePaths, err := p.readStorageDirectories(volume)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	network := localDockerName("opl-compute", compute.ID)
	order, err := contracts.WorkspaceApplicationStartupOrder(input.Revision)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	components := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)
	states := map[string]contracts.WorkspaceApplicationRuntimeComponentState{}
	declared := map[string]contracts.WorkspaceApplicationRuntimeComponentState{}
	for _, component := range components {
		declared[component.Name] = component
		component.State = "absent"
		states[component.Name] = component
	}
	observation := func() contracts.WorkspaceApplicationRuntimeObservation {
		observed := make([]contracts.WorkspaceApplicationRuntimeComponentState, 0, len(components))
		for _, component := range components {
			observed = append(observed, states[component.Name])
		}
		return contracts.WorkspaceApplicationRuntimeObservation{SchemaVersion: 1, WorkspaceID: input.WorkspaceID, RuntimeID: applicationRuntimeID(input), Status: contracts.WorkspaceApplicationRuntimeOverallStatus(observed), Components: observed}
	}
	entryURL := ""
	for _, name := range order {
		component := declared[name]
		// Ensure only absent components whose prerequisites have completed health
		// checks. Existing failed containers are observed, never recreated.
		containerName, nameErr := localDockerApplicationComponentNameForInput(input, name)
		if nameErr != nil {
			return observation(), nameErr
		}
		_, exists, inspectErr := p.inspectContainer(ctx, containerName)
		if inspectErr != nil {
			return observation(), inspectErr
		}
		if !exists && applicationComponentPrerequisitesReady(input.Revision, name, states) {
			state, err := p.ensureWorkspaceApplicationComponent(ctx, input, compute, network, storagePaths, component)
			states[name] = state
			if err != nil {
				return observation(), err
			}
			exists = true
		}
		if exists && component.Role == contracts.WorkspaceApplicationComponentMain {
			if err := p.ensureApplicationGatewayNetwork(ctx, input, compute); err != nil {
				return observation(), err
			}
		}
		state, url, err := p.readWorkspaceApplicationComponent(ctx, input, component)
		if err != nil {
			return observation(), err
		}
		states[name] = state
		if component.Role == contracts.WorkspaceApplicationComponentMain {
			entryURL = url
		}
	}
	result := observation()
	if result.Status == "ready" && entryURL != "" {
		result.Entry = &contracts.WorkspaceApplicationEntry{URL: entryURL}
	}
	return result, nil
}

func applicationComponentPrerequisitesReady(revision contracts.WorkspaceApplicationRevision, name string, states map[string]contracts.WorkspaceApplicationRuntimeComponentState) bool {
	for _, dependency := range contracts.WorkspaceApplicationComponentDependencies(revision, name) {
		if states[dependency].State != "ready" {
			return false
		}
	}
	return true
}

// Only the explicitly selected TCP entry has a public host binding.
func localDockerApplicationEntryPort(revision contracts.WorkspaceApplicationRevision) int {
	if revision.ExposurePolicy == "cloud_private" || revision.EntryPort == "" {
		return 0
	}
	if port, ok := contracts.WorkspaceApplicationEntryPort(revision); ok {
		return port.Port
	}
	return 0
}

func (p *LocalDockerProvider) localDockerApplicationEntryURL(revision contracts.WorkspaceApplicationRevision, container dockerContainerInspect) (string, error) {
	port := localDockerApplicationEntryPort(revision)
	if port == 0 {
		return "", nil
	}
	bindings := container.NetworkSettings.Ports[strconv.Itoa(port)+"/tcp"]
	if len(bindings) != 1 || bindings[0].HostIP != p.publishHost || bindings[0].HostPort == "" {
		return "", errors.New("local_docker_application_entry_binding_invalid")
	}
	return "http://" + net.JoinHostPort(p.publishHost, bindings[0].HostPort) + "/", nil
}

// ReadWorkspaceApplicationRuntime observes the declared components without
// mutating anything: absent containers are reported as absent.
func (p *LocalDockerProvider) ReadWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	if err := p.validateLocalDockerApplicationProbe(input.Revision); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	runtimeID := applicationRuntimeID(input)
	components := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)
	observed := make([]contracts.WorkspaceApplicationRuntimeComponentState, 0, len(components))
	entryURL := ""
	for _, component := range components {
		state, url, err := p.readWorkspaceApplicationComponent(ctx, input, component)
		if err != nil {
			return contracts.WorkspaceApplicationRuntimeObservation{}, err
		}
		observed = append(observed, state)
		if component.Role == contracts.WorkspaceApplicationComponentMain {
			entryURL = url
		}
	}

	status := contracts.WorkspaceApplicationRuntimeOverallStatus(observed)
	if status != "ready" {
		entryURL = ""
	}
	observation := contracts.WorkspaceApplicationRuntimeObservation{
		SchemaVersion: 1, WorkspaceID: input.WorkspaceID, RuntimeID: runtimeID,
		Status: status, Components: observed,
	}
	if status == "ready" && entryURL != "" {
		observation.Entry = &contracts.WorkspaceApplicationEntry{URL: entryURL}
	}
	return observation, nil
}

func (p *LocalDockerProvider) readWorkspaceApplicationComponent(ctx context.Context, input WorkspaceApplicationRuntimeInput, component contracts.WorkspaceApplicationRuntimeComponentState) (contracts.WorkspaceApplicationRuntimeComponentState, string, error) {
	entryURL := ""
	name, nameErr := localDockerApplicationComponentNameForInput(input, component.Name)
	if nameErr != nil {
		return component, "", nameErr
	}
	container, exists, inspectErr := p.inspectContainer(ctx, name)
	if inspectErr != nil {
		return component, "", inspectErr
	}
	if !exists {
		return contracts.WorkspaceApplicationRuntimeComponentState{Name: component.Name, Role: component.Role, Image: component.Image, State: "absent"}, "", nil
	}
	if input.AccountID == "" || !exactDockerLabels(container.Config.Labels, localDockerApplicationLabels(input, input.AccountID, component)) || container.Config.Image != component.Image {
		return component, "", errors.New("local_docker_application_component_conflict")
	}
	entryPort := 0
	if component.Role == contracts.WorkspaceApplicationComponentMain {
		if err := p.verifyLocalDockerApplicationMainCommand(ctx, input, container); err != nil {
			return component, "", err
		}
		entryPort = localDockerApplicationEntryPort(input.Revision)
		if input.SchemaVersion == 2 && contracts.WorkspaceApplicationCredentialKind(input.Revision, contracts.WorkspaceApplicationCredentialGatewayKey) {
			if err := p.verifyRuntimeGatewayNetwork(ctx, container); err != nil {
				if errors.Is(err, ErrRuntimeGatewayNetworkNotBound) {
					// The gateway is not attached yet. Report the component as not
					// ready so the owning runtime operation can still advance it,
					// instead of failing the whole application runtime read.
					return contracts.WorkspaceApplicationRuntimeComponentState{Name: component.Name, Role: component.Role, Image: component.Image, State: "pending"}, "", nil
				}
				return component, "", err
			}
		}
		if err := p.verifyApplicationConfiguration(input, container); err != nil {
			return component, "", err
		}
	}
	for port, bindings := range container.NetworkSettings.Ports {
		if len(bindings) > 0 && (entryPort == 0 || port != strconv.Itoa(entryPort)+"/tcp") {
			return component, "", errors.New("local_docker_application_component_conflict")
		}
	}
	if err := p.verifyApplicationConfigMounts(input, component.Name, container); err != nil {
		return component, "", err
	}
	if err := p.verifyApplicationExecution(input, component.Name, container); err != nil {
		return component, "", err
	}
	if component.Role == contracts.WorkspaceApplicationComponentMain {
		if err := verifyApplicationScratchMounts(input.Revision.ScratchMounts, container); err != nil {
			return component, "", err
		}
	} else {
		dependency, err := dependencySpecByName(input.Revision, component.Name)
		if err != nil {
			return component, "", err
		}
		if err := verifyApplicationScratchMounts(dependency.ScratchMounts, container); err != nil {
			return component, "", err
		}
	}
	state := localDockerApplicationComponentState(component, container)
	if component.Role == contracts.WorkspaceApplicationComponentMain {
		for _, port := range input.Revision.Ports {
			state.Ports = append(state.Ports, port.Port)
		}
		if state.State == "ready" && len(input.Revision.HealthChecks) > 0 {
			ready, probeErr := p.probeLocalDockerApplication(ctx, input, container)
			if probeErr != nil {
				return component, "", probeErr
			}
			state.ReadyCheck = "declared_http"
			if !ready {
				state.State = "pending"
			}
		}
		if state.State == "ready" {
			var entryErr error
			entryURL, entryErr = p.localDockerApplicationEntryURL(input.Revision, container)
			if entryErr != nil {
				return component, "", entryErr
			}
		}
	}
	if component.Role == contracts.WorkspaceApplicationComponentDependency {
		dependency, depErr := dependencySpecByName(input.Revision, component.Name)
		if depErr != nil {
			return component, "", depErr
		}
		if err := p.verifyApplicationDeclaredSecrets(input, container, dependency.SecretInputs); err != nil {
			return component, "", err
		}

		if state.State == "ready" && len(dependency.HealthChecks) > 0 {
			ready, healthyErr := p.probeLocalDockerDependencyChecks(ctx, input, container, dependency.HealthChecks)
			if healthyErr != nil {
				return component, "", healthyErr
			}
			if ready {
				state.ReadyCheck = dependency.HealthChecks[0].Type + "_declared"
			} else {
				state.State = "pending"
			}
		}
	}
	return state, entryURL, nil
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
	networkName string,
	storagePaths localDockerStoragePaths,
	component contracts.WorkspaceApplicationRuntimeComponentState,
) (contracts.WorkspaceApplicationRuntimeComponentState, error) {
	name, nameErr := localDockerApplicationComponentNameForInput(input, component.Name)
	if nameErr != nil {
		return localDockerApplicationComponentState(component, dockerContainerInspect{}), nameErr
	}
	labels := localDockerApplicationLabels(input, compute.AccountID, component)
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
	identity := applicationComponentWorkloadIdentity(input.Revision, component.Name)
	execSpec, err := p.applicationExecutionSpec(applicationComponentExecution(input.Revision, component.Name), identity)
	if err != nil {
		return component, err
	}
	platform, err := parseOCIPlatform(input.Revision.Platform)
	if err != nil {
		return component, err
	}
	containerConfig := &dockercontainer.Config{
		Image:  component.Image,
		Labels: labels,
		User:   execSpec.User,
	}
	hostConfig := &dockercontainer.HostConfig{
		NetworkMode: dockercontainer.NetworkMode(networkName),
		Init:        execSpec.Init,
		SecurityOpt: execSpec.SecurityOpt,
	}
	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			networkName: {Aliases: []string{component.Name}},
		},
	}
	// A declared envelope bounds the container exactly as the Kubernetes
	// provider bounds a pod, including a hard memory ceiling: Docker would
	// otherwise let a component spill into swap and hide the overrun that the
	// hosted provider reports as an eviction.
	envelope := input.Revision.Compute
	if component.Role == contracts.WorkspaceApplicationComponentDependency {
		dependency, depErr := dependencySpecByName(input.Revision, component.Name)
		if depErr != nil {
			return localDockerApplicationComponentState(component, dockerContainerInspect{}), depErr
		}
		envelope = dependency.Compute
	}
	applyLocalDockerApplicationResources(hostConfig, envelope)
	if component.Role == contracts.WorkspaceApplicationComponentDependency {
		dependency, depErr := dependencySpecByName(input.Revision, component.Name)
		if depErr != nil {
			return localDockerApplicationComponentState(component, dockerContainerInspect{}), depErr
		}
		if depErr := p.applyLocalDockerDependencySpec(input, dependency, storagePaths, containerConfig, hostConfig); depErr != nil {
			return localDockerApplicationComponentState(component, dockerContainerInspect{}), depErr
		}
		secretSpec, err := p.applicationComponentSecretSpec(input, component.Name, dependency.SecretInputs)
		if err != nil {
			return component, err
		}
		hostConfig.Mounts = append(hostConfig.Mounts, secretSpec.Mounts...)
		containerConfig.Env = append(containerConfig.Env, secretSpec.Env...)
	}
	overrideEntrypoint := component.Role == contracts.WorkspaceApplicationComponentMain && len(input.Revision.Entrypoint) > 0
	if component.Role == contracts.WorkspaceApplicationComponentMain {
		containerConfig.Env = append(containerConfig.Env, applicationEnvironmentVars(input)...)
		secretSpec, err := p.applicationSecretMountSpec(input)
		if err != nil {
			return component, err
		}
		hostConfig.Mounts = append(hostConfig.Mounts, secretSpec.Mounts...)
		containerConfig.Env = append(containerConfig.Env, secretSpec.Env...)
		if port := localDockerApplicationEntryPort(input.Revision); port != 0 {
			portKey, portErr := network.ParsePort(strconv.Itoa(port) + "/tcp")
			if portErr != nil {
				return component, portErr
			}
			if containerConfig.ExposedPorts == nil {
				containerConfig.ExposedPorts = network.PortSet{}
			}
			containerConfig.ExposedPorts[portKey] = struct{}{}
			if hostConfig.PortBindings == nil {
				hostConfig.PortBindings = network.PortMap{}
			}
			hostConfig.PortBindings[portKey] = []network.PortBinding{{HostIP: parseDockerHostIP(p.publishHost), HostPort: ""}}
		}
		for _, m := range input.Revision.PersistentMounts {
			source := localDockerApplicationPersistentSource(input, storagePaths, m)
			if err := os.MkdirAll(source, 0755); err != nil {
				return localDockerApplicationComponentState(component, dockerContainerInspect{}), err
			}
			// The main component's persistent data must belong to the identity it
			// runs as, exactly as the Tencent fsGroup establishes for its volume.
			if !m.ReadOnly {
				if err := p.workloadOwnership.Assign(source, identity.uid, identity.gid); err != nil {
					return localDockerApplicationComponentState(component, dockerContainerInspect{}), err
				}
			}
			hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{
				Type:        mount.TypeBind,
				Source:      source,
				Target:      m.MountPath,
				ReadOnly:    m.ReadOnly,
				BindOptions: &mount.BindOptions{Propagation: mount.PropagationRPrivate},
			})
		}
		for _, m := range input.Revision.ScratchMounts {
			applyLocalDockerApplicationScratchMount(hostConfig, m, identity)
		}
		if overrideEntrypoint {
			// Overriding Entrypoint clears the image CMD in Docker. Pull the
			// admitted platform before reading its defaults, then preserve CMD
			// after the entrypoint tail.
			if err := p.docker.PullImage(ctx, component.Image, *platform); err != nil {
				return component, err
			}
			imageCommand, err := p.localDockerApplicationImageCommand(ctx, component.Image, input.Revision.Platform)
			if err != nil {
				return component, err
			}
			containerConfig.Entrypoint = []string{input.Revision.Entrypoint[0]}
			containerConfig.Cmd = append(append([]string(nil), input.Revision.Entrypoint[1:]...), imageCommand...)
		}
	}
	hostConfig.Mounts = append(hostConfig.Mounts, p.applicationConfigMounts(input, component.Name)...)
	createdResp, err := p.docker.ContainerCreate(ctx, containerConfig, hostConfig, networkingConfig, platform, name)
	if !overrideEntrypoint && cerrdefs.IsNotFound(err) {
		// BuildKit pushes to the registry. A cold Engine needs the admitted
		// image and platform before it can create a container with image defaults.
		if pullErr := p.docker.PullImage(ctx, component.Image, *platform); pullErr != nil {
			return component, fmt.Errorf("local_docker_application_image_pull_failed: %w", pullErr)
		}
		createdResp, err = p.docker.ContainerCreate(ctx, containerConfig, hostConfig, networkingConfig, platform, name)
	}
	if err == nil {
		err = p.docker.ContainerStart(ctx, firstNonEmpty(createdResp.ID, name), mobyclient.ContainerStartOptions{})
	}
	if err != nil {
		return localDockerApplicationComponentState(component, dockerContainerInspect{}), err
	}
	created, exists, inspectErr := p.inspectContainer(ctx, name)
	if inspectErr != nil || !exists {
		return localDockerApplicationComponentState(component, dockerContainerInspect{}), firstNonNil(inspectErr, fmt.Errorf("local_docker_application_component_readback_missing"))
	}
	return localDockerApplicationComponentState(component, created), nil
}

// localDockerApplicationImageCommand reads only the exact admitted image and
// platform. Missing or malformed readback is not evidence of an empty CMD.
func (p *LocalDockerProvider) localDockerApplicationImageCommand(ctx context.Context, imageRef, platform string) ([]string, error) {
	plat, err := parseOCIPlatform(platform)
	if err != nil {
		return nil, errors.New("local_docker_application_image_identity_mismatch")
	}
	observed, err := p.docker.ImageInspect(ctx, imageRef, plat)
	if err != nil {
		return nil, fmt.Errorf("local_docker_application_image_command_read_failed: %w", err)
	}
	if observed.ID == "" || observed.Config == nil {
		return nil, errors.New("local_docker_application_image_command_invalid")
	}
	observedPlatform := observed.Os + "/" + observed.Architecture
	if observed.Variant != "" {
		observedPlatform += "/" + observed.Variant
	}
	if !slices.Contains(observed.RepoDigests, imageRef) || observedPlatform != platform {
		return nil, errors.New("local_docker_application_image_identity_mismatch")
	}
	return append([]string(nil), observed.Config.Cmd...), nil
}

func (p *LocalDockerProvider) verifyLocalDockerApplicationMainCommand(ctx context.Context, input WorkspaceApplicationRuntimeInput, container dockerContainerInspect) error {
	if len(input.Revision.Entrypoint) == 0 {
		return nil
	}
	cmd, err := p.localDockerApplicationImageCommand(ctx, input.Revision.Image, input.Revision.Platform)
	if err != nil {
		return err
	}
	expected := append(append([]string{}, input.Revision.Entrypoint...), cmd...)
	actual := append([]string{container.Path}, container.Args...)
	if !slices.Equal(actual, expected) {
		return errors.New("local_docker_application_component_conflict")
	}
	return nil
}

// dependencySpecByName resolves one dependency's full spec from the admitted
// revision; the main component never resolves through this path.
func dependencySpecByName(revision contracts.WorkspaceApplicationRevision, name string) (contracts.WorkspaceApplicationDependency, error) {
	for _, dependency := range revision.Dependencies {
		if dependency.Name == name {
			return dependency, nil
		}
	}
	return contracts.WorkspaceApplicationDependency{}, fmt.Errorf("local_docker_application_dependency_spec_missing")
}

// applyLocalDockerApplicationResources bounds one container by its declared
// envelope. An undeclared envelope leaves the resource fields zeroed.
func applyLocalDockerApplicationResources(hostConfig *dockercontainer.HostConfig, compute contracts.WorkspaceApplicationCompute) {
	if compute.CPULimitMilli > 0 {
		hostConfig.Resources.NanoCPUs = compute.CPULimitMilli * 1_000_000
	}
	if compute.MemoryLimitBytes > 0 {
		hostConfig.Resources.Memory = compute.MemoryLimitBytes
		hostConfig.Resources.MemorySwap = compute.MemoryLimitBytes
	}
	if compute.MemoryRequestBytes > 0 {
		hostConfig.Resources.MemoryReservation = compute.MemoryRequestBytes
	}
}

// applyLocalDockerDependencySpec populates a dependency container's typed
// Config and HostConfig. Dependencies do not inherit the main component's
// ports, environment, or mounts.
func (p *LocalDockerProvider) applyLocalDockerDependencySpec(input WorkspaceApplicationRuntimeInput, dependency contracts.WorkspaceApplicationDependency, storagePaths localDockerStoragePaths, containerConfig *dockercontainer.Config, hostConfig *dockercontainer.HostConfig) error {
	identity := applicationComponentWorkloadIdentity(input.Revision, dependency.Name)
	for _, port := range dependency.Ports {
		if port.Protocol == "TCP" {
			portKey, portErr := network.ParsePort(strconv.Itoa(port.Port) + "/tcp")
			if portErr != nil {
				return portErr
			}
			if containerConfig.ExposedPorts == nil {
				containerConfig.ExposedPorts = network.PortSet{}
			}
			containerConfig.ExposedPorts[portKey] = struct{}{}
		}
	}
	envNames := make([]string, 0, len(dependency.Command.Env))
	for name := range dependency.Command.Env {
		envNames = append(envNames, name)
	}
	sort.Strings(envNames)
	for _, name := range envNames {
		containerConfig.Env = append(containerConfig.Env, name+"="+dependency.Command.Env[name])
	}
	if len(dependency.Command.Entrypoint) > 0 {
		containerConfig.Entrypoint = []string{dependency.Command.Entrypoint[0]}
		if len(dependency.Command.Entrypoint) > 1 {
			containerConfig.Cmd = append(containerConfig.Cmd, dependency.Command.Entrypoint[1:]...)
		}
	}
	if len(dependency.Command.Args) > 0 {
		containerConfig.Cmd = append(containerConfig.Cmd, dependency.Command.Args...)
	}
	for _, m := range dependency.PersistentMounts {
		source := localDockerApplicationPersistentSource(input, storagePaths, contracts.WorkspaceApplicationMount{Name: m.Name, MountPath: m.MountPath, ReadOnly: m.ReadOnly})
		if err := os.MkdirAll(source, 0755); err != nil {
			return err
		}
		// A dependency that declares an identity owns the data it mounts just
		// like the main component; an undeclared dependency keeps its image
		// default and is not adopted.
		if !m.ReadOnly && identity != nil {
			if err := p.workloadOwnership.Assign(source, identity.uid, identity.gid); err != nil {
				return err
			}
		}
		hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{
			Type:        mount.TypeBind,
			Source:      source,
			Target:      m.MountPath,
			ReadOnly:    m.ReadOnly,
			BindOptions: &mount.BindOptions{Propagation: mount.PropagationRPrivate},
		})
	}
	for _, m := range dependency.ScratchMounts {
		applyLocalDockerApplicationScratchMount(hostConfig, m, identity)
	}
	return nil
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

func localDockerApplicationLabels(input WorkspaceApplicationRuntimeInput, accountID string, component contracts.WorkspaceApplicationRuntimeComponentState) map[string]string {
	labels := map[string]string{
		"opl.fabric.provider": "local-docker", "opl.fabric.kind": "application_runtime",
		"opl.account.id": accountID, "opl.workspace.id": input.WorkspaceID, "opl.compute.id": input.ComputeID,
		"opl.runtime.id":     applicationRuntimeID(input),
		"opl.component.name": component.Name, "opl.component.role": component.Role,
		"opl.image.ref": component.Image, "opl.configuration.digest": input.ConfigurationDigest,
	}
	if input.SchemaVersion == 0 {
		delete(labels, "opl.compute.id")
	}
	return labels
}

func (p *LocalDockerProvider) validateLocalDockerApplicationProbe(revision contracts.WorkspaceApplicationRevision) error {
	hasChecks := len(revision.HealthChecks) > 0
	for _, dependency := range revision.Dependencies {
		for _, check := range dependency.HealthChecks {
			if check.Type == "exec" {
				if len(check.Command) != 3 || check.Command[0] != "/bin/sh" || check.Command[1] != "-c" {
					return errors.New("local_docker_application_exec_probe_unsupported")
				}
			} else {
				hasChecks = true
			}
		}
	}
	if hasChecks && !contracts.ValidWorkspaceImageReference(p.applicationProbeImage) {
		return errors.New("local_docker_application_probe_image_required")
	}
	return nil
}

func (p *LocalDockerProvider) probeLocalDockerApplication(ctx context.Context, input WorkspaceApplicationRuntimeInput, container dockerContainerInspect) (bool, error) {
	checks := make([]contracts.WorkspaceApplicationDependencyHealthCheck, 0, len(input.Revision.HealthChecks))
	for _, check := range input.Revision.HealthChecks {
		checks = append(checks, contracts.WorkspaceApplicationDependencyHealthCheck{Type: "http", Port: check.Port, Path: check.Path, InitialDelaySeconds: check.InitialDelaySeconds})
	}
	return p.probeLocalDockerApplicationChecks(ctx, input, container, checks)
}

// Local supports the explicitly declared /bin/sh -c capability, not arbitrary
// exec argv. The declared shell runs a fixed wrapper; original command output
// cannot become readiness evidence or leak through the Docker runner's errors.
const localDockerApplicationExecProbeScript = `"$@" >/dev/null 2>&1
status=$?
printf 'opl-health-exit:%s\n' "$status"
`

func (p *LocalDockerProvider) probeLocalDockerDependencyChecks(ctx context.Context, input WorkspaceApplicationRuntimeInput, container dockerContainerInspect, declared []contracts.WorkspaceApplicationDependencyHealthCheck) (bool, error) {
	startedAt, err := time.Parse(time.RFC3339Nano, container.State.StartedAt)
	if err != nil {
		return false, errors.New("local_docker_application_started_at_invalid")
	}
	for _, check := range declared {
		if p.now().Before(startedAt.Add(time.Duration(check.InitialDelaySeconds) * time.Second)) {
			return false, nil
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	networkChecks := make([]contracts.WorkspaceApplicationDependencyHealthCheck, 0, len(declared))
	for _, check := range declared {
		if check.Type != "exec" {
			networkChecks = append(networkChecks, check)
			continue
		}
		if len(check.Command) != 3 || check.Command[0] != "/bin/sh" || check.Command[1] != "-c" {
			return false, errors.New("local_docker_application_exec_probe_unsupported")
		}
		cmd := append([]string{check.Command[0], "-c", localDockerApplicationExecProbeScript, "opl-health"}, check.Command...)
		output, runErr := p.docker.ContainerExec(probeCtx, container.ID, cmd, nil)
		if runErr != nil || probeCtx.Err() != nil {
			return false, errors.Join(errors.New("local_docker_application_exec_probe_failed"), probeCtx.Err())
		}
		marker := strings.TrimSuffix(string(output), "\n")
		codeText, ok := strings.CutPrefix(marker, "opl-health-exit:")
		code, err := strconv.Atoi(codeText)
		if !ok || err != nil || code < 0 || code > 255 || strconv.Itoa(code) != codeText || string(output) != marker+"\n" {
			return false, errors.New("local_docker_application_exec_probe_result_invalid")
		}
		if code != 0 {
			return false, nil
		}
	}
	if len(networkChecks) == 0 {
		return true, nil
	}
	return p.probeLocalDockerApplicationChecks(probeCtx, input, container, networkChecks)
}

// All declared checks run in the target network namespace, never against a
// Docker bridge IP from the host or by assuming tools in the application image.
func (p *LocalDockerProvider) probeLocalDockerApplicationChecks(ctx context.Context, input WorkspaceApplicationRuntimeInput, container dockerContainerInspect, declared []contracts.WorkspaceApplicationDependencyHealthCheck) (bool, error) {
	startedAt, err := time.Parse(time.RFC3339Nano, container.State.StartedAt)
	if err != nil {
		return false, errors.New("local_docker_application_started_at_invalid")
	}
	for _, check := range declared {
		if p.now().Before(startedAt.Add(time.Duration(check.InitialDelaySeconds) * time.Second)) {
			return false, nil
		}
	}
	checks, err := json.Marshal(declared)
	if err != nil {
		return false, err
	}
	probeName := "opl-app-probe-" + stableSuffix(input.WorkspaceID, container.ID, time.Now().UTC().Format(time.RFC3339Nano))[:24]
	// The execution deadline covers container startup and all declared probes. The
	// provider is the single cleanup owner; auto-remove would keep docker run
	// waiting for storage teardown and race a second cleanup on timeout.
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	pidsLimit := int64(64)
	probeCfg := &dockercontainer.Config{
		Image:      p.applicationProbeImage,
		User:       "65534:65534",
		Env:        []string{"GOMAXPROCS=2"},
		Entrypoint: []string{"/usr/local/bin/opl-owner-probe"},
		Cmd:        []string{"--app-probe", string(checks)},
		Labels: map[string]string{
			"opl.fabric.kind":  "application_probe",
			"opl.workspace.id": input.WorkspaceID,
		},
	}
	probeHostCfg := &dockercontainer.HostConfig{
		NetworkMode:    dockercontainer.NetworkMode("container:" + container.ID),
		ReadonlyRootfs: true,
		CapDrop:        []string{"ALL"},
		SecurityOpt:    []string{"no-new-privileges"},
		Resources: dockercontainer.Resources{
			PidsLimit: &pidsLimit,
			Memory:    128 * 1024 * 1024,
			NanoCPUs:  250_000_000,
		},
	}
	output, runErr := p.runForegroundContainer(probeCtx, probeName, probeCfg, probeHostCfg)
	cancel()
	// Docker Desktop teardown has been observed taking 32.7 seconds after the
	// probe exits successfully. Its separate one-minute cleanup budget includes
	// ownership inspection and verified absence, without extending HTTP/run time.
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
	defer cleanupCancel()
	cleanupErr := p.removeApplicationProbe(cleanupCtx, input.WorkspaceID, probeName)
	if runErr != nil || cleanupErr != nil {
		return false, errors.Join(runErr, cleanupErr)
	}
	var result struct {
		Ready *bool `json:"ready"`
	}
	if err := json.Unmarshal(output, &result); err != nil || result.Ready == nil {
		return false, errors.New("local_docker_application_probe_result_invalid")
	}
	return *result.Ready, nil
}

func (p *LocalDockerProvider) runForegroundContainer(ctx context.Context, name string, cfg *dockercontainer.Config, hostCfg *dockercontainer.HostConfig) ([]byte, error) {
	created, err := p.docker.ContainerCreate(ctx, cfg, hostCfg, nil, nil, name)
	if err != nil {
		return nil, err
	}
	containerID := firstNonEmpty(created.ID, name)
	if err := p.docker.ContainerStart(ctx, containerID, mobyclient.ContainerStartOptions{}); err != nil {
		return nil, err
	}
	statusCh, errCh := p.docker.ContainerWait(ctx, containerID, dockercontainer.WaitConditionNotRunning)
	select {
	case waitErr := <-errCh:
		if waitErr != nil {
			return nil, waitErr
		}
	case status := <-statusCh:
		if status.Error != nil && status.Error.Message != "" {
			return nil, errors.New(status.Error.Message)
		}
		if status.StatusCode != 0 {
			return nil, fmt.Errorf("container exited with status %d", status.StatusCode)
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	logs, err := p.docker.ContainerLogs(ctx, containerID, mobyclient.ContainerLogsOptions{ShowStdout: true})
	if err != nil {
		return nil, err
	}
	return readDockerStdoutStream(logs)
}

func (p *LocalDockerProvider) removeApplicationProbe(ctx context.Context, workspaceID, name string) error {
	containerInspect, exists, err := p.inspectContainer(ctx, name)
	if err != nil || !exists {
		return err
	}
	if containerInspect.Config.Image != p.applicationProbeImage || containerInspect.Config.Labels["opl.fabric.kind"] != "application_probe" || containerInspect.Config.Labels["opl.workspace.id"] != workspaceID {
		return errors.New("local_docker_application_probe_owner_mismatch")
	}
	if err := p.docker.ContainerRemove(ctx, name, mobyclient.ContainerRemoveOptions{Force: containerInspect.State.Running}); err != nil {
		return err
	}
	if _, exists, err := p.inspectContainer(ctx, name); err != nil {
		return err
	} else if exists {
		return errors.New("local_docker_application_probe_cleanup_pending")
	}
	return nil
}

func (p *LocalDockerProvider) ensureApplicationGatewayNetwork(ctx context.Context, input WorkspaceApplicationRuntimeInput, compute ComputeAllocation) error {
	if !contracts.WorkspaceApplicationCredentialKind(input.Revision, contracts.WorkspaceApplicationCredentialGatewayKey) || p.runtimeGatewayContainer == "" {
		return nil
	}
	name, err := localDockerApplicationComponentNameForInput(input, contracts.WorkspaceApplicationComponentMain)
	if err != nil {
		return err
	}
	container, exists, err := p.inspectContainer(ctx, name)
	if err != nil || !exists {
		return firstNonNil(err, ErrWorkspaceLaunchResourceAbsent)
	}
	bound, err := p.runtimeGatewayNetworkStatus(ctx, container, compute)
	if err != nil || bound {
		return err
	}
	attempt, err := beginProviderMutation(ctx, "local_docker_application_gateway_network", "workspace_application_runtime", applicationRuntimeID(input), input.ComputeID)
	if err != nil {
		return err
	}
	err = p.ensureRuntimeGatewayNetwork(ctx, container, compute, attempt)
	if attempt != nil {
		return errors.Join(err, attempt.complete(ctx, "", map[string]string{"runtimeId": applicationRuntimeID(input)}, err))
	}
	return err
}
