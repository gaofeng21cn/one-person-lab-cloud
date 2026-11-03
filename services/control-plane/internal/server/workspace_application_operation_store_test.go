package server

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

type workspaceApplicationLateRuntimeFabric struct {
	fakeFabricClient
	entered chan struct{}
	release chan struct{}
	claimed atomic.Bool
}

func (f *workspaceApplicationLateRuntimeFabric) EnsureWorkspaceApplicationRuntime(ctx context.Context, input clients.WorkspaceApplicationRuntimeInput, _ string) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	if f.claimed.CompareAndSwap(false, true) {
		close(f.entered)
		select {
		case <-f.release:
		case <-ctx.Done():
			return contracts.WorkspaceApplicationRuntimeObservation{}, ctx.Err()
		}
	}
	components := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)
	for index := range components {
		components[index].State = "ready"
	}
	return contracts.WorkspaceApplicationRuntimeObservation{SchemaVersion: 1, WorkspaceID: input.WorkspaceID,
		RuntimeID: contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID), Status: "ready", Components: components}, nil
}

func TestWorkspaceApplicationOperationCASMemory(t *testing.T) {
	testWorkspaceApplicationOperationCAS(t, newMemoryTableStore())
}

func TestWorkspaceApplicationOperationCASPostgres(t *testing.T) {
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	testWorkspaceApplicationOperationCAS(t, store)
}

func testWorkspaceApplicationOperationCAS(t *testing.T, store controlPlaneTableStore) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	fabric := &workspaceApplicationLateRuntimeFabric{entered: make(chan struct{}), release: make(chan struct{})}
	service := newTestService(&fakeLedgerClient{}, fabric)
	first, operationID := applicationDeploymentWorkerFixture(t, store, service)
	secondServer, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	second := secondServer.(*controlPlaneHTTPHandler).app

	// Separate server locks model two replicas. Hold the first provider response
	// until the second replica commits activation and its receipt.
	late := make(chan error, 1)
	go func() { late <- first.app.runWorkspaceApplicationDeployment(ctx, service, operationID) }()
	select {
	case <-fabric.entered:
	case <-ctx.Done():
		t.Fatal("first worker did not reach runtime creation")
	}
	runDeploymentWorkerToCompletion(t, second, service, operationID)
	completed, _, _ := store.GetRuntimeOperation(ctx, operationID)
	close(fabric.release)
	select {
	case err := <-late:
		if !errors.Is(err, errWorkspaceApplicationOperationCASConflict) {
			t.Fatalf("late runtime result error=%v", err)
		}
	case <-ctx.Done():
		t.Fatal("late worker did not finish")
	}
	retained, _, _ := store.GetRuntimeOperation(ctx, operationID)
	if retained["result"] != completed["result"] || retained["status"] != "succeeded" {
		t.Fatal("late runtime result replaced the completed deployment")
	}
	workspace, _, _ := store.GetWorkspace(ctx, "ws-alpha")
	selected, found, err := second.currentWorkspaceApplicationDeployment(ctx, workspace)
	if err != nil || !found || selected.OperationID != operationID || selected.ActivationAt == "" || selected.Phase != workspaceApplicationDeploymentActivePhase {
		t.Fatalf("late worker damaged current selection: found=%v intent=%+v err=%v", found, selected, err)
	}
	if err := first.app.runWorkspaceApplicationDeployment(ctx, service, operationID); err != nil {
		t.Fatalf("worker could not resume from current state: %v", err)
	}

	// Even with a fresh expected result, progress persistence cannot rewrite
	// the accepted command by supplying a newly computed valid request hash.
	changed := selected
	changed.TargetRevision = "other-revision"
	changed.RequestHash = workspaceApplicationDeploymentRequestHash(changed)
	before := stringValue(retained["result"])
	if err := second.persistWorkspaceApplicationDeployment(ctx, retained, changed, "succeeded"); !errors.Is(err, errWorkspaceApplicationOperationCASConflict) {
		t.Fatalf("changed deployment command error=%v", err)
	}
	if stringValue(retained["result"]) != before {
		t.Fatal("failed deployment persistence mutated the caller snapshot")
	}
	for _, field := range []string{"id", "operationId", "accountId", "workspaceId", "resourceId", "resourceKind", "action", "createdAt"} {
		desired := cloneMap(retained)
		desired[field] = stringValue(desired[field]) + "-changed"
		if err := store.PersistWorkspaceApplicationOperation(ctx, before, desired); !errors.Is(err, errWorkspaceApplicationOperationCASConflict) {
			t.Fatalf("changed row identity %s error=%v", field, err)
		}
	}
}

func TestWorkspaceApplicationProgressRejectsRetiredActionsMemory(t *testing.T) {
	testWorkspaceApplicationProgressRejectsRetiredActions(t, newMemoryTableStore())
}

func TestWorkspaceApplicationProgressRejectsRetiredActionsPostgres(t *testing.T) {
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	testWorkspaceApplicationProgressRejectsRetiredActions(t, store)
}

func testWorkspaceApplicationProgressRejectsRetiredActions(t *testing.T, store controlPlaneTableStore) {
	t.Helper()
	ctx := context.Background()
	request := workspaceDefaultApplicationRequest{SchemaVersion: 1, OperationID: workspaceDefaultApplicationOperationID("workspace-launch-alpha"), LaunchOperationID: "workspace-launch-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", OwnerUserID: "owner-alpha", Sub2APIUserID: 41, WorkspaceKeyGroupID: 5, Revision: defaultOPLApplicationRevision("repo.example/opl-app@sha256:" + strings.Repeat("b", 64)), Phase: "credentials_required"}
	for _, phase := range []string{"credentials_required", "preparing_credentials", "waiting_resources", "installing", "deployed", "failed"} {
		t.Run(phase, func(t *testing.T) {
			request.Phase = phase
			row, err := workspaceDefaultApplicationRow(request)
			if err != nil {
				t.Fatal(err)
			}
			mustStore(t, store.SaveRuntimeOperation(ctx, row))
			before, found, err := store.GetRuntimeOperation(ctx, request.OperationID)
			if err != nil || !found {
				t.Fatal("history missing", err)
			}
			desired := cloneMap(before)
			desired["status"] = "succeeded"
			next := request
			next.Phase, next.LastError = "deployed", "retired-writer"
			payload, err := json.Marshal(next)
			if err != nil {
				t.Fatal(err)
			}
			desired["result"] = string(payload)
			if err := store.PersistWorkspaceApplicationOperation(ctx, stringValue(before["result"]), desired); !errors.Is(err, errWorkspaceApplicationOperationCASConflict) {
				t.Fatalf("retired progress write error=%v", err)
			}
			retained, found, err := store.GetRuntimeOperation(ctx, request.OperationID)
			if err != nil || !found || !reflect.DeepEqual(retained, before) {
				t.Fatalf("retired progress changed history: retained=%+v err=%v", retained, err)
			}
		})
	}
	other := map[string]any{"id": "unrelated-application-cas-test", "operationId": "unrelated-application-cas-test", "action": "workspace.gateway_key.rotate", "result": "{}", "status": "started"}
	mustStore(t, store.SaveRuntimeOperation(ctx, other))
	other["status"] = "succeeded"
	if err := store.PersistWorkspaceApplicationOperation(ctx, "{}", other); !errors.Is(err, errWorkspaceApplicationOperationCASConflict) {
		t.Fatalf("unrelated action progress write error=%v", err)
	}
}
