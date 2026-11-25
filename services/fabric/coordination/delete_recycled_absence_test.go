package coordination_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/fabric/internal/fabric"
)

// This file reproduces the shape of one real prepaid deletion whose original
// CBS terminate outcome was never confirmed.
//
// Fabric persists the dispatch authority before it calls the provider, so the
// journal of such a disk reads dispatch_authorized_uncertain with mutation
// count 0 while the disk itself already sits in the provider's recycle state,
// unbound. The disk is then reclaimed by the provider on its own, without any
// further request from this product. The release must be adopted from that
// authoritative absence, the terminate RPC must never be sent a second time,
// and the same delete operation must move on to the compute handle.
//
// The provider boundary here is the only thing this test drives: the disk is
// present and recycled, then absent. Everything else - the dispatcher, the
// Fabric service, the destroy journal and the one-send fence - is the real
// production code.

type recycledCBSProvider struct {
	fabric.Provider

	absent     bool
	terminates int
	reads      int
}

func (p *recycledCBSProvider) DestroyStorageVolume(_ context.Context, volume fabric.StorageVolume) (fabric.StorageVolume, error) {
	p.terminates++
	// The original terminate RPC left the product and its reply was lost. The
	// provider cannot prove the disk is still there, so the outcome stays
	// unconfirmed and the journal keeps the phase it was given.
	volume.DestroyState = fabric.StorageDestroyStateUnconfirmedSend
	return volume, errors.New("CBS terminate reply was lost")
}

func (p *recycledCBSProvider) ReadStorageVolumeStatus(_ context.Context, volume fabric.StorageVolume) (fabric.StorageVolume, error) {
	p.reads++
	volume.ProviderData["storageVolumeId"] = volume.ProviderResourceID
	volume.ProviderData["describeCbsRequestId"] = "req-describe-cbs-recycled"
	unbound := false
	volume.BindingPresent = &unbound
	if p.absent {
		volume.Status, volume.CBSStatus = "external_deleted", "NOT_FOUND"
		volume.ProviderData["cbsStatus"], volume.ProviderData["status"] = "NOT_FOUND", "external_deleted"
		volume.ProviderData["storageDestroyPhase"], volume.ProviderData["storageDestroyMutationCount"] = "absence_confirmed", "0"
		volume.ProviderData["describeCbsRequestId"] = "req-describe-cbs-absent"
		return volume, nil
	}
	volume.Status, volume.CBSStatus = "pending", "TORECYCLE"
	volume.ProviderData["cbsStatus"], volume.ProviderData["status"] = "TORECYCLE", "pending"
	volume.ProviderData["storageDestroyPhase"], volume.ProviderData["storageDestroyMutationCount"] = "dispatch_authorized_uncertain", "0"
	return volume, nil
}

// recycledCBSVolume is the original prepaid disk identity: a Tencent CBS in one
// region, bound to one workspace through its own PV/PVC names.
func recycledCBSVolume(id, workspace string) fabric.StorageVolume {
	return fabric.StorageVolume{
		ID: id, OperationID: "op:" + id, AccountID: "account", WorkspaceID: workspace,
		Status: "ready", Provider: "tencent-tke", ProviderResourceID: "disk-" + id,
		ProviderRequestID: "req-storage:" + id, SizeGB: 10, StorageClass: "cbs-static", DiskType: "CLOUD_BSSD",
		RenewFlag: "NOTIFY_AND_MANUAL_RENEW", Deadline: "2026-12-01T00:00:00Z", Zone: "ap-guangzhou-3",
		CreatedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		ProviderData: map[string]string{
			"region": "ap-guangzhou", "pvName": "opl-" + id + "-pv", "pvcName": "opl-" + id + "-data",
			"diskType": "CLOUD_BSSD", "zone": "ap-guangzhou-3",
		},
		CostTags: map[string]string{
			"opl_account_id": "account", "opl_workspace_id": workspace, "opl_resource_id": id, "opl_operation_id": "op:" + id,
		},
	}
}

// TestLocalDispatcherAdoptsRecycledDiskAbsenceWithoutResendingTerminate proves
// the release of one prepaid disk whose original terminate outcome was never
// confirmed: while the disk is still recycled the handle stays pending and the
// terminate RPC is not sent again; once the provider reports the disk absent,
// that absence is adopted, the original journal is closed without a second
// mutation, and the compute handle behind it becomes releasable.
func TestLocalDispatcherAdoptsRecycledDiskAbsenceWithoutResendingTerminate(t *testing.T) {
	const workspace, storageID, computeID = "workspace-recycled-cbs", "res_storage_recycled", "res_compute_recycled"
	provider := &recycledCBSProvider{}
	fixture := &deletionProviderFixture{
		storageDestroy: func(volume fabric.StorageVolume) (fabric.StorageVolume, error) {
			return provider.DestroyStorageVolume(context.Background(), volume)
		},
		storageReadback: func(volume fabric.StorageVolume) (fabric.StorageVolume, error) {
			return provider.ReadStorageVolumeStatus(context.Background(), volume)
		},
		computeDestroy: func(allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
			allocation.Status = "external_deleted"
			return allocation, nil
		},
		computeReadback: func(allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
			allocation.Status = "external_deleted"
			allocation.CVMStatus, allocation.TKEStatus = "NOT_FOUND", "NOT_FOUND"
			absent := false
			allocation.MachinePresent = &absent
			return allocation, nil
		},
	}
	system := newDeletionReadbackSystem(t, fixture, deletionComputeAllocation(computeID, workspace), recycledCBSVolume(storageID, workspace))
	intent := deletionIntent(workspace, computeID, storageID)
	intent.AccountID = "account"

	// First pass: the original terminate is dispatched once and its reply is
	// lost. The disk is still recycled, so the handle must stay pending.
	fact, err := system.dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindStorage)
	if err != nil {
		t.Fatalf("first storage deletion: %v", err)
	}
	if fact.State != coordination.DeletionStatePending {
		t.Fatalf("recycled disk state=%q, want %q", fact.State, coordination.DeletionStatePending)
	}
	if provider.terminates != 1 {
		t.Fatalf("terminate sends=%d, want exactly 1", provider.terminates)
	}

	// The journal of that disk now carries the unconfirmed dispatch authority.
	latest, found, err := system.store.LatestResourceOperation(context.Background(), "storage_volume", storageID)
	if err != nil || !found {
		t.Fatalf("latest destroy operation found=%v err=%v", found, err)
	}
	originalOperationID := latest.OperationID
	persisted, ok := latest.RedactedProviderPayload["resource"].(fabric.StorageVolume)
	if !ok {
		t.Fatalf("persisted destroy resource is unreadable: %#v", latest)
	}
	if persisted.ProviderData["storageDestroyPhase"] != "dispatch_authorized_uncertain" || persisted.ProviderData["storageDestroyMutationCount"] != "0" {
		t.Fatalf("persisted destroy phase=%q count=%q, want the unconfirmed dispatch authority",
			persisted.ProviderData["storageDestroyPhase"], persisted.ProviderData["storageDestroyMutationCount"])
	}

	// Second pass while the disk is still recycled: still pending, still no
	// second terminate, and the original operation is not replaced.
	fact, err = system.dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindStorage)
	if err != nil {
		t.Fatalf("second storage deletion: %v", err)
	}
	if fact.State != coordination.DeletionStatePending {
		t.Fatalf("recycled replay state=%q, want %q", fact.State, coordination.DeletionStatePending)
	}
	if provider.terminates != 1 {
		t.Fatalf("recycled replay terminate sends=%d, want exactly 1", provider.terminates)
	}
	latest, found, err = system.store.LatestResourceOperation(context.Background(), "storage_volume", storageID)
	if err != nil || !found || latest.OperationID != originalOperationID {
		t.Fatalf("recycled replay replaced the original destroy: found=%v err=%v latest=%#v", found, err, latest)
	}

	// The provider reclaims the disk on its own. Nothing else is requested.
	provider.absent = true

	// Third pass: the authoritative absence is adopted, still without a
	// terminate RPC, and the original operation is closed.
	fact, err = system.dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindStorage)
	if err != nil {
		t.Fatalf("absent storage deletion: %v", err)
	}
	if fact.State != coordination.DeletionStateAbsent || fact.ProviderStatus != "external_deleted" {
		t.Fatalf("absent disk fact=%+v, want an absent handle confirmed by the owner readback", fact)
	}
	if fact.EvidenceRef == "" || fact.ObservedAt.IsZero() {
		t.Fatalf("absent disk fact lacks owner readback evidence: %+v", fact)
	}
	if provider.terminates != 1 {
		t.Fatalf("absence convergence terminate sends=%d, want exactly 1", provider.terminates)
	}
	latest, found, err = system.store.LatestResourceOperation(context.Background(), "storage_volume", storageID)
	if err != nil || !found || latest.OperationID != originalOperationID {
		t.Fatalf("absence convergence replaced the original destroy: found=%v err=%v latest=%#v", found, err, latest)
	}

	// The compute handle behind that disk is now releasable: the same delete
	// operation moves on instead of waiting for a storage that is already gone.
	fact, err = system.dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindCompute)
	if err != nil {
		t.Fatalf("compute deletion after storage absence: %v", err)
	}
	if fact.State != coordination.DeletionStateAbsent {
		t.Fatalf("compute state=%q, want %q", fact.State, coordination.DeletionStateAbsent)
	}
	// The compute destroy is dispatched by the owning service's own worker, so
	// the mutation is observed rather than assumed.
	waitForDeletionCondition(t, 5*time.Second, "one compute destroy mutation", func() bool {
		_, _, computeDestroy, _ := fixture.calls()
		return computeDestroy == 1
	})
	_, _, computeDestroy, _ := fixture.calls()
	if computeDestroy != 1 {
		t.Fatalf("compute destroy mutations=%d, want exactly 1", computeDestroy)
	}
}

// TestLocalDispatcherRecycledDiskAbsenceSurvivesRestart proves the same
// adoption survives a restart: a rebuilt service replays the original journal,
// reads the authoritative absence and never re-sends the terminate.
func TestLocalDispatcherRecycledDiskAbsenceSurvivesRestart(t *testing.T) {
	const workspace, storageID = "workspace-recycled-restart", "res_storage_recycled_restart"
	provider := &recycledCBSProvider{}
	fixture := &deletionProviderFixture{
		storageDestroy: func(volume fabric.StorageVolume) (fabric.StorageVolume, error) {
			return provider.DestroyStorageVolume(context.Background(), volume)
		},
		storageReadback: func(volume fabric.StorageVolume) (fabric.StorageVolume, error) {
			return provider.ReadStorageVolumeStatus(context.Background(), volume)
		},
	}
	system := newDeletionReadbackSystem(t, fixture, fabric.ComputeAllocation{}, recycledCBSVolume(storageID, workspace))
	intent := deletionIntent(workspace, "", storageID)
	intent.AccountID = "account"

	if _, err := system.dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindStorage); err != nil {
		t.Fatalf("first storage deletion: %v", err)
	}
	if provider.terminates != 1 {
		t.Fatalf("terminate sends=%d, want exactly 1", provider.terminates)
	}
	provider.absent = true

	// A restarted service owns the same journal and the same one-send fence.
	restarted := &deletionReadbackSystem{
		fixture:    fixture,
		store:      system.store,
		dispatcher: coordination.NewLocalDispatcher(fabric.NewServiceWithOperationStore(fixture, system.store), fabric.NewLocalDockerProvider()),
	}
	fact, err := restarted.dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindStorage)
	if err != nil {
		t.Fatalf("storage deletion after restart: %v", err)
	}
	if fact.State != coordination.DeletionStateAbsent {
		t.Fatalf("restart state=%q, want %q", fact.State, coordination.DeletionStateAbsent)
	}
	if provider.terminates != 1 {
		t.Fatalf("restart terminate sends=%d, want exactly 1", provider.terminates)
	}
}
