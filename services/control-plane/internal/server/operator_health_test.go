package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

type noRuntimeFanoutFabric struct {
	fakeFabricClient
	mu    sync.Mutex
	calls int
}

func (f *noRuntimeFanoutFabric) WorkspaceRuntimeStatus(_ context.Context, workspaceID string) (clients.WorkspaceRuntime, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return clients.WorkspaceRuntime{ID: "runtime-" + workspaceID, WorkspaceID: workspaceID, Status: "running", Ready: true}, nil
}

type runtimeHealthSummaryFabric struct {
	noRuntimeFanoutFabric
	observations     contracts.RuntimeObservations
	observationErr   error
	observationCalls int
}

type readinessLedger struct {
	fakeLedgerClient
	err   error
	calls int
}

func (l *readinessLedger) Ready(context.Context) error {
	l.calls++
	return l.err
}

type boundedOperatorHealthStore struct {
	*memoryTableStore
	listWorkspaceCalls int
	pageWorkspaceCalls int
}

func (s *boundedOperatorHealthStore) ListWorkspaces(ctx context.Context, accountID string) ([]map[string]any, error) {
	s.listWorkspaceCalls++
	return s.memoryTableStore.ListWorkspaces(ctx, accountID)
}

func (s *boundedOperatorHealthStore) PageWorkspaces(ctx context.Context, accountID string, query tablePageQuery) (tablePage, error) {
	s.pageWorkspaceCalls++
	return s.memoryTableStore.PageWorkspaces(ctx, accountID, query)
}

func (f *runtimeHealthSummaryFabric) RuntimeObservations(context.Context) (contracts.RuntimeObservations, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observationCalls++
	return f.observations, f.observationErr
}

func operatorRuntimeFixtureObservations(count int) contracts.RuntimeObservations {
	result := contracts.RuntimeObservations{ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Items: []contracts.RuntimeObservation{}}
	for i := 0; i < count; i++ {
		result.Items = append(result.Items, contracts.RuntimeObservation{ObjectRef: fmt.Sprintf("object-%d", i), WorkspaceID: fmt.Sprintf("ws-%d", i), Ownership: contracts.RuntimeOwnershipUnregistered, DesiredState: contracts.ResourceObservedRunning, ObservedState: contracts.ResourceObservedPending})
	}
	return result
}

func TestOperatorHealthUsesSingleFabricRuntimeObservationAndLocalSet(t *testing.T) {
	store := &boundedOperatorHealthStore{memoryTableStore: newMemoryTableStore()}
	fabric := &runtimeHealthSummaryFabric{observations: operatorRuntimeFixtureObservations(5)}
	for _, workspaceID := range []string{"ws-0", "ws-1"} {
		workspace := workspaceLaunchUnitActivationProjectionRow(t, workspaceID, "acct-alpha", "usr-alpha")
		mustStore(t, store.SaveWorkspace(context.Background(), workspace))
	}
	ledger := &readinessLedger{}
	server, err := NewPersistentServer(controlplane.NewService(ledger, fabric, newOperatorProjectionClient()), store)
	if err != nil {
		t.Fatal(err)
	}
	response := requestWithSession(t, server, reservedOperatorSessionForTest(t, server), http.MethodGet, "/api/operator/health", "")
	if response.Code != http.StatusOK {
		t.Fatalf("health = %d: %s", response.Code, response.Body.String())
	}
	if fabric.calls != 0 || fabric.observationCalls != 1 || store.listWorkspaceCalls != 1 || store.pageWorkspaceCalls != 0 {
		t.Fatalf("reads status=%d observations=%d lists=%d pages=%d", fabric.calls, fabric.observationCalls, store.listWorkspaceCalls, store.pageWorkspaceCalls)
	}
	health := mapField(decodeOperatorEnvelope(t, response), "data")
	runtime := mapField(health, "runtime")
	data := mapField(runtime, "data")
	if runtime["available"] != true || runtime["source"] != "runtime" || data["ready"] != false || data["businessTotal"] != float64(2) || data["observedTotal"] != float64(5) || data["unmatchedCount"] != float64(3) || data["attentionCount"] != float64(5) {
		t.Fatalf("runtime=%#v", runtime)
	}
	if _, exists := data["items"]; exists {
		t.Fatal("health leaks per-workspace items")
	}
	if ledger.calls != 1 || mapField(mapField(health, "ledger"), "data")["ready"] != true {
		t.Fatalf("ledger=%#v", health["ledger"])
	}
}

func TestOperatorHealthMarksLedgerUnavailableWhenReadyzFails(t *testing.T) {
	ledger := &readinessLedger{err: errors.New("postgres unavailable")}
	server, err := NewPersistentServer(controlplane.NewService(ledger, &fakeFabricClient{}, newOperatorProjectionClient()), newMemoryTableStore())
	if err != nil {
		t.Fatal(err)
	}
	response := requestWithSession(t, server, reservedOperatorSessionForTest(t, server), http.MethodGet, "/api/operator/health", "")
	if response.Code != http.StatusOK {
		t.Fatalf("operator health without Ledger = %d: %s", response.Code, response.Body.String())
	}
	var envelope map[string]any
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	ledgerEnvelope := mapField(mapField(envelope, "data"), "ledger")
	if ledger.calls != 1 || ledgerEnvelope["available"] != false || ledgerEnvelope["status"] != "unavailable" {
		t.Fatalf("Ledger readiness calls=%d envelope=%#v", ledger.calls, ledgerEnvelope)
	}
}

func TestOperatorHealthMarksRuntimeUnavailableWithoutRealProbe(t *testing.T) {
	store := newMemoryTableStore()
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, newOperatorProjectionClient()), store)
	if err != nil {
		t.Fatal(err)
	}
	response := requestWithSession(t, server, reservedOperatorSessionForTest(t, server), http.MethodGet, "/api/operator/health", "")
	if response.Code != http.StatusOK {
		t.Fatalf("operator health without runtime = %d: %s", response.Code, response.Body.String())
	}
	var envelope map[string]any
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	runtimeEnvelope := mapField(mapField(envelope, "data"), "runtime")
	if runtimeEnvelope["available"] != false || runtimeEnvelope["status"] != "unavailable" {
		t.Fatalf("Runtime without probe = %#v", runtimeEnvelope)
	}
}

func TestOperatorHealthNeverFansOutAcrossWorkspaceRuntimes(t *testing.T) {
	store := newMemoryTableStore()
	for index := 0; index < 1000; index++ {
		workspaceID := fmt.Sprintf("ws-%04d", index)
		mustStore(t, store.SaveWorkspace(context.Background(), map[string]any{
			"id": workspaceID, "ownerAccountId": "acct-alpha", "ownerUserId": "usr-alpha", "accountId": "acct-alpha", "state": "active",
			"createdAt": "2026-07-18T00:00:00Z", "updatedAt": "2026-07-19T00:00:00Z",
		}))
	}
	fabric := &noRuntimeFanoutFabric{}
	runtime := (&controlPlaneServer{tables: store}).operatorRuntimeHealth(context.Background(), controlplane.NewService(fakeLedgerClient{}, fabric, newOperatorProjectionClient()))
	fabric.mu.Lock()
	calls := fabric.calls
	fabric.mu.Unlock()
	if calls != 0 || runtime["available"] != false || runtime["status"] != "unavailable" {
		t.Fatalf("runtime health must fail closed without a Fabric summary: calls=%d runtime=%#v", calls, runtime)
	}
}
