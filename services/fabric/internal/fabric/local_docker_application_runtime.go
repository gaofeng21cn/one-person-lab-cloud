package fabric

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
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
	if err := p.validateLocalDockerApplicationRuntimeIdentity(input, compute); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if err := p.validateLocalDockerApplicationProbe(input.Revision); err != nil {
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
	ensureErr := error(nil)
	for _, component := range components {
		state, componentErr := p.ensureWorkspaceApplicationComponent(ctx, input, compute, network, storagePaths, component)
		observed = append(observed, state)
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
	if ensureErr != nil {
		return observation, ensureErr
	}
	return p.ReadWorkspaceApplicationRuntime(ctx, input)
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
	runtimeID := workspaceApplicationRuntimeID(input.WorkspaceID)
	components := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)
	observed := make([]contracts.WorkspaceApplicationRuntimeComponentState, 0, len(components))
	entryURL := ""
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
		if input.AccountID == "" || !exactDockerLabels(container.Config.Labels, localDockerApplicationLabels(input, input.AccountID, component)) || container.Config.Image != component.Image {
			return contracts.WorkspaceApplicationRuntimeObservation{}, errors.New("local_docker_application_component_conflict")
		}
		entryPort := 0
		if component.Role == contracts.WorkspaceApplicationComponentMain {
			entryPort = localDockerApplicationEntryPort(input.Revision)
		}
		for port, bindings := range container.NetworkSettings.Ports {
			if len(bindings) > 0 && (entryPort == 0 || port != strconv.Itoa(entryPort)+"/tcp") {
				return contracts.WorkspaceApplicationRuntimeObservation{}, errors.New("local_docker_application_component_conflict")
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
					return contracts.WorkspaceApplicationRuntimeObservation{}, probeErr
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
					return contracts.WorkspaceApplicationRuntimeObservation{}, entryErr
				}
			}
		}
		observed = append(observed, state)
	}
	status := contracts.WorkspaceApplicationRuntimeOverallStatus(observed)
	if status != "ready" {
		entryURL = ""
	}
	return contracts.WorkspaceApplicationRuntimeObservation{
		SchemaVersion: 1, WorkspaceID: input.WorkspaceID, RuntimeID: runtimeID,
		Status: status, EntryURL: entryURL, Components: observed,
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
	args = append(args, "--network", network, "--platform", input.Revision.Platform)
	if component.Role == contracts.WorkspaceApplicationComponentMain {
		if port := localDockerApplicationEntryPort(input.Revision); port != 0 {
			args = append(args, "-p", p.publishHost+"::"+strconv.Itoa(port)+"/tcp")
		}
		for _, mount := range input.Revision.PersistentMounts {
			source := filepath.Join(storagePaths.Data, mount.Name)
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
			args = append(args, "--mount", "type=tmpfs,target="+mount.MountPath+",tmpfs-mode=0755")
		}
		if len(input.Revision.Entrypoint) > 0 {
			args = append(args, "--entrypoint", input.Revision.Entrypoint[0])
		}
	}
	args = append(args, component.Image)
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
	return map[string]string{
		"opl.fabric.provider": "local-docker", "opl.fabric.kind": "application_runtime",
		"opl.account.id": accountID, "opl.workspace.id": input.WorkspaceID,
		"opl.runtime.id":     workspaceApplicationRuntimeID(input.WorkspaceID),
		"opl.component.name": component.Name, "opl.component.role": component.Role,
		"opl.image.ref": component.Image, "opl.configuration.digest": input.ConfigurationDigest,
	}
}

func (p *LocalDockerProvider) validateLocalDockerApplicationProbe(revision contracts.WorkspaceApplicationRevision) error {
	if len(revision.HealthChecks) > 0 && !contracts.ValidWorkspaceImageReference(p.applicationProbeImage) {
		return errors.New("local_docker_application_probe_image_required")
	}
	return nil
}

const localDockerApplicationProbeScript = `
const checks = JSON.parse(process.argv[1]);
const results = await Promise.all(checks.map(async check => {
  try {
    const response = await fetch('http://127.0.0.1:' + check.port + check.path, { signal: AbortSignal.timeout(5000), redirect: 'manual' });
    const ready = response.status >= 200 && response.status < 400;
    await response.body?.cancel();
    return ready;
  } catch { return false; }
}));
process.stdout.write(JSON.stringify({ready: results.every(Boolean)}));
`

func (p *LocalDockerProvider) probeLocalDockerApplication(ctx context.Context, input WorkspaceApplicationRuntimeInput, container dockerContainerInspect) (bool, error) {
	startedAt, err := time.Parse(time.RFC3339Nano, container.State.StartedAt)
	if err != nil {
		return false, errors.New("local_docker_application_started_at_invalid")
	}
	for _, check := range input.Revision.HealthChecks {
		if p.now().Before(startedAt.Add(time.Duration(check.InitialDelaySeconds) * time.Second)) {
			return false, nil
		}
	}
	checks, err := json.Marshal(input.Revision.HealthChecks)
	if err != nil {
		return false, err
	}
	probeName := "opl-app-probe-" + stableSuffix(input.WorkspaceID, container.ID, time.Now().UTC().Format(time.RFC3339Nano))[:24]
	// The Docker invocation includes container startup and teardown; each HTTP
	// request has its separate five-second deadline inside the probe.
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	output, runErr := p.runner.Run(probeCtx, nil, "run", "--rm", "--pull", "never", "--name", probeName,
		"--network", "container:"+container.ID, "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--pids-limit", "64", "--memory", "128m", "--cpus", "0.25", "--user", "65534:65534",
		"--label", "opl.fabric.kind=application_probe", "--label", "opl.workspace.id="+input.WorkspaceID,
		"--entrypoint", "node", p.applicationProbeImage, "--input-type=module", "-e", localDockerApplicationProbeScript, string(checks))
	if runErr != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, exists, inspectErr := p.inspectContainer(cleanupCtx, probeName)
		if inspectErr != nil {
			return false, errors.Join(runErr, inspectErr)
		}
		if exists {
			_, cleanupErr := p.runner.Run(cleanupCtx, nil, "container", "rm", "--force", probeName)
			return false, errors.Join(runErr, cleanupErr)
		}
		return false, runErr
	}
	var result struct {
		Ready *bool `json:"ready"`
	}
	if err := json.Unmarshal(output, &result); err != nil || result.Ready == nil {
		return false, errors.New("local_docker_application_probe_result_invalid")
	}
	return *result.Ready, nil
}
