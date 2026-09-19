package fabric

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

type workspaceLaunchDeleteProjectionProvider struct {
	testProvider
	detachCalls        atomic.Int32
	storageDeleteCalls atomic.Int32
	computeDeleteCalls atomic.Int32
}

func (*workspaceLaunchDeleteProjectionProvider) Descriptor() ProviderDescriptor {
	descriptor := testProvider{}.Descriptor()
	descriptor.Name = "local-docker"
	descriptor.RequiresMonthlyPricing = false
	return descriptor
}

func (*workspaceLaunchDeleteProjectionProvider) ResolveWorkspacePlan(_ context.Context, input WorkspaceLaunchPlanInput) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"compute": map[string]any{"cpu": 2, "memoryGb": 4},
		"storage": map[string]any{"sizeGb": input.SizeGB},
	})
}

func (*workspaceLaunchDeleteProjectionProvider) ValidateWorkspaceImageReference(value string) bool {
	repository, _, ok := immutableLocalDockerImage(value)
	return ok && repository == "ghcr.io/gaofeng21cn/one-person-lab-app"
}

func (p *workspaceLaunchDeleteProjectionProvider) EnsureWorkspaceLaunchStage(ctx context.Context, request WorkspaceLaunchProviderRequest) (WorkspaceLaunchProviderResult, error) {
	binding := request.Input.Binding
	resources := request.Input.Resources
	state := map[string]any{}
	switch binding.Stage {
	case "ensure_compute_allocation":
		compute := ComputeAllocation{
			ID: workspaceLaunchComputeID(binding), OperationID: binding.FabricOperationID, AccountID: binding.AccountID, WorkspaceID: binding.WorkspaceID,
			PackageID: request.Input.PackageID, Status: "running", Provider: "local-docker", ProviderResourceID: "network/test-compute",
			ProviderRequestID: "test-compute-create", NodePoolID: "local-docker", InstanceType: "test-2c4g", Zone: "local", ChargeType: "LOCAL",
		}
		if err := recordWorkspaceLaunchDeleteProjectionResource(ctx, "test_compute_create", "compute_allocation", compute.ID, compute); err != nil {
			return WorkspaceLaunchProviderResult{}, err
		}
		resources.ComputeAllocationID, resources.ComputeBindingRef = compute.ID, binding.FabricOperationID
		state["compute"] = compute
	case "storage":
		volume := StorageVolume{
			ID: workspaceLaunchStorageID(binding), OperationID: binding.IdempotencyKey, AccountID: binding.AccountID, WorkspaceID: binding.WorkspaceID,
			Status: "ready", Provider: "local-docker", ProviderResourceID: "directory/test-storage", ProviderRequestID: "test-storage-create",
			SizeGB: request.Input.SizeGB, StorageClass: "host-directory", DiskType: "local-directory", Zone: "local",
		}
		if err := recordWorkspaceLaunchDeleteProjectionResource(ctx, "test_storage_create", "storage_volume", volume.ID, volume); err != nil {
			return WorkspaceLaunchProviderResult{}, err
		}
		resources.StorageID, resources.StorageBindingRef = volume.ID, binding.FabricOperationID
		state["storage"] = volume
	case "attachment":
		attachment := StorageAttachment{
			ID: workspaceLaunchAttachmentID(binding), OperationID: binding.IdempotencyKey, WorkspaceID: binding.WorkspaceID,
			ComputeID: resources.ComputeAllocationID, VolumeID: resources.StorageID, Status: "attached", Provider: "local-docker",
			ProviderAttachmentID: "docker/test-compute/test-storage", ProviderRequestID: "test-attachment-create", CreatedAt: time.Now().UTC(),
		}
		resources.AttachmentID, resources.AttachmentBindingRef = attachment.ID, binding.FabricOperationID
		state["attachment"] = attachment
	default:
		return WorkspaceLaunchProviderResult{}, ErrWorkspaceLaunchInputInvalid
	}
	providerState, err := json.Marshal(state)
	return WorkspaceLaunchProviderResult{Resources: resources, ProviderState: providerState}, err
}

func (*workspaceLaunchDeleteProjectionProvider) ReadWorkspaceLaunchStage(_ context.Context, request WorkspaceLaunchProviderRequest) (WorkspaceLaunchProviderResult, error) {
	return WorkspaceLaunchProviderResult{Resources: request.Current.Resources, ProviderState: request.Current.ProviderState}, nil
}

func recordWorkspaceLaunchDeleteProjectionResource(ctx context.Context, action, kind, id string, resource any) error {
	attempt, err := beginProviderMutation(ctx, action, kind, id, id)
	if err != nil {
		return err
	}
	return attempt.complete(ctx, "test-provider-request", resource, nil)
}

func (p *workspaceLaunchDeleteProjectionProvider) DetachStorageAttachment(_ context.Context, attachment StorageAttachment) (StorageAttachment, error) {
	p.detachCalls.Add(1)
	attachment.Status = "detached"
	return attachment, nil
}

func (p *workspaceLaunchDeleteProjectionProvider) DestroyStorageVolume(_ context.Context, volume StorageVolume) (StorageVolume, error) {
	p.storageDeleteCalls.Add(1)
	volume.Status = "destroyed"
	return volume, nil
}

func (p *workspaceLaunchDeleteProjectionProvider) DestroyComputeAllocation(_ context.Context, allocation ComputeAllocation) (ComputeAllocation, error) {
	p.computeDeleteCalls.Add(1)
	allocation.Status = "destroyed"
	return allocation, nil
}

func workspaceLaunchDeleteProjectionFixture(t *testing.T) (*Service, *MemoryOperationStore, *workspaceLaunchDeleteProjectionProvider, WorkspaceLaunchResources) {
	t.Helper()
	ctx := context.Background()
	store := NewMemoryOperationStore()
	provider := &workspaceLaunchDeleteProjectionProvider{}
	service := NewServiceWithOperationStore(provider, store)
	image := "ghcr.io/gaofeng21cn/one-person-lab-app@sha256:" + strings.Repeat("a", 64)
	launchHash := strings.Repeat("b", 64)
	preflight, err := service.PreflightWorkspaceLaunch(ctx, WorkspaceLaunchPreflightInput{
		SchemaVersion: 1, LaunchOperationID: "launch-same-process-delete", AccountID: "acct-delete", WorkspaceID: "ws-delete",
		PackageID: "basic", SizeGB: 10, WorkspaceImageDigest: image, RequestHash: launchHash,
	})
	if err != nil || !preflight.Available {
		t.Fatalf("preflight=%#v err=%v", preflight, err)
	}
	resources := WorkspaceLaunchResources{}
	for _, stage := range []struct{ name, action string }{
		{"ensure_compute_allocation", "ensure_compute_allocation"},
		{"storage", "ensure_storage"},
		{"attachment", "ensure_attachment"},
	} {
		binding := WorkspaceLaunchStageBinding{
			SchemaVersion: 1, LaunchOperationID: "launch-same-process-delete", AccountID: "acct-delete", WorkspaceID: "ws-delete",
			Stage: stage.name, Action: stage.action, FabricOperationID: "launch-same-process-delete:" + stage.name,
			IdempotencyKey: "launch-same-process-delete:" + stage.name,
		}
		input := WorkspaceLaunchStageInput{
			Binding: binding, ProviderProfileRef: "local-docker", ProviderBindingRef: preflight.ProviderBindingRef, SpecDigest: preflight.SpecDigest,
			PackageID: "basic", SizeGB: 10, WorkspaceImageDigest: image, Resources: resources,
		}
		input.Binding.RequestHash = workspaceLaunchStageRequestHash(input, launchHash)
		result, ensureErr := service.EnsureWorkspaceLaunchStage(ctx, input)
		if ensureErr != nil || result.State != "ready" {
			t.Fatalf("stage=%s result=%#v err=%v", stage.name, result, ensureErr)
		}
		resources = result.Resources
	}
	return service, store, provider, resources
}

func TestWorkspaceLaunchResourcesDeleteWithoutFabricRestart(t *testing.T) {
	for _, resourceKind := range []string{"attachment", "storage", "compute"} {
		t.Run(resourceKind, func(t *testing.T) {
			ctx := context.Background()
			service, _, provider, resources := workspaceLaunchDeleteProjectionFixture(t)
			if _, exists := service.computes[resources.ComputeAllocationID]; exists {
				t.Fatal("fixture unexpectedly projected staged compute before cache-miss recovery")
			}

			switch resourceKind {
			case "attachment":
				detached, err := service.DetachStorageAttachment(ctx, resources.AttachmentID)
				if err != nil || detached.Status != "detached" || provider.detachCalls.Load() != 1 {
					t.Fatalf("detach=%#v calls=%d err=%v", detached, provider.detachCalls.Load(), err)
				}
			case "storage":
				destroyed, err := service.DestroyStorageVolume(ctx, resources.StorageID)
				if err != nil || destroyed.Status != "destroyed" || provider.storageDeleteCalls.Load() != 1 {
					t.Fatalf("storage=%#v calls=%d err=%v", destroyed, provider.storageDeleteCalls.Load(), err)
				}
			case "compute":
				destroying, err := service.DestroyComputeAllocation(ctx, resources.ComputeAllocationID)
				if err != nil || destroying.Status != "destroying" {
					t.Fatalf("compute start=%#v err=%v", destroying, err)
				}
				waitForOperation(t, service, "destroy_compute_allocation", "compute_allocation", resources.ComputeAllocationID, "succeeded")
				destroyed, err := service.DestroyComputeAllocation(ctx, resources.ComputeAllocationID)
				if err != nil || destroyed.Status != "destroyed" || provider.computeDeleteCalls.Load() != 1 {
					t.Fatalf("compute=%#v calls=%d err=%v", destroyed, provider.computeDeleteCalls.Load(), err)
				}
			}
		})
	}
}

func TestWorkspaceLaunchDeleteDoesNotRecoverInvalidOrConflictingCanonicalResource(t *testing.T) {
	for _, resourceKind := range []string{"attachment", "storage", "compute"} {
		for _, scenario := range []string{"invalid", "conflict"} {
			t.Run(resourceKind+"/"+scenario, func(t *testing.T) {
				service, store, provider, resources := workspaceLaunchDeleteProjectionFixture(t)
				stage := map[string]string{"attachment": "attachment", "storage": "storage", "compute": "ensure_compute_allocation"}[resourceKind]
				if scenario == "invalid" {
					invalidateWorkspaceLaunchDeleteStage(t, store, stage, resources)
				} else {
					appendConflictingWorkspaceLaunchDeleteStage(t, store, stage, resources)
				}

				var err error
				switch resourceKind {
				case "attachment":
					_, err = service.DetachStorageAttachment(context.Background(), resources.AttachmentID)
				case "storage":
					_, err = service.DestroyStorageVolume(context.Background(), resources.StorageID)
				case "compute":
					_, err = service.DestroyComputeAllocation(context.Background(), resources.ComputeAllocationID)
				}
				expected := map[string]string{"attachment": "storage_attachment_not_found", "storage": "storage_volume_not_found", "compute": "compute_allocation_not_found"}[resourceKind]
				if err == nil || err.Error() != expected {
					t.Fatalf("invalid %s delete err=%v", resourceKind, err)
				}
				if provider.detachCalls.Load() != 0 || provider.storageDeleteCalls.Load() != 0 || provider.computeDeleteCalls.Load() != 0 {
					t.Fatalf("invalid state reached provider detach=%d storage=%d compute=%d", provider.detachCalls.Load(), provider.storageDeleteCalls.Load(), provider.computeDeleteCalls.Load())
				}
			})
		}
	}
}

func invalidateWorkspaceLaunchDeleteStage(t *testing.T, store *MemoryOperationStore, stage string, resources WorkspaceLaunchResources) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	for index := range store.operation {
		binding, ok := decodeLaunchStageBinding(store.operation[index])
		if !ok || binding.Stage != stage {
			continue
		}
		record, ok := decodeWorkspaceLaunchStageRecord(store.operation[index])
		if !ok {
			t.Fatalf("%s stage record missing", stage)
		}
		id := map[string]string{"ensure_compute_allocation": resources.ComputeAllocationID, "storage": resources.StorageID, "attachment": resources.AttachmentID}[stage]
		record.ProviderState = json.RawMessage(`{"` + map[string]string{"ensure_compute_allocation": "compute", "storage": "storage", "attachment": "attachment"}[stage] + `":{"id":"` + id + `"}}`)
		setWorkspaceLaunchStageRecord(&store.operation[index], record)
		return
	}
	t.Fatalf("%s stage operation missing", stage)
}

func appendConflictingWorkspaceLaunchDeleteStage(t *testing.T, store *MemoryOperationStore, stage string, resources WorkspaceLaunchResources) {
	t.Helper()
	binding := WorkspaceLaunchStageBinding{
		SchemaVersion: 1, LaunchOperationID: "launch-conflict-" + stage, AccountID: "acct-delete", WorkspaceID: "ws-delete",
		Stage: stage, Action: map[string]string{"ensure_compute_allocation": "ensure_compute_allocation", "storage": "ensure_storage", "attachment": "ensure_attachment"}[stage],
		FabricOperationID: "launch-conflict:" + stage, IdempotencyKey: "launch-conflict:" + stage, RequestHash: strings.Repeat("c", 64),
	}
	requestResources := WorkspaceLaunchResources{}
	if stage == "storage" || stage == "attachment" {
		requestResources.ComputeAllocationID, requestResources.ComputeBindingRef = resources.ComputeAllocationID, resources.ComputeBindingRef
	}
	if stage == "attachment" {
		requestResources.StorageID, requestResources.StorageBindingRef = resources.StorageID, resources.StorageBindingRef
	}
	input := WorkspaceLaunchStageInput{
		Binding: binding, ProviderProfileRef: "local-docker", ProviderBindingRef: "binding-conflict", SpecDigest: strings.Repeat("d", 64), Resources: requestResources,
	}
	operation, record, err := newWorkspaceLaunchStageOperation(input, "local-docker", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]any{}
	switch stage {
	case "ensure_compute_allocation":
		compute := ComputeAllocation{
			ID: workspaceLaunchComputeID(binding), OperationID: binding.FabricOperationID, AccountID: binding.AccountID, WorkspaceID: binding.WorkspaceID,
			PackageID: "basic", Status: "running", Provider: "local-docker", ProviderResourceID: "network/conflict", ProviderRequestID: "test-conflict-compute",
			PoolID: "test-2c4g", NodePoolID: "local-docker", InstanceType: "test-2c4g", Zone: "local", ChargeType: "LOCAL", CreatedAt: time.Now().UTC(),
		}
		record.Resources.ComputeAllocationID, record.Resources.ComputeBindingRef = compute.ID, binding.FabricOperationID
		state["compute"] = compute
	case "storage":
		volume := StorageVolume{
			ID: workspaceLaunchStorageID(binding), OperationID: binding.IdempotencyKey, AccountID: binding.AccountID, WorkspaceID: binding.WorkspaceID,
			Status: "ready", Provider: "local-docker", ProviderResourceID: "directory/conflict", ProviderRequestID: "test-conflict-storage",
			SizeGB: 10, StorageClass: "host-directory", DiskType: "local-directory", Zone: "local", CreatedAt: time.Now().UTC(),
		}
		record.Resources.StorageID, record.Resources.StorageBindingRef = volume.ID, binding.FabricOperationID
		state["storage"] = volume
	case "attachment":
		attachment := StorageAttachment{
			ID: workspaceLaunchAttachmentID(binding), OperationID: binding.IdempotencyKey, WorkspaceID: binding.WorkspaceID,
			ComputeID: resources.ComputeAllocationID, VolumeID: resources.StorageID, Status: "attached", Provider: "local-docker",
			ProviderAttachmentID: "docker/conflict", ProviderRequestID: "test-conflict-attachment", CreatedAt: time.Now().UTC(),
		}
		record.Resources.AttachmentID, record.Resources.AttachmentBindingRef = attachment.ID, binding.FabricOperationID
		state["attachment"] = attachment
	}
	record.ProviderState, _ = json.Marshal(state)
	operation.Status, operation.FinishedAt = "succeeded", time.Now().UTC()
	setWorkspaceLaunchStageRecord(&operation, record)
	if err := store.Append(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceLaunchDeleteReturnsOperationStoreReplayError(t *testing.T) {
	_, store, provider, resources := workspaceLaunchDeleteProjectionFixture(t)
	service := NewServiceWithOperationStore(provider, failingListOperationStore{OperationStore: store})

	for _, testCase := range []struct {
		name   string
		delete func() error
	}{
		{name: "attachment", delete: func() error {
			_, err := service.DetachStorageAttachment(context.Background(), resources.AttachmentID)
			return err
		}},
		{name: "storage", delete: func() error {
			_, err := service.DestroyStorageVolume(context.Background(), resources.StorageID)
			return err
		}},
		{name: "compute", delete: func() error {
			_, err := service.DestroyComputeAllocation(context.Background(), resources.ComputeAllocationID)
			return err
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.delete(); err == nil || err.Error() != "operation store unavailable" {
				t.Fatalf("delete replay error=%v", err)
			}
		})
	}
	if provider.detachCalls.Load() != 0 || provider.storageDeleteCalls.Load() != 0 || provider.computeDeleteCalls.Load() != 0 {
		t.Fatalf("operation store error reached provider detach=%d storage=%d compute=%d", provider.detachCalls.Load(), provider.storageDeleteCalls.Load(), provider.computeDeleteCalls.Load())
	}
}

type blockingWorkspaceLaunchDeleteListStore struct {
	OperationStore
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingWorkspaceLaunchDeleteListStore) List(ctx context.Context) ([]FabricOperation, error) {
	s.once.Do(func() { close(s.started) })
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.release:
		return s.OperationStore.List(ctx)
	}
}

func TestWorkspaceLaunchDeleteHydrationDoesNotOverwriteConcurrentResourceState(t *testing.T) {
	service, store, _, resources := workspaceLaunchDeleteProjectionFixture(t)
	blockingStore := &blockingWorkspaceLaunchDeleteListStore{OperationStore: store, started: make(chan struct{}), release: make(chan struct{})}
	service.operationHistory = blockingStore
	done := make(chan error, 1)
	go func() { done <- service.hydrateMissingResourceState(context.Background()) }()
	<-blockingStore.started

	concurrentCompute := ComputeAllocation{ID: resources.ComputeAllocationID, Status: "concurrent-compute", ProviderRequestID: "concurrent-compute"}
	concurrentStorage := StorageVolume{ID: resources.StorageID, Status: "concurrent-storage", ProviderRequestID: "concurrent-storage"}
	concurrentAttachment := StorageAttachment{ID: resources.AttachmentID, Status: "concurrent-attachment", ProviderRequestID: "concurrent-attachment"}
	service.mu.Lock()
	service.computes[resources.ComputeAllocationID] = concurrentCompute
	service.volumes[resources.StorageID] = concurrentStorage
	service.attachments[resources.AttachmentID] = concurrentAttachment
	service.mu.Unlock()
	close(blockingStore.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	service.mu.Lock()
	defer service.mu.Unlock()
	if service.computes[resources.ComputeAllocationID].ProviderRequestID != concurrentCompute.ProviderRequestID ||
		service.volumes[resources.StorageID].ProviderRequestID != concurrentStorage.ProviderRequestID ||
		service.attachments[resources.AttachmentID].ProviderRequestID != concurrentAttachment.ProviderRequestID {
		t.Fatalf("hydrate overwrote concurrent state compute=%#v storage=%#v attachment=%#v", service.computes[resources.ComputeAllocationID], service.volumes[resources.StorageID], service.attachments[resources.AttachmentID])
	}
}

// Static Tencent CBS bindings deliberately have no dynamic StorageClass. Exercise
// the real adapter's successful storage/attachment stages, not a synthetic volume
// that fills this optional field, then recover the persisted authority unchanged.
func TestTencentStaticLaunchResourcesRecoverForReads(t *testing.T) {
	for _, mode := range []string{"", string(contracts.WorkspaceProvisioningResourceOnly)} {
		for _, reopen := range []bool{false, true} {
			t.Run(mode+"/restart="+map[bool]string{false: "false", true: "true"}[reopen], func(t *testing.T) {
				ctx := context.Background()
				service, store, provider, preflight, image, launchHash := newTencentWorkspaceLaunchService(t)
				if mode != "" {
					admission, err := service.workspaceLaunchPreflight(ctx, preflight.ProviderBindingRef)
					if err != nil {
						t.Fatal(err)
					}
					admission.Input.ProvisioningMode, admission.Input.WorkspaceImageDigest = mode, ""
					admission.ProviderBindingRef = workspaceLaunchPreflightBindingRef(admission)
					if err := service.persistWorkspaceLaunchPreflight(ctx, admission); err != nil {
						t.Fatal(err)
					}
					preflight.ProviderBindingRef = admission.ProviderBindingRef
					image = ""
				}
				computeInput := workspaceLaunchStageFixtureInput(preflight, image, launchHash, "ensure_compute_allocation", "ensure_compute_allocation", WorkspaceLaunchResources{})
				compute := ComputeAllocation{
					ID: workspaceLaunchComputeID(computeInput.Binding), OperationID: computeInput.Binding.FabricOperationID,
					AccountID: "acct-alpha", WorkspaceID: "ws-alpha", PackageID: "basic", NodePoolID: "np-basic", Status: "running",
					MachineName: "machine-alpha", NodeName: "node-alpha", InstanceID: "ins-alpha", Zone: "ap-guangzhou-3", Provider: "tencent-tke",
					ProviderResourceID: "ins-alpha", ProviderRequestID: "req-compute", InstanceType: "SA5.MEDIUM4", ChargeType: "PREPAID",
				}
				resources := WorkspaceLaunchResources{ComputeAllocationID: compute.ID, ComputeBindingRef: computeInput.Binding.FabricOperationID}
				seedTencentWorkspaceLaunchStage(t, store, preflight, image, launchHash, "ensure_compute_allocation", "ensure_compute_allocation", WorkspaceLaunchResources{}, resources, tencentWorkspaceLaunchState{Compute: &compute}, 0)
				mutations := 0
				var manifest []byte
				provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
					if request.Action == "create_storage_volume" {
						mutations++
					} else if request.Action != "sync_storage_volume" {
						t.Fatalf("unexpected provider mutation/action %s", request.Action)
					}
					return provisionerResponse{OK: true, Status: "ready", StorageVolumeID: "disk-alpha", CBSStatus: "UNATTACHED", ProviderRequestID: "req-cbs-read",
						ProviderData: map[string]string{"diskType": "CLOUD_BSSD", "zone": compute.Zone, "sizeGb": "10", "renewFlag": "NOTIFY_AND_MANUAL_RENEW", "deadline": "2026-10-19T00:00:00Z", "region": "ap-guangzhou"}}, nil
				}
				provider.kubectl = func(_ context.Context, args []string, stdin []byte) ([]byte, error) {
					if len(args) > 0 && args[0] == "apply" {
						mutations++
						manifest = append([]byte(nil), stdin...)
						return nil, nil
					}
					if len(args) > 2 && args[0] == "get" && strings.HasPrefix(args[1], "pv/") && strings.HasPrefix(args[2], "pvc/") {
						return tencentStorageBindingReadback(t, manifest, false), nil
					}
					t.Fatalf("unexpected kubectl mutation/action %#v", args)
					return nil, nil
				}
				for _, stage := range []struct{ name, action string }{{"storage", "ensure_storage"}, {"attachment", "ensure_attachment"}} {
					input := workspaceLaunchStageFixtureInput(preflight, image, launchHash, stage.name, stage.action, resources)
					input.ProvisioningMode = mode
					result, err := service.EnsureWorkspaceLaunchStage(ctx, input)
					if err != nil || result.State != "ready" {
						t.Fatalf("%s result=%#v err=%v", stage.name, result, err)
					}
					resources = result.Resources
				}
				before, err := store.List(ctx)
				if err != nil {
					t.Fatal(err)
				}
				for _, op := range before {
					candidate, _, _, isCandidate, valid := canonicalWorkspaceLaunchDeleteStage(op, "storage")
					if isCandidate && (!valid || candidate.volume.StorageClass != "") {
						t.Fatalf("real static storage stage rejected or invented class: valid=%v volume=%#v", valid, candidate.volume)
					}
				}
				originalMutations := mutations
				if reopen {
					reopened := NewMemoryOperationStore()
					for _, op := range before {
						if err := reopened.Append(ctx, op); err != nil {
							t.Fatal(err)
						}
					}
					store = reopened
					service = NewServiceWithOperationStore(provider, store)
				}
				if _, found := service.GetComputeAllocation(ctx, resources.ComputeAllocationID); !found {
					t.Fatal("compute not recovered")
				}
				inputs := ProviderFactsBatchInput{Items: []ProviderFactInput{
					{AccountID: compute.AccountID, WorkspaceID: compute.WorkspaceID, ResourceType: "storage", ResourceID: resources.StorageID},
					{AccountID: compute.AccountID, WorkspaceID: compute.WorkspaceID, ResourceType: "attachment", ResourceID: resources.AttachmentID},
				}}
				facts, err := service.ProviderFactsBatch(ctx, inputs)
				if err != nil {
					t.Fatal(err)
				}
				for _, fact := range facts.Items {
					if !fact.Available || fact.ErrorCode != "" {
						t.Fatalf("resource read failed: %#v", fact)
					}
				}
				for i := range inputs.Items {
					inputs.Items[i].AccountID = "foreign-account"
				}
				facts, err = service.ProviderFactsBatch(ctx, inputs)
				if err != nil {
					t.Fatal(err)
				}
				for _, fact := range facts.Items {
					if fact.Available || fact.ErrorCode != "provider_fact_identity_mismatch" {
						t.Fatalf("foreign resource accepted: %#v", fact)
					}
				}
				after, err := store.List(ctx)
				if err != nil || !reflect.DeepEqual(before, after) || mutations != originalMutations {
					t.Fatalf("read rewrote authority or dispatched mutation: err=%v mutations=%d/%d", err, mutations, originalMutations)
				}
			})
		}
	}
}
