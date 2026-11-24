package fabric

import (
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"

	contracts "opl-cloud/packages/contracts/go"
)

func TestApplicationScratchPreservesDeclaredLinuxSemantics(t *testing.T) {
	mode := uint32(0700)
	uid, gid := int64(10001), int64(10001)
	m := contracts.WorkspaceApplicationMount{Name: "state", MountPath: "/state", Mode: &mode, UserID: &uid, GroupID: &gid, SizeBytes: 1 << 30, Executable: true}
	var hostCfg container.HostConfig
	applyLocalDockerApplicationScratchMount(&hostCfg, m, nil)
	expected := "rw,nosuid,nodev,exec,mode=0700,uid=10001,gid=10001,size=1073741824"
	if hostCfg.Tmpfs["/state"] != expected || len(hostCfg.Mounts) != 0 {
		t.Fatalf("tmpfs=%v mounts=%v", hostCfg.Tmpfs, hostCfg.Mounts)
	}
	inspected := dockerContainerInspect{}
	inspected.HostConfig.Tmpfs = map[string]string{"/state": expected}
	if err := verifyApplicationScratchMounts([]contracts.WorkspaceApplicationMount{m}, inspected); err != nil {
		t.Fatal(err)
	}
	inspected.HostConfig.Tmpfs["/state"] = "rw,mode=0777"
	if err := verifyApplicationScratchMounts([]contracts.WorkspaceApplicationMount{m}, inspected); err == nil {
		t.Fatal("permission/ownership drift accepted")
	}
	if err := contracts.ValidateWorkspaceApplicationMountOptions([]contracts.WorkspaceApplicationMount{m}, nil); err == nil {
		t.Fatal("deployment may not rewrite persistent permissions")
	}
}

func TestApplicationScratchDefaultsToTheWorkloadIdentity(t *testing.T) {
	m := contracts.WorkspaceApplicationMount{Name: "recovery", MountPath: "/recovery"}
	// An undeclared dependency keeps the exact Docker default for its tmpfs.
	var depHostCfg container.HostConfig
	applyLocalDockerApplicationScratchMount(&depHostCfg, m, nil)
	if len(depHostCfg.Mounts) != 1 || depHostCfg.Mounts[0].Type != mount.TypeTmpfs || depHostCfg.Mounts[0].Target != "/recovery" ||
		depHostCfg.Mounts[0].TmpfsOptions == nil || depHostCfg.Mounts[0].TmpfsOptions.Mode != 0755 || len(depHostCfg.Tmpfs) != 0 {
		t.Fatalf("undeclared identity scratch=%+v tmpfs=%v", depHostCfg.Mounts, depHostCfg.Tmpfs)
	}
	// The main component's scratch space belongs to the identity it runs as,
	// with the same tmpfs flags Docker applies by default.
	identity := &localDockerWorkloadIdentity{uid: workspaceApplicationWorkloadIdentity, gid: workspaceApplicationWorkloadIdentity}
	expected := "rw,nosuid,nodev,noexec,mode=0755,uid=10001,gid=10001"
	var mainHostCfg container.HostConfig
	applyLocalDockerApplicationScratchMount(&mainHostCfg, m, identity)
	if mainHostCfg.Tmpfs["/recovery"] != expected || len(mainHostCfg.Mounts) != 0 {
		t.Fatalf("workload identity scratch=%v mounts=%v", mainHostCfg.Tmpfs, mainHostCfg.Mounts)
	}
}
