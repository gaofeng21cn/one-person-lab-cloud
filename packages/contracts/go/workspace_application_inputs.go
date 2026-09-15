package contracts

import (
	"errors"
	"path"
	"regexp"
	"strings"
)

var workspaceApplicationInputNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,127}$`)

// WorkspaceApplicationSecretInputs enumerates consumers, including bindings
// shared across components. Values never enter the declaration.
func WorkspaceApplicationSecretInputs(revision WorkspaceApplicationRevision) []WorkspaceApplicationSecretInput {
	inputs := append([]WorkspaceApplicationSecretInput{}, revision.SecretInputs...)
	for _, component := range revision.Dependencies {
		inputs = append(inputs, component.SecretInputs...)
	}
	return inputs
}

func WorkspaceApplicationConfigInputs(revision WorkspaceApplicationRevision) []WorkspaceApplicationConfigInput {
	inputs := append([]WorkspaceApplicationConfigInput{}, revision.ConfigInputs...)
	for _, component := range revision.Dependencies {
		inputs = append(inputs, component.ConfigInputs...)
	}
	return inputs
}

func validWorkspaceApplicationInputTarget(target string) bool {
	return target != "/" && strings.HasPrefix(target, "/") && path.Clean(target) == target && !strings.ContainsAny(target, "\x00\r\n,")
}

func validateWorkspaceApplicationInputs(secrets []WorkspaceApplicationSecretInput, configs []WorkspaceApplicationConfigInput, environment map[string]string, mounts []WorkspaceApplicationMount) error {
	names, targets, envs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	// A component has one mount namespace. Directory mounts may contain file
	// inputs, but a file can never be another mount's directory or exact target.
	for _, mount := range mounts {
		if _, exists := targets[mount.MountPath]; exists {
			return errors.New("workspace_application_mount_target_conflict")
		}
		targets[mount.MountPath] = false
	}
	fileTargetAvailable := func(target string) bool {
		for existing, isFile := range targets {
			if target == existing || strings.HasPrefix(existing, target+"/") || isFile && strings.HasPrefix(target, existing+"/") {
				return false
			}
		}
		return true
	}
	for _, input := range secrets {
		if !workspaceApplicationInputNamePattern.MatchString(input.Name) || names[input.Name] || (input.Target == "") == (input.Env == "") {
			return errors.New("workspace_application_secret_input_invalid")
		}
		names[input.Name] = true
		if input.Target != "" {
			if !validWorkspaceApplicationInputTarget(input.Target) || !fileTargetAvailable(input.Target) {
				return errors.New("workspace_application_secret_target_invalid")
			}
			targets[input.Target] = true
		} else {
			_, configured := environment[input.Env]
			if !ValidWorkspaceApplicationEnvironmentName(input.Env) || envs[input.Env] || configured {
				return errors.New("workspace_application_secret_env_invalid")
			}
			envs[input.Env] = true
		}
	}
	names = map[string]bool{}
	for _, input := range configs {
		if !workspaceApplicationInputNamePattern.MatchString(input.Name) || names[input.Name] || !validWorkspaceApplicationInputTarget(input.Target) || !fileTargetAvailable(input.Target) {
			return errors.New("workspace_application_config_input_invalid")
		}
		names[input.Name], targets[input.Target] = true, true
	}
	return nil
}
