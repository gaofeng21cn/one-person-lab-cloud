package contracts

import (
	"errors"
	"regexp"
	"strings"
)

var (
	workspaceApplicationIDPattern       = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	workspaceApplicationVersionPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
	workspaceApplicationPlatformPattern = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9._-]+$`)
	workspaceApplicationPortNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,14}$`)
)

// WorkspaceApplicationRevision is the immutable, provider-neutral description
// admitted before a Workspace deployment. Image references are digest-pinned;
// tags are discovery input and are not persisted here.
type WorkspaceApplicationRevision struct {
	SchemaVersion    int                               `json:"schemaVersion"`
	ApplicationID    string                            `json:"applicationId"`
	Version          string                            `json:"version"`
	Platform         string                            `json:"platform"`
	Image            string                            `json:"image"`
	Entrypoint       []string                          `json:"entrypoint,omitempty"`
	Ports            []WorkspaceApplicationPort        `json:"ports,omitempty"`
	HealthChecks     []WorkspaceApplicationHealthCheck `json:"healthChecks,omitempty"`
	Resources        WorkspaceApplicationResources     `json:"resources"`
	PersistentMounts []WorkspaceApplicationMount       `json:"persistentMounts,omitempty"`
	ScratchMounts    []WorkspaceApplicationMount       `json:"scratchMounts,omitempty"`
	SecretInputs     []WorkspaceApplicationSecretInput `json:"secretInputs,omitempty"`
	Dependencies     []WorkspaceApplicationDependency  `json:"dependencies,omitempty"`
	ExposurePolicy   string                            `json:"exposurePolicy"`
}

type WorkspaceApplicationPort struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

type WorkspaceApplicationHealthCheck struct {
	Port                int    `json:"port"`
	Path                string `json:"path"`
	InitialDelaySeconds int    `json:"initialDelaySeconds,omitempty"`
}

type WorkspaceApplicationResources struct {
	CPU      int `json:"cpu"`
	MemoryGB int `json:"memoryGb"`
}

type WorkspaceApplicationMount struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
	ReadOnly  bool   `json:"readOnly,omitempty"`
}

type WorkspaceApplicationSecretInput struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}

type WorkspaceApplicationDependency struct {
	Name  string `json:"name"`
	Image string `json:"image"`
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
	if revision.SchemaVersion != 1 || !workspaceApplicationIDPattern.MatchString(strings.TrimSpace(revision.ApplicationID)) ||
		!workspaceApplicationVersionPattern.MatchString(strings.TrimSpace(revision.Version)) ||
		!workspaceApplicationPlatformPattern.MatchString(strings.TrimSpace(revision.Platform)) ||
		!ValidWorkspaceImageReference(revision.Image) || revision.Resources.CPU <= 0 || revision.Resources.MemoryGB <= 0 {
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
		if check.Port < 1 || check.Port > 65535 || !strings.HasPrefix(check.Path, "/") {
			return errors.New("workspace_application_health_check_invalid")
		}
	}
	for _, secret := range revision.SecretInputs {
		if strings.TrimSpace(secret.Name) == "" || strings.TrimSpace(secret.Target) == "" {
			return errors.New("workspace_application_secret_input_invalid")
		}
	}
	for _, dependency := range revision.Dependencies {
		if strings.TrimSpace(dependency.Name) == "" || !ValidWorkspaceImageReference(dependency.Image) {
			return errors.New("workspace_application_dependency_invalid")
		}
	}
	return nil
}
