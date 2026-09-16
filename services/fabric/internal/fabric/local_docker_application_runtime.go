package fabric

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

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
		entryPort = localDockerApplicationEntryPort(input.Revision)
		if input.SchemaVersion == 2 && input.Revision.RuntimeProfile == "opl_app" {
			if err := p.verifyRuntimeGatewayNetwork(ctx, container); err != nil {
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
	network string,
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
	args := append([]string{"run", "-d", "--name", name}, dockerLabelArgs(labels)...)
	executionArgs, err := p.applicationExecutionArgs(applicationComponentExecution(input.Revision, component.Name))
	if err != nil {
		return component, err
	}
	args = append(args, executionArgs...)
	args = append(args, "--network", network, "--network-alias", component.Name, "--platform", input.Revision.Platform)
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
	args = append(args, localDockerApplicationResourceArgs(envelope)...)
	if component.Role == contracts.WorkspaceApplicationComponentDependency {
		dependency, depErr := dependencySpecByName(input.Revision, component.Name)
		if depErr != nil {
			return localDockerApplicationComponentState(component, dockerContainerInspect{}), depErr
		}
		depArgs, depErr := localDockerDependencyRunArgs(input, dependency, storagePaths)
		if depErr != nil {
			return localDockerApplicationComponentState(component, dockerContainerInspect{}), depErr
		}
		args = append(args, depArgs...)
		secretArgs, err := p.applicationComponentSecretArgs(input, component.Name, dependency.SecretInputs)
		if err != nil {
			return component, err
		}
		args = append(args, secretArgs...)

	}
	if component.Role == contracts.WorkspaceApplicationComponentMain {
		args = append(args, applicationEnvironmentArgs(input)...)
		secretArgs, err := p.applicationSecretMountArgs(input)
		if err != nil {
			return component, err
		}
		args = append(args, secretArgs...)
		if port := localDockerApplicationEntryPort(input.Revision); port != 0 {
			args = append(args, "-p", p.publishHost+"::"+strconv.Itoa(port)+"/tcp")
		}
		for _, mount := range input.Revision.PersistentMounts {
			source := localDockerApplicationPersistentSource(input, storagePaths, mount)
			if err := os.MkdirAll(source, 0755); err != nil {
				return localDockerApplicationComponentState(component, dockerContainerInspect{}), err
			}
			binding := "type=bind,source=" + source + ",target=" + mount.MountPath + ",bind-propagation=rprivate"
			if mount.ReadOnly {
				binding += ",readonly"
			}
			args = append(args, "--mount", binding)
		}
		for _, mount := range input.Revision.ScratchMounts {
			args = append(args, localDockerApplicationScratchArgs(mount)...)
		}
		if len(input.Revision.Entrypoint) > 0 {
			args = append(args, "--entrypoint", input.Revision.Entrypoint[0])
		}
	}
	args = append(args, p.applicationConfigMountArgs(input, component.Name)...)
	args = append(args, component.Image)
	if component.Role == contracts.WorkspaceApplicationComponentDependency {
		dependency, err := dependencySpecByName(input.Revision, component.Name)
		if err != nil {
			return component, err
		}
		if len(dependency.Command.Entrypoint) > 1 {
			args = append(args, dependency.Command.Entrypoint[1:]...)
		}
		args = append(args, dependency.Command.Args...)
	}
	if component.Role == contracts.WorkspaceApplicationComponentMain && len(input.Revision.Entrypoint) > 1 {
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

// localDockerApplicationResourceArgs bounds one container by its declared
// envelope. An undeclared envelope adds no flag, so an application that states
// nothing keeps the previous unbounded shape.
func localDockerApplicationResourceArgs(compute contracts.WorkspaceApplicationCompute) []string {
	args := []string{}
	if compute.CPULimitMilli > 0 {
		args = append(args, "--cpus", strconv.FormatFloat(float64(compute.CPULimitMilli)/1000, 'f', -1, 64))
	}
	if compute.MemoryLimitBytes > 0 {
		limit := strconv.FormatInt(compute.MemoryLimitBytes, 10)
		args = append(args, "--memory", limit, "--memory-swap", limit)
	}
	if compute.MemoryRequestBytes > 0 {
		args = append(args, "--memory-reservation", strconv.FormatInt(compute.MemoryRequestBytes, 10))
	}
	return args
}

// localDockerDependencyRunArgs returns Docker flags only: the component caller
// appends the image once, followed by its process arguments. Dependency flags
// do not inherit the main component's ports, environment, or mounts.
func localDockerDependencyRunArgs(input WorkspaceApplicationRuntimeInput, dependency contracts.WorkspaceApplicationDependency, storagePaths localDockerStoragePaths) ([]string, error) {
	args := []string{}
	for _, port := range dependency.Ports {
		if port.Protocol == "TCP" {
			args = append(args, "--expose", strconv.Itoa(port.Port))
		}
	}
	for name, value := range dependency.Command.Env {
		args = append(args, "--env", name+"="+value)
	}
	if len(dependency.Command.Entrypoint) > 0 {
		args = append(args, "--entrypoint", dependency.Command.Entrypoint[0])
	}
	for _, mount := range dependency.PersistentMounts {
		source := localDockerApplicationPersistentSource(input, storagePaths, contracts.WorkspaceApplicationMount{Name: mount.Name, MountPath: mount.MountPath, ReadOnly: mount.ReadOnly})
		if err := os.MkdirAll(source, 0755); err != nil {
			return nil, err
		}
		binding := "type=bind,source=" + source + ",target=" + mount.MountPath + ",bind-propagation=rprivate"
		if mount.ReadOnly {
			binding += ",readonly"
		}
		args = append(args, "--mount", binding)
	}
	for _, mount := range dependency.ScratchMounts {
		args = append(args, localDockerApplicationScratchArgs(mount)...)
	}
	return args, nil
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

const localDockerApplicationProbeScript = `
import net from 'node:net';
const checks = JSON.parse(process.argv[1]);
const results = await Promise.all(checks.map(async check => {
  try {
    if (check.type === 'tcp') {
      return await new Promise(resolve => {
        const socket = net.createConnection({host: '127.0.0.1', port: check.port});
        const finish = ready => { socket.destroy(); resolve(ready); };
        socket.setTimeout(5000);
        socket.once('connect', () => finish(true));
        socket.once('timeout', () => finish(false));
        socket.once('error', () => finish(false));
      });
    }
    if (check.type !== 'http') return false;
    const response = await fetch('http://127.0.0.1:' + check.port + check.path, { signal: AbortSignal.timeout(5000), redirect: 'manual' });
    const ready = response.status >= 200 && response.status < 400;
    await response.body?.cancel();
    return ready;
  } catch { return false; }
}));
process.stdout.write(JSON.stringify({ready: results.every(Boolean)}));
`

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
		args := []string{"exec", container.ID, check.Command[0], "-c", localDockerApplicationExecProbeScript, "opl-health"}
		args = append(args, check.Command...)
		output, runErr := p.runner.Run(probeCtx, nil, args...)
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
	output, runErr := p.runner.Run(probeCtx, nil, "run", "--pull", "never", "--name", probeName,
		"--network", "container:"+container.ID, "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--pids-limit", "64", "--memory", "128m", "--cpus", "0.25", "--user", "65534:65534",
		"--label", "opl.fabric.kind=application_probe", "--label", "opl.workspace.id="+input.WorkspaceID,
		"--entrypoint", "node", p.applicationProbeImage, "--input-type=module", "-e", localDockerApplicationProbeScript, string(checks))
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

func (p *LocalDockerProvider) removeApplicationProbe(ctx context.Context, workspaceID, name string) error {
	container, exists, err := p.inspectContainer(ctx, name)
	if err != nil || !exists {
		return err
	}
	if container.Config.Image != p.applicationProbeImage || container.Config.Labels["opl.fabric.kind"] != "application_probe" || container.Config.Labels["opl.workspace.id"] != workspaceID {
		return errors.New("local_docker_application_probe_owner_mismatch")
	}
	args := []string{"container", "rm"}
	if container.State.Running {
		args = append(args, "--force")
	}
	args = append(args, name)
	if _, err := p.runner.Run(ctx, nil, args...); err != nil {
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
	if input.Revision.RuntimeProfile != "opl_app" || p.runtimeGatewayContainer == "" {
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
