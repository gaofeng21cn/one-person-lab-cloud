package application

import (
	"errors"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/domain/provisioning"
)

func validRevision() contracts.WorkspaceApplicationRevision {
	return contracts.WorkspaceApplicationRevision{
		SchemaVersion: 1, ApplicationID: "knowledge-app", Version: "1.0.0", Platform: "linux/amd64",
		Image:            "repo.example/apps/knowledge@sha256:" + repeat('a', 64),
		Ports:            []contracts.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: "TCP"}},
		PersistentMounts: []contracts.WorkspaceApplicationMount{{Name: "data", MountPath: "/data"}},
		ExposurePolicy:   "application",
	}
}

func repeat(character byte, count int) string {
	bytes := make([]byte, count)
	for index := range bytes {
		bytes[index] = character
	}
	return string(bytes)
}

func TestRevisionDigestIsStableAndContentSensitive(t *testing.T) {
	revision := validRevision()
	digest, err := RevisionDigest(revision)
	if err != nil {
		t.Fatalf("RevisionDigest() unexpected error: %v", err)
	}
	if len(digest) != 64 {
		t.Fatalf("digest = %q, want 64 hex characters", digest)
	}
	same, err := RevisionDigest(validRevision())
	if err != nil || same != digest {
		t.Fatalf("identical revisions produced digests %q and %q (err=%v)", digest, same, err)
	}
	changed := revision
	other, err := RevisionDigest(changed)
	if err != nil {
		t.Fatal(err)
	}
	if other == digest {
		t.Fatal("a content change must change the revision digest")
	}
	if _, err := RevisionDigest(contracts.WorkspaceApplicationRevision{SchemaVersion: 1}); err == nil {
		t.Fatal("an invalid revision must not produce a digest")
	}
}

func TestDecideRevisionAdmission(t *testing.T) {
	candidate := validRevision()
	decision, err := DecideRevisionAdmission(nil, candidate)
	if err != nil || decision != AdmissionNew {
		t.Fatalf("first admission = %q/%v, want new", decision, err)
	}
	identical, err := DecideRevisionAdmission(&candidate, validRevision())
	if err != nil || identical != AdmissionIdentical {
		t.Fatalf("replay admission = %q/%v, want identical", identical, err)
	}
	changed := candidate
	changed.Image = "repo.example/apps/knowledge@sha256:" + repeat('b', 64)
	if _, err := DecideRevisionAdmission(&candidate, changed); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("changed content under the same identity error = %v, want %v", err, ErrRevisionConflict)
	}
	otherIdentity := candidate
	otherIdentity.ApplicationID = "other-app"
	if _, err := DecideRevisionAdmission(&candidate, otherIdentity); err == nil {
		t.Fatal("a caller-supplied prior with a different identity must be rejected")
	}
	invalid := candidate
	invalid.ExposurePolicy = "public"
	if _, err := DecideRevisionAdmission(nil, invalid); err == nil {
		t.Fatal("an invalid candidate must not be admitted")
	}
}

func TestParseRevisionRef(t *testing.T) {
	ref, err := ParseRevisionRef("knowledge-app@1.0.0")
	if err != nil || ref.ApplicationID != "knowledge-app" || ref.Version != "1.0.0" {
		t.Fatalf("ParseRevisionRef() = %#v, %v", ref, err)
	}
	for _, invalid := range []string{"", "knowledge-app", "@1.0.0", "knowledge-app@", "a@b@c", "knowledge-app@1.0.0@x"} {
		if _, err := ParseRevisionRef(invalid); err == nil {
			t.Fatalf("ParseRevisionRef(%q) must fail", invalid)
		}
	}
	if _, err := ParseRevisionRef(provisioning.ApplicationBindingOPLApp); err == nil {
		t.Fatal("the retained OPL App binding is not a revision reference")
	}
}

func TestValidateDeploymentTransition(t *testing.T) {
	revision := validRevision()
	deployment := contracts.WorkspaceApplicationDeployment{
		SchemaVersion: 1, OperationID: "workspace-application-deploy-unit", WorkspaceID: "ws-unit",
		ApplicationID: "knowledge-app", TargetRevision: "1.0.0", ConfigurationDigest: repeat('c', 64),
		ExpectedWorkspaceVersion: 3, IdempotencyKey: "workspace-application-deploy-unit:1",
	}
	if err := ValidateDeploymentTransition(deployment, revision, provisioning.ApplicationBindingEmpty); err != nil {
		t.Fatalf("empty-binding transition error = %v", err)
	}
	if err := ValidateDeploymentTransition(deployment, revision, provisioning.ApplicationBindingOPLApp); err != nil {
		t.Fatalf("opl-app replacement transition error = %v", err)
	}
	if err := ValidateDeploymentTransition(deployment, revision, "knowledge-app@0.9.0"); !errors.Is(err, ErrDeploymentTransitionInvalid) {
		t.Fatalf("mismatched previous identity error = %v, want %v", err, ErrDeploymentTransitionInvalid)
	}
	upgrade := deployment
	upgrade.PreviousApplicationID, upgrade.PreviousRevision = "knowledge-app", "0.9.0"
	if err := ValidateDeploymentTransition(upgrade, revision, "knowledge-app@0.9.0"); err != nil {
		t.Fatalf("matching previous transition error = %v", err)
	}
	unadmitted := contracts.WorkspaceApplicationRevision{ApplicationID: "other-app", Version: "1.0.0"}
	if err := ValidateDeploymentTransition(deployment, unadmitted, provisioning.ApplicationBindingEmpty); !errors.Is(err, ErrRevisionNotAdmitted) {
		t.Fatalf("unadmitted target error = %v, want %v", err, ErrRevisionNotAdmitted)
	}
	brokenDeployment := deployment
	brokenDeployment.IdempotencyKey = ""
	if err := ValidateDeploymentTransition(brokenDeployment, revision, provisioning.ApplicationBindingEmpty); err == nil {
		t.Fatal("a deployment failing contract validation must be rejected")
	}
	brokenBinding := "not-a-reference"
	if err := ValidateDeploymentTransition(deployment, revision, brokenBinding); !errors.Is(err, ErrDeploymentTransitionInvalid) {
		t.Fatalf("unparseable binding error = %v, want %v", err, ErrDeploymentTransitionInvalid)
	}
}
