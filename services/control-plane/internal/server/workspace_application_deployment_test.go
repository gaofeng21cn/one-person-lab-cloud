package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/domain/provisioning"
)

func admitKnowledgeRevisionForTest(t *testing.T, server http.Handler, operator *httptest.ResponseRecorder) {
	t.Helper()
	revision := `{"schemaVersion":1,"applicationId":"knowledge-app","version":"1.0.0","platform":"linux/amd64",` +
		`"image":"repo.example/apps/knowledge@sha256:` + strings.Repeat("a", 64) + `",` +
		`"ports":[{"name":"http","port":8080,"protocol":"TCP"}],` +
		`"resources":{"cpu":2,"memoryGb":4},"exposurePolicy":"application"}`
	admitted := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-revisions", revision, "admit-deploy-test")
	if admitted.Code != http.StatusOK {
		t.Fatalf("revision admission status=%d body=%s", admitted.Code, admitted.Body.String())
	}
}

func deployIntentBody(configurationDigest string) string {
	return `{"workspaceId":"ws-alpha","applicationId":"knowledge-app","targetRevision":"1.0.0","configurationDigest":"` + configurationDigest + `"}`
}

func TestApplicationDeploymentIntentHTTP(t *testing.T) {
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	operator := operatorSessionForTest(t, fixture.server)
	admitKnowledgeRevisionForTest(t, fixture.server, operator)

	first := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, "/api/operator/application-deployments", deployIntentBody(strings.Repeat("c", 64)), "deploy-knowledge-first")
	if first.Code != http.StatusAccepted {
		t.Fatalf("first intent status=%d body=%s", first.Code, first.Body.String())
	}
	var admitted struct {
		Intent struct {
			OperationID              string `json:"operationId"`
			Phase                    string `json:"phase"`
			ExpectedWorkspaceVersion int64  `json:"expectedWorkspaceVersion"`
			CurrentBinding           string `json:"currentBinding"`
		} `json:"intent"`
	}
	if json.Unmarshal(first.Body.Bytes(), &admitted) != nil || admitted.Intent.OperationID == "" ||
		admitted.Intent.Phase != workspaceApplicationDeploymentIntentPhase ||
		admitted.Intent.ExpectedWorkspaceVersion != 0 || admitted.Intent.CurrentBinding != provisioning.ApplicationBindingEmpty {
		t.Fatalf("first intent body=%s", first.Body.String())
	}

	replay := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, "/api/operator/application-deployments", deployIntentBody(strings.Repeat("c", 64)), "deploy-knowledge-first")
	var replayed struct {
		Intent struct {
			OperationID string `json:"operationId"`
		} `json:"intent"`
	}
	if replay.Code != http.StatusAccepted || json.Unmarshal(replay.Body.Bytes(), &replayed) != nil ||
		replayed.Intent.OperationID != admitted.Intent.OperationID {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}

	changed := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, "/api/operator/application-deployments", deployIntentBody(strings.Repeat("d", 64)), "deploy-knowledge-first")
	if changed.Code != http.StatusConflict {
		t.Fatalf("changed intent under the same key status=%d body=%s", changed.Code, changed.Body.String())
	}

	notAdmitted := `{"workspaceId":"ws-alpha","applicationId":"unknown-app","targetRevision":"1.0.0","configurationDigest":"` + strings.Repeat("c", 64) + `"}`
	missing := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, "/api/operator/application-deployments", notAdmitted, "deploy-unknown")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), "workspace_application_revision_not_found") {
		t.Fatalf("unadmitted revision status=%d body=%s", missing.Code, missing.Body.String())
	}

	workspace := cloneMap(fixture.workspace)
	workspace["applicationBinding"] = "other-app@2.0.0"
	mustStore(t, fixture.store.SaveWorkspace(context.Background(), workspace))
	mismatch := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, "/api/operator/application-deployments", deployIntentBody(strings.Repeat("c", 64)), "deploy-knowledge-mismatch")
	if mismatch.Code != http.StatusConflict || !strings.Contains(mismatch.Body.String(), "workspace_application_deployment_transition_invalid") {
		t.Fatalf("binding mismatch status=%d body=%s", mismatch.Code, mismatch.Body.String())
	}

	suspended := cloneMap(fixture.workspace)
	suspended["state"], suspended["status"] = "suspended", "suspended"
	mustStore(t, fixture.store.SaveWorkspace(context.Background(), suspended))
	unready := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, "/api/operator/application-deployments", deployIntentBody(strings.Repeat("c", 64)), "deploy-knowledge-unready")
	if unready.Code != http.StatusConflict || !strings.Contains(unready.Body.String(), "workspace_application_resources_unready") {
		t.Fatalf("unready workspace status=%d body=%s", unready.Code, unready.Body.String())
	}

	read := requestWithSession(t, fixture.server, operator, http.MethodGet, "/api/operator/application-deployments/"+admitted.Intent.OperationID, "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"phase":"intent"`) {
		t.Fatalf("intent read status=%d body=%s", read.Code, read.Body.String())
	}
}

func TestApplicationDeploymentIntentPostgres(t *testing.T) {
	admin := openControlPlaneTestPostgres(t)
	database := fmt.Sprintf("control_plane_application_deploy_%d", time.Now().UnixNano())
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
	command := workspaceLaunchResourceOnlyUnitCommand()
	command.OperationID, command.AccountID, command.WorkspaceID = "workspace-launch-alpha", "acct-alpha", "ws-alpha"
	operation, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := operation.stagePlan()
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range plan[:len(plan)-1] {
		operation.Stage = stage
		facts := workspaceLaunchReadyFacts(stage)
		for key, value := range map[string]any{"computeAllocationId": "compute-alpha", "storageId": "storage-alpha", "attachmentId": "attachment-alpha"} {
			if _, ok := facts[key]; ok {
				facts[key] = value
			}
		}
		observation, err := reduceWorkspaceLaunchStageObservation(&operation, workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: facts})
		if err != nil {
			t.Fatalf("seed stage %s: %v", stage, err)
		}
		attempt := operation.Attempts[stage]
		attempt.Attempted, attempt.Confirmed, attempt.Status = 1, 1, "confirmed"
		attempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(operation, 1)
		operation.Attempts[stage], operation.Observations[stage] = attempt, observation
	}
	operation.Stage, operation.Status = contracts.StageSucceeded, contracts.StatusSucceeded
	for key, value := range map[string]any{
		"paidThrough": "2026-10-12T00:00:00Z", "periodStart": "2026-09-12T00:00:00Z", "billingAnchorDay": 12,
	} {
		operation.raw[key], _ = json.Marshal(value)
	}
	launchRow, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), launchRow))
	workspace, err := workspaceLaunchActivationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveWorkspace(context.Background(), workspace))

	operatorSession := operatorSessionForTest(t, server)
	admitKnowledgeRevisionForTest(t, server, operatorSession)
	first := requestWithMutationKeyForTest(t, server, operatorSession, http.MethodPost, "/api/operator/application-deployments", deployIntentBody(strings.Repeat("c", 64)), "deploy-pg-first")
	if first.Code != http.StatusAccepted || !strings.Contains(first.Body.String(), `"phase":"intent"`) {
		t.Fatalf("postgres intent status=%d body=%s", first.Code, first.Body.String())
	}
	var admitted struct {
		Intent struct {
			OperationID              string `json:"operationId"`
			ExpectedWorkspaceVersion int64  `json:"expectedWorkspaceVersion"`
		} `json:"intent"`
	}
	if json.Unmarshal(first.Body.Bytes(), &admitted) != nil || admitted.Intent.ExpectedWorkspaceVersion != 0 {
		t.Fatalf("postgres intent body=%s", first.Body.String())
	}
	restarted, err := NewPersistentServer(newTestService(&fakeLedgerClient{}, &fakeFabricClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	read := requestWithSession(t, restarted, operatorSessionForTest(t, restarted), http.MethodGet, "/api/operator/application-deployments/"+admitted.Intent.OperationID, "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), admitted.Intent.OperationID) {
		t.Fatalf("postgres restart readback status=%d body=%s", read.Code, read.Body.String())
	}
}
