package fabric

import (
	contracts "opl-cloud/packages/contracts/go"
	"reflect"
	"testing"
)

func TestApplicationScratchPreservesDeclaredLinuxSemantics(t *testing.T) {
	mode := uint32(0700)
	uid, gid := int64(10001), int64(10001)
	mount := contracts.WorkspaceApplicationMount{Name: "state", MountPath: "/state", Mode: &mode, UserID: &uid, GroupID: &gid, SizeBytes: 1 << 30, Executable: true}
	args := localDockerApplicationScratchArgs(mount)
	expected := "rw,nosuid,nodev,exec,mode=0700,uid=10001,gid=10001,size=1073741824"
	if !reflect.DeepEqual(args, []string{"--tmpfs", "/state:" + expected}) {
		t.Fatalf("args=%v", args)
	}
	container := dockerContainerInspect{}
	container.HostConfig.Tmpfs = map[string]string{"/state": expected}
	if err := verifyApplicationScratchMounts([]contracts.WorkspaceApplicationMount{mount}, container); err != nil {
		t.Fatal(err)
	}
	container.HostConfig.Tmpfs["/state"] = "rw,mode=0777"
	if err := verifyApplicationScratchMounts([]contracts.WorkspaceApplicationMount{mount}, container); err == nil {
		t.Fatal("permission/ownership drift accepted")
	}
	if err := contracts.ValidateWorkspaceApplicationMountOptions([]contracts.WorkspaceApplicationMount{mount}, nil); err == nil {
		t.Fatal("deployment may not rewrite persistent permissions")
	}
}
