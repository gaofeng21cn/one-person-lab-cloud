package fabric

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Cross the actual journal JSON boundary: resource is a decoded map, never a
// typed StorageVolume masquerading as a persisted payload.
func storageFinalReleaseFixture(t *testing.T, store OperationStore) (StorageVolume, StorageFinalReleaseInput) {
	t.Helper()
	volume := storageDestroyTestVolume("storage-final-release")
	volume.ProviderData["region"] = "ap-guangzhou"
	volume.ProviderData["pvName"] = k8sName(volume.ID) + "-pv"
	volume.ProviderData["pvcName"] = k8sName(volume.ID) + "-data"
	create := newOperation("create_storage_volume", "storage_volume", volume.ID, volume.AccountID, volume.WorkspaceID, volume.OperationID, "", time.Time{})
	volume.CostTags = oplCostTags(volume.AccountID, volume.WorkspaceID, volume.ID, create.OperationID)
	appendSucceededStorageCreate(t, store, volume)
	volume = storageDestroyPhaseResult(volume, storageDestroyPhaseDispatchAuthorized, "0", "destroying", "UNATTACHED")
	volume.DestroyState = StorageDestroyStateUnconfirmedSend
	volume.CBSStatus = "TORECYCLE"
	volume.ProviderData["cbsStatus"] = "TORECYCLE"
	volume.ProviderData["storageVolumeId"] = volume.ProviderResourceID
	volume.ProviderData["describeCbsRequestId"] = "old-journal-describe-cbs"
	operation := newOperation("destroy_storage_volume", "storage_volume", volume.ID, volume.AccountID, volume.WorkspaceID, "", hashInput(map[string]string{"id": volume.ID}), time.Now().UTC())
	operation.ID, operation.Status, operation.CreatedAt = "original-storage-destroy", string(StorageFinalReleaseStatusStarted), operation.StartedAt
	fillOperationResource(&operation, volume)
	operation = storageFinalReleaseJSONOperation(t, operation)
	if _, ok := operation.RedactedProviderPayload["resource"].(map[string]any); !ok {
		t.Fatal("fixture did not cross journal JSON boundary")
	}
	if err := store.Append(t.Context(), operation); err != nil {
		t.Fatal(err)
	}
	return volume, StorageFinalReleaseInput{AccountID: volume.AccountID, WorkspaceID: volume.WorkspaceID, DestroyOperationID: operation.OperationID, Confirmation: StorageFinalReleaseConfirmation}
}

func storageFinalReleaseJSONOperation(t *testing.T, operation FabricOperation) FabricOperation {
	t.Helper()
	data, err := json.Marshal(operation)
	if err != nil {
		t.Fatal(err)
	}
	var decoded FabricOperation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

type storageFinalReleaseTestProvider struct {
	*TencentProvider
	calls   atomic.Int32
	reads   atomic.Int32
	absent  atomic.Bool
	release func(context.Context, StorageVolume) (string, error)
}

func newStorageFinalReleaseTestProvider(t *testing.T, frozen StorageVolume) *storageFinalReleaseTestProvider {
	t.Helper()
	p := &storageFinalReleaseTestProvider{TencentProvider: NewTencentProvider()}
	bindings := newStorageDeleteTestFixture(t, frozen, false, false)
	p.kubectl = func(ctx context.Context, args []string, data []byte) ([]byte, error) {
		if len(args) == 0 || args[0] != "get" {
			return nil, fmt.Errorf("unexpected kubectl mutation: %v", args)
		}
		return bindings.run(ctx, args, data)
	}
	p.provision = func(ctx context.Context, request provisionerRequest) (provisionerResponse, error) {
		p.reads.Add(1)
		if err := ctx.Err(); err != nil {
			return provisionerResponse{}, err
		}
		if request.Action != "read_storage_for_delete" || request.Storage.ID != frozen.ProviderResourceID || !reflect.DeepEqual(request.Tags, frozen.CostTags) {
			return provisionerResponse{}, fmt.Errorf("unexpected storage read: %+v", request)
		}
		state, status, phase := "TORECYCLE", "pending", storageDestroyPhaseTerminateAttempted
		if p.absent.Load() {
			state, status, phase = "NOT_FOUND", "external_deleted", storageDestroyPhaseAbsenceConfirmed
		}
		return provisionerResponse{OK: true, StorageVolumeID: request.Storage.ID, CBSStatus: state, Status: status, ProviderRequestID: "describe-final-release", ProviderData: map[string]string{
			"region": request.Region, "storageVolumeId": request.Storage.ID, "cbsStatus": state, "status": status,
			"storageDestroyPhase": phase, "storageDestroyMutationCount": "1", "describeCbsRequestId": "describe-exact-cbs",
		}}, nil
	}
	return p
}

func (p *storageFinalReleaseTestProvider) ReleaseRecycledStorageVolume(ctx context.Context, volume StorageVolume) (string, error) {
	p.calls.Add(1)
	if p.release != nil {
		return p.release(ctx, volume)
	}
	return "final-release-request", errors.New("lost provider response")
}

func storageFinalReleaseOriginalJournal(t *testing.T, store OperationStore) []FabricOperation {
	t.Helper()
	operations, err := store.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var original []FabricOperation
	for _, operation := range operations {
		if operation.Action != storageFinalReleaseAction {
			original = append(original, storageFinalReleaseJSONOperation(t, operation))
		}
	}
	return original
}

func storageFinalReleaseChild(t *testing.T, store OperationStore, id string) FabricOperation {
	t.Helper()
	operation, found, err := store.LatestResourceOperation(t.Context(), "storage_final_release", id)
	if err != nil || !found {
		t.Fatalf("child missing: found=%v err=%v", found, err)
	}
	return operation
}

func TestStorageFinalReleaseReplaysLostResponseWithoutAnotherMutation(t *testing.T) {
	testStorageFinalReleaseRestart(t, NewMemoryOperationStore(), func(store OperationStore) OperationStore { return store })
}

func testStorageFinalReleaseRestart(t *testing.T, store OperationStore, reopen func(OperationStore) OperationStore) {
	t.Helper()
	volume, input := storageFinalReleaseFixture(t, store)
	before := storageFinalReleaseOriginalJournal(t, store)
	provider := newStorageFinalReleaseTestProvider(t, volume)
	service := NewServiceWithOperationStore(provider, store)
	provider.release = func(ctx context.Context, request StorageVolume) (string, error) {
		fence := storageFinalReleaseChild(t, store, volume.ID)
		if fence.ID != stableID("fop-storage-final-release-dispatch", volume.ID) || fence.Status != string(StorageFinalReleaseStatusStarted) || !reflect.DeepEqual(volume, request) {
			t.Fatal("send did not retain frozen resource and durable started fence")
		}
		return "lost-release-request", errors.New("lost provider response")
	}
	got, err := service.ReleaseRecycledStorageVolume(t.Context(), volume.ID, input)
	if err == nil || got.Status != StorageFinalReleaseStatusPending || !got.DispatchAttempted || got.AbsenceConfirmed || provider.calls.Load() != 1 {
		t.Fatalf("first=%+v err=%v calls=%d", got, err, provider.calls.Load())
	}
	child := storageFinalReleaseChild(t, store, volume.ID)
	if child.Status != string(StorageFinalReleaseStatusFailed) || child.ProviderRequestID != "lost-release-request" {
		t.Fatalf("missing uncertain outcome: %+v", child)
	}
	store = reopen(store)
	service = NewServiceWithOperationStore(provider, store)
	pending, err := service.ReleaseRecycledStorageVolume(t.Context(), volume.ID, input)
	if err != nil || pending.Status != StorageFinalReleaseStatusPending || !pending.DispatchAttempted || pending.AbsenceConfirmed || pending.OperationID != got.OperationID || provider.calls.Load() != 1 {
		t.Fatalf("reopened=%+v err=%v calls=%d", pending, err, provider.calls.Load())
	}
	provider.absent.Store(true)
	confirmed, err := service.ReleaseRecycledStorageVolume(t.Context(), volume.ID, input)
	if err != nil || confirmed.Status != StorageFinalReleaseStatusSucceeded || !confirmed.AbsenceConfirmed || provider.calls.Load() != 1 {
		t.Fatalf("confirmed=%+v err=%v calls=%d", confirmed, err, provider.calls.Load())
	}
	if child := storageFinalReleaseChild(t, store, volume.ID); child.ErrorCode != "" || child.Status != string(StorageFinalReleaseStatusSucceeded) {
		t.Fatalf("uncleared child failure: %+v", child)
	}
	read, err := service.ReadStorageDestroyStatus(t.Context(), volume.ID)
	if err != nil || read.BindingPresent == nil || *read.BindingPresent || !storageDestroyReadbackConfirmsAbsence(read) {
		t.Fatalf("destroy status=%+v err=%v", read, err)
	}
	if !reflect.DeepEqual(before, storageFinalReleaseOriginalJournal(t, store)) {
		t.Fatal("final release or GET rewrote original journal")
	}
	// Ordinary Destroy must still converge the SAME original operation.
	result, err := service.DestroyStorageVolume(t.Context(), volume.ID)
	latest, found, latestErr := store.LatestResourceOperation(t.Context(), "storage_volume", volume.ID)
	if err != nil || !storageDestroyReadbackConfirmsAbsence(result) || latestErr != nil || !found || latest.OperationID != input.DestroyOperationID || latest.Status != string(StorageFinalReleaseStatusSucceeded) || provider.calls.Load() != 1 {
		t.Fatalf("original convergence=%+v err=%v latest=%+v latestErr=%v", result, err, latest, latestErr)
	}
}

func TestPostgresStorageFinalReleaseFenceSurvivesReopen(t *testing.T) {
	databaseURL := fabricTestDatabaseURL(t)
	reopen := func(store OperationStore) OperationStore {
		if store != nil {
			if err := store.(*PostgresOperationStore).client.Close(); err != nil {
				t.Fatal(err)
			}
		}
		next, err := newTestPostgresOperationStore(databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = next.client.Close() })
		return next
	}
	testStorageFinalReleaseRestart(t, reopen(nil), reopen)
}

func TestStorageFinalReleaseRejectsAuthorizationAndFrozenIdentity(t *testing.T) {
	for _, field := range []string{"confirmation", "account", "workspace", "destroy", "resource", "provider", "tags", "caller-tag", "payload-size-type", "payload-unknown-field", "payload-id", "journal-hash", "journal-provider", "journal-account", "journal-request", "phase", "deadline"} {
		t.Run(field, func(t *testing.T) {
			store := NewMemoryOperationStore()
			volume, input := storageFinalReleaseFixture(t, store)
			provider := newStorageFinalReleaseTestProvider(t, volume)
			id := volume.ID
			switch field {
			case "confirmation":
				input.Confirmation = ""
			case "account":
				input.AccountID = "foreign"
			case "workspace":
				input.WorkspaceID = "foreign"
			case "destroy":
				input.DestroyOperationID = "foreign"
			case "resource":
				id = "foreign"
			default:
				original := &store.operation[len(store.operation)-1]
				resource := original.RedactedProviderPayload["resource"].(map[string]any)
				switch field {
				case "provider":
					resource["provider"], original.Provider = "local-docker", "local-docker"
				case "tags":
					delete(resource, "costTags")
				case "caller-tag":
					resource["costTags"].(map[string]any)["opl_operation_id"] = "foreign"
				case "payload-size-type":
					resource["sizeGb"] = "10"
				case "payload-unknown-field":
					resource["unexpectedField"] = true
				case "payload-id":
					resource["id"] = "foreign"
				case "journal-hash":
					original.RequestHash = "foreign"
				case "journal-provider":
					original.Provider = "foreign"
				case "journal-account":
					original.AccountID = "foreign"
				case "journal-request":
					original.ProviderRequestID = "foreign"
				case "phase":
					delete(resource["providerData"].(map[string]any), "storageDestroyPhase")
				case "deadline":
					resource["deadline"] = "invalid"
				}
			}
			before := storageFinalReleaseOriginalJournal(t, store)
			_, err := NewServiceWithOperationStore(provider, store).ReleaseRecycledStorageVolume(t.Context(), id, input)
			if err == nil || provider.calls.Load() != 0 || len(store.operation) != len(before) || !reflect.DeepEqual(before, storageFinalReleaseOriginalJournal(t, store)) {
				t.Fatalf("err=%v calls=%d entries=%d", err, provider.calls.Load(), len(store.operation))
			}
		})
	}
}

func TestStorageFinalReleaseStrictPreconditionsDoNotConsumeFence(t *testing.T) {
	for _, invalid := range []string{"missing-binding", "present-binding", "unattached", "attached", "missing-cbs", "conflicting-cbs", "missing-describe", "wrong-phase", "wrong-mutation-count", "foreign-resource", "foreign-region", "read-error", "partial-absence"} {
		t.Run(invalid, func(t *testing.T) {
			store := NewMemoryOperationStore()
			volume, input := storageFinalReleaseFixture(t, store)
			provider := newStorageFinalReleaseTestProvider(t, volume)
			read := provider.provision
			provider.provision = func(ctx context.Context, request provisionerRequest) (provisionerResponse, error) {
				response, err := read(ctx, request)
				switch invalid {
				case "unattached":
					response.CBSStatus, response.ProviderData["cbsStatus"] = "UNATTACHED", "UNATTACHED"
				case "attached":
					response.CBSStatus, response.ProviderData["cbsStatus"] = "ATTACHED", "ATTACHED"
				case "missing-cbs":
					response.CBSStatus = ""
				case "conflicting-cbs":
					response.ProviderData["cbsStatus"] = "UNATTACHED"
				case "missing-describe":
					delete(response.ProviderData, "describeCbsRequestId")
				case "wrong-phase":
					response.ProviderData["storageDestroyPhase"] = storageDestroyPhaseDispatchAuthorized
				case "wrong-mutation-count":
					response.ProviderData["storageDestroyMutationCount"] = "0"
				case "foreign-resource":
					response.StorageVolumeID = "disk-foreign"
				case "foreign-region":
					response.ProviderData["region"] = "foreign"
				case "read-error":
					err = errors.New("read unavailable")
				case "partial-absence":
					response.CBSStatus, response.Status = "NOT_FOUND", "external_deleted"
				}
				return response, err
			}
			service := NewServiceWithOperationStore(provider, store)
			if invalid == "missing-binding" || invalid == "present-binding" {
				service.optionalProviders.storageVolumeStatus = storageFinalReleaseStatusReaderFunc(func(ctx context.Context, request StorageVolume) (StorageVolume, error) {
					observed, err := provider.ReadStorageVolumeStatus(ctx, request)
					if invalid == "missing-binding" {
						observed.BindingPresent = nil
					} else {
						present := true
						observed.BindingPresent = &present
					}
					return observed, err
				})
			}
			before := storageFinalReleaseOriginalJournal(t, store)
			if _, err := service.ReleaseRecycledStorageVolume(t.Context(), volume.ID, input); err == nil || provider.calls.Load() != 0 || len(store.operation) != len(before) {
				t.Fatalf("err=%v calls=%d entries=%d", err, provider.calls.Load(), len(store.operation))
			}
			// Repair only the observation; authorization must remain unused.
			provider.provision = read
			service.optionalProviders.storageVolumeStatus = provider
			if result, err := service.ReleaseRecycledStorageVolume(t.Context(), volume.ID, input); err == nil || !result.DispatchAttempted || provider.calls.Load() != 1 {
				t.Fatalf("repaired=%+v err=%v calls=%d", result, err, provider.calls.Load())
			}
			if !reflect.DeepEqual(before, storageFinalReleaseOriginalJournal(t, store)) {
				t.Fatal("precondition rejection rewrote original journal")
			}
		})
	}
}

type storageFinalReleaseStatusReaderFunc func(context.Context, StorageVolume) (StorageVolume, error)

func (read storageFinalReleaseStatusReaderFunc) ReadStorageVolumeStatus(ctx context.Context, volume StorageVolume) (StorageVolume, error) {
	return read(ctx, volume)
}

func TestStorageFinalReleaseAlreadyAbsentUsesZeroMutations(t *testing.T) {
	store := NewMemoryOperationStore()
	volume, input := storageFinalReleaseFixture(t, store)
	provider := newStorageFinalReleaseTestProvider(t, volume)
	provider.absent.Store(true)
	before := storageFinalReleaseOriginalJournal(t, store)
	for range 2 {
		result, err := NewServiceWithOperationStore(provider, store).ReleaseRecycledStorageVolume(t.Context(), volume.ID, input)
		if err != nil || result.Status != StorageFinalReleaseStatusSucceeded || result.DispatchAttempted || !result.AbsenceConfirmed || provider.calls.Load() != 0 {
			t.Fatalf("result=%+v err=%v calls=%d", result, err, provider.calls.Load())
		}
	}
	if !reflect.DeepEqual(before, storageFinalReleaseOriginalJournal(t, store)) {
		t.Fatal("absence read rewrote original journal")
	}
}

func TestStorageFinalReleaseConcurrentServicesSendOnce(t *testing.T) {
	store := NewMemoryOperationStore()
	testStorageFinalReleaseConcurrentServices(t, store, store)
}

func TestPostgresStorageFinalReleaseConcurrentServicesSendOnce(t *testing.T) {
	databaseURL := fabricTestDatabaseURL(t)
	stores := make([]OperationStore, 2)
	for i := range stores {
		store, err := newTestPostgresOperationStore(databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.client.Close() })
		stores[i] = store
	}
	testStorageFinalReleaseConcurrentServices(t, stores[0], stores[1])
}

func testStorageFinalReleaseConcurrentServices(t *testing.T, store, secondStore OperationStore) {
	t.Helper()
	volume, input := storageFinalReleaseFixture(t, store)
	provider := newStorageFinalReleaseTestProvider(t, volume)
	services := []*Service{NewServiceWithOperationStore(provider, store), NewServiceWithOperationStore(provider, secondStore)}
	var group sync.WaitGroup
	for i := range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := services[i%len(services)].ReleaseRecycledStorageVolume(t.Context(), volume.ID, input)
			if !result.DispatchAttempted || result.Status != StorageFinalReleaseStatusPending || (err != nil && !strings.Contains(err.Error(), "lost provider response")) {
				t.Errorf("result=%+v err=%v", result, err)
			}
		}()
	}
	group.Wait()
	if provider.calls.Load() != 1 {
		t.Fatalf("calls=%d", provider.calls.Load())
	}
}

func TestStorageFinalReleaseStartedFenceAndHashConflict(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			store := NewMemoryOperationStore()
			volume, input := storageFinalReleaseFixture(t, store)
			provider := newStorageFinalReleaseTestProvider(t, volume)
			operation := newOperation(storageFinalReleaseAction, "storage_final_release", volume.ID, volume.AccountID, volume.WorkspaceID, volume.ID, hashInput(input), time.Now().UTC())
			operation.CallerService, operation.Provider = "operator", volume.Provider
			operation.RedactedProviderPayload = map[string]any{"originalDestroyOperationId": input.DestroyOperationID, "dispatchAttempted": true}
			if conflict {
				operation.RequestHash = "foreign"
			}
			if err := NewServiceWithOperationStore(provider, store).recordOperation(t.Context(), operation, string(StorageFinalReleaseStatusStarted), nil, nil); err != nil {
				t.Fatal(err)
			}
			result, err := NewServiceWithOperationStore(provider, store).ReleaseRecycledStorageVolume(t.Context(), volume.ID, input)
			if (err != nil) != conflict || provider.calls.Load() != 0 || !conflict && (!result.DispatchAttempted || result.Status != StorageFinalReleaseStatusPending) {
				t.Fatalf("result=%+v err=%v calls=%d", result, err, provider.calls.Load())
			}
		})
	}
}

type storageFinalReleaseWriteStore struct {
	OperationStore
	writes atomic.Int32
	append func(context.Context, FabricOperation) error
	claim  func(context.Context, FabricOperation) (FabricOperation, bool, error)
}

func (store *storageFinalReleaseWriteStore) Append(ctx context.Context, operation FabricOperation) error {
	store.writes.Add(1)
	if err := ctx.Err(); err != nil {
		return err
	}
	if store.append != nil {
		return store.append(ctx, operation)
	}
	return store.OperationStore.Append(ctx, operation)
}

func (store *storageFinalReleaseWriteStore) ClaimRuntime(ctx context.Context, operation FabricOperation) (FabricOperation, bool, error) {
	store.writes.Add(1)
	if err := ctx.Err(); err != nil {
		return FabricOperation{}, false, err
	}
	if store.claim != nil {
		return store.claim(ctx, operation)
	}
	return store.OperationStore.ClaimRuntime(ctx, operation)
}

func TestStorageFinalReleaseCancellationPersistsOutcomeAndFence(t *testing.T) {
	for _, absent := range []bool{false, true} {
		t.Run(fmt.Sprint(absent), func(t *testing.T) {
			memory := NewMemoryOperationStore()
			volume, input := storageFinalReleaseFixture(t, memory)
			store := &storageFinalReleaseWriteStore{OperationStore: memory}
			provider := newStorageFinalReleaseTestProvider(t, volume)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			provider.release = func(context.Context, StorageVolume) (string, error) {
				cancel()
				provider.absent.Store(absent)
				return "cancelled-release", context.Canceled
			}
			if absent {
				// A completed observation may race caller cancellation; its proven
				// success still needs a non-cancelled outcome write.
				read := provider.provision
				provider.provision = func(ctx context.Context, request provisionerRequest) (provisionerResponse, error) {
					return read(context.WithoutCancel(ctx), request)
				}
			}
			store.append = func(ctx context.Context, operation FabricOperation) error {
				if operation.Status != string(StorageFinalReleaseStatusStarted) {
					deadline, bounded := ctx.Deadline()
					if ctx.Err() != nil || !bounded || time.Until(deadline) > 5*time.Second {
						t.Fatal("outcome write did not use bounded WithoutCancel context")
					}
				}
				return memory.Append(ctx, operation)
			}
			result, err := NewServiceWithOperationStore(provider, store).ReleaseRecycledStorageVolume(ctx, volume.ID, input)
			if (err == nil) != absent || !result.DispatchAttempted || result.AbsenceConfirmed != absent || store.writes.Load() != 2 {
				t.Fatalf("result=%+v err=%v writes=%d", result, err, store.writes.Load())
			}
			child := storageFinalReleaseChild(t, store, volume.ID)
			want := StorageFinalReleaseStatusFailed
			if absent {
				want = StorageFinalReleaseStatusSucceeded
			}
			if child.Status != string(want) || child.ProviderRequestID != "cancelled-release" {
				t.Fatalf("outcome=%+v", child)
			}
			provider.absent.Store(false)
			_, err = NewServiceWithOperationStore(provider, store).ReleaseRecycledStorageVolume(t.Context(), volume.ID, input)
			if err != nil || provider.calls.Load() != 1 {
				t.Fatalf("restart err=%v calls=%d", err, provider.calls.Load())
			}
		})
	}
}

func TestStorageFinalReleaseFenceWriteFailureNeverSends(t *testing.T) {
	memory := NewMemoryOperationStore()
	volume, input := storageFinalReleaseFixture(t, memory)
	store := &storageFinalReleaseWriteStore{OperationStore: memory, claim: func(context.Context, FabricOperation) (FabricOperation, bool, error) {
		return FabricOperation{}, false, errors.New("claim refused")
	}}
	provider := newStorageFinalReleaseTestProvider(t, volume)
	result, err := NewServiceWithOperationStore(provider, store).ReleaseRecycledStorageVolume(t.Context(), volume.ID, input)
	if err == nil || result.DispatchAttempted || provider.calls.Load() != 0 || len(memory.operation) != 2 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, provider.calls.Load())
	}
}

func TestReadStorageDestroyStatusUsesFrozenPhaseAndNeverWrites(t *testing.T) {
	for _, fenced := range []bool{false, true} {
		t.Run(fmt.Sprint(fenced), func(t *testing.T) {
			memory := NewMemoryOperationStore()
			volume, input := storageFinalReleaseFixture(t, memory)
			store := &storageFinalReleaseWriteStore{OperationStore: memory}
			provider := newStorageFinalReleaseTestProvider(t, volume)
			service := NewServiceWithOperationStore(provider, store)
			if fenced {
				_, _ = service.ReleaseRecycledStorageVolume(t.Context(), volume.ID, input)
			}
			store.writes.Store(0)
			cached := cloneStorageVolume(volume)
			cached.ProviderData = map[string]string{"cached": "untouched"}
			cached.CBSStatus = "ATTACHED"
			service.volumes[volume.ID] = cached
			before, _ := json.Marshal(memory.operation)
			service.optionalProviders.storageVolumeStatus = storageFinalReleaseStatusReaderFunc(func(ctx context.Context, request StorageVolume) (StorageVolume, error) {
				owner, ok := ctx.Value(workspaceStorageDeleteOwnerContextKey{}).(StorageVolume)
				if !ok || !reflect.DeepEqual(volume, owner) || !sameStorageDestroyStableIdentity(volume, request) || request.ProviderData["storageDestroyPhase"] != volume.ProviderData["storageDestroyPhase"] || request.CBSStatus != "" || request.BindingPresent != nil {
					t.Fatal("GET did not pass frozen original phase and owner")
				}
				return provider.ReadStorageVolumeStatus(ctx, request)
			})
			observed, err := service.ReadStorageDestroyStatus(t.Context(), volume.ID)
			if err != nil || observed.BindingPresent == nil || *observed.BindingPresent || observed.CBSStatus != "TORECYCLE" || observed.ObservedAt == "" || observed.ReadbackID == "" {
				t.Fatalf("observed=%+v err=%v", observed, err)
			}
			after, _ := json.Marshal(memory.operation)
			if string(before) != string(after) || store.writes.Load() != 0 || !reflect.DeepEqual(service.volumes[volume.ID], cached) {
				t.Fatal("GET changed journal or cached volume")
			}
		})
	}
}

func TestReadStorageDestroyStatusRejectsUnavailableOrConflictingRead(t *testing.T) {
	for _, failure := range []string{"missing-original", "wrong-latest-action", "malformed-payload", "unavailable-reader", "conflicting-read", "provider-error"} {
		t.Run(failure, func(t *testing.T) {
			store := NewMemoryOperationStore()
			volume, _ := storageFinalReleaseFixture(t, store)
			provider := newStorageFinalReleaseTestProvider(t, volume)
			if failure == "missing-original" {
				store.operation = nil
			} else if failure == "wrong-latest-action" {
				store.operation[len(store.operation)-1].Action = "create_storage_volume"
			} else if failure == "malformed-payload" {
				store.operation[len(store.operation)-1].RedactedProviderPayload["resource"].(map[string]any)["sizeGb"] = "10"
			}
			service := NewServiceWithOperationStore(provider, store)
			if failure == "unavailable-reader" {
				service.optionalProviders.storageVolumeStatus = nil
			} else if failure == "conflicting-read" || failure == "provider-error" {
				service.optionalProviders.storageVolumeStatus = storageFinalReleaseStatusReaderFunc(func(_ context.Context, request StorageVolume) (StorageVolume, error) {
					if failure == "provider-error" {
						return request, errors.New("read unavailable")
					}
					request.CostTags["opl_workspace_id"] = "foreign"
					return request, nil
				})
			}
			before, _ := json.Marshal(store.operation)
			result, err := service.ReadStorageDestroyStatus(t.Context(), volume.ID)
			after, _ := json.Marshal(store.operation)
			if err == nil || result.ObservedAt != "" || result.ReadbackID != "" || string(before) != string(after) || provider.calls.Load() != 0 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestStorageFinalReleaseDurableFenceSurvivesWriteResponseLoss(t *testing.T) {
	for _, failure := range []string{"fence-reply-lost", "outcome-write-failed"} {
		t.Run(failure, func(t *testing.T) {
			memory := NewMemoryOperationStore()
			volume, input := storageFinalReleaseFixture(t, memory)
			store := &storageFinalReleaseWriteStore{OperationStore: memory}
			store.claim = func(ctx context.Context, operation FabricOperation) (FabricOperation, bool, error) {
				persisted, claimed, err := memory.ClaimRuntime(ctx, operation)
				if err == nil && failure == "fence-reply-lost" {
					return FabricOperation{}, false, errors.New("fence reply lost after commit")
				}
				return persisted, claimed, err
			}
			store.append = func(context.Context, FabricOperation) error {
				return errors.New("outcome write unavailable")
			}
			provider := newStorageFinalReleaseTestProvider(t, volume)
			_, err := NewServiceWithOperationStore(provider, store).ReleaseRecycledStorageVolume(t.Context(), volume.ID, input)
			wantCalls := int32(1)
			if failure == "fence-reply-lost" {
				wantCalls = 0
			}
			if err == nil || provider.calls.Load() != wantCalls || storageFinalReleaseChild(t, memory, volume.ID).Status != string(StorageFinalReleaseStatusStarted) {
				t.Fatalf("err=%v calls=%d", err, provider.calls.Load())
			}
			result, err := NewServiceWithOperationStore(provider, memory).ReleaseRecycledStorageVolume(t.Context(), volume.ID, input)
			if err != nil || !result.DispatchAttempted || provider.calls.Load() != wantCalls {
				t.Fatalf("restarted=%+v err=%v calls=%d", result, err, provider.calls.Load())
			}
		})
	}
}

func TestStorageFinalReleaseResponseJSONContract(t *testing.T) {
	var input StorageFinalReleaseInput
	if err := json.Unmarshal([]byte(`{"accountId":"account","workspaceId":"workspace","destroyOperationId":"destroy","confirmation":"RELEASE_ORIGINAL_RECYCLED_CBS"}`), &input); err != nil {
		t.Fatal(err)
	}
	if input.AccountID != "account" || input.WorkspaceID != "workspace" || input.DestroyOperationID != "destroy" || input.Confirmation != StorageFinalReleaseConfirmation {
		t.Fatalf("HTTP input lost binding: %+v", input)
	}
	out := StorageFinalReleaseResult{OperationID: "release", Status: StorageFinalReleaseStatusSucceeded, DispatchAttempted: true, AbsenceConfirmed: true}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, map[string]any{"operationId": "release", "status": string(StorageFinalReleaseStatusSucceeded), "dispatchAttempted": true, "absenceConfirmed": true}) {
		t.Fatalf("HTTP result=%s", data)
	}
}

type storageFinalReleaseScopedStore struct {
	OperationStore
	denyList bool
}

func (store *storageFinalReleaseScopedStore) List(ctx context.Context) ([]FabricOperation, error) {
	if store.denyList {
		return nil, errors.New("global operation scan forbidden")
	}
	return store.OperationStore.List(ctx)
}

func TestStorageFinalReleaseUsesOnlyScopedJournalQueries(t *testing.T) {
	store := &storageFinalReleaseScopedStore{OperationStore: NewMemoryOperationStore()}
	volume, input := storageFinalReleaseFixture(t, store)
	provider := newStorageFinalReleaseTestProvider(t, volume)
	service := NewServiceWithOperationStore(provider, store)
	store.denyList = true
	result, err := service.ReleaseRecycledStorageVolume(t.Context(), volume.ID, input)
	if err == nil || !strings.Contains(err.Error(), "lost provider response") || !result.DispatchAttempted || provider.calls.Load() != 1 {
		t.Fatalf("first=%+v err=%v calls=%d", result, err, provider.calls.Load())
	}
	result, err = service.ReleaseRecycledStorageVolume(t.Context(), volume.ID, input)
	if err != nil || !result.DispatchAttempted || provider.calls.Load() != 1 {
		t.Fatalf("fenced=%+v err=%v calls=%d", result, err, provider.calls.Load())
	}
	if _, err := service.ReadStorageDestroyStatus(t.Context(), volume.ID); err != nil {
		t.Fatalf("GET used global history: %v", err)
	}
}

func TestStorageFinalReleaseSparseReadNeverInheritsJournalProof(t *testing.T) {
	for _, missing := range []string{"cbs", "disk", "describe", "binding", "all"} {
		t.Run(missing, func(t *testing.T) {
			store := NewMemoryOperationStore()
			volume, input := storageFinalReleaseFixture(t, store)
			provider := newStorageFinalReleaseTestProvider(t, volume)
			read := provider.provision
			provider.provision = func(ctx context.Context, request provisionerRequest) (provisionerResponse, error) {
				response, err := read(ctx, request)
				switch missing {
				case "cbs":
					delete(response.ProviderData, "cbsStatus")
				case "disk":
					delete(response.ProviderData, "storageVolumeId")
				case "describe":
					delete(response.ProviderData, "describeCbsRequestId")
				}
				return response, err
			}
			service := NewServiceWithOperationStore(provider, store)
			if missing == "binding" || missing == "all" {
				service.optionalProviders.storageVolumeStatus = storageFinalReleaseStatusReaderFunc(func(ctx context.Context, request StorageVolume) (StorageVolume, error) {
					if missing == "all" {
						return request, nil
					}
					observed, err := provider.ReadStorageVolumeStatus(ctx, request)
					observed.BindingPresent = request.BindingPresent
					return observed, err
				})
			}
			before, _ := json.Marshal(store.operation)
			if result, err := service.ReleaseRecycledStorageVolume(t.Context(), volume.ID, input); err == nil || result.DispatchAttempted || provider.calls.Load() != 0 {
				t.Fatalf("sparse=%+v err=%v calls=%d", result, err, provider.calls.Load())
			}
			after, _ := json.Marshal(store.operation)
			if string(before) != string(after) {
				t.Fatal("sparse observation consumed fence or rewrote journal")
			}
		})
	}
}

func TestStorageFinalReleaseRejectsUnknownPersistedFields(t *testing.T) {
	testStorageFinalReleaseUnknownPersistedFields(t, func(*testing.T) OperationStore { return NewMemoryOperationStore() })
}

func TestPostgresStorageFinalReleaseRejectsUnknownPersistedFields(t *testing.T) {
	testStorageFinalReleaseUnknownPersistedFields(t, func(t *testing.T) OperationStore {
		store, err := newTestPostgresOperationStore(fabricTestDatabaseURL(t))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.client.Close() })
		return store
	})
}

func testStorageFinalReleaseUnknownPersistedFields(t *testing.T, newStore func(*testing.T) OperationStore) {
	t.Helper()
	for _, boundary := range []string{"original-resource", "child-fence"} {
		t.Run(boundary, func(t *testing.T) {
			store := newStore(t)
			volume, input := storageFinalReleaseFixture(t, store)
			provider := newStorageFinalReleaseTestProvider(t, volume)
			var operation FabricOperation
			if boundary == "original-resource" {
				var err error
				operation, _, err = store.LatestResourceOperation(t.Context(), "storage_volume", volume.ID)
				if err != nil {
					t.Fatal(err)
				}
				operation = storageFinalReleaseJSONOperation(t, operation)
				operation.RedactedProviderPayload["resource"].(map[string]any)["unexpectedField"] = true
			} else {
				operation = newOperation(storageFinalReleaseAction, "storage_final_release", volume.ID, volume.AccountID, volume.WorkspaceID, volume.ID, hashInput(input), time.Now().UTC())
				operation.CallerService, operation.Provider = "operator", volume.Provider
				operation.RedactedProviderPayload = map[string]any{"originalDestroyOperationId": input.DestroyOperationID, "dispatchAttempted": true, "unexpectedField": true}
			}
			operation.ID, operation.Status, operation.CreatedAt = "unknown-field-record", string(StorageFinalReleaseStatusStarted), time.Now().UTC()
			if err := store.Append(t.Context(), operation); err != nil {
				t.Fatal(err)
			}
			before, err := store.List(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			service := NewServiceWithOperationStore(provider, store)
			if result, err := service.ReleaseRecycledStorageVolume(t.Context(), volume.ID, input); err == nil || provider.calls.Load() != 0 || result.DispatchAttempted {
				t.Fatalf("unknown fields accepted: result=%+v err=%v calls=%d", result, err, provider.calls.Load())
			}
			if boundary == "original-resource" {
				if _, err := service.ReadStorageDestroyStatus(t.Context(), volume.ID); err == nil {
					t.Fatal("GET accepted unknown resource fields")
				}
			}
			after, err := store.List(t.Context())
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("rejection changed persistent journal: %v", err)
			}
		})
	}
}

func TestPostgresStorageFinalReleaseLostAdvisorySessionSendsOnce(t *testing.T) {
	databaseURL := fabricTestDatabaseURL(t)
	applicationName := "storage-final-release-lock-" + stableID(databaseURL)[:12]
	lockDatabaseURL := databaseURL + " application_name=" + applicationName
	if parsed, err := url.Parse(databaseURL); err == nil && parsed.Scheme != "" {
		query := parsed.Query()
		query.Set("application_name", applicationName)
		parsed.RawQuery = query.Encode()
		lockDatabaseURL = parsed.String()
	}
	first, err := newTestPostgresOperationStore(lockDatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.client.Close() })
	second, err := newTestPostgresOperationStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.client.Close() })
	frozen, input := storageFinalReleaseFixture(t, first)
	before := storageFinalReleaseOriginalJournal(t, first)
	providerA := newStorageFinalReleaseTestProvider(t, frozen)
	providerB := newStorageFinalReleaseTestProvider(t, frozen)
	serviceA, serviceB := NewServiceWithOperationStore(providerA, first), NewServiceWithOperationStore(providerB, second)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	entered, resume := make(chan struct{}), make(chan struct{})
	var resumeOnce sync.Once
	defer resumeOnce.Do(func() { close(resume) })
	read, paused := providerA.provision, false
	providerA.provision = func(ctx context.Context, request provisionerRequest) (provisionerResponse, error) {
		response, err := read(ctx, request)
		if !paused {
			paused = true
			close(entered)
			select {
			case <-resume:
			case <-ctx.Done():
				return response, ctx.Err()
			}
		}
		return response, err
	}
	type outcome struct {
		result StorageFinalReleaseResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := serviceA.ReleaseRecycledStorageVolume(ctx, frozen.ID, input)
		done <- outcome{result, err}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("first service did not reach its fresh read")
	}
	var killed bool
	err = first.db.QueryRowContext(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name=$1 AND pid IN (SELECT pid FROM pg_locks WHERE locktype='advisory')`, applicationName).Scan(&killed)
	if err != nil || !killed {
		t.Fatalf("terminate own test advisory session: killed=%v err=%v", killed, err)
	}
	resultB, errB := serviceB.ReleaseRecycledStorageVolume(ctx, frozen.ID, input)
	if errB == nil || !resultB.DispatchAttempted || providerB.calls.Load() != 1 {
		t.Fatalf("second service result=%+v err=%v calls=%d", resultB, errB, providerB.calls.Load())
	}
	resumeOnce.Do(func() { close(resume) })
	var resultA outcome
	select {
	case resultA = <-done:
	case <-ctx.Done():
		t.Fatal("first service did not finish after lock loss")
	}
	operations, err := first.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	starts := 0
	for _, operation := range operations {
		if operation.ResourceKind == "storage_final_release" && operation.Status == string(StorageFinalReleaseStatusStarted) {
			starts++
		}
	}
	if providerA.calls.Load() != 0 || providerB.calls.Load() != 1 || starts != 1 {
		t.Fatalf("lost-lock dispatches: first=%d second=%d started=%d firstErr=%v", providerA.calls.Load(), providerB.calls.Load(), starts, resultA.err)
	}
	if resultA.err != nil || !resultA.result.DispatchAttempted || resultA.result.Status != StorageFinalReleaseStatusPending || resultA.result.OperationID != resultB.OperationID {
		t.Fatalf("losing claim must be read-only: result=%+v err=%v", resultA.result, resultA.err)
	}
	if !reflect.DeepEqual(before, storageFinalReleaseOriginalJournal(t, first)) {
		t.Fatal("lock loss rewrote original destroy journal")
	}
}

func TestStorageFinalReleaseLosingClaimOnlyReads(t *testing.T) {
	for _, absent := range []bool{false, true} {
		t.Run(fmt.Sprint(absent), func(t *testing.T) {
			memory := NewMemoryOperationStore()
			frozen, input := storageFinalReleaseFixture(t, memory)
			store := &storageFinalReleaseWriteStore{OperationStore: memory}
			provider := newStorageFinalReleaseTestProvider(t, frozen)
			store.claim = func(ctx context.Context, operation FabricOperation) (FabricOperation, bool, error) {
				if operation.ID != stableID("fop-storage-final-release-dispatch", frozen.ID) || operation.Status != string(StorageFinalReleaseStatusStarted) || operation.CreatedAt.IsZero() {
					t.Fatalf("dispatch claim has no stable identity: %+v", operation)
				}
				// Another invocation wins after this invocation's initial read.
				persisted, _, err := memory.ClaimRuntime(ctx, operation)
				provider.absent.Store(absent)
				return persisted, false, err
			}
			result, err := NewServiceWithOperationStore(provider, store).ReleaseRecycledStorageVolume(t.Context(), frozen.ID, input)
			if err != nil || !result.DispatchAttempted || result.AbsenceConfirmed != absent || provider.calls.Load() != 0 || provider.reads.Load() != 2 || store.writes.Load() != 1 {
				t.Fatalf("loser result=%+v err=%v sends=%d reads=%d writes=%d", result, err, provider.calls.Load(), provider.reads.Load(), store.writes.Load())
			}
			want := StorageFinalReleaseStatusPending
			if absent {
				want = StorageFinalReleaseStatusSucceeded
			}
			if result.Status != want || storageFinalReleaseChild(t, memory, frozen.ID).Status != string(StorageFinalReleaseStatusStarted) {
				t.Fatalf("loser appended an outcome: %+v", result)
			}
		})
	}
}

func TestStorageFinalReleaseLosingClaimRejectsConflictingRecord(t *testing.T) {
	for _, field := range []string{"action", "resource", "kind", "account", "workspace", "operation", "idempotency", "hash", "provider", "caller", "status", "destroy-payload", "missing-attempt", "unknown-payload"} {
		t.Run(field, func(t *testing.T) {
			memory := NewMemoryOperationStore()
			frozen, input := storageFinalReleaseFixture(t, memory)
			store := &storageFinalReleaseWriteStore{OperationStore: memory}
			store.claim = func(ctx context.Context, operation FabricOperation) (FabricOperation, bool, error) {
				persisted, _, err := memory.ClaimRuntime(ctx, operation)
				persisted = storageFinalReleaseJSONOperation(t, persisted)
				switch field {
				case "action":
					persisted.Action = "foreign"
				case "resource":
					persisted.ResourceID = "foreign"
				case "kind":
					persisted.ResourceKind = "foreign"
				case "account":
					persisted.AccountID = "foreign"
				case "workspace":
					persisted.WorkspaceID = "foreign"
				case "operation":
					persisted.OperationID = "foreign"
				case "idempotency":
					persisted.IdempotencyKey = "foreign"
				case "hash":
					persisted.RequestHash = "foreign"
				case "provider":
					persisted.Provider = "foreign"
				case "caller":
					persisted.CallerService = "foreign"
				case "status":
					persisted.Status = "unknown"
				case "destroy-payload":
					persisted.RedactedProviderPayload["originalDestroyOperationId"] = "foreign"
				case "missing-attempt":
					delete(persisted.RedactedProviderPayload, "dispatchAttempted")
				case "unknown-payload":
					persisted.RedactedProviderPayload["unexpectedField"] = true
				}
				return persisted, false, err
			}
			provider := newStorageFinalReleaseTestProvider(t, frozen)
			result, err := NewServiceWithOperationStore(provider, store).ReleaseRecycledStorageVolume(t.Context(), frozen.ID, input)
			if err == nil || result.DispatchAttempted || provider.calls.Load() != 0 || store.writes.Load() != 1 || len(memory.operation) != 3 {
				t.Fatalf("conflicting claim accepted: result=%+v err=%v sends=%d writes=%d", result, err, provider.calls.Load(), store.writes.Load())
			}
		})
	}
}
