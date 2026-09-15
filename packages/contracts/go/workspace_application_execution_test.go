package contracts

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestApplicationExecutionRejectsHostAuthority(t *testing.T) {
	zero, negative := int64(0), int64(-1)
	for _, execution := range []WorkspaceApplicationExecution{{UserID: &zero}, {UserID: &negative}, {GroupID: &zero}, {SeccompProfile: "unconfined"}, {SeccompProfile: "/tmp/profile.json"}} {
		if err := ValidateWorkspaceApplicationExecution(execution); err == nil {
			t.Fatalf("invalid execution accepted: %#v", execution)
		}
	}
	revision := validWorkspaceApplicationRevision()
	revision.RuntimeProfile = "opl_app"
	uid := int64(10002)
	revision.Execution.UserID = &uid
	if err := ValidateWorkspaceApplicationRevision(revision); err == nil {
		t.Fatal("OPL ABI identity changed")
	}
}

func TestOmittedExecutionPreservesRetainedRevisionBytes(t *testing.T) {
	revision := validWorkspaceApplicationRevision()
	body, err := json.Marshal(revision)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte(`"execution"`)) {
		t.Fatal("empty added requirement changes retained revision identity")
	}
}

func TestApplicationDataMountNamesCannotEscapeBinding(t *testing.T) {
	for _, name := range []string{"../app-data-other", "/absolute", ".", "..", "a/b", "a\\b", "data,source=x"} {
		if err := ValidateWorkspaceApplicationMountOptions([]WorkspaceApplicationMount{{Name: name, MountPath: "/data"}}, nil); err == nil {
			t.Fatalf("unsafe mount name %q accepted", name)
		}
	}
	if err := ValidateWorkspaceApplicationMountOptions([]WorkspaceApplicationMount{{Name: "mysql_data", MountPath: "/var/lib/mysql"}}, nil); err != nil {
		t.Fatal(err)
	}
}
