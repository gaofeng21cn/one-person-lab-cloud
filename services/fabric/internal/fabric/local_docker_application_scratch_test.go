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
	args := localDockerApplicationScratchArgs(mount, nil)
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

func TestApplicationScratchDefaultsToTheWorkloadIdentity(t *testing.T) {
	mount := contracts.WorkspaceApplicationMount{Name: "recovery", MountPath: "/recovery"}
	// An undeclared dependency keeps the exact Docker default for its tmpfs.
	if args := localDockerApplicationScratchArgs(mount, nil); !reflect.DeepEqual(args, []string{"--mount", "type=tmpfs,target=/recovery,tmpfs-mode=0755"}) {
		t.Fatalf("undeclared identity scratch=%v", args)
	}
	// The main component's scratch space belongs to the identity it runs as,
	// with the same tmpfs flags Docker applies by default.
	identity := &localDockerWorkloadIdentity{uid: workspaceApplicationWorkloadIdentity, gid: workspaceApplicationWorkloadIdentity}
	expected := "rw,nosuid,nodev,noexec,mode=0755,uid=10001,gid=10001"
	if args := localDockerApplicationScratchArgs(mount, identity); !reflect.DeepEqual(args, []string{"--tmpfs", "/recovery:" + expected}) {
		t.Fatalf("workload identity scratch=%v", args)
	}
}
