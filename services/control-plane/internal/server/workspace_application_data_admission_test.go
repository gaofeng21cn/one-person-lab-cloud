package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

func historicalApplicationDataMaterialForTest() contracts.WorkspaceApplicationDataMaterial {
	return contracts.WorkspaceApplicationDataMaterial{
		SchemaVersion: 1, ApplicationID: "knowledge-app", Version: "data-1.0.0",
		Artifacts:   []contracts.WorkspaceApplicationDataArtifact{{Name: "documents", SHA256: strings.Repeat("a", 64), SizeBytes: 1024}},
		RestoreTool: contracts.WorkspaceApplicationRestoreTool{Image: "repo.example/restore@sha256:" + strings.Repeat("b", 64)},
	}
}

// Seed a persisted historical row directly; data admission has no live writer.
func seedApplicationDataMaterialHistoryForTest(t *testing.T, store controlPlaneTableStore) map[string]any {
	t.Helper()
	material := historicalApplicationDataMaterialForTest()
	payload, err := json.Marshal(material)
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	row := map[string]any{"id": "historical-knowledge-data-1", "applicationId": material.ApplicationID, "version": material.Version, "digest": digest, "payload": string(payload), "admittedByUserId": "historical-operator"}
	switch store := store.(type) {
	case *memoryTableStore:
		store.mu.Lock()
		store.applicationDataMaterials[material.ApplicationID+"@"+material.Version] = cloneMap(row)
		store.mu.Unlock()
	case *postgresEntStateStore:
		mustStore(t, saveRecord(context.Background(), stringValue(row["id"]), row, store.client.ApplicationDataMaterial.Create(), applicationDataMaterialEntFields))
	default:
		t.Fatalf("unsupported historical fixture store %T", store)
	}
	return row
}

func TestApplicationDataMaterialHistoryPostgres(t *testing.T) {
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	testApplicationDataMaterialHistory(t, store)
}

func testApplicationDataMaterialHistory(t *testing.T, store controlPlaneTableStore) {
	t.Helper()
	expected := seedApplicationDataMaterialHistoryForTest(t, store)
	row, found, err := store.AdmittedApplicationDataMaterial(context.Background(), "knowledge-app", "data-1.0.0")
	if err != nil || !found {
		t.Fatalf("historical material found=%v err=%v", found, err)
	}
	for field, value := range expected {
		if !reflect.DeepEqual(row[field], value) {
			t.Fatalf("historical field %s=%v want=%v", field, row[field], value)
		}
	}
	// Recreate the HTTP server over the existing persisted row.
	server, err := NewPersistentServer(newTestService(&fakeLedgerClient{}, &fakeFabricClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := operatorSessionForTest(t, server)
	read := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/application-data-materials/knowledge-app/data-1.0.0", "")
	var response struct {
		Decision     string         `json:"decision"`
		DataMaterial map[string]any `json:"dataMaterial"`
	}
	if read.Code != http.StatusOK || json.Unmarshal(read.Body.Bytes(), &response) != nil || response.Decision != "identical" || response.DataMaterial["payload"] != expected["payload"] || response.DataMaterial["digest"] != expected["digest"] {
		t.Fatalf("historical read status=%d body=%s", read.Code, read.Body.String())
	}
	missing := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/application-data-materials/knowledge-app/9.9.9", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing history status=%d", missing.Code)
	}
	_, found, err = store.AdmittedApplicationDataMaterial(context.Background(), "another-app", "data-1.0.0")
	if err != nil || found {
		t.Fatalf("history leaked across application identity: found=%v err=%v", found, err)
	}
}
