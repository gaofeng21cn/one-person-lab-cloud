package coordination

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/fabric/internal/fabric"
)

// fakeTencentProvider drives the real Fabric resource service through the exact
// Tencent resource shape without any live Tencent call. The embedded local
// provider only supplies the unrelated ports; every method this path can reach is
// overridden, and application deployment panics so the test proves the dispatcher
// performs resource work only.
type fakeTencentProvider struct {
	*fabric.LocalDockerProvider
	mu           sync.Mutex
	computeKeys  []string
	storageKeys  []string
	attachKeys   []string
	computeCalls int
	storageCalls int
	attachCalls  int
	deployCalls  int
}

func newFakeTencentProvider() *fakeTencentProvider {
	return &fakeTencentProvider{LocalDockerProvider: fabric.NewLocalDockerProvider()}
}

func fakeTencentPlan(packageID string) fabric.ComputePlan {
	return fabric.ComputePlan{ID: packageID + "-pool", Server: "2c4g", CPU: 2, MemoryGB: 4, DiskGB: 10, InstanceType: "SA5.MEDIUM4"}
}

func (p *fakeTencentProvider) Descriptor() fabric.ProviderDescriptor {
	plan := fakeTencentPlan("basic")
	return fabric.ProviderDescriptor{Name: "tencent-tke", RequiresMonthlyPricing: true, Plans: map[string]fabric.ComputePlan{"basic": plan}}
}

func (p *fakeTencentProvider) ResolveAcceptedResourcePlan(tenant string, plan *api.ResourcePlanSnapshot) (fabric.AcceptedTencentResourcePlan, error) {
	if plan.GetProvider() != "tencent-tke" || plan.GetProviderProfileId() != "tencent-tke" || plan.GetBillingMode() != "PREPAID_MONTHLY" || plan.GetPrepaidMonths() <= 0 {
		return fabric.AcceptedTencentResourcePlan{}, fmt.Errorf("tencent_accepted_profile_mismatch")
	}
	if tenant != "tenant-a" {
		return fabric.AcceptedTencentResourcePlan{}, fmt.Errorf("tencent_account_binding_required")
	}
	return fabric.AcceptedTencentResourcePlan{PackageID: "basic", AccountID: "acct-a", NodePoolID: "np-basic", Zone: "ap-guangzhou-3", Region: "ap-guangzhou"}, nil
}

func (p *fakeTencentProvider) MonthlyPreflight(_ context.Context, input fabric.MonthlyPreflightInput) (fabric.MonthlyPreflight, error) {
	requestIDs := map[string]string{"quota": "req-quota", "price": "req-price"}
	if input.ResourceType == "compute" {
		requestIDs = map[string]string{"nodePool": "req-pool", "subnets": "req-subnet", "availability": "req-avail", "quota": "req-quota"}
	}
	return fabric.MonthlyPreflight{ResourceType: input.ResourceType, PackageID: input.PackageID, NodePoolID: "np-basic", SizeGB: input.SizeGB, Zone: input.Zone, Available: true, ChargeType: "PREPAID", PeriodMonths: 1, RenewFlag: "NOTIFY_AND_MANUAL_RENEW", ProviderPriceCNY: 42, ProviderRequestIDs: requestIDs}, nil
}

func (p *fakeTencentProvider) PrepareComputeAllocation(_ context.Context, input fabric.ComputeAllocationInput) (fabric.ComputeAllocationPreparation, error) {
	plan := fakeTencentPlan(input.PackageID)
	return fabric.ComputeAllocationPreparation{PoolID: plan.ID, PackageID: input.PackageID, NodePoolID: input.NodePoolID, InstanceType: plan.InstanceType, Zone: "ap-guangzhou-3", MaxReplicas: 20, BaselineReplicas: 0, TargetReplicas: 1, ProviderRequestID: "req-prepare-instance"}, nil
}

func (p *fakeTencentProvider) CreateComputeAllocation(_ context.Context, execution fabric.ComputeAllocationExecution) (fabric.ComputeAllocation, error) {
	p.mu.Lock()
	p.computeCalls++
	p.computeKeys = append(p.computeKeys, execution.Allocation.ID)
	p.mu.Unlock()
	allocation := execution.Allocation
	allocation.Status = "running"
	allocation.Provider = "tencent-tke"
	allocation.PoolID = execution.Plan.PoolID
	allocation.InstanceType = execution.Plan.InstanceType
	allocation.InstanceID = "ins-" + allocation.ID + "-0001"
	allocation.CVMInstanceID = allocation.InstanceID
	allocation.ProviderResourceID = allocation.InstanceID
	allocation.NodeName = "10.66.0.10"
	allocation.MachineName = "machine-" + allocation.ID
	allocation.PrivateIP = "10.66.0.10"
	allocation.Zone = "ap-guangzhou-3"
	allocation.ChargeType = "PREPAID"
	allocation.RenewFlag = "NOTIFY_AND_MANUAL_RENEW"
	allocation.Deadline = "2026-10-29T00:00:00Z"
	allocation.ProviderRequestID = "req-cvm-" + allocation.ID
	allocation.ProviderData = map[string]string{"region": "ap-guangzhou", "zone": "ap-guangzhou-3", "nodePoolId": "np-basic", "clusterId": "cls-1", "machineName": allocation.MachineName, "instanceType": allocation.InstanceType, "chargeType": "PREPAID", "renewFlag": "NOTIFY_AND_MANUAL_RENEW", "deadline": allocation.Deadline}
	return allocation, nil
}

func (p *fakeTencentProvider) TagComputeMachine(context.Context, fabric.ProviderMachine, fabric.MachineOwnership) error {
	return nil
}

func (p *fakeTencentProvider) SyncComputeAllocation(_ context.Context, allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
	return allocation, nil
}

func (p *fakeTencentProvider) ValidateComputeAllocation(allocation fabric.ComputeAllocation, prepared fabric.ComputeAllocationPreparation) error {
	if allocation.Provider != "tencent-tke" || allocation.NodePoolID != prepared.NodePoolID || allocation.PackageID != prepared.PackageID ||
		allocation.InstanceType != prepared.InstanceType || allocation.MachineName == "" || allocation.NodeName == "" || allocation.Zone == "" ||
		allocation.ChargeType != "PREPAID" || allocation.RenewFlag != "NOTIFY_AND_MANUAL_RENEW" || allocation.Deadline == "" {
		return fmt.Errorf("compute_provider_readback_mismatch")
	}
	return nil
}

func (p *fakeTencentProvider) ReadComputeAllocation(_ context.Context, allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
	return allocation, nil
}

func (p *fakeTencentProvider) CreateStorageVolume(_ context.Context, input fabric.StorageVolumeInput) (fabric.StorageVolume, error) {
	p.mu.Lock()
	p.storageCalls++
	p.storageKeys = append(p.storageKeys, input.IdempotencyKey)
	p.mu.Unlock()
	return fabric.StorageVolume{ID: input.ID, OperationID: input.IdempotencyKey, AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, Status: "ready", Provider: "tencent-tke", ProviderResourceID: "disk-" + input.ID + "-0001", ProviderRequestID: "req-cbs-" + input.ID, SizeGB: input.SizeGB, DiskType: "CLOUD_BSSD", Zone: input.Zone, Deadline: "2026-10-29T00:00:00Z", ProviderData: map[string]string{"region": "ap-guangzhou", "zone": input.Zone, "diskType": "CLOUD_BSSD", "sizeGb": fmt.Sprint(input.SizeGB)}, CreatedAt: time.Now().UTC()}, nil
}

func (p *fakeTencentProvider) SyncStorageVolume(_ context.Context, volume fabric.StorageVolume) (fabric.StorageVolume, error) {
	return volume, nil
}

func (p *fakeTencentProvider) ReadStorageVolume(_ context.Context, volume fabric.StorageVolume) (fabric.StorageVolume, error) {
	return volume, nil
}

func (p *fakeTencentProvider) CreateStorageAttachment(_ context.Context, input fabric.StorageAttachmentInput, compute fabric.ComputeAllocation, volume fabric.StorageVolume) (fabric.StorageAttachment, error) {
	p.mu.Lock()
	p.attachCalls++
	p.attachKeys = append(p.attachKeys, input.IdempotencyKey)
	p.mu.Unlock()
	return fabric.StorageAttachment{ID: "att-" + input.ComputeID, OperationID: input.IdempotencyKey, WorkspaceID: input.WorkspaceID, ComputeID: input.ComputeID, VolumeID: input.VolumeID, Status: "attached", Provider: "tencent-tke", ProviderAttachmentID: "pv/pv-x:pvc/pvc-x", ProviderRequestID: "req-att-" + input.ComputeID, CreatedAt: time.Now().UTC()}, nil
}

func (p *fakeTencentProvider) ReadStorageAttachment(_ context.Context, attachment fabric.StorageAttachment, _ fabric.ComputeAllocation, _ fabric.StorageVolume) (fabric.StorageAttachment, error) {
	return attachment, nil
}

func (p *fakeTencentProvider) DetachStorageAttachment(context.Context, fabric.StorageAttachment) (fabric.StorageAttachment, error) {
	return fabric.StorageAttachment{}, nil
}

// Application deployment must never be reachable from the resource dispatcher.
func (p *fakeTencentProvider) CreateWorkspaceRuntime(context.Context, fabric.WorkspaceRuntimeInput, fabric.ComputeAllocation, fabric.StorageVolume) (fabric.WorkspaceRuntime, error) {
	p.mu.Lock()
	p.deployCalls++
	p.mu.Unlock()
	return fabric.WorkspaceRuntime{}, fmt.Errorf("dispatcher_deployed_application")
}

func (p *fakeTencentProvider) DestroyWorkspaceRuntime(context.Context, string) (fabric.WorkspaceRuntime, error) {
	p.mu.Lock()
	p.deployCalls++
	p.mu.Unlock()
	return fabric.WorkspaceRuntime{}, fmt.Errorf("dispatcher_deployed_application")
}

func (p *fakeTencentProvider) WorkspaceRuntimeStatus(context.Context, string) (fabric.WorkspaceRuntime, error) {
	return fabric.WorkspaceRuntime{}, fmt.Errorf("dispatcher_deployed_application")
}

func tencentIntent(operationID string) ResourceIntent {
	return ResourceIntent{
		TenantID: "tenant-a", WorkspaceID: "workspace-a", OperationID: operationID,
		ResourceSetID: "rset-" + operationID, ComputeID: "res-compute-" + operationID, StorageID: "res-storage-" + operationID,
		Plan: &api.ResourcePlanSnapshot{ComputePlanId: "cp", StoragePlanId: "sp", ProviderProfileId: "tencent-tke", ProviderComputeSkuId: "pool-basic-2c4g", ProviderStorageSkuId: "CLOUD_BSSD", Vcpus: 2, MemoryMib: 4096, CapacityGib: 10, PrepaidMonths: 1, ProviderCapabilityVersion: "v1", Provider: "tencent-tke", Region: "ap-guangzhou", BillingMode: "PREPAID_MONTHLY"},
	}
}

func dispatchUntilConfirmed(t *testing.T, dispatcher ResourceDispatcher, intent ResourceIntent) *ResourceResult {
	t.Helper()
	for attempt := 0; attempt < 100; attempt++ {
		result, err := dispatcher.EnsureResources(context.Background(), intent)
		if err == nil {
			return result
		}
		if !errors.Is(err, errResourceDispatchPending) {
			t.Fatalf("dispatch attempt %d: %v", attempt, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("resource dispatch never confirmed")
	return nil
}

func TestTencentDispatcherConfirmsPrepaidMonthlyResourcesAndNetworkPlacement(t *testing.T) {
	provider := newFakeTencentProvider()
	service := fabric.NewService(provider)
	dispatcher := NewTencentDispatcher(service, provider)
	if dispatcher.Provider() != "tencent-tke" {
		t.Fatalf("dispatcher provider=%q", dispatcher.Provider())
	}
	intent := tencentIntent("op-confirm")
	result := dispatchUntilConfirmed(t, dispatcher, intent)

	if result.Binding.GetAccountId() != "acct-a" || result.Binding.GetComputeAllocationId() != intent.ComputeID ||
		result.Binding.GetStorageVolumeId() != intent.StorageID || result.Binding.GetDataAttachmentId() != "att-"+intent.ComputeID ||
		result.Binding.GetDataAttachmentOperationId() != intent.OperationID+":attachment" {
		t.Fatalf("binding=%v", result.Binding)
	}
	if result.Compute.Provider != "tencent-tke" || result.Compute.InstanceID == "" || result.Compute.ChargeType != "PREPAID" ||
		result.Compute.RenewFlag != "NOTIFY_AND_MANUAL_RENEW" || result.Compute.NodePoolID != "np-basic" || result.Compute.Zone != "ap-guangzhou-3" {
		t.Fatalf("compute=%+v", result.Compute)
	}
	if result.Storage.ProviderResourceID != "disk-"+intent.StorageID+"-0001" || result.Storage.SizeGB != 10 || result.Storage.Zone != "ap-guangzhou-3" {
		t.Fatalf("storage=%+v", result.Storage)
	}
	if result.Network.ProviderReference != "tke-node-pool/np-basic" || result.Network.Zone != "ap-guangzhou-3" || result.Network.Region != "ap-guangzhou" {
		t.Fatalf("network=%+v", result.Network)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.deployCalls != 0 {
		t.Fatalf("dispatcher deployed an application %d times", provider.deployCalls)
	}
	if len(provider.computeKeys) == 0 || len(provider.storageKeys) == 0 || len(provider.attachKeys) == 0 {
		t.Fatalf("provider was not reached: compute=%v storage=%v attach=%v", provider.computeKeys, provider.storageKeys, provider.attachKeys)
	}
	for _, key := range provider.computeKeys {
		if key != intent.ComputeID {
			t.Fatalf("compute identity changed across attempts: %v", provider.computeKeys)
		}
	}
	for _, key := range provider.storageKeys {
		if key != intent.OperationID+":storage" {
			t.Fatalf("storage key changed across attempts: %v", provider.storageKeys)
		}
	}
}

func TestTencentDispatcherKeepsUnknownPendingAndReplaysOriginalPurchaseIdentity(t *testing.T) {
	provider := newFakeTencentProvider()
	service := fabric.NewService(provider)
	dispatcher := NewTencentDispatcher(service, provider)
	intent := tencentIntent("op-replay")

	// The first call may observe the asynchronous allocation as still pending; it
	// must never be reported as absent or confirmed, and it must not mint a new ID.
	result, err := dispatcher.EnsureResources(context.Background(), intent)
	if err == nil {
		t.Fatal("unconfirmed compute was reported as confirmed")
	}
	if !errors.Is(err, errResourceDispatchPending) {
		t.Fatalf("pending dispatch error=%v", err)
	}
	if result != nil {
		t.Fatalf("pending dispatch returned a result: %v", result)
	}
	confirmed := dispatchUntilConfirmed(t, dispatcher, intent)
	if confirmed.Compute.ID != intent.ComputeID || confirmed.Storage.ID != intent.StorageID {
		t.Fatalf("replay changed resource identity: %+v", confirmed.Binding)
	}
}

func TestTencentDispatcherRejectsUnapprovedBillingPlanBeforeAnyProviderCall(t *testing.T) {
	provider := newFakeTencentProvider()
	service := fabric.NewService(provider)
	dispatcher := NewTencentDispatcher(service, provider)
	intent := tencentIntent("op-billing")
	intent.Plan.BillingMode = "POSTPAID_BY_HOUR"
	if _, err := dispatcher.EnsureResources(context.Background(), intent); err == nil || errors.Is(err, errResourceDispatchPending) {
		t.Fatalf("postpaid billing err=%v", err)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.computeCalls != 0 || provider.storageCalls != 0 || provider.attachCalls != 0 {
		t.Fatalf("provider was reached for a rejected plan: %d %d %d", provider.computeCalls, provider.storageCalls, provider.attachCalls)
	}
}

func TestTencentDispatcherNilWithoutProvider(t *testing.T) {
	if dispatcher := NewTencentDispatcher(nil, newFakeTencentProvider()); dispatcher != nil {
		t.Fatal("dispatcher built without a service")
	}
	if dispatcher := NewTencentDispatcher(fabric.NewService(newFakeTencentProvider()), nil); dispatcher != nil {
		t.Fatal("dispatcher built without a provider")
	}
}
