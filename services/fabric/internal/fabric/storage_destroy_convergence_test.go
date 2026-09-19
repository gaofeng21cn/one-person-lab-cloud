package fabric

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// cbsLifecycleStorageProvider models the Tencent CBS truth: one disk that is
// either absent, present and attached, or present and detached. Its destroy path
// refuses while the disk is attached (matching the provisioner precondition) and
// otherwise terminates it.
type cbsLifecycleStorageProvider struct {
	testProvider
	mu         sync.Mutex
	present    bool
	attached   bool
	dispatches int
	readbacks  int
}

func (p *cbsLifecycleStorageProvider) state() (bool, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.present, p.attached
}

func (p *cbsLifecycleStorageProvider) DispatchCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dispatches
}

func (p *cbsLifecycleStorageProvider) ReadStorageVolumeStatus(_ context.Context, volume StorageVolume) (StorageVolume, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.readbacks++
	if !p.present {
		return cbsAbsentReadback(volume), nil
	}
	volume.Status, volume.ProviderRequestID = "ready", "req-cbs-readback"
	if p.attached {
		volume.CBSStatus = "ATTACHED"
	} else {
		volume.CBSStatus = "UNATTACHED"
	}
	return volume, nil
}

func (p *cbsLifecycleStorageProvider) DestroyStorageVolume(_ context.Context, volume StorageVolume) (StorageVolume, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.present {
		// Nothing to dispatch: the disk is already gone.
		return cbsAbsentReadback(volume), nil
	}
	if p.attached {
		// The provider refuses to terminate an attached disk. No CBS RPC ran, so
		// the refusal carries the retryable classification the provider owns.
		refused := storageDestroyPhaseResult(volume, "terminate_not_attempted", "0", "ready", "ATTACHED")
		refused.DestroyState = StorageDestroyStatePendingRetry
		return refused, errors.New("storage_volume_detach_unverified: Tencent CBS is still ATTACHED after bounded detach reconciliation.")
	}
	p.dispatches++
	p.present = false
	return cbsAbsentReadback(volume), nil
}

func cbsAbsentReadback(volume StorageVolume) StorageVolume {
	volume = storageDestroyPhaseResult(volume, "absence_confirmed", "1", "external_deleted", "NOT_FOUND")
	volume.ProviderData["storageVolumeId"] = volume.ProviderResourceID
	volume.ProviderData["cbsStatus"] = "NOT_FOUND"
	volume.ProviderData["status"] = "external_deleted"
	volume.ProviderData["describeCbsRequestId"] = "req-describe-cbs-absent"
	return volume
}

// storageDestroyAttachedFailurePersisted records the exact durable evidence the
// owning operation persists after the provider refused to terminate an attached
// disk.
func storageDestroyAttachedFailurePersisted(t *testing.T, store OperationStore, resource StorageVolume) StorageVolume {
	t.Helper()
	failed := storageDestroyPhaseResult(resource, "terminate_not_attempted", "0", "ready", "ATTACHED")
	appendFailedStorageDestroy(t, store, failed)
	return failed
}

func TestStorageDestroyRefusesAndStaysRetryableWhileCbsAttached(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryOperationStore()
	resource := storageDestroyTestVolume("storage-attached-refusal")
	appendSucceededStorageCreate(t, store, resource)
	provider := &cbsLifecycleStorageProvider{present: true, attached: true}
	service := NewServiceWithOperationStore(provider, store)

	result, err := service.DestroyStorageVolume(ctx, resource.ID)
	if !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatalf("attached refusal result=%#v err=%v", result, err)
	}
	if storageDestroyReadbackConfirmsAbsence(result) || result.Status == "external_deleted" || result.Status == "destroyed" {
		t.Fatalf("attached disk reported as destroyed: %#v", result)
	}
	if provider.DispatchCount() != 0 {
		t.Fatalf("attached disk was terminated: dispatches=%d", provider.DispatchCount())
	}
	latest, found, err := store.LatestResourceOperation(ctx, "storage_volume", resource.ID)
	if err != nil || !found || latest.Status != "failed" {
		t.Fatalf("latest=%#v found=%v err=%v", latest, found, err)
	}
	var persisted StorageVolume
	if !decodeOperationResource(latest, &persisted) || persisted.ProviderData["storageDestroyMutationCount"] != "0" {
		t.Fatalf("persisted refusal evidence=%#v", persisted)
	}
}

func TestStorageDestroyRedispatchConvergesAfterCbsDetaches(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryOperationStore()
	resource := storageDestroyTestVolume("storage-detach-then-destroy")
	appendSucceededStorageCreate(t, store, resource)
	storageDestroyAttachedFailurePersisted(t, store, resource)

	// The disk is now detached: the persisted evidence proves no CBS mutation was
	// dispatched, so the owned operation may terminate it exactly once.
	provider := &cbsLifecycleStorageProvider{present: true, attached: false}
	service := NewServiceWithOperationStore(provider, store)
	result, err := service.DestroyStorageVolume(ctx, resource.ID)
	if err != nil || !storageDestroyReadbackConfirmsAbsence(result) {
		t.Fatalf("converged result=%#v err=%v", result, err)
	}
	if provider.DispatchCount() != 1 {
		t.Fatalf("dispatches=%d", provider.DispatchCount())
	}
	// A further replay reads back the absence and must not terminate again.
	result, err = service.DestroyStorageVolume(ctx, resource.ID)
	if err != nil || !storageDestroyReadbackConfirmsAbsence(result) || provider.DispatchCount() != 1 {
		t.Fatalf("replay result=%#v err=%v dispatches=%d", result, err, provider.DispatchCount())
	}
}

func TestStorageDestroyNeverRedispatchesAfterUncertainCbsSend(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryOperationStore()
	resource := storageDestroyTestVolume("storage-uncertain-send")
	appendSucceededStorageCreate(t, store, resource)
	appendFailedStorageDestroy(t, store, storageDestroyPhaseResult(resource, "terminate_attempted", "1", "ready", "UNATTACHED"))

	provider := &cbsLifecycleStorageProvider{present: true, attached: false}
	service := NewServiceWithOperationStore(provider, store)
	result, err := service.DestroyStorageVolume(ctx, resource.ID)
	if !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatalf("uncertain send result=%#v err=%v", result, err)
	}
	if provider.DispatchCount() != 0 {
		t.Fatalf("uncertain send redispatched: dispatches=%d", provider.DispatchCount())
	}
	if storageDestroyReadbackConfirmsAbsence(result) {
		t.Fatalf("uncertain send reported absence: %#v", result)
	}
	// A later authoritative absence readback still converges without a mutation.
	provider.mu.Lock()
	provider.present = false
	provider.mu.Unlock()
	result, err = service.DestroyStorageVolume(ctx, resource.ID)
	if err != nil || !storageDestroyReadbackConfirmsAbsence(result) || provider.DispatchCount() != 0 {
		t.Fatalf("absence convergence result=%#v err=%v dispatches=%d", result, err, provider.DispatchCount())
	}
}

func TestStorageDestroyAlreadyAbsentConvergesWithoutMutation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryOperationStore()
	resource := storageDestroyTestVolume("storage-already-absent")
	appendSucceededStorageCreate(t, store, resource)
	storageDestroyAttachedFailurePersisted(t, store, resource)

	provider := &cbsLifecycleStorageProvider{present: false}
	service := NewServiceWithOperationStore(provider, store)
	result, err := service.DestroyStorageVolume(ctx, resource.ID)
	if err != nil || !storageDestroyReadbackConfirmsAbsence(result) {
		t.Fatalf("absent result=%#v err=%v", result, err)
	}
	if provider.DispatchCount() != 0 {
		t.Fatalf("absent disk was terminated: dispatches=%d", provider.DispatchCount())
	}
}
