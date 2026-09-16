package contracts

import (
	"errors"
	"strings"
)

// WorkspaceApplicationDependencyPort declares one TCP/UDP port a dependency
// component serves inside the application network. Unlike the main entry
// port, dependency ports are never published externally.
type WorkspaceApplicationDependencyPort struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

// WorkspaceApplicationDependencyHealthCheck probes one dependency over TCP
// or HTTP, or executes an explicit argv inside the component. Every declared
// check must pass before the component is ready.
type WorkspaceApplicationDependencyHealthCheck struct {
	Command             []string `json:"command,omitempty"`
	Type                string   `json:"type"` // tcp | http | exec
	Port                int      `json:"port"`
	Path                string   `json:"path,omitempty"`
	InitialDelaySeconds int      `json:"initialDelaySeconds,omitempty"`
}

// WorkspaceApplicationDependencyMount declares a volume mount for one
// dependency component. Named mounts share the application's data namespace:
// the same WorkspaceApplicationDataDirectory layout as main persistent mounts.
type WorkspaceApplicationDependencyMount = WorkspaceApplicationMount

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
	if err := ValidateWorkspaceApplicationExecution(dependency.Execution); err != nil {
		return err
	}
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
		if check.InitialDelaySeconds < 0 {
			return errors.New("workspace_application_dependency_health_check_invalid")
		}
		if check.Type == "exec" {
			if check.Port != 0 || check.Path != "" || len(check.Command) == 0 || len(check.Command) > 32 {
				return errors.New("workspace_application_dependency_health_check_invalid")
			}
			for i, arg := range check.Command {
				if len(arg) > 4096 || strings.ContainsRune(arg, 0) || i == 0 && arg == "" {
					return errors.New("workspace_application_dependency_health_check_invalid")
				}
			}
			continue
		}
		if len(check.Command) != 0 || (check.Type != "tcp" && check.Type != "http") || check.Port < 1 || check.Port > 65535 {
			return errors.New("workspace_application_dependency_health_check_invalid")
		}
		if check.Type == "http" && !strings.HasPrefix(check.Path, "/") || check.Type == "tcp" && check.Path != "" {
			return errors.New("workspace_application_dependency_health_check_invalid")
		}
	}

	if err := ValidateWorkspaceApplicationCompute(dependency.Compute); err != nil {
		return err
	}
	if err := ValidateWorkspaceApplicationMountOptions(dependency.PersistentMounts, dependency.ScratchMounts); err != nil {
		return err
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
		if !ValidWorkspaceApplicationEnvironmentName(name) {
			return errors.New("workspace_application_dependency_env_invalid")
		}
	}
	if err := validateWorkspaceApplicationInputs(dependency.SecretInputs, dependency.ConfigInputs, dependency.Command.Env, append(append([]WorkspaceApplicationMount{}, dependency.PersistentMounts...), dependency.ScratchMounts...)); err != nil {
		return err
	}
	return nil
}
