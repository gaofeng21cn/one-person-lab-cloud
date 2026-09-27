package fabric

import (
	"context"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

// applicationObservationWorkload builds the physical inventory of one
// application component deployment exactly the way the publisher manifest
// declares it, so the ownership tests exercise the same label surface the
// Tencent discovery reads in production.
func applicationObservationWorkload(input WorkspaceApplicationRuntimeInput, componentName, accountID, runtimeID, operationID string) []any {
	tags := oplCostTags(accountID, input.WorkspaceID, runtimeID, operationID)
	labels := stringAnyMap(k8sCostLabels(tags))
	labels["oplcloud.cn/account-id"] = accountID
	labels["oplcloud.cn/workspace-id"] = k8sCostLabelValue(input.WorkspaceID)
	labels["oplcloud.cn/runtime-id"] = k8sCostLabelValue(runtimeID)
	labels["oplcloud.cn/runtime-operation-id"] = k8sCostLabelValue(operationID)
	labels["oplcloud.cn/component-name"] = k8sCostLabelValue(componentName)
	labels["app.kubernetes.io/name"] = "opl-workspace-application"
	annotations := stringAnyMap(tags)
	template := map[string]any{"metadata": map[string]any{"labels": labels}, "spec": map[string]any{"containers": []any{map[string]any{"name": "app", "image": "repo.example/apps/main@sha256:" + strings.Repeat("a", 64)}}}}
	deployment := map[string]any{"kind": "Deployment", "metadata": map[string]any{
		"uid": "deploy-" + componentName, "name": workspaceApplicationComponentResourceName(input, componentName),
		"generation": float64(1), "labels": labels, "annotations": annotations,
	}, "spec": map[string]any{"replicas": float64(1), "template": template}, "status": map[string]any{"observedGeneration": float64(1), "updatedReplicas": float64(1), "readyReplicas": float64(1), "availableReplicas": float64(1)}}
	return []any{deployment}
}

// applicationOwnerObservedRuntime runs the observation pipeline over one
// workload set and returns the single observation item.
func applicationOwnerObservedRuntime(t *testing.T, store *MemoryOperationStore, workloads []any) contracts.RuntimeObservation {
	t.Helper()
	service := NewServiceWithOperationStore(inventoryProvider(t, workloads), store)
	result, err := service.RuntimeObservations(context.Background())
	if err != nil {
		t.Fatalf("runtime observations: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("observation items = %d: %#v", len(result.Items), result.Items)
	}
	return result.Items[0]
}

// The overlay test above proves the verified path. These tests pin every
// rejection shape: the platform must keep failing closed on foreign identity,
// corrupted records and undeclared components instead of guessing an owner.

func TestApplicationRuntimeOwnershipRejectsForeignIdentity(t *testing.T) {
	store := NewMemoryOperationStore()
	creator := runtimeTestService(&recordingApplicationRuntimeProvider{}, store)
	revision := applicationRevisionForTest()
	revision.Dependencies = nil
	input := applicationRuntimeInput("app-owner-foreign", revision)
	if _, err := creator.CreateWorkspaceApplicationRuntime(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	rows, err := store.List(context.Background())
	if err != nil || len(rows) != 1 {
		t.Fatalf("durable records=%d err=%v", len(rows), err)
	}
	workloads := applicationObservationWorkload(input, "main", "acct-alpha", applicationRuntimeID(input), input.RuntimeOperationID)
	if observed := applicationOwnerObservedRuntime(t, store, workloads); observed.Ownership != contracts.RuntimeOwnershipVerified {
		t.Fatalf("baseline ownership = %s (%s)", observed.Ownership, observed.ReasonCode)
	}
	foreign := applicationObservationWorkload(input, "main", "acct-beta", applicationRuntimeID(input), input.RuntimeOperationID)
	if observed := applicationOwnerObservedRuntime(t, store, foreign); observed.Ownership == contracts.RuntimeOwnershipVerified {
		t.Fatalf("a foreign account must never verify: %#v", observed)
	}
	renamed := applicationObservationWorkload(input, "main", "acct-alpha", "rt_app_someone-else", input.RuntimeOperationID)
	if observed := applicationOwnerObservedRuntime(t, store, renamed); observed.Ownership == contracts.RuntimeOwnershipVerified {
		t.Fatalf("a foreign runtime identity must never verify: %#v", observed)
	}
}

func TestApplicationRuntimeOwnershipRejectsCorruptedRecord(t *testing.T) {
	store := NewMemoryOperationStore()
	creator := runtimeTestService(&recordingApplicationRuntimeProvider{}, store)
	revision := applicationRevisionForTest()
	revision.Dependencies = nil
	input := applicationRuntimeInput("app-owner-corrupt", revision)
	if _, err := creator.CreateWorkspaceApplicationRuntime(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	rows, err := store.List(context.Background())
	if err != nil || len(rows) != 1 {
		t.Fatalf("durable records=%d err=%v", len(rows), err)
	}
	workloads := applicationObservationWorkload(input, "main", "acct-alpha", applicationRuntimeID(input), input.RuntimeOperationID)
	if observed := applicationOwnerObservedRuntime(t, store, workloads); observed.Ownership != contracts.RuntimeOwnershipVerified {
		t.Fatalf("baseline ownership = %s (%s)", observed.Ownership, observed.ReasonCode)
	}
	// A request hash that no longer proves the retained input is a corrupted
	// record: the workload stays, but ownership must not verify.
	corrupted := rows[0]
	corrupted.RequestHash = strings.Repeat("0", 64)
	rebuilt := NewMemoryOperationStore()
	if err := rebuilt.Append(context.Background(), corrupted); err != nil {
		t.Fatal(err)
	}
	if observed := applicationOwnerObservedRuntime(t, rebuilt, workloads); observed.Ownership != contracts.RuntimeOwnershipConflict {
		t.Fatalf("corrupted record ownership = %s (%s)", observed.Ownership, observed.ReasonCode)
	}
}

func TestApplicationRuntimeOwnershipRejectsUndeclaredComponent(t *testing.T) {
	store := NewMemoryOperationStore()
	creator := runtimeTestService(&recordingApplicationRuntimeProvider{}, store)
	revision := applicationRevisionForTest()
	revision.Dependencies = nil
	input := applicationRuntimeInput("app-owner-undeclared", revision)
	if _, err := creator.CreateWorkspaceApplicationRuntime(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	workloads := applicationObservationWorkload(input, "unlisted-sidecar", "acct-alpha", applicationRuntimeID(input), input.RuntimeOperationID)
	observed := applicationOwnerObservedRuntime(t, store, workloads)
	if observed.Ownership == contracts.RuntimeOwnershipVerified {
		t.Fatalf("an undeclared component must never verify: %#v", observed)
	}
	if observed.Ownership != contracts.RuntimeOwnershipConflict {
		t.Fatalf("an identity-matching deployment of a component the revision never declared is a conflict: %s (%s)", observed.Ownership, observed.ReasonCode)
	}
}

func TestApplicationRuntimeOwnershipKeepsUnownedAndUnrecordedDistinct(t *testing.T) {
	store := NewMemoryOperationStore()
	revision := applicationRevisionForTest()
	revision.Dependencies = nil
	input := applicationRuntimeInput("app-owner-unrecorded", revision)
	// A live application deployment whose creation record was never written
	// stays unregistered; it must not inherit another workspace record.
	workloads := applicationObservationWorkload(input, "main", "acct-alpha", applicationRuntimeID(input), input.RuntimeOperationID)
	if observed := applicationOwnerObservedRuntime(t, store, workloads); observed.Ownership != contracts.RuntimeOwnershipUnregistered {
		t.Fatalf("unrecorded deployment ownership = %s", observed.Ownership)
	}
}

func TestApplicationRuntimeOwnershipVerifiesEveryDeclaredComponent(t *testing.T) {
	// The default test revision declares main plus the retrieval dependency;
	// both physical deployments verify on their own component identity, and a
	// legitimate multi-component application is never an owner conflict.
	store := NewMemoryOperationStore()
	creator := runtimeTestService(&recordingApplicationRuntimeProvider{}, store)
	revision := applicationRevisionForTest()
	input := applicationRuntimeInput("app-owner-multi", revision)
	if _, err := creator.CreateWorkspaceApplicationRuntime(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	workloads := append(
		applicationObservationWorkload(input, "main", "acct-alpha", applicationRuntimeID(input), input.RuntimeOperationID),
		applicationObservationWorkload(input, "retrieval", "acct-alpha", applicationRuntimeID(input), input.RuntimeOperationID)...,
	)
	service := NewServiceWithOperationStore(inventoryProvider(t, workloads), store)
	result, err := service.RuntimeObservations(context.Background())
	if err != nil || len(result.Items) != 2 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	for _, item := range result.Items {
		if item.Ownership != contracts.RuntimeOwnershipVerified || item.RuntimeID != applicationRuntimeID(input) || item.AccountID != "acct-alpha" {
			t.Fatalf("component ownership = %#v", item)
		}
	}
}

func TestApplicationRuntimeOwnerCandidatesMemoryRoundtrip(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryOperationStore()
	creator := runtimeTestService(&recordingApplicationRuntimeProvider{}, store)
	revision := applicationRevisionForTest()
	revision.Dependencies = nil
	input := applicationRuntimeInput("app-owner-query", revision)
	if _, err := creator.CreateWorkspaceApplicationRuntime(ctx, input); err != nil {
		t.Fatal(err)
	}
	candidates, err := store.WorkspaceApplicationRuntimeOwnerCandidates(ctx, input.WorkspaceID)
	if err != nil || len(candidates) != 1 || candidates[0].ResourceKind != "workspace_application_runtime" || candidates[0].Status != "succeeded" {
		t.Fatalf("candidates=%#v err=%v", candidates, err)
	}
	if other, err := store.WorkspaceApplicationRuntimeOwnerCandidates(ctx, "workspace-other"); err != nil || len(other) != 0 {
		t.Fatalf("workspace scoping leaked: %#v err=%v", other, err)
	}
}
