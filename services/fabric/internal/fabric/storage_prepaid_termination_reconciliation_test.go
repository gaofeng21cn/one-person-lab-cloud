package fabric

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// The original prepaid CBS terminate was dispatched but never confirmed: the
// journal keeps the "authorized, outcome unknown" phase and zero mutation
// count, so Fabric refuses to re-send it and refuses the final release that
// would finish the recycled disk. An operator may accept that unresolved
// dispatch exactly once, and the provider still proves the disk is recycled
// and unbound before it terminates it.
func storagePrepaidTerminationReconciliationFixture(t *testing.T, store OperationStore) (StorageVolume, StoragePrepaidTerminationReconciliationInput) {
	t.Helper()
	volume, release := storageFinalReleaseFixture(t, store)
	return volume, StoragePrepaidTerminationReconciliationInput{
		AccountID:              volume.AccountID,
		WorkspaceID:            volume.WorkspaceID,
		DestroyOperationID:     release.DestroyOperationID,
		AuthorizationReference: "owner-reconciliation-1",
		Confirmation:           StoragePrepaidTerminationReconciliationConfirmation,
	}
}

func storagePrepaidTerminationDestroyJournal(t *testing.T, store OperationStore, id string) []FabricOperation {
	t.Helper()
	operations, err := store.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var rows []FabricOperation
	for _, operation := range operations {
		if operation.ResourceID == id && operation.Action == "destroy_storage_volume" {
			rows = append(rows, storageFinalReleaseJSONOperation(t, operation))
		}
	}
	return rows
}

func storagePrepaidTerminationClaim(t *testing.T, store OperationStore, id string) FabricOperation {
	t.Helper()
	operation, found, err := store.LatestResourceOperation(t.Context(), storagePrepaidTerminationReconciliationResourceKind, id)
	if err != nil || !found {
		t.Fatalf("reconciliation claim missing: found=%v err=%v", found, err)
	}
	return operation
}

func TestStoragePrepaidTerminationReconciliationRefusesUnboundIntent(t *testing.T) {
	store := NewMemoryOperationStore()
	volume, input := storagePrepaidTerminationReconciliationFixture(t, store)
	provider := newStorageFinalReleaseTestProvider(t, volume)
	service := NewServiceWithOperationStore(provider, store)
	for _, change := range []func(*StoragePrepaidTerminationReconciliationInput){
		func(in *StoragePrepaidTerminationReconciliationInput) { in.Confirmation = "" },
		func(in *StoragePrepaidTerminationReconciliationInput) { in.Confirmation = "RELEASE_ORIGINAL_RECYCLED_CBS" },
		func(in *StoragePrepaidTerminationReconciliationInput) { in.AuthorizationReference = "" },
		func(in *StoragePrepaidTerminationReconciliationInput) { in.AuthorizationReference = "not a token" },
		func(in *StoragePrepaidTerminationReconciliationInput) { in.AccountID = "foreign-account" },
		func(in *StoragePrepaidTerminationReconciliationInput) { in.WorkspaceID = "foreign-workspace" },
		func(in *StoragePrepaidTerminationReconciliationInput) { in.DestroyOperationID = "foreign-destroy" },
	} {
		invalid := input
		change(&invalid)
		if _, err := service.ReconcilePrepaidStorageTermination(t.Context(), volume.ID, invalid); err == nil {
			t.Fatal("unbound reconciliation intent admitted")
		}
		if provider.calls.Load() != 0 {
			t.Fatal("refusal sent a mutation")
		}
	}
}

func TestStoragePrepaidTerminationReconciliationRefusesRecordedDispatch(t *testing.T) {
	for _, shape := range []struct {
		phase         string
		mutationCount string
	}{
		{storageDestroyPhaseTerminateAttempted, "1"},
		{storageDestroyPhaseDispatchAuthorized, "1"},
		{storageDestroyPhaseNotAttempted, "0"},
		{storageDestroyPhaseAbsenceConfirmed, "1"},
	} {
		t.Run(shape.phase+"-"+shape.mutationCount, func(t *testing.T) {
			store := NewMemoryOperationStore()
			volume, input := storagePrepaidTerminationReconciliationFixture(t, store)
			resolved := storageDestroyPhaseResult(volume, shape.phase, shape.mutationCount, "destroying", "UNATTACHED")
			resolved.CBSStatus = "TORECYCLE"
			resolved.ProviderData["cbsStatus"] = "TORECYCLE"
			resolved.ProviderData["storageVolumeId"] = resolved.ProviderResourceID
			resolved.ProviderData["describeCbsRequestId"] = "describe-cbs"
			operation := newOperation("destroy_storage_volume", "storage_volume", resolved.ID, resolved.AccountID, resolved.WorkspaceID, "", hashInput(map[string]string{"id": resolved.ID}), time.Now().UTC().Add(time.Hour))
			operation.ID, operation.Status, operation.CreatedAt = "original-storage-destroy-"+shape.phase, "started", operation.StartedAt
			fillOperationResource(&operation, resolved)
			if err := store.Append(t.Context(), storageFinalReleaseJSONOperation(t, operation)); err != nil {
				t.Fatal(err)
			}
			provider := newStorageFinalReleaseTestProvider(t, volume)
			service := NewServiceWithOperationStore(provider, store)
			if _, err := service.ReconcilePrepaidStorageTermination(t.Context(), volume.ID, input); err == nil {
				t.Fatal("a decided or recorded dispatch was reconciled")
			}
			if provider.calls.Load() != 0 {
				t.Fatal("refusal sent a mutation")
			}
		})
	}
}

func TestStoragePrepaidTerminationReconciliationRefusesWhenFinalReleaseOwnsTheDisk(t *testing.T) {
	store := NewMemoryOperationStore()
	volume, input := storagePrepaidTerminationReconciliationFixture(t, store)
	release := newOperation(storageFinalReleaseAction, "storage_final_release", volume.ID, volume.AccountID, volume.WorkspaceID, "", hashInput(map[string]string{"id": volume.ID}), time.Now().UTC())
	release.ID, release.Status = "existing-storage-final-release", "started"
	fillOperationResource(&release, volume)
	if err := store.Append(t.Context(), storageFinalReleaseJSONOperation(t, release)); err != nil {
		t.Fatal(err)
	}
	provider := newStorageFinalReleaseTestProvider(t, volume)
	service := NewServiceWithOperationStore(provider, store)
	if _, err := service.ReconcilePrepaidStorageTermination(t.Context(), volume.ID, input); err == nil {
		t.Fatal("a disk already owned by a final release was reconciled")
	}
	if provider.calls.Load() != 0 {
		t.Fatal("refusal sent a mutation")
	}
}

func TestStoragePrepaidTerminationReconciliationCompletesUnresolvedDispatchOnce(t *testing.T) {
	store := NewMemoryOperationStore()
	volume, input := storagePrepaidTerminationReconciliationFixture(t, store)
	before := storagePrepaidTerminationDestroyJournal(t, store, volume.ID)
	provider := newStorageFinalReleaseTestProvider(t, volume)
	service := NewServiceWithOperationStore(provider, store)
	provider.release = func(ctx context.Context, request StorageVolume) (string, error) {
		claim := storagePrepaidTerminationClaim(t, store, volume.ID)
		if claim.Status != "started" || claim.CallerService != "operator" || claim.AccountID != volume.AccountID || claim.WorkspaceID != volume.WorkspaceID {
			t.Fatalf("missing durable operator claim: %+v", claim)
		}
		if !reflect.DeepEqual(volume, request) {
			t.Fatal("completion did not retain the frozen original resource")
		}
		return "reconcile-release-request", nil
	}
	// The dispatch is accepted while the disk is still recycled, so the first
	// attempt reports an unconfirmed outcome and never re-sends.
	first, err := service.ReconcilePrepaidStorageTermination(t.Context(), volume.ID, input)
	if err == nil || !first.DispatchAttempted || first.AbsenceConfirmed || provider.calls.Load() != 1 {
		t.Fatalf("first=%+v err=%v calls=%d", first, err, provider.calls.Load())
	}
	claim := storagePrepaidTerminationClaim(t, store, volume.ID)
	if claim.ErrorCode == "" {
		t.Fatal("unconfirmed outcome was not recorded")
	}
	// The same authorization reference replays the same claim without another
	// completion, and reports the absence once the provider confirms it.
	provider.absent.Store(true)
	replay, err := service.ReconcilePrepaidStorageTermination(t.Context(), volume.ID, input)
	if err != nil || !replay.DispatchAttempted || !replay.AbsenceConfirmed || replay.OperationID != first.OperationID || provider.calls.Load() != 1 {
		t.Fatalf("replay=%+v err=%v calls=%d", replay, err, provider.calls.Load())
	}
	// A different operator reference never opens a second completion.
	other := input
	other.AuthorizationReference = "owner-reconciliation-2"
	if _, err := service.ReconcilePrepaidStorageTermination(t.Context(), volume.ID, other); err == nil {
		t.Fatal("a second authorization reference opened another completion")
	}
	if provider.calls.Load() != 1 {
		t.Fatal("a second completion was dispatched")
	}
	if !reflect.DeepEqual(before, storagePrepaidTerminationDestroyJournal(t, store, volume.ID)) {
		t.Fatal("reconciliation rewrote the original destroy journal")
	}
	// The platform's own deletion still converges the SAME original operation
	// once the provider reports the disk gone.
	result, err := service.DestroyStorageVolume(t.Context(), volume.ID)
	latest, found, latestErr := store.LatestResourceOperation(t.Context(), "storage_volume", volume.ID)
	if err != nil || !storageDestroyReadbackConfirmsAbsence(result) || latestErr != nil || !found || latest.OperationID != input.DestroyOperationID {
		t.Fatalf("original convergence=%+v err=%v latest=%+v latestErr=%v", result, err, latest, latestErr)
	}
}

func TestStoragePrepaidTerminationReconciliationKeepsFailedOutcomeUnretried(t *testing.T) {
	store := NewMemoryOperationStore()
	volume, input := storagePrepaidTerminationReconciliationFixture(t, store)
	provider := newStorageFinalReleaseTestProvider(t, volume)
	service := NewServiceWithOperationStore(provider, store)
	provider.release = func(ctx context.Context, request StorageVolume) (string, error) {
		return "", errors.New("provider refused the recycled disk")
	}
	if _, err := service.ReconcilePrepaidStorageTermination(t.Context(), volume.ID, input); err == nil {
		t.Fatal("a refused completion reported success")
	}
	claim := storagePrepaidTerminationClaim(t, store, volume.ID)
	if claim.Status != "failed" || claim.ErrorCode == "" {
		t.Fatalf("missing failed outcome: %+v", claim)
	}
	if _, err := service.ReconcilePrepaidStorageTermination(t.Context(), volume.ID, input); err != nil {
		t.Fatalf("replay of a failed completion must not send again: %v", err)
	}
	if provider.calls.Load() != 1 {
		t.Fatalf("failed completion was retried: %d", provider.calls.Load())
	}
}
