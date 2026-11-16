package server

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"opl-cloud/services/control-plane/internal/domain/application"
)

// The operator revision surface is retired: the route that served the
// operator revision read is no longer registered. The persisted revision and
// the decoder stay readable through the retained store path.
//
// The memory-store variant runs without a database, so the retained read path
// stays proven in the database-free CI lane.

func TestApplicationRevisionStoreAndHistoryMemory(t *testing.T) {
	testApplicationRevisionStoreAndHistory(t, newMemoryTableStore())
}

func TestApplicationRevisionStoreAndHistoryPostgres(t *testing.T) {
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	testApplicationRevisionStoreAndHistory(t, store)
}

func testApplicationRevisionStoreAndHistory(t *testing.T, store controlPlaneTableStore) {
	t.Helper()
	ctx := context.Background()
	revision := knowledgeApplicationRevisionForTest()
	digest, err := application.RevisionDigest(revision)
	if err != nil {
		t.Fatal(err)
	}
	seedAdmittedApplicationRevisionForTest(t, store, revision)
	first, found, err := store.AdmittedApplicationRevision(ctx, revision.ApplicationID, revision.Version)
	if err != nil || !found || stringValue(first["id"]) == "" || stringValue(first["digest"]) != digest {
		t.Fatalf("historical revision=%+v found=%v err=%v", first, found, err)
	}
	retained, found, err := store.AdmittedApplicationRevision(ctx, revision.ApplicationID, revision.Version)
	if err != nil || !found || !reflect.DeepEqual(first, retained) {
		t.Fatalf("repeated history read=%+v found=%v err=%v", retained, found, err)
	}
	decoded, valid := decodeApplicationRevisionPayload(stringValue(retained["payload"]))
	if !valid || !reflect.DeepEqual(decoded, revision) {
		t.Fatalf("stored revision decoder=%+v valid=%v", decoded, valid)
	}
	// A fresh server reads persisted history without re-admitting the revision.
	server, err := NewPersistentServer(newTestService(&fakeLedgerClient{}, &fakeFabricClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := operatorSessionForTest(t, server)
	// The retired operator revision route stays retired. The retained history
	// is served by the deployment history route, not by an operator revision
	// admission surface.
	read := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/application-revisions/knowledge-app/1.0.0", "")
	if read.Code != http.StatusNotFound {
		t.Fatalf("retired operator revision route status=%d body=%s", read.Code, read.Body.String())
	}
}

func TestApplicationRevisionPayloadDecoderRejectsMalformedHistory(t *testing.T) {
	for _, payload := range []string{`{`, `{"schemaVersion":"invalid"}`, `{"ports":"invalid"}`} {
		if _, valid := decodeApplicationRevisionPayload(payload); valid {
			t.Fatalf("decoder accepted malformed history %s", payload)
		}
	}
}
