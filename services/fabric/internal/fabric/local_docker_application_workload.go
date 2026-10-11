package fabric

import (
	"fmt"
	"os"
	"syscall"

	contracts "opl-cloud/packages/contracts/go"
)

// workspaceApplicationWorkloadIdentity is the uid and gid the OPL family of
// workspace workloads runs as. The Tencent provider declares it as the default
// runAsUser/runAsGroup for the main component, and the contracts module treats
// it as the owner of the data a legacy OPL layout reuses. Local-Docker mirrors
// the same default so a main component whose image declares its own default
// user still runs as the identity its persistent data belongs to.
const workspaceApplicationWorkloadIdentity = 10001

// localDockerWorkloadIdentity is one component's resolved runtime identity.
type localDockerWorkloadIdentity struct {
	uid int64
	gid int64
}

// applicationComponentWorkloadIdentity resolves the identity the Local provider
// schedules a component as. The main component defaults to the OPL workload
// identity exactly as the Tencent provider does; a dependency keeps its image
// default user until it declares an identity of its own.
func applicationComponentWorkloadIdentity(revision contracts.WorkspaceApplicationRevision, name string) *localDockerWorkloadIdentity {
	execution := applicationComponentExecution(revision, name)
	if name == contracts.WorkspaceApplicationComponentMain {
		identity := localDockerWorkloadIdentity{uid: workspaceApplicationWorkloadIdentity, gid: workspaceApplicationWorkloadIdentity}
		if execution.UserID != nil {
			identity.uid = *execution.UserID
		}
		if execution.GroupID != nil {
			identity.gid = *execution.GroupID
		}
		return &identity
	}
	if execution.UserID == nil {
		return nil
	}
	identity := localDockerWorkloadIdentity{uid: *execution.UserID, gid: *execution.UserID}
	if execution.GroupID != nil {
		identity.gid = *execution.GroupID
	}
	return &identity
}

// localDockerWorkloadOwnership assigns host paths to the identity a workspace
// workload runs as. The Tencent provider receives that contract from the
// cluster through runAsUser/runAsGroup and fsGroup; Local-Docker must own the
// equivalent filesystem capability itself, so this interface is the single
// seam between admitted workload data and the privileged filesystem.
type localDockerWorkloadOwnership interface {
	// Assign makes path owned by uid and grouped to gid. A negative uid or gid
	// keeps the current value.
	Assign(path string, uid, gid int64) error
}

// osLocalDockerWorkloadOwnership is the real capability. It never takes
// ownership of data the provider did not create: only a path the running
// provider owns (or that is unclaimed root-owned provider state) is adopted,
// and an unprivileged provider fails closed instead of guessing.
type osLocalDockerWorkloadOwnership struct{}

func (osLocalDockerWorkloadOwnership) Assign(path string, uid, gid int64) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("local_docker_application_workload_path_invalid")
	}
	currentUID, currentGID := int64(stat.Uid), int64(stat.Gid)
	targetUID, targetGID := currentUID, currentGID
	if uid >= 0 {
		targetUID = uid
	}
	if gid >= 0 {
		targetGID = gid
	}
	if targetUID == currentUID && targetGID == currentGID {
		return nil
	}
	if currentUID != 0 && currentUID != int64(os.Geteuid()) {
		return fmt.Errorf("local_docker_application_workload_owner_conflict: path=%s owner=%d", path, currentUID)
	}
	if err := os.Lchown(path, int(targetUID), int(targetGID)); err != nil {
		return fmt.Errorf("local_docker_application_workload_ownership_failed: %w", err)
	}
	return nil
}
