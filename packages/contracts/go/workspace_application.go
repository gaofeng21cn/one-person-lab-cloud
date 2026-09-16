package contracts

import (
	"errors"
	"regexp"
	"strings"
)

var (
	workspaceApplicationIDPattern            = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	workspaceApplicationVersionPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
	workspaceApplicationPlatformPattern      = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9._-]+$`)
	workspaceApplicationPortNamePattern      = regexp.MustCompile(`^[a-z][a-z0-9-]{0,14}$`)
	workspaceApplicationComponentNamePattern = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// WorkspaceApplicationRevision is the immutable, provider-neutral description
// admitted before a Workspace deployment. Image references are digest-pinned;
// tags are discovery input and are not persisted here.
type WorkspaceApplicationRevision struct {
	Execution     WorkspaceApplicationExecution `json:"execution,omitzero"`
	SchemaVersion int                           `json:"schemaVersion"`
	ApplicationID string                        `json:"applicationId"`
	Version       string                        `json:"version"`
	Platform      string                        `json:"platform"`
	Image         string                        `json:"image"`
	// RuntimeProfile explicitly opts an admitted application into the OPL App
	// authentication and Gateway ABI. An ordinary application receives no such credentials.
	RuntimeProfile string                     `json:"runtimeProfile,omitempty"`
	Entrypoint     []string                   `json:"entrypoint,omitempty"`
	Ports          []WorkspaceApplicationPort `json:"ports,omitempty"`
	// EntryPort names the declared TCP port serving the application's HTTP root.
	// An empty name declares no web entry; health probes do not publish a port.
	EntryPort        string                            `json:"entryPort,omitempty"`
	HealthChecks     []WorkspaceApplicationHealthCheck `json:"healthChecks,omitempty"`
	PersistentMounts []WorkspaceApplicationMount       `json:"persistentMounts,omitempty"`
	ScratchMounts    []WorkspaceApplicationMount       `json:"scratchMounts,omitempty"`
	SecretInputs     []WorkspaceApplicationSecretInput `json:"secretInputs,omitempty"`
	ConfigInputs     []WorkspaceApplicationConfigInput `json:"configInputs,omitempty"`
	Dependencies     []WorkspaceApplicationDependency  `json:"dependencies,omitempty"`
	ExposurePolicy   string                            `json:"exposurePolicy"`
	Compute          WorkspaceApplicationCompute       `json:"compute,omitzero"`
}

// WorkspaceApplicationCompute is the resource envelope one component declares
// for itself. Requests drive scheduling and the target-node feasibility check;
// limits bound the component so one runaway process cannot consume its
// siblings. Both are required facts for hosting an application whose working
// set is larger than a single process.
type WorkspaceApplicationCompute struct {
	CPURequestMilli    int64 `json:"cpuRequestMilli,omitempty"`
	CPULimitMilli      int64 `json:"cpuLimitMilli,omitempty"`
	MemoryRequestBytes int64 `json:"memoryRequestBytes,omitempty"`
	MemoryLimitBytes   int64 `json:"memoryLimitBytes,omitempty"`
}

// Declared reports whether the component states any resource requirement.
// An undeclared envelope is admitted for compatibility, but it schedules as
// BestEffort and cannot be checked against a target node.
func (compute WorkspaceApplicationCompute) Declared() bool {
	return compute.CPURequestMilli != 0 || compute.CPULimitMilli != 0 ||
		compute.MemoryRequestBytes != 0 || compute.MemoryLimitBytes != 0
}

const (
	workspaceApplicationCPUCeilingMilli    = 128_000
	workspaceApplicationMemoryCeilingBytes = 1 << 40
)

// ValidateWorkspaceApplicationCompute rejects an envelope that cannot be
// scheduled: a negative value, a request above its own limit, or a value beyond
// what one workspace component may claim.
func ValidateWorkspaceApplicationCompute(compute WorkspaceApplicationCompute) error {
	values := []int64{compute.CPURequestMilli, compute.CPULimitMilli, compute.MemoryRequestBytes, compute.MemoryLimitBytes}
	for _, value := range values {
		if value < 0 {
			return errors.New("workspace_application_compute_invalid")
		}
	}
	if compute.CPURequestMilli > workspaceApplicationCPUCeilingMilli || compute.CPULimitMilli > workspaceApplicationCPUCeilingMilli ||
		compute.MemoryRequestBytes > workspaceApplicationMemoryCeilingBytes || compute.MemoryLimitBytes > workspaceApplicationMemoryCeilingBytes {
		return errors.New("workspace_application_compute_exceeds_ceiling")
	}
	if compute.CPURequestMilli > 0 && compute.CPULimitMilli > 0 && compute.CPURequestMilli > compute.CPULimitMilli {
		return errors.New("workspace_application_compute_request_above_limit")
	}
	if compute.MemoryRequestBytes > 0 && compute.MemoryLimitBytes > 0 && compute.MemoryRequestBytes > compute.MemoryLimitBytes {
		return errors.New("workspace_application_compute_request_above_limit")
	}
	return nil
}

type WorkspaceApplicationPort struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

// WorkspaceApplicationEntryPort resolves the explicitly selected HTTP port.
// Exposure policy is enforced by the provider at its publication boundary.
func WorkspaceApplicationEntryPort(revision WorkspaceApplicationRevision) (WorkspaceApplicationPort, bool) {
	for _, port := range revision.Ports {
		if revision.EntryPort != "" && port.Name == revision.EntryPort && port.Protocol == "TCP" {
			return port, true
		}
	}
	return WorkspaceApplicationPort{}, false
}

type WorkspaceApplicationHealthCheck struct {
	Port                int    `json:"port"`
	Path                string `json:"path"`
	InitialDelaySeconds int    `json:"initialDelaySeconds,omitempty"`
}

type WorkspaceApplicationMount struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
	ReadOnly  bool   `json:"readOnly,omitempty"`
	// Scratch options are explicit; persistent data permissions are never widened.
	Mode       *uint32 `json:"mode,omitempty"`
	UserID     *int64  `json:"userId,omitempty"`
	GroupID    *int64  `json:"groupId,omitempty"`
	SizeBytes  int64   `json:"sizeBytes,omitempty"`
	Executable bool    `json:"executable,omitempty"`
}

type WorkspaceApplicationSecretInput struct {
	Name   string `json:"name"`
	Target string `json:"target,omitempty"`
	Env    string `json:"env,omitempty"`
}

// WorkspaceApplicationConfigInput names a non-secret file supplied by Configuration.Files.
type WorkspaceApplicationConfigInput struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}

type WorkspaceApplicationDependency struct {
	Execution        WorkspaceApplicationExecution               `json:"execution,omitzero"`
	DependsOn        []string                                    `json:"dependsOn,omitempty"`
	Name             string                                      `json:"name"`
	Image            string                                      `json:"image"`
	Ports            []WorkspaceApplicationDependencyPort        `json:"ports,omitempty"`
	HealthChecks     []WorkspaceApplicationDependencyHealthCheck `json:"healthChecks,omitempty"`
	PersistentMounts []WorkspaceApplicationDependencyMount       `json:"persistentMounts,omitempty"`
	ScratchMounts    []WorkspaceApplicationDependencyMount       `json:"scratchMounts,omitempty"`
	Command          WorkspaceApplicationDependencyCommand       `json:"command,omitempty"`
	SecretInputs     []WorkspaceApplicationSecretInput           `json:"secretInputs,omitempty"`
	ConfigInputs     []WorkspaceApplicationConfigInput           `json:"configInputs,omitempty"`
	Compute          WorkspaceApplicationCompute                 `json:"compute,omitzero"`
}

// WorkspaceApplicationDeployment is the immutable cross-owner intent for one
// application operation. Provider identities and secrets remain outside this
// contract; Fabric returns those facts through its observation contract.
type WorkspaceApplicationDeployment struct {
	SchemaVersion            int      `json:"schemaVersion"`
	OperationID              string   `json:"operationId"`
	WorkspaceID              string   `json:"workspaceId"`
	ApplicationID            string   `json:"applicationId"`
	TargetRevision           string   `json:"targetRevision"`
	PreviousApplicationID    string   `json:"previousApplicationId,omitempty"`
	PreviousRevision         string   `json:"previousRevision,omitempty"`
	ConfigurationDigest      string   `json:"configurationDigest"`
	SecretBindingVersions    []string `json:"secretBindingVersions,omitempty"`
	DataBindingIDs           []string `json:"dataBindingIds,omitempty"`
	ExpectedWorkspaceVersion int64    `json:"expectedWorkspaceVersion"`
	IdempotencyKey           string   `json:"idempotencyKey"`
}

func ValidateWorkspaceApplicationDeployment(deployment WorkspaceApplicationDeployment) error {
	if deployment.SchemaVersion != 1 || strings.TrimSpace(deployment.OperationID) == "" ||
		!workspaceApplicationIDPattern.MatchString(strings.TrimSpace(deployment.WorkspaceID)) ||
		!workspaceApplicationIDPattern.MatchString(strings.TrimSpace(deployment.ApplicationID)) ||
		!workspaceApplicationVersionPattern.MatchString(strings.TrimSpace(deployment.TargetRevision)) ||
		strings.TrimSpace(deployment.ConfigurationDigest) == "" ||
		deployment.ExpectedWorkspaceVersion < 0 || strings.TrimSpace(deployment.IdempotencyKey) == "" {
		return errors.New("workspace_application_deployment_invalid")
	}
	if deployment.PreviousApplicationID != "" && !workspaceApplicationIDPattern.MatchString(deployment.PreviousApplicationID) {
		return errors.New("workspace_application_previous_application_invalid")
	}
	if deployment.PreviousRevision != "" && !workspaceApplicationVersionPattern.MatchString(deployment.PreviousRevision) {
		return errors.New("workspace_application_previous_revision_invalid")
	}
	seen := map[string]struct{}{}
	for _, binding := range append(append([]string{}, deployment.SecretBindingVersions...), deployment.DataBindingIDs...) {
		if strings.TrimSpace(binding) == "" {
			return errors.New("workspace_application_binding_invalid")
		}
		if _, found := seen[binding]; found {
			return errors.New("workspace_application_binding_duplicate")
		}
		seen[binding] = struct{}{}
	}
	return nil
}

func ValidateWorkspaceApplicationRevision(revision WorkspaceApplicationRevision) error {
	if err := ValidateWorkspaceApplicationExecution(revision.Execution); err != nil {
		return err
	}
	if revision.RuntimeProfile == "opl_app" && (revision.Execution.UserID != nil && *revision.Execution.UserID != 10001 || revision.Execution.GroupID != nil && *revision.Execution.GroupID != 10001) {
		return errors.New("workspace_application_opl_identity_invalid")
	}
	if revision.RuntimeProfile != "" && revision.RuntimeProfile != "opl_app" {
		return errors.New("workspace_application_runtime_profile_invalid")
	}
	if revision.SchemaVersion != 1 || !workspaceApplicationIDPattern.MatchString(strings.TrimSpace(revision.ApplicationID)) ||
		!workspaceApplicationVersionPattern.MatchString(strings.TrimSpace(revision.Version)) ||
		!workspaceApplicationPlatformPattern.MatchString(strings.TrimSpace(revision.Platform)) ||
		!ValidWorkspaceImageReference(revision.Image) {
		return errors.New("workspace_application_revision_invalid")
	}
	if revision.ExposurePolicy != "anonymous" && revision.ExposurePolicy != "application" && revision.ExposurePolicy != "cloud_private" {
		return errors.New("workspace_application_exposure_policy_invalid")
	}
	seenPorts := map[string]struct{}{}
	for _, port := range revision.Ports {
		if !workspaceApplicationPortNamePattern.MatchString(port.Name) || port.Port < 1 || port.Port > 65535 ||
			(port.Protocol != "TCP" && port.Protocol != "UDP") {
			return errors.New("workspace_application_port_invalid")
		}
		if _, found := seenPorts[port.Name]; found {
			return errors.New("workspace_application_port_duplicate")
		}
		seenPorts[port.Name] = struct{}{}
	}
	if revision.EntryPort != "" {
		if _, found := WorkspaceApplicationEntryPort(revision); !found {
			return errors.New("workspace_application_entry_port_invalid")
		}
	}
	if err := ValidateWorkspaceApplicationCompute(revision.Compute); err != nil {
		return err
	}
	if err := ValidateWorkspaceApplicationMountOptions(revision.PersistentMounts, revision.ScratchMounts); err != nil {
		return err
	}
	seenMounts := map[string]struct{}{}
	for _, mount := range append(append([]WorkspaceApplicationMount{}, revision.PersistentMounts...), revision.ScratchMounts...) {
		if strings.TrimSpace(mount.Name) == "" || !strings.HasPrefix(mount.MountPath, "/") || strings.Contains(mount.MountPath, "..") {
			return errors.New("workspace_application_mount_invalid")
		}
		if _, found := seenMounts[mount.Name]; found {
			return errors.New("workspace_application_mount_duplicate")
		}
		seenMounts[mount.Name] = struct{}{}
	}
	for _, check := range revision.HealthChecks {
		if check.Port < 1 || check.Port > 65535 || !strings.HasPrefix(check.Path, "/") || check.InitialDelaySeconds < 0 {
			return errors.New("workspace_application_health_check_invalid")
		}
	}
	if err := validateWorkspaceApplicationInputs(revision.SecretInputs, revision.ConfigInputs, nil, append(append([]WorkspaceApplicationMount{}, revision.PersistentMounts...), revision.ScratchMounts...)); err != nil {
		return err
	}

	seenComponents := map[string]struct{}{WorkspaceApplicationComponentMain: {}}
	for _, dependency := range revision.Dependencies {
		if !workspaceApplicationComponentNamePattern.MatchString(dependency.Name) || !ValidWorkspaceImageReference(dependency.Image) {
			return errors.New("workspace_application_dependency_invalid")
		}
		if _, found := seenComponents[dependency.Name]; found {
			return errors.New("workspace_application_dependency_duplicate")
		}
		seenComponents[dependency.Name] = struct{}{}
		if err := ValidateWorkspaceApplicationDependency(dependency); err != nil {
			return err
		}
	}
	if _, err := WorkspaceApplicationStartupOrder(revision); err != nil {
		return err
	}
	return nil
}
