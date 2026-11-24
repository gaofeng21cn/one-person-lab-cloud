package fabric

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"

	contracts "opl-cloud/packages/contracts/go"
)

// localDockerApplicationScratchTmpfsOptions builds the tmpfs mount option
// string for a scratch mount with declared options or a resolved workload
// identity. An undeclared mount with no resolved identity uses a structured
// mount.TypeTmpfs entry with mode 0755 instead.
func localDockerApplicationScratchTmpfsOptions(m contracts.WorkspaceApplicationMount, identity *localDockerWorkloadIdentity) (string, bool) {
	if !contracts.WorkspaceApplicationScratchOptionsDeclared(m) {
		if identity == nil {
			return "", false
		}
		options := []string{"rw", "nosuid", "nodev", "noexec", "mode=0755",
			"uid=" + strconv.FormatInt(identity.uid, 10), "gid=" + strconv.FormatInt(identity.gid, 10)}
		return strings.Join(options, ","), true
	}
	options := []string{"rw", "nosuid", "nodev"}
	if m.Executable {
		options = append(options, "exec")
	} else {
		options = append(options, "noexec")
	}
	mode := uint32(0755)
	if m.Mode != nil {
		mode = *m.Mode
	}
	options = append(options, fmt.Sprintf("mode=%04o", mode))
	if m.UserID != nil {
		options = append(options, "uid="+strconv.FormatInt(*m.UserID, 10))
	}
	if m.GroupID != nil {
		options = append(options, "gid="+strconv.FormatInt(*m.GroupID, 10))
	}
	if m.SizeBytes != 0 {
		options = append(options, "size="+strconv.FormatInt(m.SizeBytes, 10))
	}
	return strings.Join(options, ","), true
}

// applyLocalDockerApplicationScratchMount configures one scratch mount on the
// Moby HostConfig. A mount that declares no options receives the same tmpfs
// flags Docker applies by default, extended with the resolved workload identity
// so the component can write its own scratch space; the Tencent provider gets
// that capability from the pod fsGroup instead.
func applyLocalDockerApplicationScratchMount(hostConfig *container.HostConfig, m contracts.WorkspaceApplicationMount, identity *localDockerWorkloadIdentity) {
	if options, useTmpfsMap := localDockerApplicationScratchTmpfsOptions(m, identity); useTmpfsMap {
		if hostConfig.Tmpfs == nil {
			hostConfig.Tmpfs = map[string]string{}
		}
		hostConfig.Tmpfs[m.MountPath] = options
		return
	}
	hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{
		Type:         mount.TypeTmpfs,
		Target:       m.MountPath,
		TmpfsOptions: &mount.TmpfsOptions{Mode: 0755},
	})
}

func verifyApplicationScratchMounts(mounts []contracts.WorkspaceApplicationMount, containerInspect dockerContainerInspect) error {
	for _, m := range mounts {
		if !contracts.WorkspaceApplicationScratchOptionsDeclared(m) {
			continue
		}
		expected, _ := localDockerApplicationScratchTmpfsOptions(m, nil)
		if containerInspect.HostConfig.Tmpfs[m.MountPath] != expected {
			return errors.New("local_docker_application_scratch_options_mismatch")
		}
	}
	return nil
}
