package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

func TestRetiredOperatorApplicationWritesPreserveHistoryAndHaveNoSideEffects(t *testing.T) {
	ctx := context.Background()
	store := newMemoryTableStore()
	handler, operationID := applicationDeploymentWorkerFixture(t, store, newTestService(&fakeLedgerClient{}, &fakeFabricClient{}))
	operator := operatorSessionForTest(t, handler)
	materialRow := seedApplicationDataMaterialHistoryForTest(t, store)
	row, found, err := store.GetRuntimeOperation(ctx, operationID)
	if err != nil || !found {
		t.Fatalf("deployment fixture found=%v err=%v", found, err)
	}
	intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, handler.app.markWorkspaceApplicationManualReview(ctx, row, intent, "historical_preflight_failed"))
	failed, _, err := store.GetRuntimeOperation(ctx, operationID)
	if err != nil {
		t.Fatal(err)
	}
	failedIntent, err := decodeWorkspaceApplicationDeploymentIntent(failed)
	if err != nil {
		t.Fatal(err)
	}
	revisionRow, found, err := store.AdmittedApplicationRevision(ctx, "knowledge-app", "1.0.0")
	if err != nil || !found {
		t.Fatalf("revision fixture found=%v err=%v", found, err)
	}

	// Observe all three real outbound clients. Any provider, receipt, credential
	// or Key request is recorded by this transport without making a network call.
	var externalCalls []string
	transport := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		externalCalls = append(externalCalls, r.Method+" "+r.URL.String())
		return nil, errors.New("unexpected external call from retired operator route")
	})}
	sub2API, err := clients.NewSub2APIHTTPClient(clients.Sub2APIConfig{BaseURL: "https://sub2api.example.test", AdminEmail: "operator@example.test", AdminPassword: "test-password", Timeout: time.Second}, transport)
	if err != nil {
		t.Fatal(err)
	}
	observedService := controlplane.NewService(
		clients.NewLedgerHTTPClient("https://ledger.example.test", "test-token", transport),
		clients.NewFabricHTTPClientWithCapability("https://fabric.example.test", "test-token", "test-capability", transport), sub2API)
	// Install the observed clients after startup authentication has established
	// the operator session; route closures retain this same service pointer.
	*handler.service = *observedService
	server := handler

	encode := func(value any) string {
		t.Helper()
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(payload)
	}
	snapshot := func() string {
		t.Helper()
		store.mu.Lock()
		defer store.mu.Unlock()
		return encode(map[string]any{"revisions": store.applicationRevisions, "dataMaterials": store.applicationDataMaterials, "operations": store.runtimeOps, "workspaces": store.workspaces, "auditEvents": store.auditEvents})
	}
	before := snapshot()
	revision := knowledgeApplicationRevisionForTest()
	revision.Version = "2.0.0"
	material := historicalApplicationDataMaterialForTest()
	material.Version = "data-2.0.0"
	deployment := struct {
		WorkspaceID    string                                             `json:"workspaceId"`
		ApplicationID  string                                             `json:"applicationId"`
		TargetRevision string                                             `json:"targetRevision"`
		Revision       contracts.WorkspaceApplicationRevision             `json:"revision"`
		Configuration  contracts.WorkspaceApplicationRuntimeConfiguration `json:"configuration"`
	}{"ws-alpha", revision.ApplicationID, revision.Version, revision, contracts.WorkspaceApplicationRuntimeConfiguration{Environment: map[string]string{"CONFIG": "retired-request"}}}
	for _, test := range []struct{ name, path, body string }{
		{"revision", "/api/operator/application-revisions", encode(revision)},
		{"data_material", "/api/operator/application-data-materials", encode(material)},
		{"deployment", "/api/operator/application-deployments", encode(deployment)},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, test.path, test.body, "retired-"+test.name)
			if response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("retired POST %s status=%d body=%s", test.path, response.Code, response.Body.String())
			}
			if snapshot() != before {
				t.Fatalf("retired POST %s changed persisted revision/data/deployment/workspace/audit state", test.path)
			}
			if len(externalCalls) != 0 {
				t.Fatalf("retired POST called external owners: %v", externalCalls)
			}
		})
	}
	for _, test := range []struct {
		name, path string
		expected   map[string]any
	}{
		{"revision", "/api/operator/application-revisions/knowledge-app/1.0.0", map[string]any{"decision": "identical", "revision": revisionRow}},
		{"data_material", "/api/operator/application-data-materials/knowledge-app/data-1.0.0", map[string]any{"decision": "identical", "dataMaterial": materialRow}},
		{"deployment", "/api/operator/application-deployments/" + operationID, map[string]any{"status": "manual_review", "intent": failedIntent}},
	} {
		t.Run("history_"+test.name, func(t *testing.T) {
			read := requestWithSession(t, server, operator, http.MethodGet, test.path, "")
			var actual, expected any
			if err := json.Unmarshal([]byte(encode(test.expected)), &expected); err != nil {
				t.Fatal(err)
			}
			if read.Code != http.StatusOK || json.Unmarshal(read.Body.Bytes(), &actual) != nil || !reflect.DeepEqual(actual, expected) {
				t.Fatalf("retained GET %s status=%d body=%s", test.path, read.Code, read.Body.String())
			}
		})
	}
	if snapshot() != before || len(externalCalls) != 0 {
		t.Fatalf("history reads changed state or called external owners: %v", externalCalls)
	}
}
