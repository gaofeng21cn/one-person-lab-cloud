package server

import (
	"context"
	"testing"
	"time"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// The renewal fixtures stay because the provider reconciliation suite still
// seeds an expired Workspace with its compute and storage children. The
// renewal worker that used these fixtures is retired with the renewal
// execution.
type workspaceRenewalWorkerFixture struct {
	app            *controlPlaneServer
	service        *controlplane.Service
	sub2API        *monthlySub2API
	fabric         *monthlyFabric
	ledger         *monthlyLedger
	events         *[]string
	workspace      map[string]any
	compute        map[string]any
	storage        map[string]any
	paidThrough    time.Time
	renewedThrough time.Time
}

func newWorkspaceRenewalWorkerFixture(t *testing.T, balances []int64) workspaceRenewalWorkerFixture {
	t.Helper()
	return newWorkspaceRenewalWorkerFixtureAt(t, balances, time.Date(2026, 8, 31, 9, 30, 0, 0, time.UTC))
}

func newWorkspaceRenewalWorkerFixtureAt(t *testing.T, balances []int64, paidThrough time.Time) workspaceRenewalWorkerFixture {
	t.Helper()
	app, service, sub2API, fabric, ledger, events := newMonthlyBillingTest(t, balances)
	renewedThrough := nextBillingMonth(paidThrough, paidThrough.Day())
	ownerID, workspaceID := "usr-monthly-owner", "workspace-monthly"
	computeID, storageID := "compute-workspace-monthly", "storage-workspace-monthly"
	compute := monthlyActiveResource("compute", computeID, paidThrough)
	storage := monthlyActiveResource("storage", storageID, paidThrough)
	for _, row := range []map[string]any{compute, storage} {
		row["workspaceId"], row["ownerUserId"], row["autoRenew"] = workspaceID, ownerID, false
		if !monthlyPriceSnapshotAvailable(row) {
			t.Fatalf("test child price snapshot invalid: %#v", row)
		}
	}
	storage["computeAllocationId"] = computeID
	mustStore(t, app.tables.SaveCompute(context.Background(), compute))
	mustStore(t, app.tables.SaveStorage(context.Background(), storage))
	billing, code := workspaceBillingStateFromChildren(compute, storage, workspaceBillingChildIdentity{
		AccountID: "acct-monthly", OwnerUserID: ownerID, WorkspaceID: workspaceID, PackageID: "basic",
		ComputeID: computeID, StorageID: storageID, StorageGB: 10,
	})
	if code != "" {
		t.Fatalf("test Workspace billing state: %s", code)
	}
	billing["autoRenew"], billing["authorizedBy"], billing["authorizedAt"] = true, ownerID, paidThrough.AddDate(0, -1, 0).Format(time.RFC3339Nano)
	workspace := mergeMaps(map[string]any{
		"id": workspaceID, "accountId": "acct-monthly", "ownerAccountId": "acct-monthly", "ownerUserId": ownerID,
		"state": "running", "status": "running", "currentComputeAllocationId": computeID, "storageId": storageID,
		"currentAttachmentId": "attachment-workspace-monthly", "attachmentId": "attachment-workspace-monthly",
		"workspaceApiKeyId": int64(9),
	}, billing)
	mustStore(t, app.tables.SaveWorkspace(context.Background(), workspace))
	fabric.computeRenew = clients.ComputeAllocation{
		ID: computeID, AccountID: "acct-monthly", WorkspaceID: workspaceID, PackageID: "basic", Status: "running",
		Provider: "fabric", ProviderResourceID: stringValue(compute["providerResourceId"]), ProviderRequestID: "renew-compute-workspace",
		Zone: "provider-zone", Deadline: renewedThrough.Format(time.RFC3339),
	}
	fabric.storageRenew = clients.StorageVolume{
		ID: storageID, AccountID: "acct-monthly", WorkspaceID: workspaceID, Status: "available", Provider: "fabric",
		ProviderResourceID: stringValue(storage["providerResourceId"]), ProviderRequestID: "renew-storage-workspace", SizeGB: 10,
		Deadline: renewedThrough.Format(time.RFC3339), Zone: "provider-zone",
	}
	fabric.computeSync, fabric.storageSync = fabric.computeRenew, fabric.storageRenew
	return workspaceRenewalWorkerFixture{
		app: app, service: service, sub2API: sub2API, fabric: fabric, ledger: ledger, events: events,
		workspace: workspace, compute: compute, storage: storage, paidThrough: paidThrough, renewedThrough: renewedThrough,
	}
}

func (f workspaceRenewalWorkerFixture) operation(t *testing.T) map[string]any {
	t.Helper()
	operations, err := f.app.tables.ListRuntimeOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range operations {
		if stringValue(operation["action"]) == "workspace.renewal" && stringValue(operation["workspaceId"]) == stringValue(f.workspace["id"]) {
			return operation
		}
	}
	t.Fatal("Workspace renewal operation not found")
	return nil
}

func newWorkspaceRenewalRuntimeFixture(t *testing.T, balances []int64) workspaceRenewalWorkerFixture {
	t.Helper()
	fixture := newWorkspaceRenewalWorkerFixture(t, balances)
	seedD4RenewalRuntime(t, fixture.app.tables, fixture.workspace, fixture.fabric)
	return fixture
}
