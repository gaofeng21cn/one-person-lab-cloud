package server

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"opl-cloud/services/control-plane/internal/domain/application"
)

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
	read := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/application-revisions/knowledge-app/1.0.0", "")
	var response struct {
		Decision string         `json:"decision"`
		Revision map[string]any `json:"revision"`
	}
	if read.Code != http.StatusOK || json.Unmarshal(read.Body.Bytes(), &response) != nil || response.Decision != "identical" || response.Revision["payload"] != first["payload"] || response.Revision["digest"] != digest {
		t.Fatalf("history read status=%d body=%s", read.Code, read.Body.String())
	}
	missing := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/application-revisions/knowledge-app/9.9.9", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing history status=%d", missing.Code)
	}
}

func TestApplicationRevisionPayloadDecoderRejectsMalformedHistory(t *testing.T) {
	for _, payload := range []string{`{`, `{"schemaVersion":"invalid"}`, `{"ports":"invalid"}`} {
		if _, valid := decodeApplicationRevisionPayload(payload); valid {
			t.Fatalf("decoder accepted malformed history %s", payload)
		}
	}
}
