package fabric

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
)

// localDockerApplicationScratchArgs renders one scratch mount. A mount that
// declares no options receives the same tmpfs flags Docker applies by default,
// extended with the resolved workload identity so the component can write its
// own scratch space; the Tencent provider gets that capability from the pod
// fsGroup instead. A mount with declared options keeps its exact declaration.
func localDockerApplicationScratchArgs(mount contracts.WorkspaceApplicationMount, identity *localDockerWorkloadIdentity) []string {
	if !contracts.WorkspaceApplicationScratchOptionsDeclared(mount) {
		if identity == nil {
			return []string{"--mount", "type=tmpfs,target=" + mount.MountPath + ",tmpfs-mode=0755"}
		}
		options := []string{"rw", "nosuid", "nodev", "noexec", "mode=0755",
			"uid=" + strconv.FormatInt(identity.uid, 10), "gid=" + strconv.FormatInt(identity.gid, 10)}
		return []string{"--tmpfs", mount.MountPath + ":" + strings.Join(options, ",")}
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
		_, expected, _ := strings.Cut(localDockerApplicationScratchArgs(mount, nil)[1], ":")
		if container.HostConfig.Tmpfs[mount.MountPath] != expected {
			return errors.New("local_docker_application_scratch_options_mismatch")
		}
	}
	return nil
}
