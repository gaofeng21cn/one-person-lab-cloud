package fabric

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
)

func localDockerApplicationScratchArgs(mount contracts.WorkspaceApplicationMount) []string {
	if !contracts.WorkspaceApplicationScratchOptionsDeclared(mount) {
		return []string{"--mount", "type=tmpfs,target=" + mount.MountPath + ",tmpfs-mode=0755"}
	}
	options := []string{"rw", "nosuid", "nodev"}
	if mount.Executable {
		options = append(options, "exec")
	} else {
		options = append(options, "noexec")
	}
	mode := uint32(0755)
	if mount.Mode != nil {
		mode = *mount.Mode
	}
	options = append(options, fmt.Sprintf("mode=%04o", mode))
	if mount.UserID != nil {
		options = append(options, "uid="+strconv.FormatInt(*mount.UserID, 10))
	}
	if mount.GroupID != nil {
		options = append(options, "gid="+strconv.FormatInt(*mount.GroupID, 10))
	}
	if mount.SizeBytes != 0 {
		options = append(options, "size="+strconv.FormatInt(mount.SizeBytes, 10))
	}
	return []string{"--tmpfs", mount.MountPath + ":" + strings.Join(options, ",")}
}

func verifyApplicationScratchMounts(mounts []contracts.WorkspaceApplicationMount, container dockerContainerInspect) error {
	for _, mount := range mounts {
		if !contracts.WorkspaceApplicationScratchOptionsDeclared(mount) {
			continue
		}
		_, expected, _ := strings.Cut(localDockerApplicationScratchArgs(mount)[1], ":")
		if container.HostConfig.Tmpfs[mount.MountPath] != expected {
			return errors.New("local_docker_application_scratch_options_mismatch")
		}
	}
	return nil
}
