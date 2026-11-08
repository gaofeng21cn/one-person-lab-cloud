package coordination_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"sync"
	"testing"
	"time"

	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/fabric/internal/fabric"
)

// TestLocalDispatcherDeletionNeverReportsAbsenceWithoutOwnerReadback proves the
// provider adapter's release step: a handle the owning surface cannot identify or
// read back stays an error — never absent — so the coordination layer can only
// record it as unknown or pending. No provider mutation is dispatched for a
// handle the owner never confirmed.
func TestLocalDispatcherDeletionNeverReportsAbsenceWithoutOwnerReadback(t *testing.T) {
	t.Setenv("OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON", `{"schemaVersion":1,"profileId":"profile","capabilityVersion":"v1","region":"local","accountBindings":[{"tenantId":"tenant","accountId":"account"}],"packages":[{"id":"package","name":"Package","available":true,"compute":{"id":"compute-sku","server":"2c4g","cpu":2,"memoryGb":4,"instanceType":"local-2c4g"},"storage":{"id":"storage-sku","sizeGb":10,"quotaPolicy":"linux-project"}}]}`)
	provider := fabric.NewLocalDockerProvider()
	service := fabric.NewService(provider)
	dispatcher := coordination.NewLocalDispatcher(service, provider)
	intent := coordination.ResourceDeletionIntent{TenantID: "tenant", WorkspaceID: "workspace-unreadback", OperationID: "op-unreadback", ResourceSetID: "rset-unreadback", AccountID: "account", ComputeID: "res_missing_compute", StorageID: "res_missing_storage"}
	for _, kind := range []string{coordination.DeletionKindStorage, coordination.DeletionKindCompute} {
		fact, err := dispatcher.DeleteResource(context.Background(), intent, kind)
		if err == nil || fact.State == coordination.DeletionStateAbsent {
			t.Fatalf("kind %s reported %q with err=%v", kind, fact.State, err)
		}
	}
	// The mount binding has no provider-confirmed identity in this intent, so the
	// adapter refuses instead of assuming the binding is gone.
	fact, err := dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindAttachment)
	if err == nil || fact.State == coordination.DeletionStateAbsent {
		t.Fatalf("an unowned attachment reported %q with err=%v", fact.State, err)
	}
}

// managedSecretDispatcher is declared by the managed-secret write suite, which
// entered main after the retained producer's commit. Extending
// ResourceDispatcher with DeleteResource leaves that fixture one method short of
// the interface even though its tests never dispatch a deletion. The adaptation
// belongs to this slice's interface change, so the method lives here, in this
// slice's own test file, rather than editing the managed-secret suite: it
// refuses instead of inventing a deletion fact.
func (d *managedSecretDispatcher) DeleteResource(context.Context, coordination.ResourceDeletionIntent, string) (coordination.ResourceDeletionFact, error) {
	return coordination.ResourceDeletionFact{}, errors.New("fixture_provider_delete_unavailable")
}

// deletionProviderFixture is the typed external provider behind the real Fabric
// service that the deletion readback coverage below drives. It replaces only the
// provider's destroy and readback surfaces, so every fact still flows through
// the real dispatcher and the live Service deletion branch — including the
// StorageDeletionAbsent and ComputeDeletionAbsent owner-readback predicates.
// Unconfigured surfaces fail closed instead of answering, and no hook reaches a
// real provider or Docker daemon.
type deletionProviderFixture struct {
	fabric.Provider

	mu              sync.Mutex
	storageDestroy  func(fabric.StorageVolume) (fabric.StorageVolume, error)
	storageReadback func(fabric.StorageVolume) (fabric.StorageVolume, error)
	computeDestroy  func(fabric.ComputeAllocation) (fabric.ComputeAllocation, error)
	computeReadback func(fabric.ComputeAllocation) (fabric.ComputeAllocation, error)
	computeRelease  func(fabric.ComputeAllocation) (string, error)

	storageDestroyCalls  int
	storageReadbackCalls int
	computeDestroyCalls  int
	computeReadbackCalls int
	computeReleaseCalls  int
}

func (p *deletionProviderFixture) DestroyStorageVolume(_ context.Context, volume fabric.StorageVolume) (fabric.StorageVolume, error) {
	p.mu.Lock()
	p.storageDestroyCalls++
	hook := p.storageDestroy
	p.mu.Unlock()
	if hook == nil {
		return fabric.StorageVolume{}, errors.New("fixture storage destroy is not configured")
	}
	return hook(volume)
}

func (p *deletionProviderFixture) ReadStorageVolumeStatus(_ context.Context, volume fabric.StorageVolume) (fabric.StorageVolume, error) {
	p.mu.Lock()
	p.storageReadbackCalls++
	hook := p.storageReadback
	p.mu.Unlock()
	if hook == nil {
		return fabric.StorageVolume{}, errors.New("fixture storage readback is not configured")
	}
	return hook(volume)
}

func (p *deletionProviderFixture) DestroyComputeAllocation(_ context.Context, allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
	p.mu.Lock()
	p.computeDestroyCalls++
	hook := p.computeDestroy
	p.mu.Unlock()
	if hook == nil {
		return fabric.ComputeAllocation{}, errors.New("fixture compute destroy is not configured")
	}
	return hook(allocation)
}

func (p *deletionProviderFixture) ReadComputeDestroyStatus(_ context.Context, allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
	p.mu.Lock()
	p.computeReadbackCalls++
	hook := p.computeReadback
	p.mu.Unlock()
	if hook == nil {
		return fabric.ComputeAllocation{}, errors.New("fixture compute readback is not configured")
	}
	return hook(allocation)
}

func (p *deletionProviderFixture) ReleaseRecycledComputeAllocation(_ context.Context, allocation fabric.ComputeAllocation) (string, error) {
	p.mu.Lock()
	p.computeReleaseCalls++
	hook := p.computeRelease
	p.mu.Unlock()
	if hook == nil {
		return "", errors.New("fixture compute final release is not configured")
	}
	return hook(allocation)
}

func (p *deletionProviderFixture) releaseCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.computeReleaseCalls
}

func (p *deletionProviderFixture) setStorageReadback(hook func(fabric.StorageVolume) (fabric.StorageVolume, error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.storageReadback = hook
}

func (p *deletionProviderFixture) setComputeReadback(hook func(fabric.ComputeAllocation) (fabric.ComputeAllocation, error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.computeReadback = hook
}

func (p *deletionProviderFixture) calls() (storageDestroy, storageReadback, computeDestroy, computeReadback int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.storageDestroyCalls, p.storageReadbackCalls, p.computeDestroyCalls, p.computeReadbackCalls
}

type workspaceRecycledStorageProvider struct {
	*fabric.LocalDockerProvider
	absent               bool
	releaseResponseLost  bool
	sends                int
	created              fabric.StorageVolume
	destroyCalls         int
	storageReadbackCalls int
}

func (p *workspaceRecycledStorageProvider) CreateStorageVolume(_ context.Context, input fabric.StorageVolumeInput) (fabric.StorageVolume, error) {
	volume := fabric.StorageVolume{
		ID: input.ID, OperationID: input.IdempotencyKey, AccountID: input.AccountID, WorkspaceID: input.WorkspaceID,
		Status: "ready", Provider: "tencent-tke", ProviderResourceID: "disk-" + input.ID, ProviderRequestID: "create-cbs-request",
		SizeGB: input.SizeGB, StorageClass: "cbs-static", DiskType: "CLOUD_BSSD", RenewFlag: "NOTIFY_AND_MANUAL_RENEW",
		Deadline: "2026-12-01T00:00:00Z", Zone: input.Zone, CreatedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		ProviderData: map[string]string{"region": "ap-guangzhou", "pvName": "opl-" + input.ID + "-pv", "pvcName": "opl-" + input.ID + "-data", "diskType": "CLOUD_BSSD", "zone": input.Zone},
		CostTags:     map[string]string{"opl_account_id": input.AccountID, "opl_workspace_id": input.WorkspaceID, "opl_resource_id": input.ID, "opl_operation_id": input.OperationID},
	}
	volume.CostTags["opl_operation_id"] = input.OperationID
	p.created = cloneStorageVolumeForTest(volume)
	return volume, nil
}

func (p *workspaceRecycledStorageProvider) DestroyStorageVolume(_ context.Context, volume fabric.StorageVolume) (fabric.StorageVolume, error) {
	p.destroyCalls++
	volume = cloneStorageVolumeForTest(volume)
	if p.absent {
		volume.Status, volume.CBSStatus, volume.DestroyState = "external_deleted", "NOT_FOUND", ""
		volume.ProviderData["storageDestroyPhase"] = "absence_confirmed"
		volume.ProviderData["storageDestroyMutationCount"] = "0"
		volume.ProviderData["status"], volume.ProviderData["cbsStatus"] = "external_deleted", "NOT_FOUND"
		return volume, nil
	}
	volume.Status, volume.CBSStatus, volume.DestroyState = "destroying", "TORECYCLE", "unconfirmed_send"
	volume.ProviderData["storageDestroyPhase"] = "terminate_attempted"
	volume.ProviderData["storageDestroyMutationCount"] = "1"
	return volume, errors.New("CBS remains in recycle state")
}

func (p *workspaceRecycledStorageProvider) ReadStorageVolumeStatus(_ context.Context, volume fabric.StorageVolume) (fabric.StorageVolume, error) {
	p.storageReadbackCalls++
	volume = cloneStorageVolumeForTest(volume)
	if volume.ProviderData == nil {
		volume.ProviderData = map[string]string{}
	}
	present := false
	volume.BindingPresent = &present
	volume.ProviderData["storageVolumeId"] = volume.ProviderResourceID
	volume.ProviderData["describeCbsRequestId"] = "fresh-cbs-readback"
	volume.ProviderData["storageDestroyMutationCount"] = "1"
	if p.absent {
		volume.Status, volume.CBSStatus = "external_deleted", "NOT_FOUND"
		volume.ProviderData["status"], volume.ProviderData["cbsStatus"] = "external_deleted", "NOT_FOUND"
		volume.ProviderData["storageDestroyPhase"] = "absence_confirmed"
	} else {
		volume.Status, volume.CBSStatus = "pending", "TORECYCLE"
		volume.ProviderData["status"] = "pending"
		volume.ProviderData["cbsStatus"] = "TORECYCLE"
		volume.ProviderData["storageDestroyPhase"] = "terminate_attempted"
	}
	return volume, nil
}

func (p *workspaceRecycledStorageProvider) ReleaseRecycledStorageVolume(_ context.Context, _ fabric.StorageVolume) (string, error) {
	p.sends++
	if p.releaseResponseLost {
		return "workspace-cbs-final-release-lost", errors.New("final release response lost")
	}
	p.absent = true
	return "workspace-cbs-final-release", nil
}

func cloneStorageVolumeForTest(volume fabric.StorageVolume) fabric.StorageVolume {
	volume.ProviderData = maps.Clone(volume.ProviderData)
	volume.CostTags = maps.Clone(volume.CostTags)
	return volume
}

func TestWorkspaceDeletionOwnsStorageFinalReleaseFence(t *testing.T) {
	const workspaceID, computeID, storageID = "workspace-cbs-final-release", "compute-cbs-final-release", "storage-cbs-final-release"
	provider := &workspaceRecycledStorageProvider{LocalDockerProvider: fabric.NewLocalDockerProvider()}
	store := fabric.NewMemoryOperationStore()
	compute := deletionComputeAllocation(computeID, workspaceID)
	compute.AccountID, compute.Provider, compute.Zone = "account", "tencent-tke", "ap-guangzhou-3"
	compute.CostTags = map[string]string{"opl_account_id": "account", "opl_workspace_id": workspaceID, "opl_resource_id": computeID, "opl_operation_id": "create-compute"}
	appendSeedOperation(t, store, "create_compute_allocation", "compute_allocation", compute)
	service := fabric.NewServiceWithOperationStore(provider, store)
	volume, err := service.CreateStorageVolume(context.Background(), fabric.StorageVolumeInput{
		ID: storageID, AccountID: "account", WorkspaceID: workspaceID, ComputeID: computeID, Zone: compute.Zone,
		SizeGB: 10, IdempotencyKey: "create-storage:" + storageID,
	})
	if err != nil {
		t.Fatalf("create storage through Fabric service: %v", err)
	}
	provider.created = volume
	dispatcher := coordination.NewLocalDispatcher(service, fabric.NewLocalDockerProvider())
	intent := deletionIntent(workspaceID, computeID, storageID)
	intent.AccountID = "account"
	fact, err := dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindStorage)
	if err != nil || fact.State != coordination.DeletionStateAbsent || fact.ProviderStatus != "external_deleted" {
		t.Fatalf("first Workspace deletion fact=%+v err=%v created=%+v", fact, err, provider.created)
	}
	if provider.sends != 1 || provider.destroyCalls != 1 {
		t.Fatalf("first pass final releases=%d provider destroy sends=%d, want exactly 1 each", provider.sends, provider.destroyCalls)
	}

	operations, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var originalDestroy, finalRelease fabric.FabricOperation
	for _, operation := range operations {
		switch operation.Action {
		case "destroy_storage_volume":
			originalDestroy = operation
		case "release_recycled_storage_volume":
			finalRelease = operation
		}
	}
	if originalDestroy.OperationID == "" || originalDestroy.Status != "succeeded" {
		t.Fatalf("original destroy journal=%+v, want the original Workspace destroy to finalize after absence readback", originalDestroy)
	}
	if finalRelease.Status != "succeeded" || finalRelease.CallerService != "workspace" ||
		finalRelease.RedactedProviderPayload["originalDestroyOperationId"] != originalDestroy.OperationID {
		t.Fatalf("final-release journal=%+v, want successful Workspace fence bound to original destroy", finalRelease)
	}

	final, err := dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindStorage)
	if err != nil || final.State != coordination.DeletionStateAbsent || final.ProviderStatus != "external_deleted" {
		t.Fatalf("finalized Workspace deletion fact=%+v err=%v", final, err)
	}
	if provider.sends != 1 {
		t.Fatalf("replayed Workspace deletion sent final release %d times", provider.sends)
	}
}

func TestWorkspaceDeletionRebuildReconcilesLostFinalReleaseWithoutResend(t *testing.T) {
	const workspaceID, computeID, storageID = "workspace-cbs-final-release-rebuild", "compute-cbs-final-release-rebuild", "storage-cbs-final-release-rebuild"
	provider := &workspaceRecycledStorageProvider{LocalDockerProvider: fabric.NewLocalDockerProvider(), releaseResponseLost: true}
	store := fabric.NewMemoryOperationStore()
	compute := deletionComputeAllocation(computeID, workspaceID)
	compute.AccountID, compute.Provider, compute.Zone = "account", "tencent-tke", "ap-guangzhou-3"
	compute.CostTags = map[string]string{"opl_account_id": "account", "opl_workspace_id": workspaceID, "opl_resource_id": computeID, "opl_operation_id": "create-compute"}
	appendSeedOperation(t, store, "create_compute_allocation", "compute_allocation", compute)
	service := fabric.NewServiceWithOperationStore(provider, store)
	volume, err := service.CreateStorageVolume(context.Background(), fabric.StorageVolumeInput{
		ID: storageID, AccountID: "account", WorkspaceID: workspaceID, ComputeID: computeID, Zone: compute.Zone,
		SizeGB: 10, IdempotencyKey: "create-storage:" + storageID,
	})
	if err != nil {
		t.Fatalf("create storage through Fabric service: %v", err)
	}
	provider.created = volume
	intent := deletionIntent(workspaceID, computeID, storageID)
	intent.AccountID = "account"
	dispatcher := coordination.NewLocalDispatcher(service, fabric.NewLocalDockerProvider())
	if _, err := dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindStorage); err == nil {
		t.Fatal("lost final-release response was reported as a completed deletion")
	}
	if provider.sends != 1 || provider.absent {
		t.Fatalf("first pass sends=%d absent=%v, want one uncertain send with CBS still recycled", provider.sends, provider.absent)
	}
	originalDestroy, finalRelease := workspaceStorageOperations(t, store, storageID)
	if originalDestroy.OperationID == "" || finalRelease.Status != "failed" || finalRelease.CallerService != "workspace" {
		t.Fatalf("first pass journals destroy=%+v finalRelease=%+v", originalDestroy, finalRelease)
	}

	// Rebuild the owner service from the same durable journal. The retry must use
	// the original destroy operation and the failed final-release fence, so it
	// may only read the provider and never send a second release.
	rebuilt := fabric.NewServiceWithOperationStore(provider, store)
	rebuiltDispatcher := coordination.NewLocalDispatcher(rebuilt, fabric.NewLocalDockerProvider())
	pending, err := rebuiltDispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindStorage)
	if err != nil || pending.State != coordination.DeletionStatePending {
		t.Fatalf("rebuilt pending fact=%+v err=%v", pending, err)
	}
	if provider.sends != 1 {
		t.Fatalf("replayed Workspace deletion sent final release %d times", provider.sends)
	}
	gotDestroy, gotFinalRelease := workspaceStorageOperations(t, store, storageID)
	if gotDestroy.OperationID != originalDestroy.OperationID || gotFinalRelease.OperationID != finalRelease.OperationID {
		t.Fatalf("replay changed operation identity: destroy %q->%q final release %q->%q", originalDestroy.OperationID, gotDestroy.OperationID, finalRelease.OperationID, gotFinalRelease.OperationID)
	}

	provider.absent = true
	completed, err := rebuiltDispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindStorage)
	if err != nil || completed.State != coordination.DeletionStateAbsent || completed.ProviderStatus != "external_deleted" {
		t.Fatalf("absence readback did not complete deletion: fact=%+v err=%v", completed, err)
	}
	if provider.sends != 1 {
		t.Fatalf("completion replay sent final release %d times", provider.sends)
	}
	gotDestroy, _ = workspaceStorageOperations(t, store, storageID)
	if gotDestroy.OperationID != originalDestroy.OperationID || gotDestroy.Status != "succeeded" {
		t.Fatalf("original destroy did not converge in place: %+v", gotDestroy)
	}
}

func workspaceStorageOperations(t *testing.T, store *fabric.MemoryOperationStore, storageID string) (fabric.FabricOperation, fabric.FabricOperation) {
	t.Helper()
	operations, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var destroy, finalRelease fabric.FabricOperation
	for _, operation := range operations {
		if operation.ResourceID != storageID {
			continue
		}
		switch operation.Action {
		case "destroy_storage_volume":
			destroy = operation
		case "release_recycled_storage_volume":
			finalRelease = operation
		}
	}
	return destroy, finalRelease
}

// deletionReadbackSystem is one real dispatcher over one real Fabric service
// whose only replaced boundary is the typed provider fixture. The concrete
// provider argument only satisfies the dispatcher constructor; DeleteResource
// reaches the provider exclusively through the service.
type deletionReadbackSystem struct {
	fixture    *deletionProviderFixture
	store      *fabric.MemoryOperationStore
	dispatcher coordination.ResourceDispatcher
}

func newDeletionReadbackSystem(t *testing.T, fixture *deletionProviderFixture, seedCompute fabric.ComputeAllocation, seedVolume fabric.StorageVolume) *deletionReadbackSystem {
	t.Helper()
	store := fabric.NewMemoryOperationStore()
	if seedVolume.ID != "" {
		appendSeedOperation(t, store, "create_storage_volume", "storage_volume", seedVolume)
	}
	if seedCompute.ID != "" {
		appendSeedOperation(t, store, "create_compute_allocation", "compute_allocation", seedCompute)
	}
	service := fabric.NewServiceWithOperationStore(fixture, store)
	dispatcher := coordination.NewLocalDispatcher(service, fabric.NewLocalDockerProvider())
	if dispatcher == nil {
		t.Fatal("local dispatcher is unavailable")
	}
	return &deletionReadbackSystem{fixture: fixture, store: store, dispatcher: dispatcher}
}

// appendSeedOperation records one successful owner resource operation, which is
// exactly how the live service replays its resource maps at construction.
func appendSeedOperation(t *testing.T, store *fabric.MemoryOperationStore, action, resourceKind string, resource any) {
	t.Helper()
	resourceID := ""
	switch value := resource.(type) {
	case fabric.StorageVolume:
		resourceID = value.ID
	case fabric.ComputeAllocation:
		resourceID = value.ID
	default:
		t.Fatalf("unsupported seed resource %T", resource)
	}
	// The seed predates any live dispatch: the destroy claim the retry must
	// replay is the latest resource operation at that point.
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	operation := fabric.FabricOperation{
		ID: "fop-seed-" + resourceKind + "-" + resourceID, OperationID: "seed:" + resourceKind + ":" + resourceID,
		CallerService: "control-plane", Action: action, ResourceKind: resourceKind, ResourceID: resourceID,
		Status: "succeeded", StartedAt: now, CreatedAt: now,
		RedactedProviderPayload: map[string]any{"resource": resource},
	}
	if err := store.Append(context.Background(), operation); err != nil {
		t.Fatalf("seed %s operation: %v", resourceKind, err)
	}
}

func deletionStorageVolume(id, workspace string) fabric.StorageVolume {
	return fabric.StorageVolume{
		ID: id, OperationID: "op:" + id, AccountID: "account", WorkspaceID: workspace,
		Status: "ready", Provider: "local-docker", ProviderResourceID: "directory/" + workspace,
		ProviderRequestID: "req-storage:" + id, SizeGB: 10, StorageClass: "host-directory", DiskType: "local-directory",
		Zone: "local", CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}
}

func deletionComputeAllocation(id, workspace string) fabric.ComputeAllocation {
	return fabric.ComputeAllocation{
		ID: id, OperationID: "op:" + id, AccountID: "account", WorkspaceID: workspace,
		PackageID: "package", Status: "running", Provider: "local-docker", ProviderResourceID: "network/" + workspace,
		ProviderRequestID: "req-compute:" + id, PoolID: "local-docker", NodePoolID: "local-docker",
		InstanceType: "local-2c4g", Zone: "local", CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}
}

func deletionIntent(workspace, computeID, storageID string) coordination.ResourceDeletionIntent {
	return coordination.ResourceDeletionIntent{
		TenantID: "tenant", WorkspaceID: workspace, OperationID: "op:delete:" + workspace,
		ResourceSetID: "rset:" + workspace, AccountID: "account", ComputeID: computeID, StorageID: storageID,
	}
}

func waitForDeletionCondition(t *testing.T, timeout time.Duration, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

func destroyOperationRecorded(t *testing.T, store *fabric.MemoryOperationStore, action, resourceID string) bool {
	t.Helper()
	operations, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list operations: %v", err)
	}
	for _, operation := range operations {
		if operation.Action == action && operation.ResourceID == resourceID && operation.Status == "succeeded" {
			return true
		}
	}
	return false
}

// TestLocalDispatcherStorageDeletionConfirmsAbsenceFromOwnerReadback proves the
// accepted storage handle is released through the real dispatcher and confirmed
// absent only by the provider's own post-destroy readback: exactly one destroy
// mutation is dispatched, and the fact carries the readback's evidence.
func TestLocalDispatcherStorageDeletionConfirmsAbsenceFromOwnerReadback(t *testing.T) {
	const workspace, storageID = "workspace-storage-absent", "res_storage_absent"
	fixture := &deletionProviderFixture{
		storageDestroy: func(volume fabric.StorageVolume) (fabric.StorageVolume, error) {
			volume.Status = "destroyed"
			return volume, nil
		},
		storageReadback: func(volume fabric.StorageVolume) (fabric.StorageVolume, error) {
			volume.Status = "external_deleted"
			return volume, nil
		},
	}
	system := newDeletionReadbackSystem(t, fixture, fabric.ComputeAllocation{}, deletionStorageVolume(storageID, workspace))
	fact, err := system.dispatcher.DeleteResource(context.Background(), deletionIntent(workspace, "", storageID), coordination.DeletionKindStorage)
	if err != nil {
		t.Fatalf("storage deletion: %v", err)
	}
	if fact.State != coordination.DeletionStateAbsent {
		t.Fatalf("storage state=%q, want %q", fact.State, coordination.DeletionStateAbsent)
	}
	if fact.ProviderStatus != "external_deleted" || fact.EvidenceRef == "" || fact.ObservedAt.IsZero() {
		t.Fatalf("storage absence fact lacks owner readback evidence: %+v", fact)
	}
	storageDestroy, storageReadback, _, _ := fixture.calls()
	if storageDestroy != 1 || storageReadback != 1 {
		t.Fatalf("storage destroy=%d readback=%d, want 1 and 1", storageDestroy, storageReadback)
	}
}

// TestLocalDispatcherStorageDeletionKeepsPendingWhileTheOwnerStillReportsTheVolume
// keeps a dispatched-but-unfinished release pending: the provider classifies the
// destroy as retryable, the readback still reports the volume, and the fact is
// never promoted to absent just because a destroy call returned.
func TestLocalDispatcherStorageDeletionKeepsPendingWhileTheOwnerStillReportsTheVolume(t *testing.T) {
	const workspace, storageID = "workspace-storage-present", "res_storage_present"
	fixture := &deletionProviderFixture{
		storageDestroy: func(volume fabric.StorageVolume) (fabric.StorageVolume, error) {
			volume.DestroyState = "pending_retry"
			return volume, errors.Join(fabric.ErrWorkspaceLaunchPending, errors.New("provider destroy is still converging"))
		},
		storageReadback: func(volume fabric.StorageVolume) (fabric.StorageVolume, error) {
			volume.Status = "ready"
			volume.DestroyState = "pending_retry"
			return volume, nil
		},
	}
	system := newDeletionReadbackSystem(t, fixture, fabric.ComputeAllocation{}, deletionStorageVolume(storageID, workspace))
	fact, err := system.dispatcher.DeleteResource(context.Background(), deletionIntent(workspace, "", storageID), coordination.DeletionKindStorage)
	if err != nil {
		t.Fatalf("storage deletion: %v", err)
	}
	if fact.State != coordination.DeletionStatePending {
		t.Fatalf("storage state=%q, want %q", fact.State, coordination.DeletionStatePending)
	}
	if fact.ProviderStatus != "ready" || fact.DestroyState != "pending_retry" {
		t.Fatalf("storage pending fact lost the owner classification: %+v", fact)
	}
}

// TestLocalDispatcherStorageDeletionKeepsPendingOnUnconfirmableOwnerEvidence
// proves a readback that claims deletion yet does not carry the exact accepted
// handle identity stays pending: absence is never inferred from a status alone.
func TestLocalDispatcherStorageDeletionKeepsPendingOnUnconfirmableOwnerEvidence(t *testing.T) {
	const workspace, storageID = "workspace-storage-mismatch", "res_storage_mismatch"
	fixture := &deletionProviderFixture{
		storageDestroy: func(volume fabric.StorageVolume) (fabric.StorageVolume, error) {
			volume.Status = "destroyed"
			return volume, nil
		},
		storageReadback: func(volume fabric.StorageVolume) (fabric.StorageVolume, error) {
			volume.Status = "external_deleted"
			volume.ProviderResourceID = "volume-elsewhere"
			return volume, nil
		},
	}
	system := newDeletionReadbackSystem(t, fixture, fabric.ComputeAllocation{}, deletionStorageVolume(storageID, workspace))
	fact, err := system.dispatcher.DeleteResource(context.Background(), deletionIntent(workspace, "", storageID), coordination.DeletionKindStorage)
	if err != nil {
		t.Fatalf("storage deletion: %v", err)
	}
	if fact.State != coordination.DeletionStatePending {
		t.Fatalf("storage state=%q, want %q for unconfirmable evidence", fact.State, coordination.DeletionStatePending)
	}
}

// TestLocalDispatcherStorageDeletionReportsFailureWhenOwnerReadbackFails proves
// an unreadable owner surface surfaces as an error and never as absence.
func TestLocalDispatcherStorageDeletionReportsFailureWhenOwnerReadbackFails(t *testing.T) {
	const workspace, storageID = "workspace-storage-readback-fail", "res_storage_readback_fail"
	fixture := &deletionProviderFixture{
		storageDestroy: func(volume fabric.StorageVolume) (fabric.StorageVolume, error) {
			volume.Status = "destroyed"
			return volume, nil
		},
		storageReadback: func(fabric.StorageVolume) (fabric.StorageVolume, error) {
			return fabric.StorageVolume{}, errors.New("provider storage readback unavailable")
		},
	}
	system := newDeletionReadbackSystem(t, fixture, fabric.ComputeAllocation{}, deletionStorageVolume(storageID, workspace))
	fact, err := system.dispatcher.DeleteResource(context.Background(), deletionIntent(workspace, "", storageID), coordination.DeletionKindStorage)
	if err == nil {
		t.Fatal("failed storage readback was not surfaced")
	}
	if fact.State == coordination.DeletionStateAbsent {
		t.Fatalf("failed storage readback reported %q", fact.State)
	}
}

// TestLocalDispatcherStorageDeletionRetryReusesTheDispatchClaimWithoutSecondMutation
// proves a retried release replays the retained dispatch claim and reconciles by
// readback only: the provider never receives a second destroy mutation.
func TestLocalDispatcherStorageDeletionRetryReusesTheDispatchClaimWithoutSecondMutation(t *testing.T) {
	const workspace, storageID = "workspace-storage-retry", "res_storage_retry"
	fixture := &deletionProviderFixture{
		storageDestroy: func(volume fabric.StorageVolume) (fabric.StorageVolume, error) {
			volume.Status = "destroyed"
			return volume, nil
		},
		storageReadback: func(volume fabric.StorageVolume) (fabric.StorageVolume, error) {
			volume.Status = "ready"
			return volume, nil
		},
	}
	system := newDeletionReadbackSystem(t, fixture, fabric.ComputeAllocation{}, deletionStorageVolume(storageID, workspace))
	intent := deletionIntent(workspace, "", storageID)
	first, err := system.dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindStorage)
	if err != nil {
		t.Fatalf("first storage deletion: %v", err)
	}
	if first.State != coordination.DeletionStatePending {
		t.Fatalf("first storage state=%q, want %q", first.State, coordination.DeletionStatePending)
	}
	fixture.setStorageReadback(func(volume fabric.StorageVolume) (fabric.StorageVolume, error) {
		volume.Status = "external_deleted"
		return volume, nil
	})
	second, err := system.dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindStorage)
	if err != nil {
		t.Fatalf("retried storage deletion: %v", err)
	}
	if second.State != coordination.DeletionStateAbsent {
		t.Fatalf("retried storage state=%q, want %q", second.State, coordination.DeletionStateAbsent)
	}
	storageDestroy, storageReadback, _, _ := fixture.calls()
	if storageDestroy != 1 {
		t.Fatalf("storage destroy calls=%d, want exactly 1", storageDestroy)
	}
	if storageReadback != 2 {
		t.Fatalf("storage readback calls=%d, want 2", storageReadback)
	}
}

// TestLocalDispatcherComputeDeletionConfirmsAbsenceFromOwnerReadback proves the
// compute handle is released through the real dispatcher and confirmed absent by
// the provider's own destroy-status readback, after exactly one destroy mutation.
func TestLocalDispatcherComputeDeletionConfirmsAbsenceFromOwnerReadback(t *testing.T) {
	const workspace, computeID = "workspace-compute-absent", "res_compute_absent"
	fixture := &deletionProviderFixture{
		computeDestroy: func(allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
			allocation.Status = "external_deleted"
			return allocation, nil
		},
		computeReadback: func(allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
			allocation.Status = "external_deleted"
			allocation.CVMStatus = "NOT_FOUND"
			allocation.TKEStatus = "NOT_FOUND"
			absent := false
			allocation.MachinePresent = &absent
			return allocation, nil
		},
	}
	system := newDeletionReadbackSystem(t, fixture, deletionComputeAllocation(computeID, workspace), fabric.StorageVolume{})
	fact, err := system.dispatcher.DeleteResource(context.Background(), deletionIntent(workspace, computeID, ""), coordination.DeletionKindCompute)
	if err != nil {
		t.Fatalf("compute deletion: %v", err)
	}
	if fact.State != coordination.DeletionStateAbsent {
		t.Fatalf("compute state=%q, want %q", fact.State, coordination.DeletionStateAbsent)
	}
	if fact.ProviderStatus != "external_deleted" || fact.EvidenceRef == "" || fact.ObservedAt.IsZero() || fact.ProviderReference == "" {
		t.Fatalf("compute absence fact lacks owner readback evidence: %+v", fact)
	}
	waitForDeletionCondition(t, 5*time.Second, "one compute destroy mutation", func() bool {
		_, _, computeDestroy, _ := fixture.calls()
		return computeDestroy == 1
	})
	waitForDeletionCondition(t, 5*time.Second, "the recorded compute destroy outcome", func() bool {
		return destroyOperationRecorded(t, system.store, "destroy_compute_allocation", computeID)
	})
	_, _, computeDestroy, computeReadback := fixture.calls()
	if computeDestroy != 1 || computeReadback != 1 {
		t.Fatalf("compute destroy=%d readback=%d, want 1 and 1", computeDestroy, computeReadback)
	}
}

// TestLocalDispatcherComputeDeletionKeepsPendingWithoutMachinePresenceEvidence
// proves a readback that cannot report the authoritative machine presence stays
// pending: an unreported presence fact is not treated as absence.
func TestLocalDispatcherComputeDeletionKeepsPendingWithoutMachinePresenceEvidence(t *testing.T) {
	const workspace, computeID = "workspace-compute-present", "res_compute_present"
	fixture := &deletionProviderFixture{
		computeDestroy: func(allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
			allocation.Status = "external_deleted"
			return allocation, nil
		},
		computeReadback: func(allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
			allocation.Status = "external_deleted"
			allocation.CVMStatus = "NOT_FOUND"
			allocation.TKEStatus = "NOT_FOUND"
			allocation.MachinePresent = nil
			allocation.DestroyState = "pending_retry"
			return allocation, nil
		},
	}
	system := newDeletionReadbackSystem(t, fixture, deletionComputeAllocation(computeID, workspace), fabric.StorageVolume{})
	fact, err := system.dispatcher.DeleteResource(context.Background(), deletionIntent(workspace, computeID, ""), coordination.DeletionKindCompute)
	if err != nil {
		t.Fatalf("compute deletion: %v", err)
	}
	if fact.State != coordination.DeletionStatePending {
		t.Fatalf("compute state=%q, want %q without presence evidence", fact.State, coordination.DeletionStatePending)
	}
	if fact.DestroyState != "pending_retry" {
		t.Fatalf("compute pending fact lost the owner classification: %+v", fact)
	}
}

// TestLocalDispatcherComputeDeletionRejectsMismatchedOwnerReadbackIdentity proves
// a readback for a different machine is refused instead of being classified:
// the owning surface's identity check is enforced inside the service path.
func TestLocalDispatcherComputeDeletionRejectsMismatchedOwnerReadbackIdentity(t *testing.T) {
	const workspace, computeID = "workspace-compute-mismatch", "res_compute_mismatch"
	fixture := &deletionProviderFixture{
		computeDestroy: func(allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
			allocation.Status = "external_deleted"
			return allocation, nil
		},
		computeReadback: func(allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
			allocation.MachineName = "machine-somewhere-else"
			return allocation, nil
		},
	}
	system := newDeletionReadbackSystem(t, fixture, deletionComputeAllocation(computeID, workspace), fabric.StorageVolume{})
	fact, err := system.dispatcher.DeleteResource(context.Background(), deletionIntent(workspace, computeID, ""), coordination.DeletionKindCompute)
	if err == nil {
		t.Fatal("mismatched compute readback identity was accepted")
	}
	if fact.State == coordination.DeletionStateAbsent {
		t.Fatalf("mismatched compute readback reported %q", fact.State)
	}
}

// TestLocalDispatcherComputeDeletionReportsFailureWhenOwnerReadbackFails proves
// an unreadable compute destroy status surfaces as an error and never as absence.
func TestLocalDispatcherComputeDeletionReportsFailureWhenOwnerReadbackFails(t *testing.T) {
	const workspace, computeID = "workspace-compute-readback-fail", "res_compute_readback_fail"
	fixture := &deletionProviderFixture{
		computeDestroy: func(allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
			allocation.Status = "external_deleted"
			return allocation, nil
		},
		computeReadback: func(fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
			return fabric.ComputeAllocation{}, errors.New("provider compute readback unavailable")
		},
	}
	system := newDeletionReadbackSystem(t, fixture, deletionComputeAllocation(computeID, workspace), fabric.StorageVolume{})
	fact, err := system.dispatcher.DeleteResource(context.Background(), deletionIntent(workspace, computeID, ""), coordination.DeletionKindCompute)
	if err == nil {
		t.Fatal("failed compute readback was not surfaced")
	}
	if fact.State == coordination.DeletionStateAbsent {
		t.Fatalf("failed compute readback reported %q", fact.State)
	}
}

// TestLocalDispatcherComputeDeletionRetryReusesTheDispatchClaimWithoutSecondMutation
// proves a retried compute release replays the retained dispatch claim: the
// provider receives the destroy mutation exactly once, and the retry converges
// by reading the owner surface again.
func TestLocalDispatcherComputeDeletionRetryReusesTheDispatchClaimWithoutSecondMutation(t *testing.T) {
	const workspace, computeID = "workspace-compute-retry", "res_compute_retry"
	fixture := &deletionProviderFixture{
		computeDestroy: func(allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
			allocation.Status = "external_deleted"
			return allocation, nil
		},
		computeReadback: func(allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
			allocation.Status = "running"
			return allocation, nil
		},
	}
	system := newDeletionReadbackSystem(t, fixture, deletionComputeAllocation(computeID, workspace), fabric.StorageVolume{})
	intent := deletionIntent(workspace, computeID, "")
	first, err := system.dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindCompute)
	if err != nil {
		t.Fatalf("first compute deletion: %v", err)
	}
	if first.State != coordination.DeletionStatePending {
		t.Fatalf("first compute state=%q, want %q", first.State, coordination.DeletionStatePending)
	}
	waitForDeletionCondition(t, 5*time.Second, "the recorded compute destroy outcome", func() bool {
		return destroyOperationRecorded(t, system.store, "destroy_compute_allocation", computeID)
	})
	fixture.setComputeReadback(func(allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
		allocation.Status = "external_deleted"
		allocation.CVMStatus = "NOT_FOUND"
		allocation.TKEStatus = "NOT_FOUND"
		absent := false
		allocation.MachinePresent = &absent
		return allocation, nil
	})
	second, err := system.dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindCompute)
	if err != nil {
		t.Fatalf("retried compute deletion: %v", err)
	}
	if second.State != coordination.DeletionStateAbsent {
		t.Fatalf("retried compute state=%q, want %q", second.State, coordination.DeletionStateAbsent)
	}
	_, _, computeDestroy, _ := fixture.calls()
	if computeDestroy != 1 {
		t.Fatalf("compute destroy calls=%d, want exactly 1", computeDestroy)
	}
}

// The persisted identity matches computeDestroyPhaseResource in Fabric's owner
// suite. A started original destroy must recover by readback, never by sending
// another DestroyComputeAllocation before the separate final-release action.
func newComputeFinalReleaseDeletionSystem(t *testing.T, fixture *deletionProviderFixture) (*deletionReadbackSystem, fabric.ComputeAllocation, fabric.FabricOperation) {
	t.Helper()
	allocation := fabric.ComputeAllocation{
		ID: "compute-durable-delete", OperationID: "op-create-durable-delete", AccountID: "acct-durable-delete", WorkspaceID: "ws-durable-delete", PackageID: "basic",
		Status: "running", Provider: "tencent-tke", ProviderResourceID: "ins-durable-delete", ProviderRequestID: "req-create-durable-delete",
		PoolID: "pool-basic-2c4g", NodePoolID: "np-basic", InstanceID: "ins-durable-delete", CVMInstanceID: "ins-durable-delete",
		MachineName: "machine-durable-delete", NodeName: "node-durable-delete", PrivateIP: "10.0.0.18", InstanceType: "SA5.MEDIUM4", Zone: "ap-guangzhou-3",
		CVMStatus: "RUNNING", ChargeType: "PREPAID", RenewFlag: "NOTIFY_AND_MANUAL_RENEW", Deadline: "2026-09-20T00:00:00Z", ServiceName: "opl-compute-durable-delete",
		NodeSelector: map[string]any{"kubernetes.io/hostname": "node-durable-delete"},
		ProviderData: map[string]string{
			"clusterId": "cls-alpha", "region": "ap-guangzhou", "machineName": "machine-durable-delete", "instanceType": "SA5.MEDIUM4", "cpu": "2", "memoryGb": "4", "zone": "ap-guangzhou-3",
			"machineType": "NativeCVM", "cvmApplicable": "true",
		},
		CostTags: map[string]string{
			"opl_account_id": "acct-durable-delete", "opl_workspace_id": "ws-durable-delete", "opl_resource_id": "compute-durable-delete", "opl_operation_id": "op-create-durable-delete",
		},
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	store := fabric.NewMemoryOperationStore()
	appendSeedOperation(t, store, "create_compute_allocation", "compute_allocation", allocation)
	allocation.Status = "destroying"
	allocation.ProviderData = maps.Clone(allocation.ProviderData)
	allocation.ProviderData["computeDestroyPhase"] = "dispatch_authorized_uncertain"
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	// Use the owner's canonical operation identity so later Destroy retries
	// remain the same original operation across service reconstruction.
	destroyID := sha256.Sum256([]byte(allocation.ID + ":compute_allocation:destroy_compute_allocation"))
	original := fabric.FabricOperation{
		ID: "fop-original-compute-destroy", OperationID: fmt.Sprintf("op_destroy_compute_allocation_%x", destroyID[:6]), CallerService: "control-plane",
		Action: "destroy_compute_allocation", ResourceKind: "compute_allocation", ResourceID: allocation.ID,
		AccountID: allocation.AccountID, WorkspaceID: allocation.WorkspaceID, Provider: allocation.Provider, ProviderRequestID: allocation.ProviderRequestID,
		Status: "started", StartedAt: now, CreatedAt: now, RedactedProviderPayload: map[string]any{"resource": allocation},
	}
	if err := store.Append(context.Background(), original); err != nil {
		t.Fatalf("seed original compute destroy: %v", err)
	}
	service := fabric.NewServiceWithOperationStore(fixture, store)
	return &deletionReadbackSystem{fixture: fixture, store: store, dispatcher: coordination.NewLocalDispatcher(service, fabric.NewLocalDockerProvider())}, allocation, original
}

func computeFinalReleaseDeletionReadback(allocation fabric.ComputeAllocation, absent bool) fabric.ComputeAllocation {
	allocation.ProviderData = maps.Clone(allocation.ProviderData)
	present := false
	allocation.MachinePresent = &present
	allocation.Status, allocation.CVMStatus, allocation.TKEStatus = "destroying", "SHUTDOWN", "NOT_FOUND"
	allocation.DestroyState = "pending_retry"
	allocation.ProviderRequestID = "req-recycled-compute-readback"
	allocation.ProviderData["machinePresent"], allocation.ProviderData["tkeStatus"] = "false", "NOT_FOUND"
	allocation.ProviderData["cvmStatus"], allocation.ProviderData["isolatedSource"] = "SHUTDOWN", "MANMADE"
	allocation.ProviderData["describeClusterMachinesReq"] = "req-read-machine-absent"
	allocation.ProviderData["verifyMachineDeletedReqId"] = "req-read-machine-absent"
	allocation.ProviderData["describeCvmRequestId"] = "req-read-cvm"
	if absent {
		allocation.Status, allocation.CVMStatus, allocation.DestroyState = "external_deleted", "NOT_FOUND", ""
		allocation.ProviderRequestID = "req-final-compute-absence"
		allocation.ProviderData["cvmStatus"] = "NOT_FOUND"
		allocation.ProviderData["computeDestroyPhase"] = "cloud_absence_confirmed"
	}
	return allocation
}

func assertComputeFinalReleaseJournal(t *testing.T, store *fabric.MemoryOperationStore, original fabric.FabricOperation, wantStatus string) {
	t.Helper()
	operations, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	originalFound, finalFound := false, false
	for _, operation := range operations {
		if operation.ID == original.ID {
			originalFound = true
			if !reflect.DeepEqual(operation, original) {
				t.Fatal("final release changed the persisted original destroy")
			}
		}
		if operation.Action != "release_recycled_compute_allocation" {
			continue
		}
		if operation.ResourceID != original.ResourceID || operation.AccountID != original.AccountID || operation.WorkspaceID != original.WorkspaceID ||
			operation.CallerService != "workspace" || operation.RedactedProviderPayload["originalDestroyOperationId"] != original.OperationID {
			t.Fatalf("final release lost the original destroy binding: %+v", operation)
		}
		if operation.Status == wantStatus {
			finalFound = true
		}
	}
	if !originalFound || !finalFound {
		t.Fatalf("original destroy found=%v, final release status %q found=%v", originalFound, wantStatus, finalFound)
	}
}

func waitForComputeFinalReleaseDestroyRecovery(t *testing.T, store *fabric.MemoryOperationStore, resourceID string) {
	t.Helper()
	waitForDeletionCondition(t, 5*time.Second, "compute destroy recovery outcome", func() bool {
		operations, err := store.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for index := len(operations) - 1; index >= 0; index-- {
			operation := operations[index]
			if operation.Action == "destroy_compute_allocation" && operation.ResourceID == resourceID {
				return operation.Status == "failed" || operation.Status == "succeeded"
			}
		}
		return false
	})
}

// This provider fixture replaces the private Tencent cleanup boundary. Model
// its completed cleanup/ownership outcome explicitly in the original destroy
// journal; final release alone must never fabricate this owner completion.
func appendComputeFinalReleaseDestroyFinalization(t *testing.T, store *fabric.MemoryOperationStore, original fabric.FabricOperation, allocation fabric.ComputeAllocation) {
	t.Helper()
	finalized := original
	finalized.ID = original.ID + "-finalized"
	finalized.Status = "succeeded"
	finalized.CreatedAt = time.Now().UTC()
	finalized.FinishedAt = finalized.CreatedAt
	absent := computeFinalReleaseDeletionReadback(allocation, true)
	finalized.ProviderRequestID = absent.ProviderRequestID
	finalized.RedactedProviderPayload = map[string]any{"resource": absent}
	if err := store.Append(context.Background(), finalized); err != nil {
		t.Fatalf("record original destroy finalization: %v", err)
	}
}

func TestLocalDispatcherComputeFinalReleaseConfirmsFreshAbsenceOnlyAfterDestroyFinalization(t *testing.T) {
	var mu sync.Mutex
	absent, readsAfterRelease := false, 0
	fixture := &deletionProviderFixture{}
	system, allocation, original := newComputeFinalReleaseDeletionSystem(t, fixture)
	fixture.computeReadback = func(input fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
		mu.Lock()
		defer mu.Unlock()
		if absent {
			readsAfterRelease++
		}
		return computeFinalReleaseDeletionReadback(input, absent), nil
	}
	fixture.computeRelease = func(input fabric.ComputeAllocation) (string, error) {
		assertComputeFinalReleaseJournal(t, system.store, original, "started")
		if !reflect.DeepEqual(input, allocation) {
			t.Errorf("final release did not use the frozen original allocation: %+v", input)
		}
		mu.Lock()
		absent = true
		mu.Unlock()
		return "req-final-release", nil
	}
	intent := deletionIntent(allocation.WorkspaceID, allocation.ID, "")
	intent.AccountID = allocation.AccountID
	fact, err := system.dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindCompute)
	if err != nil || fact.State != coordination.DeletionStatePending || fact.ProviderStatus != "external_deleted" || fact.DestroyState != "" ||
		fact.ProviderReference != allocation.InstanceID || fact.EvidenceRef == "" || fact.ObservedAt.IsZero() {
		t.Fatalf("fresh absence before original destroy finalization fact=%+v err=%v", fact, err)
	}
	// One read confirms the release inside Fabric; another is the dispatcher's
	// fresh read. The initial SHUTDOWN read cannot have proved absence.
	mu.Lock()
	reads := readsAfterRelease
	mu.Unlock()
	if reads < 2 || fixture.releaseCalls() != 1 {
		t.Fatalf("post-release readbacks=%d release calls=%d", reads, fixture.releaseCalls())
	}
	assertComputeFinalReleaseJournal(t, system.store, original, "succeeded")
	waitForComputeFinalReleaseDestroyRecovery(t, system.store, allocation.ID)
	if destroyOperationRecorded(t, system.store, original.Action, allocation.ID) {
		t.Fatal("final release fabricated original destroy finalization")
	}
	appendComputeFinalReleaseDestroyFinalization(t, system.store, original, allocation)
	final, err := system.dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindCompute)
	if err != nil || final.State != coordination.DeletionStateAbsent || final.ProviderStatus != "external_deleted" || final.DestroyState != "" ||
		final.ProviderReference != allocation.InstanceID || final.EvidenceRef == "" || final.EvidenceRef == fact.EvidenceRef ||
		final.ObservedAt.IsZero() || final.ObservedAt.Before(fact.ObservedAt) {
		t.Fatalf("fresh absence after original destroy finalization fact=%+v err=%v", final, err)
	}
	assertComputeFinalReleaseJournal(t, system.store, original, "succeeded")
	_, _, destroys, _ := fixture.calls()
	if destroys != 0 || fixture.releaseCalls() != 1 {
		t.Fatalf("original destroy calls=%d final release calls=%d", destroys, fixture.releaseCalls())
	}
}

func TestLocalDispatcherComputeFinalReleaseLostResponseAndRestartReadOnly(t *testing.T) {
	var mu sync.Mutex
	absent := false
	lostResponse := errors.New("final release response lost")
	fixture := &deletionProviderFixture{}
	system, allocation, original := newComputeFinalReleaseDeletionSystem(t, fixture)
	fixture.computeReadback = func(input fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
		mu.Lock()
		defer mu.Unlock()
		return computeFinalReleaseDeletionReadback(input, absent), nil
	}
	fixture.computeRelease = func(input fabric.ComputeAllocation) (string, error) {
		assertComputeFinalReleaseJournal(t, system.store, original, "started")
		if !reflect.DeepEqual(input, allocation) {
			t.Errorf("final release did not use the frozen original allocation: %+v", input)
		}
		return "", lostResponse
	}
	intent := deletionIntent(allocation.WorkspaceID, allocation.ID, "")
	intent.AccountID = allocation.AccountID
	first, err := system.dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindCompute)
	if !errors.Is(err, lostResponse) || first.State == coordination.DeletionStateAbsent || fixture.releaseCalls() != 1 {
		t.Fatalf("lost response fact=%+v err=%v release calls=%d", first, err, fixture.releaseCalls())
	}
	assertComputeFinalReleaseJournal(t, system.store, original, "failed")
	waitForComputeFinalReleaseDestroyRecovery(t, system.store, allocation.ID)

	// Retry through two dispatchers sharing a freshly constructed Service. The
	// durable resource fence, rather than a dispatcher-local flag, owns dispatch.
	service := fabric.NewServiceWithOperationStore(fixture, system.store)
	dispatchers := []coordination.ResourceDispatcher{
		coordination.NewLocalDispatcher(service, fabric.NewLocalDockerProvider()),
		coordination.NewLocalDispatcher(service, fabric.NewLocalDockerProvider()),
	}
	for _, dispatcher := range dispatchers {
		fact, err := dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindCompute)
		if err != nil || fact.State != coordination.DeletionStatePending || fact.ProviderStatus != "destroying" || fact.DestroyState != "pending_retry" ||
			fact.ProviderReference != allocation.InstanceID || fact.EvidenceRef == "" || fact.ObservedAt.IsZero() {
			t.Fatalf("restarted read-only retry fact=%+v err=%v", fact, err)
		}
		waitForComputeFinalReleaseDestroyRecovery(t, system.store, allocation.ID)
	}
	if fixture.releaseCalls() != 1 {
		t.Fatalf("lost-response restart repeated final release %d times", fixture.releaseCalls())
	}
	mu.Lock()
	absent = true
	mu.Unlock()
	pending, err := dispatchers[0].DeleteResource(context.Background(), intent, coordination.DeletionKindCompute)
	if err != nil || pending.State != coordination.DeletionStatePending || pending.ProviderStatus != "external_deleted" || pending.DestroyState != "" ||
		pending.ProviderReference != allocation.InstanceID || pending.EvidenceRef == "" || pending.ObservedAt.IsZero() {
		t.Fatalf("fresh absence before original destroy finalization fact=%+v err=%v", pending, err)
	}
	waitForComputeFinalReleaseDestroyRecovery(t, system.store, allocation.ID)
	if destroyOperationRecorded(t, system.store, original.Action, allocation.ID) {
		t.Fatal("lost-response recovery fabricated original destroy finalization")
	}
	appendComputeFinalReleaseDestroyFinalization(t, system.store, original, allocation)
	restarted := coordination.NewLocalDispatcher(fabric.NewServiceWithOperationStore(fixture, system.store), fabric.NewLocalDockerProvider())
	final, err := restarted.DeleteResource(context.Background(), intent, coordination.DeletionKindCompute)
	if err != nil || final.State != coordination.DeletionStateAbsent || final.ProviderStatus != "external_deleted" || final.DestroyState != "" ||
		final.ProviderReference != allocation.InstanceID || final.EvidenceRef == "" || final.ObservedAt.IsZero() {
		t.Fatalf("finalized absence after restart fact=%+v err=%v", final, err)
	}
	assertComputeFinalReleaseJournal(t, system.store, original, "failed")
	_, _, destroys, _ := fixture.calls()
	if destroys != 0 || fixture.releaseCalls() != 1 {
		t.Fatalf("restart destroy calls=%d final release calls=%d", destroys, fixture.releaseCalls())
	}
}

func TestLocalDispatcherComputeFinalReleaseLostResponseAndRestartConcurrentReadOnly(t *testing.T) {
	lostResponse := errors.New("final release response lost")
	fixture := &deletionProviderFixture{
		computeReadback: func(input fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
			return computeFinalReleaseDeletionReadback(input, false), nil
		},
	}
	system, allocation, original := newComputeFinalReleaseDeletionSystem(t, fixture)
	fixture.computeRelease = func(input fabric.ComputeAllocation) (string, error) {
		if !reflect.DeepEqual(input, allocation) {
			t.Errorf("final release did not use the original allocation: %+v", input)
		}
		return "", lostResponse
	}
	// Every persisted started destroy attempt needs a terminal journal outcome.
	// Waiting only for the latest outcome can leave another recovery worker
	// running after the concurrent requests, including on an assertion failure.
	waitForDestroyWorkers := func() {
		t.Helper()
		waitForDeletionCondition(t, 5*time.Second, "all original compute destroy recovery outcomes", func() bool {
			operations, err := system.store.List(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			started, finished := 0, 0
			for _, operation := range operations {
				if operation.Action != original.Action || operation.ResourceID != allocation.ID {
					continue
				}
				switch operation.Status {
				case "started":
					started++
				case "failed", "succeeded":
					finished++
				}
			}
			return started > 0 && finished >= started
		})
	}
	t.Cleanup(waitForDestroyWorkers)
	intent := deletionIntent(allocation.WorkspaceID, allocation.ID, "")
	intent.AccountID = allocation.AccountID
	first, err := system.dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindCompute)
	if !errors.Is(err, lostResponse) || first.State == coordination.DeletionStateAbsent || fixture.releaseCalls() != 1 {
		t.Fatalf("lost response fact=%+v err=%v release calls=%d", first, err, fixture.releaseCalls())
	}
	waitForDestroyWorkers()
	assertComputeFinalReleaseJournal(t, system.store, original, "failed")

	service := fabric.NewServiceWithOperationStore(fixture, system.store)
	dispatchers := []coordination.ResourceDispatcher{
		coordination.NewLocalDispatcher(service, fabric.NewLocalDockerProvider()),
		coordination.NewLocalDispatcher(service, fabric.NewLocalDockerProvider()),
	}
	type result struct {
		fact coordination.ResourceDeletionFact
		err  error
	}
	const concurrency = 6
	results := make(chan result, concurrency)
	start := make(chan struct{})
	var requests sync.WaitGroup
	for index := 0; index < concurrency; index++ {
		requests.Add(1)
		go func(dispatcher coordination.ResourceDispatcher) {
			defer requests.Done()
			<-start
			fact, err := dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindCompute)
			results <- result{fact: fact, err: err}
		}(dispatchers[index%len(dispatchers)])
	}
	close(start)
	requests.Wait()
	waitForDestroyWorkers()
	close(results)
	for result := range results {
		fact := result.fact
		if result.err != nil || fact.State != coordination.DeletionStatePending || fact.ProviderStatus != "destroying" || fact.DestroyState != "pending_retry" ||
			fact.ProviderReference != allocation.InstanceID || fact.EvidenceRef == "" || fact.ObservedAt.IsZero() {
			t.Errorf("concurrent restarted read-only retry fact=%+v err=%v", fact, result.err)
		}
	}
	assertComputeFinalReleaseJournal(t, system.store, original, "failed")
	_, _, destroys, _ := fixture.calls()
	if destroys != 0 || fixture.releaseCalls() != 1 {
		t.Fatalf("concurrent restart destroy calls=%d final release calls=%d", destroys, fixture.releaseCalls())
	}
}

func TestLocalDispatcherComputeFinalReleaseRejectsFailedFreshReadback(t *testing.T) {
	fixture := &deletionProviderFixture{
		computeReadback: func(input fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
			return computeFinalReleaseDeletionReadback(input, false), nil
		},
	}
	system, allocation, original := newComputeFinalReleaseDeletionSystem(t, fixture)
	freshReadError := errors.New("fresh compute absence readback unavailable")
	fixture.computeRelease = func(fabric.ComputeAllocation) (string, error) {
		assertComputeFinalReleaseJournal(t, system.store, original, "started")
		var mu sync.Mutex
		read := false
		fixture.setComputeReadback(func(input fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
			mu.Lock()
			defer mu.Unlock()
			if read {
				return fabric.ComputeAllocation{}, freshReadError
			}
			// The final-release owner holds its pool lock while consuming this
			// confirmation. Every subsequent read, including the dispatcher's
			// fresh read, fails instead of reusing that confirmation.
			read = true
			return computeFinalReleaseDeletionReadback(input, true), nil
		})
		return "req-final-release", nil
	}
	intent := deletionIntent(allocation.WorkspaceID, allocation.ID, "")
	intent.AccountID = allocation.AccountID
	fact, err := system.dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindCompute)
	if !errors.Is(err, freshReadError) || fact.State == coordination.DeletionStateAbsent {
		t.Fatalf("failed fresh readback fact=%+v err=%v", fact, err)
	}
	assertComputeFinalReleaseJournal(t, system.store, original, "succeeded")
	waitForComputeFinalReleaseDestroyRecovery(t, system.store, allocation.ID)
	_, _, destroys, _ := fixture.calls()
	if destroys != 0 || fixture.releaseCalls() != 1 {
		t.Fatalf("destroy calls=%d final release calls=%d", destroys, fixture.releaseCalls())
	}
}

// secretDeletionProvider reports the Workspace gateway Secret binding absent on
// the owning readback surface, exactly like the provider after a confirmed
// cleanup. It is the typed provider boundary for the Secret readback coverage:
// the release itself still runs through the real local dispatcher and the real
// Fabric service.
type secretDeletionProvider struct {
	fabric.Provider
}

func (p *secretDeletionProvider) BindWorkspaceRuntimeGatewaySecret(context.Context, fabric.WorkspaceRuntimeGatewaySecretInput) (fabric.WorkspaceRuntimeGatewaySecretBinding, error) {
	return fabric.WorkspaceRuntimeGatewaySecretBinding{}, errors.New("the secret readback fixture does not bind")
}

func (p *secretDeletionProvider) WorkspaceRuntimeGatewaySecret(context.Context, string) (fabric.WorkspaceRuntimeGatewaySecretBinding, error) {
	return fabric.WorkspaceRuntimeGatewaySecretBinding{}, fabric.ErrWorkspaceLaunchResourceAbsent
}

// TestLocalDispatcherSecretDeletionConfirmsAbsenceFromOwnerReadback proves the
// Gateway Secret binding release confirms absence from the owning readback and
// carries that readback's own evidence identity: a confirmed Secret deletion is
// never reported with an empty evidence reference.
func TestLocalDispatcherSecretDeletionConfirmsAbsenceFromOwnerReadback(t *testing.T) {
	const workspace = "workspace-secret-absent"
	store := fabric.NewMemoryOperationStore()
	service := fabric.NewServiceWithOperationStore(&secretDeletionProvider{}, store)
	dispatcher := coordination.NewLocalDispatcher(service, fabric.NewLocalDockerProvider())
	if dispatcher == nil {
		t.Fatal("local dispatcher is unavailable")
	}
	fact, err := dispatcher.DeleteResource(context.Background(), deletionIntent(workspace, "", ""), coordination.DeletionKindSecret)
	if err != nil {
		t.Fatalf("secret deletion: %v", err)
	}
	if fact.State != coordination.DeletionStateAbsent {
		t.Fatalf("secret state=%q, want %q", fact.State, coordination.DeletionStateAbsent)
	}
	if fact.ProviderStatus != "absent" || fact.EvidenceRef == "" || fact.ObservedAt.IsZero() {
		t.Fatalf("secret absence fact lacks owner readback evidence: %+v", fact)
	}
}
