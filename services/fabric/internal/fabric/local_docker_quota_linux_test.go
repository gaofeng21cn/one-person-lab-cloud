//go:build linux && opl_project_quota

package fabric

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestLinuxLocalDockerProjectQuotaEnforcesHardLimit(t *testing.T) {
	root := os.Getenv("OPL_TEST_PROJECT_QUOTA_ROOT")
	if root == "" {
		t.Skip("OPL_TEST_PROJECT_QUOTA_ROOT is not configured")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	quota := linuxLocalDockerProjectQuota{root: root}
	if err := quota.Preflight(root); err != nil {
		t.Fatalf("project quota preflight: %v", err)
	}
	directory, err := os.MkdirTemp(root, ".opl-quota-integration-")
	if err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(directory, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	projectID := localDockerInitialProjectID(fmt.Sprintf("quota-integration-%d-%d", os.Getpid(), time.Now().UnixNano()))
	const hardLimitBytes = uint64(1024 * 1024)
	if err := quota.Apply(directory, projectID, hardLimitBytes); err != nil {
		_ = os.RemoveAll(directory)
		t.Fatalf("apply project quota: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(directory)
		_ = quota.Clear(root, projectID)
	})
	for _, path := range []string{directory, data} {
		state, err := quota.Read(path)
		if err != nil || state.ProjectID != projectID || state.HardLimitBytes != hardLimitBytes || !state.Inherits {
			t.Fatalf("quota readback path=%s state=%#v err=%v", path, state, err)
		}
	}
	if err := os.Chmod(directory, 0711); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(data, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	// The privileged workflow keeps its qualification root private to root. Copy
	// the test binary to /tmp so the child can exec it after dropping privileges;
	// the helper stays outside the quota project and cannot affect its budget.
	original, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	helper, err := os.CreateTemp("/tmp", "opl-quota-writer-*.test")
	if err != nil {
		_ = original.Close()
		t.Fatal(err)
	}
	helperPath := helper.Name()
	t.Cleanup(func() {
		_ = original.Close()
		_ = helper.Close()
		_ = os.Remove(helperPath)
	})
	if _, err := io.Copy(helper, original); err != nil {
		t.Fatal(err)
	}
	if err := helper.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(helperPath, 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(helperPath, "-test.run=^TestLinuxLocalDockerProjectQuotaUnprivilegedWrite$")
	cmd.Env = []string{"OPL_TEST_PROJECT_QUOTA_WRITE_PATH=" + filepath.Join(data, "limit.bin")}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("unprivileged project quota write: %v\n%s", err, output)
	}
	if err := quota.Clear(root, projectID); err != nil {
		t.Fatalf("clear project quota: %v", err)
	}
	if err := quota.Clear(root, projectID); err != nil {
		t.Fatalf("repeat clear project quota: %v", err)
	}
	record, err := quota.ReadProject(root, projectID)
	if err != nil || record.HardLimitBytes != 0 || record.SoftLimitBytes != 0 {
		t.Fatalf("cleared project quota=%#v err=%v", record, err)
	}
}

func TestLinuxLocalDockerProjectQuotaUnprivilegedWrite(t *testing.T) {
	path := os.Getenv("OPL_TEST_PROJECT_QUOTA_WRITE_PATH")
	if path == "" {
		t.Skip("only run as the unprivileged quota writer")
	}
	if os.Geteuid() == 0 {
		t.Fatal("project quota enforcement must be checked without root privileges")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	chunk := make([]byte, 256*1024)
	var writeErr error
	for written := 0; written < 2*1024*1024; written += len(chunk) {
		if _, writeErr = file.Write(chunk); writeErr != nil {
			break
		}
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	if !errors.Is(writeErr, syscall.EDQUOT) {
		t.Fatalf("write beyond project hard limit err=%v", writeErr)
	}
}
