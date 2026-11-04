package application

import (
	"testing"

	contracts "opl-cloud/packages/contracts/go"
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
	changed.Image = "repo.example/apps/knowledge@sha256:" + repeat('b', 64)
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
