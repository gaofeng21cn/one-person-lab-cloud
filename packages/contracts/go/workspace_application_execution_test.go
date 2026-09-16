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
	// Identity is the application's own declaration; only reusing another
	// generation's data constrains it.
	revision := validWorkspaceApplicationRevision()
	uid := int64(10002)
	revision.Execution.UserID = &uid
	if err := ValidateWorkspaceApplicationRevision(revision); err != nil {
		t.Fatalf("a declared identity must be admitted on its own: %v", err)
	}
}

// Reusing a layout's data means reusing its on-disk ownership: an application
// that mounts legacy OPL data under a different identity would write files the
// layout cannot serve.
func TestLegacyDataLayoutConstrainsTheComponentIdentity(t *testing.T) {
	revision := validWorkspaceApplicationRevision()
	other := int64(10002)
	input := WorkspaceApplicationRuntimeInput{
		SchemaVersion: 2, Revision: revision, DataBindingID: "data-binding",
		DataLayout: "legacy_opl", DataSourceRuntimeOperationID: "source-runtime",
	}
	input.ConfigurationDigest, _ = WorkspaceApplicationConfigurationDigest(input.Configuration, nil, input.DataBindingID)
	if err := ValidateWorkspaceApplicationRuntimeConfiguration(input); err != nil {
		t.Fatalf("an undeclared identity adopts the layout's own: %v", err)
	}
	legacy := int64(10001)
	revision.Execution = WorkspaceApplicationExecution{UserID: &legacy, GroupID: &legacy}
	input.Revision = revision
	if err := ValidateWorkspaceApplicationRuntimeConfiguration(input); err != nil {
		t.Fatalf("the layout's own identity must be admitted: %v", err)
	}
	revision.Execution = WorkspaceApplicationExecution{UserID: &other}
	input.Revision = revision
	if err := ValidateWorkspaceApplicationRuntimeConfiguration(input); err == nil {
		t.Fatal("a foreign identity on reused data must be rejected")
	}
	revision.Execution = WorkspaceApplicationExecution{}
	revision.Dependencies = []WorkspaceApplicationDependency{{
		Name: "retrieval", Image: revision.Image, PersistentMounts: []WorkspaceApplicationDependencyMount{{Name: "data", MountPath: "/data"}},
		Execution: WorkspaceApplicationExecution{GroupID: &other},
	}}
	input.Revision = revision
	if err := ValidateWorkspaceApplicationRuntimeConfiguration(input); err == nil {
		t.Fatal("a dependency that reuses the data must obey the same ownership")
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
