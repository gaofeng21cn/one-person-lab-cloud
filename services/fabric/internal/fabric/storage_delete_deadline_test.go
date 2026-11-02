package fabric

import (
	"context"
	"reflect"
	"testing"
)

func TestTencentStorageDeleteReadbackKeepsFrozenDeadline(t *testing.T) {
	for _, tc := range []struct {
		name, deadline string
		wantError      bool
	}{
		{"shortened", "2026-10-23T00:00:00Z", false},
		{"extended", "2026-12-01T00:00:00Z", true},
		{"invalid", "invalid", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			volume := canonicalTencentStorageDestroyFixture()
			volume.Deadline = "2026-11-01T00:00:00Z"
			volume.ProviderData["deadline"] = volume.Deadline
			volume.ProviderData["storageDestroyPhase"] = storageDestroyPhaseDispatchAuthorized
			original := cloneStorageVolume(volume)
			provider := &TencentProvider{
				provision: func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
					if request.Action != "read_storage_for_delete" || request.Storage.Deadline != original.Deadline {
						t.Fatalf("unexpected read request: %#v", request)
					}
					return provisionerResponse{OK: true, StorageVolumeID: volume.ProviderResourceID, CBSStatus: "TORECYCLE", Status: "pending",
						ProviderData: map[string]string{"region": "ap-guangzhou", "deadline": tc.deadline}}, nil
				},
				kubectl: func(_ context.Context, args []string, _ []byte) ([]byte, error) {
					if args[0] != "get" {
						t.Fatalf("unexpected mutation: %v", args)
					}
					return []byte(`{"kind":"List","items":[]}`), nil
				},
			}
			ctx := context.WithValue(context.Background(), workspaceStorageDeleteOwnerContextKey{}, original)
			readback, err := provider.ReadStorageVolumeStatus(ctx, volume)
			if (err != nil) != tc.wantError {
				t.Fatalf("readback=%#v err=%v", readback, err)
			}
			if !reflect.DeepEqual(volume, original) {
				t.Fatal("readback mutated the caller's frozen volume")
			}
			if !tc.wantError && (readback.Deadline != original.Deadline || readback.ProviderData["deadline"] != tc.deadline || readback.BindingPresent == nil || *readback.BindingPresent) {
				t.Fatalf("frozen deadline or current observation lost: %#v", readback)
			}
		})
	}
}

func TestTencentStorageDeleteRecoversPersistedShortenedDeadline(t *testing.T) {
	for _, state := range []string{StorageDestroyStatePendingRetry, StorageDestroyStateUnconfirmedSend} {
		t.Run(state, func(t *testing.T) {
			store := NewMemoryOperationStore()
			testTencentStorageDeleteRecoversShortenedDeadline(t, store, func(previous OperationStore) OperationStore { return previous }, state)
		})
	}
}

func TestPostgresTencentStorageDeleteRecoversPersistedShortenedDeadline(t *testing.T) {
	databaseURL := fabricTestDatabaseURL(t)
	reopen := func(previous OperationStore) OperationStore {
		if previous != nil {
			if err := previous.(*PostgresOperationStore).client.Close(); err != nil {
				t.Fatal(err)
			}
		}
		store, err := newTestPostgresOperationStore(databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.client.Close() })
		return store
	}
	testTencentStorageDeleteRecoversShortenedDeadline(t, reopen(nil), reopen, StorageDestroyStatePendingRetry)
}

func testTencentStorageDeleteRecoversShortenedDeadline(t *testing.T, store OperationStore, reopen func(OperationStore) OperationStore, retryState string) {
	t.Helper()
	ctx := context.Background()
	volume := canonicalTencentStorageDestroyFixture()
	volume.Deadline = "2026-11-01T00:00:00Z"
	volume.ProviderData["deadline"] = volume.Deadline
	appendSucceededStorageCreate(t, store, volume)
	failed := storageDestroyPhaseResult(volume, storageDestroyPhaseDispatchAuthorized, "0", "destroying", "UNATTACHED")
	failed.ProviderData["deadline"] = "2026-10-23T00:00:00Z"
	failed.DestroyState = retryState
	appendStartedStorageDestroy(t, store, storageDestroyPhaseResult(volume, storageDestroyPhaseDispatchAuthorized, "0", "destroying", "UNATTACHED"))
	appendFailedStorageDestroy(t, store, failed)
	reads := 0
	provider := &TencentProvider{
		provision: func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
			if request.Action != "read_storage_for_delete" || request.Storage.ID != volume.ProviderResourceID {
				t.Fatalf("recovery dispatched a mutation or changed disk: %#v", request)
			}
			reads++
			return provisionerResponse{OK: true, StorageVolumeID: volume.ProviderResourceID, CBSStatus: "NOT_FOUND", Status: "external_deleted",
				ProviderData: map[string]string{"region": "ap-guangzhou", "storageVolumeId": volume.ProviderResourceID, "cbsStatus": "NOT_FOUND", "status": "external_deleted",
					"storageDestroyPhase": storageDestroyPhaseAbsenceConfirmed, "storageDestroyMutationCount": "0", "describeCbsRequestId": "req-original-disk-absent"}}, nil
		},
		kubectl: func(_ context.Context, args []string, _ []byte) ([]byte, error) {
			if args[0] != "get" {
				t.Fatalf("recovery mutated bindings: %v", args)
			}
			return []byte(`{"kind":"List","items":[]}`), nil
		},
	}
	for range 2 {
		store = reopen(store)
		service := NewServiceWithOperationStore(provider, store)
		result, err := service.DestroyStorageVolume(ctx, volume.ID)
		if err != nil || !storageDestroyReadbackConfirmsAbsence(result) || result.BindingPresent == nil || *result.BindingPresent || result.Deadline != volume.Deadline || result.DestroyState != "" {
			t.Fatalf("restarted recovery=%#v err=%v", result, err)
		}
	}
	if reads != 2 {
		t.Fatalf("expected two fresh absence reads, got %d", reads)
	}
	latest, found, err := store.LatestResourceOperation(ctx, "storage_volume", volume.ID)
	var persisted StorageVolume
	if err != nil || !found || latest.Status != "succeeded" || latest.ErrorCode != "" || !decodeOperationResource(latest, &persisted) ||
		persisted.Deadline != volume.Deadline || persisted.DestroyState != "" || !storageDestroyReadbackConfirmsAbsence(persisted) {
		t.Fatalf("recovered operation=%#v persisted=%#v found=%v err=%v", latest, persisted, found, err)
	}
}

func TestTencentStorageDeleteRejectsPersistedIdentityDrift(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*StorageVolume)
	}{
		{"extended deadline", func(v *StorageVolume) { v.ProviderData["deadline"] = "2026-12-01T00:00:00Z" }},
		{"invalid deadline", func(v *StorageVolume) { v.ProviderData["deadline"] = "invalid" }},
		{"missing deadline", func(v *StorageVolume) { delete(v.ProviderData, "deadline") }},
		{"changed original term", func(v *StorageVolume) { v.Deadline = "2026-10-23T00:00:00Z" }},
		{"foreign disk", func(v *StorageVolume) { v.ProviderResourceID = "disk-other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemoryOperationStore()
			volume := canonicalTencentStorageDestroyFixture()
			volume.Deadline = "2026-11-01T00:00:00Z"
			volume.ProviderData["deadline"] = volume.Deadline
			appendSucceededStorageCreate(t, store, volume)
			failed := storageDestroyPhaseResult(volume, storageDestroyPhaseDispatchAuthorized, "0", "destroying", "UNATTACHED")
			failed.ProviderData["deadline"] = "2026-10-23T00:00:00Z"
			tc.mutate(&failed)
			appendFailedStorageDestroy(t, store, failed)
			provider := &phasedStorageDestroyProvider{}
			service := NewServiceWithOperationStore(provider, store)
			_, err := service.DestroyStorageVolume(context.Background(), volume.ID)
			if err == nil || err.Error() != "storage_destroy_replay_identity_mismatch" || provider.destroyCalls.Load() != 0 || provider.readbackCalls.Load() != 0 {
				t.Fatalf("invalid persisted identity reached provider: err=%v destroys=%d reads=%d", err, provider.destroyCalls.Load(), provider.readbackCalls.Load())
			}
		})
	}
}

func TestTencentStorageDestroyAcceptsShortenedDeadlineObservation(t *testing.T) {
	volume := canonicalTencentStorageDestroyFixture()
	volume.DestroyState = StorageDestroyStatePendingRetry
	volume.Deadline = "2026-11-01T00:00:00Z"
	volume.ProviderData["deadline"] = volume.Deadline
	store := NewMemoryOperationStore()
	appendSucceededStorageCreate(t, store, volume)
	fixture := newStorageDeleteTestFixture(t, volume, false, false)
	destroys := 0
	provider := &TencentProvider{kubectl: fixture.run}
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		if request.Storage.ID != volume.ProviderResourceID {
			t.Fatalf("changed original disk: %#v", request)
		}
		if request.Action == "read_storage_for_delete" {
			response := canonicalTencentStorageStatusResponse(request)
			response.ProviderData["deadline"] = volume.Deadline
			return response, nil
		}
		if request.Action != "destroy_storage_volume" {
			t.Fatalf("unexpected action: %s", request.Action)
		}
		destroys++
		return provisionerResponse{OK: true, StorageVolumeID: volume.ProviderResourceID, ProviderRequestID: "req-cbs-destroy", CBSStatus: "NOT_FOUND", Status: "external_deleted", MutationCount: 1,
			ProviderData: map[string]string{"region": "ap-guangzhou", "deadline": "2026-10-23T00:00:00Z", "storageVolumeId": volume.ProviderResourceID, "cbsStatus": "NOT_FOUND", "status": "external_deleted",
				"storageDestroyPhase": storageDestroyPhaseAbsenceConfirmed, "storageDestroyMutationCount": "1", "describeCbsRequestId": "req-cbs-absent", "terminateCbsRequestId": "req-cbs-destroy"}}, nil
	}
	service := NewServiceWithOperationStore(provider, store)
	result, err := service.DestroyStorageVolume(context.Background(), volume.ID)
	if err != nil || !storageDestroyReadbackConfirmsAbsence(result) || result.Deadline != volume.Deadline || result.DestroyState != "" || destroys != 1 {
		t.Fatalf("destroy result=%#v err=%v calls=%d", result, err, destroys)
	}
}
