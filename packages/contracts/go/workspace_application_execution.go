package contracts

import (
	"errors"
	"regexp"
)

// Execution is a component's explicit process requirement, not host authority.
// A custom seccomp digest must resolve to a profile approved by the installation;
// providers reject requirements they cannot represent rather than ignoring them.
type WorkspaceApplicationExecution struct {
	UserID         *int64 `json:"userId,omitempty"`
	GroupID        *int64 `json:"groupId,omitempty"`
	Init           bool   `json:"init,omitempty"`
	SeccompProfile string `json:"seccompProfile,omitempty"`
}

var applicationSeccompDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func ValidateWorkspaceApplicationExecution(execution WorkspaceApplicationExecution) error {
	if execution.UserID != nil && (*execution.UserID <= 0 || *execution.UserID > 2147483647) || execution.GroupID != nil && (*execution.GroupID < 0 || *execution.GroupID > 2147483647) || execution.GroupID != nil && execution.UserID == nil {
		return errors.New("workspace_application_execution_identity_invalid")
	}
	if execution.SeccompProfile != "" && !applicationSeccompDigestPattern.MatchString(execution.SeccompProfile) {
		return errors.New("workspace_application_seccomp_profile_invalid")
	}
	return nil
}

func WorkspaceApplicationScratchOptionsDeclared(mount WorkspaceApplicationMount) bool {
	return mount.Mode != nil || mount.UserID != nil || mount.GroupID != nil || mount.SizeBytes != 0 || mount.Executable
}

// Only transient, runtime-owned memory mounts accept these options. Existing
// persistent data must never be chmod/chown'ed by a deployment request.
func ValidateWorkspaceApplicationMountOptions(persistent, scratch []WorkspaceApplicationMount) error {
	for _, mount := range append(append([]WorkspaceApplicationMount{}, persistent...), scratch...) {
		if !workspaceApplicationInputNamePattern.MatchString(mount.Name) || !validWorkspaceApplicationInputTarget(mount.MountPath) {
			return errors.New("workspace_application_mount_invalid")
		}
	}

	for _, mount := range persistent {
		if WorkspaceApplicationScratchOptionsDeclared(mount) {
			return errors.New("workspace_application_persistent_permissions_unsupported")
		}
	}
	for _, mount := range scratch {
		if mount.ReadOnly || mount.SizeBytes < 0 || mount.Mode != nil && *mount.Mode & ^uint32(01777) != 0 || mount.UserID != nil && (*mount.UserID < 0 || *mount.UserID > 2147483647) || mount.GroupID != nil && (*mount.GroupID < 0 || *mount.GroupID > 2147483647) {
			return errors.New("workspace_application_scratch_options_invalid")
		}
	}
	return nil
}
