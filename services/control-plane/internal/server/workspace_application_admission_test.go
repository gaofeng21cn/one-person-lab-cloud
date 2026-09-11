package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestApplicationRevisionAdmissionHTTP(t *testing.T) {
	store := newMemoryTableStore()
	server, err := NewPersistentServer(newTestService(&fakeLedgerClient{}, &fakeFabricClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := operatorSessionForTest(t, server)

	revision := func(imageDigest string) string {
		return `{"schemaVersion":1,"applicationId":"knowledge-app","version":"1.0.0","platform":"linux/amd64",` +
			`"image":"repo.example/apps/knowledge@sha256:` + imageDigest + `",` +
			`"ports":[{"name":"http","port":8080,"protocol":"TCP"}],` +
			`"resources":{"cpu":2,"memoryGb":4},"exposurePolicy":"application"}`
	}

	first := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-revisions", revision(strings.Repeat("a", 64)), "admit-knowledge-first")
	if first.Code != http.StatusOK {
		t.Fatalf("first admission status=%d body=%s", first.Code, first.Body.String())
	}
	var admitted struct {
		Decision string `json:"decision"`
		Revision struct {
			ID     string `json:"id"`
			Digest string `json:"digest"`
		} `json:"revision"`
	}
	if json.Unmarshal(first.Body.Bytes(), &admitted) != nil || admitted.Decision != "new" || admitted.Revision.ID == "" || len(admitted.Revision.Digest) != 64 {
		t.Fatalf("first admission body=%s", first.Body.String())
	}

	replay := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-revisions", revision(strings.Repeat("a", 64)), "admit-knowledge-replay")
	var replayed struct {
		Decision string `json:"decision"`
		Revision struct {
			ID string `json:"id"`
		} `json:"revision"`
	}
	if replay.Code != http.StatusOK || json.Unmarshal(replay.Body.Bytes(), &replayed) != nil ||
		replayed.Decision != "identical" || replayed.Revision.ID != admitted.Revision.ID {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}

	conflict := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-revisions", revision(strings.Repeat("b", 64)), "admit-knowledge-conflict")
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "workspace_application_revision_conflict") {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	invalid := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-revisions",
		`{"schemaVersion":1,"applicationId":"knowledge-app","version":"2.0.0","platform":"linux/amd64",`+
			`"image":"repo.example/apps/knowledge@sha256:`+strings.Repeat("a", 64)+`","resources":{"cpu":1,"memoryGb":1},"exposurePolicy":"public"}`,
		"admit-knowledge-invalid")
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "invalid_application_revision") {
		t.Fatalf("invalid admission status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	read := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/application-revisions/knowledge-app/1.0.0", "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), admitted.Revision.Digest) {
		t.Fatalf("read status=%d body=%s", read.Code, read.Body.String())
	}
	missing := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/application-revisions/knowledge-app/9.9.9", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestApplicationRevisionAdmissionPostgres(t *testing.T) {
	admin := openControlPlaneTestPostgres(t)
	database := fmt.Sprintf("control_plane_application_revision_%d", time.Now().UnixNano())
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

	revision := `{"schemaVersion":1,"applicationId":"knowledge-app","version":"1.0.0","platform":"linux/amd64",` +
		`"image":"repo.example/apps/knowledge@sha256:` + strings.Repeat("a", 64) + `",` +
		`"ports":[{"name":"http","port":8080,"protocol":"TCP"}],` +
		`"resources":{"cpu":2,"memoryGb":4},"exposurePolicy":"application"}`
	first := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-revisions", revision, "admit-pg-first")
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"decision":"new"`) {
		t.Fatalf("postgres first admission status=%d body=%s", first.Code, first.Body.String())
	}
	replay := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-revisions", revision, "admit-pg-replay")
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), `"decision":"identical"`) {
		t.Fatalf("postgres replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	conflict := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-revisions",
		strings.Replace(revision, strings.Repeat("a", 64), strings.Repeat("b", 64), 1), "admit-pg-conflict")
	if conflict.Code != http.StatusConflict {
		t.Fatalf("postgres conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	restarted := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/application-revisions/knowledge-app/1.0.0", "")
	if restarted.Code != http.StatusOK || !strings.Contains(restarted.Body.String(), `"applicationId":"knowledge-app"`) {
		t.Fatalf("postgres readback status=%d body=%s", restarted.Code, restarted.Body.String())
	}
}
