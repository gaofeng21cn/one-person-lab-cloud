package fabric

import (
	"crypto/sha256"
	"fmt"
	contracts "opl-cloud/packages/contracts/go"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplicationExecutionUsesOnlyApprovedImmutableProfile(t *testing.T) {
	p, _, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	uid, gid := int64(10001), int64(10001)
	policy := []byte(`{"defaultAction":"SCMP_ACT_ERRNO","syscalls":[]}`)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(policy))
	execution := contracts.WorkspaceApplicationExecution{UserID: &uid, GroupID: &gid, Init: true, SeccompProfile: digest}
	if _, err := p.applicationExecutionArgs(execution); err == nil {
		t.Fatal("unapproved profile accepted")
	}
	dir := filepath.Join(p.gatewaySecretRoot, "application-security-profiles")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, strings.Replace(digest, ":", "-", 1)+".json")
	if err := os.WriteFile(file, policy, 0444); err != nil {
		t.Fatal(err)
	}
	args, err := p.applicationExecutionArgs(execution)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(args, " ") != "--user 10001:10001 --init --security-opt seccomp="+file {
		t.Fatal(args)
	}
	input := applicationRuntimeInput("execution", applicationRevisionForTest())
	input.Revision.Execution = execution
	container := dockerContainerInspect{}
	container.Config.User = "10001:10001"
	enabled := true
	container.HostConfig.Init = &enabled
	container.HostConfig.SecurityOpt = []string{"seccomp=" + string(policy)}
	if err := p.verifyApplicationExecution(input, "main", container); err != nil {
		t.Fatal(err)
	}
	container.Config.User = "0:0"
	if err := p.verifyApplicationExecution(input, "main", container); err == nil {
		t.Fatal("changed identity accepted")
	}
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := p.applicationExecutionArgs(execution); err == nil {
		t.Fatal("mutable profile accepted")
	}
}
