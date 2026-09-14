package contracts

import (
	"errors"
	"regexp"
	"strings"
)

var workspaceApplicationEnvNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// WorkspaceApplicationDependencyPort declares one TCP/UDP port a dependency
// component serves inside the application network. Unlike the main entry
// port, dependency ports are never published externally.
type WorkspaceApplicationDependencyPort struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

// WorkspaceApplicationDependencyHealthCheck probes one dependency over TCP
// or HTTP. The first supported check gates the component's ready state.
type WorkspaceApplicationDependencyHealthCheck struct {
	Type                string `json:"type"` // tcp | http
	Port                int    `json:"port"`
	Path                string `json:"path,omitempty"`
	InitialDelaySeconds int    `json:"initialDelaySeconds,omitempty"`
}

// WorkspaceApplicationDependencyMount declares a volume mount for one
// dependency component. Named mounts share the application's data namespace:
// the same WorkspaceApplicationDataDirectory layout as main persistent mounts.
type WorkspaceApplicationDependencyMount struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
	ReadOnly  bool   `json:"readOnly,omitempty"`
}

// WorkspaceApplicationDependencyCommand is the exact process spec of one
// dependency component. An empty command runs the image's default entrypoint.
type WorkspaceApplicationDependencyCommand struct {
	Entrypoint []string          `json:"entrypoint,omitempty"`
	Args       []string          `json:"args,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
}

// ValidateWorkspaceApplicationDependency validates one dependency spec. It
// mirrors the main-component rules so every component obeys the same bounds.
func ValidateWorkspaceApplicationDependency(dependency WorkspaceApplicationDependency) error {
	if !workspaceApplicationComponentNamePattern.MatchString(dependency.Name) {
		return errors.New("workspace_application_dependency_invalid")
	}
	if !ValidWorkspaceImageReference(dependency.Image) {
		return errors.New("workspace_application_dependency_invalid")
	}
	seenPorts := map[string]struct{}{}
	portValues := map[int]struct{}{}
	for _, port := range dependency.Ports {
		if !workspaceApplicationPortNamePattern.MatchString(port.Name) || port.Port < 1 || port.Port > 65535 ||
			(port.Protocol != "TCP" && port.Protocol != "UDP") {
			return errors.New("workspace_application_dependency_port_invalid")
		}
		if _, found := seenPorts[port.Name]; found {
			return errors.New("workspace_application_dependency_port_duplicate")
		}
		if _, found := portValues[port.Port]; found {
			return errors.New("workspace_application_dependency_port_conflict")
		}
		seenPorts[port.Name] = struct{}{}
		portValues[port.Port] = struct{}{}
	}
	for _, check := range dependency.HealthChecks {
		if (check.Type != "tcp" && check.Type != "http") || check.Port < 1 || check.Port > 65535 || check.InitialDelaySeconds < 0 {
			return errors.New("workspace_application_dependency_health_check_invalid")
		}
		if check.Type == "http" && !regexp.MustCompile(`^/`).MatchString(check.Path) {
			return errors.New("workspace_application_dependency_health_check_invalid")
		}
		if check.Type == "tcp" && check.Path != "" {
			return errors.New("workspace_application_dependency_health_check_invalid")
		}
	}
	seenMounts := map[string]struct{}{}
	for _, mount := range append(append([]WorkspaceApplicationDependencyMount{}, dependency.PersistentMounts...), dependency.ScratchMounts...) {
		if mount.Name == "" || !strings.HasPrefix(mount.MountPath, "/") || strings.Contains(mount.MountPath, "..") {
			return errors.New("workspace_application_dependency_mount_invalid")
		}
		if _, found := seenMounts[mount.Name]; found {
			return errors.New("workspace_application_dependency_mount_duplicate")
		}
		seenMounts[mount.Name] = struct{}{}
	}
	for name := range dependency.Command.Env {
		if !workspaceApplicationEnvNamePattern.MatchString(name) {
			return errors.New("workspace_application_dependency_env_invalid")
		}
	}
	seenSecrets := map[string]struct{}{}
	for _, secret := range dependency.SecretInputs {
		if secret.Name == "" || secret.Target == "" {
			return errors.New("workspace_application_dependency_secret_input_invalid")
		}
		if _, found := seenSecrets[secret.Name]; found {
			return errors.New("workspace_application_dependency_secret_input_duplicate")
		}
		seenSecrets[secret.Name] = struct{}{}
	}
	return nil
}
