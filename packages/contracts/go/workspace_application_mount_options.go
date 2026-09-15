package contracts

import "errors"

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
