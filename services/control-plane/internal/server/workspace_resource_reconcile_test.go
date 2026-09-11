package server

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

type workspaceResourceReconcileFabric struct {
	*monthlyFabric
	facts       []clients.ProviderFact
	readErr     error
	afterRead   func()
	losePower   bool
	invalidRead bool
}

func (f *workspaceResourceReconcileFabric) ProviderFactsBatch(_ context.Context, input clients.ProviderFactsBatchInput) (clients.ProviderFactsBatch, error) {
	f.providerFactInputs = append(f.providerFactInputs, input)
	if f.afterRead != nil {
		f.afterRead()
		f.afterRead = nil
	}
	return clients.ProviderFactsBatch{Items: append([]clients.ProviderFact(nil), f.facts...)}, f.readErr
}

func (f *workspaceResourceReconcileFabric) ReadWorkspaceRuntimePower(ctx context.Context, input contracts.WorkspaceRuntimePowerInput) (contracts.WorkspaceRuntimePowerResult, error) {
	result, err := f.monthlyFabric.ReadWorkspaceRuntimePower(ctx, input)
	if f.invalidRead {
		result.Binding.RuntimeID = "different-runtime"
	}
	return result, err
}

func (f *workspaceResourceReconcileFabric) SetWorkspaceRuntimePower(ctx context.Context, input contracts.WorkspaceRuntimePowerInput) (contracts.WorkspaceRuntimePowerResult, error) {
	result, err := f.monthlyFabric.SetWorkspaceRuntimePower(ctx, input)
	if f.losePower {
		f.losePower = false
		return contracts.WorkspaceRuntimePowerResult{}, errors.New("power response lost")
	}
	return result, err
}

func newWorkspaceResourceReconcileFixture(t *testing.T) (workspaceRenewalWorkerFixture, *workspaceResourceReconcileFabric) {
	t.Helper()
	fixture := newWorkspaceRenewalRuntimeFixture(t, nil)
	// Model today's installation: Workspace/launch identities exist, CP's old
	// standalone compute/storage projections do not.
	mustStore(t, fixture.app.tables.DeleteCompute(context.Background(), stringValue(fixture.compute["id"])))
	mustStore(t, fixture.app.tables.DeleteStorage(context.Background(), stringValue(fixture.storage["id"])))
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	end := nextBillingMonth(start, start.Day())
	fixture.workspace["periodStart"], fixture.workspace["paidThrough"] = start.Format(time.RFC3339), end.Format(time.RFC3339)
	fixture.workspace["nextRenewalAt"], fixture.workspace["billingAnchorDay"] = end.Add(-monthlyRenewalLead).Format(time.RFC3339), int64(start.Day())
	mustStore(t, fixture.app.tables.SaveWorkspace(context.Background(), fixture.workspace))
	fixture.workspace, _ = fixture.app.getWorkspace(stringValue(fixture.workspace["id"]))
	fabric := &workspaceResourceReconcileFabric{monthlyFabric: fixture.fabric}
	for _, resource := range []struct{ kind, id string }{{"storage", stringValue(fixture.workspace["storageId"])}, {"compute", stringValue(fixture.workspace["computeAllocationId"])}} {
		fabric.facts = append(fabric.facts, clients.ProviderFact{
			AccountID: stringValue(fixture.workspace["accountId"]), WorkspaceID: stringValue(fixture.workspace["id"]), ResourceType: resource.kind, ResourceID: resource.id, Available: true,
			Facts: clients.ProviderResourceFacts{ProviderID: "provider-" + resource.id, Status: "running", LastReadAt: time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)},
		})
	}
	fixture.service = controlplane.NewService(fixture.ledger, fabric, fixture.sub2API)
	return fixture, fabric
}

func assertResourceReconcileNoFinancialOrResourceMutation(t *testing.T, fixture workspaceRenewalWorkerFixture) {
	t.Helper()
	if len(fixture.sub2API.charges) != 0 || len(fixture.sub2API.refunds) != 0 || len(fixture.fabric.computeCreateKeys) != 0 || len(fixture.fabric.storageCreateKeys) != 0 || len(fixture.fabric.computeRenewKeys) != 0 || len(fixture.fabric.storageRenewKeys) != 0 || fixture.fabric.computeDestroyed || len(fixture.ledger.receipts) != 0 {
		t.Fatalf("provider reconciliation changed money, bought/renewed/deleted resources, or invented a billing receipt: events=%v", *fixture.events)
	}
}

func TestWorkspaceResourceReconcileClosesExternallyDeletedResourceWithoutChildRows(t *testing.T) {
	for _, kind := range []string{"compute", "storage"} {
		t.Run(kind, func(t *testing.T) {
			fixture, fabric := newWorkspaceResourceReconcileFixture(t)
			for i := range fabric.facts {
				if fabric.facts[i].ResourceType == kind {
					fabric.facts[i].Facts.Status = "external_deleted"
				}
			}
			before := cloneMap(fixture.workspace)
			if err := fixture.app.runProviderReconcileOnce(context.Background(), fixture.service, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			after, _ := fixture.app.getWorkspace(stringValue(before["id"]))
			wantState, wantStatus := "suspended", "suspended"
			if kind == "storage" {
				wantState, wantStatus = "data_deleted", "unrecoverable"
			}
			if after["state"] != wantState || after["status"] != wantStatus || after["autoRenew"] != false {
				t.Fatalf("missing %s did not close Workspace: %#v", kind, after)
			}
			for _, key := range []string{"currentComputeAllocationId", "computeAllocationId", "storageId", "currentAttachmentId", "attachmentId", "runtimeId", "paidThrough", "periodStart", "purchaseReceiptId", "renewalStatus", "authorizedAt", "authorizedBy", "totalUsdMicros"} {
				if !reflect.DeepEqual(after[key], before[key]) {
					t.Fatalf("reconciliation rewrote retained %s: %v -> %v", key, before[key], after[key])
				}
			}
			response, accessReason := fixture.app.workspaceAccessResponse(context.Background(), after, time.Now().UTC())
			wantReason := "workspace_suspended"
			if kind == "storage" {
				wantReason = "workspace_storage_destroyed"
			}
			if accessReason != wantReason || response["openable"] != false {
				t.Fatalf("inactive Workspace remains openable: %#v", response)
			}
			if len(fabric.runtimePowerCalls) != 1 || fabric.runtimePowerState != "suspended" || fabric.runtimePowerCalls[0].PaidThrough != before["paidThrough"] || fabric.runtimePowerCalls[0].SuspensionReason != contracts.WorkspaceRuntimeSuspensionProviderResourceAbsent || fabric.runtimePowerCalls[0].MissingResourceType != kind {
				t.Fatalf("Runtime was not suspended with original paid period and explicit absence: %#v", fabric.runtimePowerCalls)
			}
			for range 2 {
				if err := fixture.app.runProviderReconcileOnce(context.Background(), fixture.service, time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
			}
			audits, err := fixture.app.tables.ListAuditEvents(context.Background(), stringValue(before["accountId"]))
			if err != nil || len(audits) != 1 || audits[0]["action"] != workspaceResourceReconcileAction || len(fabric.runtimePowerCalls) != 1 {
				t.Fatalf("replay duplicated state audit or suspension: audit=%#v power=%v err=%v", audits, fabric.runtimePowerCalls, err)
			}
			mustStore(t, fixture.app.tables.SaveWorkspace(context.Background(), before))
			after, _ = fixture.app.getWorkspace(stringValue(before["id"]))
			if after["state"] != wantState || after["autoRenew"] != false {
				t.Fatal("stale projection revived externally deleted Workspace")
			}
			assertResourceReconcileNoFinancialOrResourceMutation(t, fixture)
		})
	}
}

func TestWorkspaceResourceReconcileUnknownCannotDestroyEntitlement(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*workspaceResourceReconcileFabric)
	}{
		{"timeout", func(f *workspaceResourceReconcileFabric) { f.readErr = errors.New("timeout") }},
		{"partial_identity", func(f *workspaceResourceReconcileFabric) {
			f.facts[0].Available = false
			f.facts[0].ErrorCode = "partial_identity"
		}},
		{"stale", func(f *workspaceResourceReconcileFabric) {
			f.facts[0].Facts.LastReadAt = time.Now().Add(-2 * providerFreshnessWindow()).UTC().Format(time.RFC3339Nano)
		}},
		{"future", func(f *workspaceResourceReconcileFabric) {
			f.facts[0].Facts.LastReadAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
		}},
		{"wrong_workspace", func(f *workspaceResourceReconcileFabric) { f.facts[0].WorkspaceID = "another-workspace" }},
		{"observation_only", func(f *workspaceResourceReconcileFabric) {
			f.facts[0].Facts.Status = "running"
			f.facts[0].Observation = &contracts.ResourceObservation{Available: true, State: contracts.ResourceObservedAbsent, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, fabric := newWorkspaceResourceReconcileFixture(t)
			fabric.facts[0].Facts.Status = "external_deleted"
			test.change(fabric)
			_ = fixture.app.runProviderReconcileOnce(context.Background(), fixture.service, time.Now().UTC())
			after, _ := fixture.app.getWorkspace(stringValue(fixture.workspace["id"]))
			if after["state"] != "running" || after["autoRenew"] != true || len(fabric.runtimePowerCalls) != 0 {
				t.Fatalf("uncertain provider read changed entitlement: %#v", after)
			}
			audits, _ := fixture.app.tables.ListAuditEvents(context.Background(), stringValue(after["accountId"]))
			if len(audits) != 0 {
				t.Fatal("unknown fact created successful absence audit")
			}
			assertResourceReconcileNoFinancialOrResourceMutation(t, fixture)
		})
	}
}

func TestWorkspaceResourceReconcileStorageAbsentSurvivesUnknownComputeAndLostPowerResponse(t *testing.T) {
	fixture, fabric := newWorkspaceResourceReconcileFixture(t)
	fabric.facts[0].Facts.Status = "external_deleted"
	fabric.facts[1].Available, fabric.facts[1].ErrorCode = false, "partial_identity_machine_missing_tke_instance_missing"
	fabric.losePower = true
	if err := fixture.app.runProviderReconcileOnce(context.Background(), fixture.service, time.Now().UTC()); err == nil {
		t.Fatal("lost power response claimed success")
	}
	after, _ := fixture.app.getWorkspace(stringValue(fixture.workspace["id"]))
	if after["state"] != "data_deleted" || after["autoRenew"] != false || fabric.runtimePowerState != "suspended" {
		t.Fatal("confirmed storage loss did not close customer access before power readback")
	}
	// A new CP process resumes from Workspace truth; owner readback prevents a
	// second Runtime mutation when the first response was lost.
	app, err := newControlPlaneAppWithStore(fixture.app.tables.(*memoryTableStore))
	if err != nil {
		t.Fatal(err)
	}
	if err := app.runProviderReconcileOnce(context.Background(), fixture.service, time.Now().UTC()); err != nil || len(fabric.runtimePowerCalls) != 1 {
		t.Fatalf("restart did not converge by readback: calls=%v err=%v", fabric.runtimePowerCalls, err)
	}
	assertResourceReconcileNoFinancialOrResourceMutation(t, fixture)
}

func TestWorkspaceResourceReconcileRejectsConcurrentWorkspaceChange(t *testing.T) {
	fixture, fabric := newWorkspaceResourceReconcileFixture(t)
	fabric.facts[0].Facts.Status = "external_deleted"
	fabric.afterRead = func() {
		workspace := cloneMap(fixture.workspace)
		workspace["autoRenew"] = false
		mustStore(t, fixture.app.tables.SaveWorkspace(context.Background(), workspace))
	}
	if err := fixture.app.runProviderReconcileOnce(context.Background(), fixture.service, time.Now().UTC()); !errors.Is(err, errWorkspaceResourceReconcileConflict) {
		t.Fatalf("stale read did not lose CAS: %v", err)
	}
	after, _ := fixture.app.getWorkspace(stringValue(fixture.workspace["id"]))
	if after["state"] != "running" || len(fabric.runtimePowerCalls) != 0 {
		t.Fatal("stale provider reconciliation mutated Workspace or Runtime")
	}
	if err := fixture.app.runProviderReconcileOnce(context.Background(), fixture.service, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	after, _ = fixture.app.getWorkspace(stringValue(fixture.workspace["id"]))
	if after["state"] != "data_deleted" {
		t.Fatal("fresh reconcile did not converge after concurrent change")
	}
}

func TestWorkspaceResourceReconcilePreservesUnresolvedFinancialOperation(t *testing.T) {
	fixture, fabric := newWorkspaceResourceReconcileFixture(t)
	fabric.facts[0].Facts.Status = "external_deleted"
	operation, err := newWorkspaceRenewalOperation(fixture.workspace, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	operation.Status, operation.Phase, operation.ChargeAttempted = "manual_review", "charge", true
	before := workspaceRenewalOperationRow(operation)
	mustStore(t, fixture.app.tables.SaveRuntimeOperation(context.Background(), before))
	if err := fixture.app.runProviderReconcileOnce(context.Background(), fixture.service, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	after, _, err := fixture.app.tables.GetRuntimeOperation(context.Background(), operation.ID)
	if err != nil || after["result"] != before["result"] || len(fabric.runtimePowerCalls) != 0 || len(fabric.providerFactInputs) != 0 {
		t.Fatalf("unresolved financial operation was overwritten or bypassed: err=%v", err)
	}
	assertResourceReconcileNoFinancialOrResourceMutation(t, fixture)
}

func TestPostgresWorkspaceResourceReconcilePersistsStateAndAuditAtomically(t *testing.T) {
	fixture, fabric := newWorkspaceResourceReconcileFixture(t)
	store, db := newPostgresWorkspaceRenewalStoreWithDB(t)
	seedTenantMember(t, store, "acct-monthly", "org-monthly", "usr-monthly-owner", "monthly-owner@example.com")
	seedD4RenewalRuntime(t, store, fixture.workspace, fixture.fabric)
	var err error
	fixture.app, err = newControlPlaneAppWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	fabric.facts[0].Facts.Status = "external_deleted"
	if _, err := db.Exec(`CREATE FUNCTION reject_resource_reconcile_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action = 'workspace.resource_reconcile' THEN RAISE EXCEPTION 'forced resource audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_resource_reconcile_audit BEFORE INSERT ON control_plane_admin_audit_events FOR EACH ROW EXECUTE FUNCTION reject_resource_reconcile_audit();`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.app.runProviderReconcileOnce(context.Background(), fixture.service, time.Now().UTC()); err == nil {
		t.Fatal("state write survived a failed audit")
	}
	after, _ := fixture.app.getWorkspace(stringValue(fixture.workspace["id"]))
	if after["state"] != "running" || after["autoRenew"] != true || len(fabric.runtimePowerCalls) != 0 {
		t.Fatal("failed transaction changed business state or Runtime")
	}
	if _, err := db.Exec(`DROP TRIGGER reject_resource_reconcile_audit ON control_plane_admin_audit_events; DROP FUNCTION reject_resource_reconcile_audit();`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.app.runProviderReconcileOnce(context.Background(), fixture.service, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	after, _ = fixture.app.getWorkspace(stringValue(fixture.workspace["id"]))
	audits, err := store.ListAuditEvents(context.Background(), stringValue(fixture.workspace["accountId"]))
	if err != nil || after["state"] != "data_deleted" || after["autoRenew"] != false || len(audits) != 1 || len(fabric.runtimePowerCalls) != 1 {
		t.Fatalf("PostgreSQL did not retain coherent state/evidence: state=%v audits=%d err=%v", after["state"], len(audits), err)
	}
	assertResourceReconcileNoFinancialOrResourceMutation(t, fixture)
}

func TestWorkspaceResourceReconcileRejectsConcurrentRenewalClaim(t *testing.T) {
	fixture, fabric := newWorkspaceResourceReconcileFixture(t)
	fabric.facts[0].Facts.Status = "external_deleted"
	operation, err := newWorkspaceRenewalOperation(fixture.workspace, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	fabric.afterRead = func() {
		mustStore(t, fixture.app.tables.SaveRuntimeOperation(context.Background(), workspaceRenewalOperationRow(operation)))
	}
	if err := fixture.app.runProviderReconcileOnce(context.Background(), fixture.service, time.Now().UTC()); !errors.Is(err, errWorkspaceResourceReconcileConflict) {
		t.Fatalf("concurrent renewal claim did not invalidate absence projection: %v", err)
	}
	after, _ := fixture.app.getWorkspace(stringValue(fixture.workspace["id"]))
	if after["state"] != "running" || len(fabric.runtimePowerCalls) != 0 {
		t.Fatal("reconciliation bypassed in-flight renewal")
	}
	assertResourceReconcileNoFinancialOrResourceMutation(t, fixture)
}

func TestWorkspaceResourceReconcileDoesNotTreatUnfinishedLaunchAsExternalDeletion(t *testing.T) {
	fixture, fabric := newWorkspaceResourceReconcileFixture(t)
	fabric.facts[0].Facts.Status = "external_deleted"
	command := workspaceLaunchUnitCommand()
	command.OperationID, command.AccountID, command.OwnerUserID, command.WorkspaceID = "workspace-launch-monthly", stringValue(fixture.workspace["accountId"]), stringValue(fixture.workspace["ownerUserId"]), stringValue(fixture.workspace["id"])
	operation, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, fixture.app.tables.SaveRuntimeOperation(context.Background(), row))
	if err := fixture.app.runProviderReconcileOnce(context.Background(), fixture.service, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	after, _ := fixture.app.getWorkspace(stringValue(fixture.workspace["id"]))
	if after["state"] != "running" || len(fabric.runtimePowerCalls) != 0 || len(fabric.providerFactInputs) != 0 {
		t.Fatal("incomplete launch was interpreted as provider deletion")
	}
	assertResourceReconcileNoFinancialOrResourceMutation(t, fixture)
}

func TestWorkspaceResourceReconcileRejectsMismatchedRuntimeReadback(t *testing.T) {
	fixture, fabric := newWorkspaceResourceReconcileFixture(t)
	fabric.facts[0].Facts.Status = "external_deleted"
	fabric.invalidRead = true
	if err := fixture.app.runProviderReconcileOnce(context.Background(), fixture.service, time.Now().UTC()); err == nil {
		t.Fatal("mismatched Runtime readback claimed successful suspension")
	}
	after, _ := fixture.app.getWorkspace(stringValue(fixture.workspace["id"]))
	if after["state"] != "data_deleted" || len(fabric.runtimePowerCalls) != 0 {
		t.Fatal("unproven Runtime identity was mutated or known missing disk stayed open")
	}
	assertResourceReconcileNoFinancialOrResourceMutation(t, fixture)
}

func TestWorkspaceResourceReconcileStorageLossRemainsTerminalThroughExpiry(t *testing.T) {
	fixture, fabric := newWorkspaceResourceReconcileFixture(t)
	fabric.facts[0].Facts.Status = "external_deleted"
	if err := fixture.app.runProviderReconcileOnce(context.Background(), fixture.service, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	stale := cloneMap(fixture.workspace)
	stale["state"], stale["status"] = "suspended", "suspended"
	mustStore(t, fixture.app.tables.SaveWorkspace(context.Background(), stale))
	after, _ := fixture.app.getWorkspace(stringValue(fixture.workspace["id"]))
	if after["state"] != "data_deleted" || after["autoRenew"] != false {
		t.Fatal("stale suspended projection downgraded destroyed storage")
	}
	paidThrough, _ := parseTimeString(stringValue(after["paidThrough"]))
	if err := fixture.app.processWorkspaceRenewal(context.Background(), fixture.service, stringValue(after["id"]), paidThrough.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	after, _ = fixture.app.getWorkspace(stringValue(fixture.workspace["id"]))
	if after["state"] != "data_deleted" || after["status"] != "unrecoverable" || after["autoRenew"] != false || len(fixture.sub2API.charges) != 0 || len(fixture.sub2API.refunds) != 0 || len(fixture.fabric.computeRenewKeys) != 0 || len(fixture.fabric.storageRenewKeys) != 0 {
		t.Fatal("expiry reopened destroyed storage or attempted financial recovery")
	}
}
