package fabric

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

func TestApplicationExecutionUsesOnlyApprovedImmutableProfile(t *testing.T) {
	p, _, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	uid, gid := int64(10001), int64(10001)
	policy := []byte(`{"defaultAction":"SCMP_ACT_ERRNO","syscalls":[]}`)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(policy))
	execution := contracts.WorkspaceApplicationExecution{UserID: &uid, GroupID: &gid, Init: true, SeccompProfile: digest}
	if _, err := p.applicationExecutionSpec(execution, nil); err == nil {
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
	spec, err := p.applicationExecutionSpec(execution, nil)
	if err != nil {
		t.Fatal(err)
	}
	if spec.User != "10001:10001" || spec.Init == nil || !*spec.Init || !reflect.DeepEqual(spec.SecurityOpt, []string{"seccomp=" + string(policy)}) {
		t.Fatalf("spec=%+v", spec)
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
	if _, err := p.applicationExecutionSpec(execution, nil); err == nil {
		t.Fatal("mutable profile accepted")
	}
}

func TestApplicationExecutionDefaultsToTheOPLWorkloadIdentity(t *testing.T) {
	p, _, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	revision := applicationRevisionForTest()
	mainUser := func(revision contracts.WorkspaceApplicationRevision) string {
		t.Helper()
		spec, err := p.applicationExecutionSpec(applicationComponentExecution(revision, "main"), applicationComponentWorkloadIdentity(revision, "main"))
		if err != nil {
			t.Fatal(err)
		}
		if spec.Init != nil || len(spec.SecurityOpt) != 0 {
			t.Fatalf("unexpected extra execution options: %+v", spec)
		}
		return spec.User
	}
	// The main component defaults to the identity every legacy OPL generation
	// ran as, exactly like the Tencent provider's runAsUser/runAsGroup default.
	if user := mainUser(revision); user != "10001:10001" {
		t.Fatalf("undeclared main identity=%q", user)
	}
	// A declared uid overrides only the uid; the group keeps the same default.
	uid := int64(10002)
	revision.Execution = contracts.WorkspaceApplicationExecution{UserID: &uid}
	if user := mainUser(revision); user != "10002:10001" {
		t.Fatalf("declared uid identity=%q", user)
	}
	// A declared uid and gid override both defaults.
	gid := int64(10003)
	revision.Execution = contracts.WorkspaceApplicationExecution{UserID: &uid, GroupID: &gid}
	if user := mainUser(revision); user != "10002:10003" {
		t.Fatalf("declared identity=%q", user)
	}
	// A dependency that declares nothing keeps its image default user and is
	// not adopted by the provider.
	if identity := applicationComponentWorkloadIdentity(revision, "retrieval"); identity != nil {
		t.Fatalf("undeclared dependency identity=%#v", identity)
	}
}
