package fabric

import (
	"os"
	"sync"
	"syscall"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

// fakeLocalDockerWorkloadOwnership records the assignments the provider would
// perform with the real filesystem capability. The real capability is
// exercised end to end by the Docker integration tests and the Linux
// qualification chain where the provider actually owns the host filesystem.
type fakeLocalDockerWorkloadOwnership struct {
	mu    sync.Mutex
	calls []localDockerWorkloadOwnershipCall
}

type localDockerWorkloadOwnershipCall struct {
	path string
	uid  int64
	gid  int64
}

func (f *fakeLocalDockerWorkloadOwnership) Assign(path string, uid, gid int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, localDockerWorkloadOwnershipCall{path: path, uid: uid, gid: gid})
	return nil
}

func (f *fakeLocalDockerWorkloadOwnership) recorded() []localDockerWorkloadOwnershipCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]localDockerWorkloadOwnershipCall(nil), f.calls...)
}

func TestLocalDockerWorkloadOwnershipKeepsAlignedPathsAndFailsClosed(t *testing.T) {
	ownership := osLocalDockerWorkloadOwnership{}
	root := t.TempDir()
	// A path already owned by the running provider and grouped to the resolved
	// identity needs no change.
	if err := ownership.Assign(root, int64(os.Geteuid()), -1); err != nil {
		t.Fatalf("aligned path: %v", err)
	}
	if err := ownership.Assign(root, -1, int64(os.Getegid())); err != nil {
		t.Fatalf("aligned group: %v", err)
	}
	// Adopting a foreign identity is privileged either way: a root provider
	// takes ownership, an unprivileged one fails closed instead of guessing.
	foreign := int64(os.Geteuid()) + 1
	err := ownership.Assign(root, foreign, -1)
	if os.Geteuid() == 0 {
		if err != nil {
			t.Fatalf("privileged adoption: %v", err)
		}
		stat, statErr := os.Lstat(root)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if uid := int64(stat.Sys().(*syscall.Stat_t).Uid); uid != foreign {
			t.Fatalf("adopted uid=%d want=%d", uid, foreign)
		}
	} else if err == nil {
		t.Fatal("an unprivileged provider claimed a foreign identity")
	}
}

// applicationTestExecutionIdentity declares the identity the test process
// itself already runs as, so a unit fixture's host filesystem state is exactly
// what a privileged deployment would establish: the credential group the
// provider is asked for is the one the files already carry. The real
// filesystem capability is exercised by the Docker integration and Linux
// qualification chains instead.
func applicationTestExecutionIdentity(t *testing.T) contracts.WorkspaceApplicationExecution {
	t.Helper()
	uid, gid := int64(os.Geteuid()), int64(os.Getegid())
	if uid == 0 {
		// Only uid 0 is refused by the execution contract; a root test host
		// still gets the documented workload identity for the uid while the
		// group keeps matching the files the process itself created.
		uid = workspaceApplicationWorkloadIdentity
	}
	return contracts.WorkspaceApplicationExecution{UserID: &uid, GroupID: &gid}
}
