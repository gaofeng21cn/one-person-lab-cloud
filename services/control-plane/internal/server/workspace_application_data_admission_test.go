package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func validDataMaterialPayload() string {
	return `{"schemaVersion":1,"applicationId":"knowledge-app","version":"data-1.0.0",` +
		`"artifacts":[{"name":"documents","sha256":"` + strings.Repeat("a", 64) + `","sizeBytes":1024}],` +
		`"restoreTool":{"image":"repo.example/restore@sha256:` + strings.Repeat("b", 64) + `"}}`
}

func TestApplicationDataMaterialAdmissionHTTP(t *testing.T) {
	store := newMemoryTableStore()
	server, err := NewPersistentServer(newTestService(&fakeLedgerClient{}, &fakeFabricClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := operatorSessionForTest(t, server)

	first := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-data-materials", validDataMaterialPayload(), "admit-data-first")
	if first.Code != http.StatusOK {
		t.Fatalf("first admission status=%d body=%s", first.Code, first.Body.String())
	}
	var admitted struct {
		Decision     string `json:"decision"`
		DataMaterial struct {
			ID     string `json:"id"`
			Digest string `json:"digest"`
		} `json:"dataMaterial"`
	}
	if json.Unmarshal(first.Body.Bytes(), &admitted) != nil || admitted.Decision != "new" ||
		admitted.DataMaterial.ID == "" || len(admitted.DataMaterial.Digest) != 64 {
		t.Fatalf("first admission body=%s", first.Body.String())
	}

	replay := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-data-materials", validDataMaterialPayload(), "admit-data-replay")
	var replayed struct {
		Decision string `json:"decision"`
	}
	if replay.Code != http.StatusOK || json.Unmarshal(replay.Body.Bytes(), &replayed) != nil || replayed.Decision != "identical" {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}

	conflict := strings.Replace(validDataMaterialPayload(), strings.Repeat("a", 64), strings.Repeat("d", 64), 1)
	conflicted := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-data-materials", conflict, "admit-data-conflict")
	if conflicted.Code != http.StatusConflict || !strings.Contains(conflicted.Body.String(), "workspace_application_data_material_conflict") {
		t.Fatalf("conflict status=%d body=%s", conflicted.Code, conflicted.Body.String())
	}

	invalid := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-data-materials",
		`{"schemaVersion":1,"applicationId":"knowledge-app","version":"data-2.0.0","artifacts":[],"restoreTool":{"image":"repo.example/restore@sha256:`+strings.Repeat("b", 64)+`"}}`,
		"admit-data-invalid")
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "invalid_application_data_material") {
		t.Fatalf("invalid admission status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	read := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/application-data-materials/knowledge-app/data-1.0.0", "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), admitted.DataMaterial.Digest) {
		t.Fatalf("read status=%d body=%s", read.Code, read.Body.String())
	}
	missing := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/application-data-materials/knowledge-app/9.9.9", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestApplicationDataMaterialAdmissionPostgres(t *testing.T) {
	admin := openControlPlaneTestPostgres(t)
	database := fmt.Sprintf("control_plane_application_data_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE ` + database); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, database)
		_, _ = admin.Exec(`DROP DATABASE ` + database)
		_ = admin.Close()
	})
	store, err := newTestPostgresEntStateStore(controlPlaneTestPostgresURL(t, database, ""))
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewPersistentServer(newTestService(&fakeLedgerClient{}, &fakeFabricClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := operatorSessionForTest(t, server)

	first := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-data-materials", validDataMaterialPayload(), "admit-data-pg-first")
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"decision":"new"`) {
		t.Fatalf("postgres first admission status=%d body=%s", first.Code, first.Body.String())
	}
	replay := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-data-materials", validDataMaterialPayload(), "admit-data-pg-replay")
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), `"decision":"identical"`) {
		t.Fatalf("postgres replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	read := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/application-data-materials/knowledge-app/data-1.0.0", "")
	if read.Code != http.StatusOK {
		t.Fatalf("postgres read status=%d body=%s", read.Code, read.Body.String())
	}
}
