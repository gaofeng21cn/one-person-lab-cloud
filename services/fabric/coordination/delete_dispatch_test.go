package coordination_test

import (
	"context"
	"errors"
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

	storageDestroyCalls  int
	storageReadbackCalls int
	computeDestroyCalls  int
	computeReadbackCalls int
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
