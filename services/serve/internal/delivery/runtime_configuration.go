package delivery

import (
	"path"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
)

// validateRuntimeConfiguration admits only bindings for slots declared by the
// immutable application descriptor. Values never cross this boundary: every
// accepted input is an owner-scoped Fabric handle.
func validateRuntimeConfiguration(command *api.RuntimeDeployCommand) error {
	descriptor := command.GetDeploymentDescriptor()
	revision := descriptor.GetApplicationRevision()
	configuration := command.GetRuntimeConfiguration()
	if revision == nil {
		if configuration != nil && (len(configuration.GetConfigBindings()) > 0 || len(configuration.GetSecretBindings()) > 0 || len(configuration.GetMountBindings()) > 0) {
			return status.Error(codes.InvalidArgument, "runtime configuration has no declared application revision")
		}
		return nil
	}
	if configuration == nil {
		configuration = &api.WorkspaceApplicationRuntimeConfiguration{}
	}
	seen := map[string]bool{}
	key := func(kind, name, target, env string) error {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(target) == "" || seen[kind+"\x00"+name+"\x00"+target+"\x00"+env] {
			return status.Error(codes.InvalidArgument, "runtime configuration contains a duplicate or incomplete slot")
		}
		seen[kind+"\x00"+name+"\x00"+target+"\x00"+env] = true
		return nil
	}
	for _, binding := range configuration.GetConfigBindings() {
		if err := key("config", binding.GetInputName(), binding.GetTarget(), binding.GetEnv()); err != nil {
			return err
		}
		declared := false
		for _, input := range revision.GetConfigInputs() {
			if input.GetName() == binding.GetInputName() && input.GetTarget() == binding.GetTarget() && binding.GetEnv() == "" {
				declared = true
			}
		}
		if !declared || !validHandle(command, binding.GetHandle(), api.RuntimeInjectionHandle_CONFIG) {
			return status.Error(codes.FailedPrecondition, "runtime config binding is not declared or its handle is invalid")
		}
	}
	for _, binding := range configuration.GetSecretBindings() {
		if err := key("secret", binding.GetInputName(), binding.GetTarget(), binding.GetEnv()); err != nil {
			return err
		}
		declared := false
		for _, input := range revision.GetSecretInputs() {
			if input.GetName() == binding.GetInputName() && input.GetTarget() == binding.GetTarget() && input.GetEnv() == binding.GetEnv() {
				declared = true
			}
		}
		if !declared || binding.GetSecretBindingId() == "" || binding.GetFingerprint() == "" || !validHandle(command, binding.GetHandle(), api.RuntimeInjectionHandle_SECRET) || binding.GetFingerprint() != binding.GetHandle().GetFingerprint() {
			return status.Error(codes.FailedPrecondition, "runtime secret binding is not declared or its handle is invalid")
		}
	}
	for _, binding := range configuration.GetMountBindings() {
		if err := key("mount", binding.GetMountName(), binding.GetTarget(), binding.GetAccessMode()); err != nil {
			return err
		}
		declared := false
		kind := api.RuntimeInjectionHandle_KIND_UNSPECIFIED
		for _, input := range append(append([]*api.WorkspaceApplicationMount{}, revision.GetPersistentMounts()...), revision.GetScratchMounts()...) {
			if input.GetName() == binding.GetMountName() && input.GetMountPath() == binding.GetTarget() {
				declared = true
				if input.GetReadOnly() {
					if binding.GetAccessMode() != "read_only" {
						return status.Error(codes.FailedPrecondition, "mount access mode differs from descriptor")
					}
				} else if binding.GetAccessMode() != "read_write" {
					return status.Error(codes.FailedPrecondition, "mount access mode differs from descriptor")
				}
			}
		}
		for _, input := range revision.GetPersistentMounts() {
			if input.GetName() == binding.GetMountName() && input.GetMountPath() == binding.GetTarget() {
				kind = api.RuntimeInjectionHandle_DATA_MOUNT
			}
		}
		for _, input := range revision.GetScratchMounts() {
			if input.GetName() == binding.GetMountName() && input.GetMountPath() == binding.GetTarget() {
				kind = api.RuntimeInjectionHandle_SCRATCH_MOUNT
			}
		}
		if !declared || !validHandle(command, binding.GetHandle(), kind) {
			return status.Error(codes.FailedPrecondition, "runtime mount binding is not declared or its handle is invalid")
		}
	}
	for _, input := range revision.GetConfigInputs() {
		if !hasConfig(configuration, input.GetName(), input.GetTarget()) {
			return status.Error(codes.FailedPrecondition, "declared config input has no approved binding")
		}
	}
	for _, input := range revision.GetSecretInputs() {
		if !hasSecret(configuration, input.GetName(), input.GetTarget(), input.GetEnv()) {
			return status.Error(codes.FailedPrecondition, "declared secret input has no approved binding")
		}
	}
	for _, input := range append(append([]*api.WorkspaceApplicationMount{}, revision.GetPersistentMounts()...), revision.GetScratchMounts()...) {
		if !hasMount(configuration, input.GetName(), input.GetMountPath()) {
			return status.Error(codes.FailedPrecondition, "declared mount has no approved binding")
		}
	}
	return nil
}

func validHandle(command *api.RuntimeDeployCommand, handle *api.RuntimeInjectionHandle, kind api.RuntimeInjectionHandle_Kind) bool {
	if handle == nil || handle.GetKind() != kind || handle.GetHandleId() == "" || handle.GetDeliveryReference() == "" || strings.HasPrefix(handle.GetDeliveryReference(), "/") || path.IsAbs(handle.GetDeliveryReference()) || strings.Contains(handle.GetDeliveryReference(), "..") || handle.GetWorkspaceId() != command.GetWorkspaceId() || handle.GetRuntimeInstanceId() != command.GetRuntimeInstanceId() || handle.GetDeploymentDescriptorDigest() != command.GetDeploymentDescriptorDigest() || handle.GetExecutionEpoch() != command.GetExecutionEpoch() || handle.GetTargetSlot() == "" {
		return false
	}
	if handle.GetExpiresAt() != nil && handle.GetExpiresAt().AsTime().Before(time.Now().UTC()) {
		return false
	}
	return true
}

func hasConfig(c *api.WorkspaceApplicationRuntimeConfiguration, name, target string) bool {
	for _, v := range c.GetConfigBindings() {
		if v.GetInputName() == name && v.GetTarget() == target {
			return true
		}
	}
	return false
}
func hasSecret(c *api.WorkspaceApplicationRuntimeConfiguration, name, target, env string) bool {
	for _, v := range c.GetSecretBindings() {
		if v.GetInputName() == name && v.GetTarget() == target && v.GetEnv() == env {
			return true
		}
	}
	return false
}
func hasMount(c *api.WorkspaceApplicationRuntimeConfiguration, name, target string) bool {
	for _, v := range c.GetMountBindings() {
		if v.GetMountName() == name && v.GetTarget() == target {
			return true
		}
	}
	return false
}
