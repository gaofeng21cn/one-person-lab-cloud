package coordination

import (
	"context"
	"fmt"
	"strings"
	"time"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/fabric/internal/fabric"
)

// TencentResourceProvider is the narrow Tencent resource adapter surface this
// dispatcher uses. *fabric.TencentProvider is the only production implementation;
// the resource orchestration itself stays in the shared Fabric service.
type TencentResourceProvider interface {
	ResolveAcceptedResourcePlan(tenant string, plan *api.ResourcePlanSnapshot) (fabric.AcceptedTencentResourcePlan, error)
	ReadComputeAllocation(context.Context, fabric.ComputeAllocation) (fabric.ComputeAllocation, error)
	ReadStorageVolume(context.Context, fabric.StorageVolume) (fabric.StorageVolume, error)
	ReadStorageAttachment(context.Context, fabric.StorageAttachment, fabric.ComputeAllocation, fabric.StorageVolume) (fabric.StorageAttachment, error)
	BindWorkspaceApplicationSecret(context.Context, fabric.SecretBindInput) (fabric.SecretBindResult, error)
}

type tencentDispatcher struct {
	service  *fabric.Service
	provider TencentResourceProvider
}

// NewTencentDispatcher uses the same Fabric service and Tencent provider as the
// process's HTTP runtime routes, preserving its operation store, provider
// mutation journal and resource maps. It performs resource work only; deploying
// an application stays with Serve.
func NewTencentDispatcher(service *fabric.Service, provider TencentResourceProvider) ResourceDispatcher {
	if service == nil || provider == nil {
		return nil
	}
	return &tencentDispatcher{service: service, provider: provider}
}

func (d *tencentDispatcher) Provider() string { return providerTencentTKE }

func (d *tencentDispatcher) BindSecret(ctx context.Context, in SecretBindIntent) (SecretBindResult, error) {
	bound, err := d.provider.BindWorkspaceApplicationSecret(ctx, fabric.SecretBindInput{AccountID: in.TenantID, WorkspaceID: in.WorkspaceID, RuntimeInstanceID: in.RuntimeInstanceID, SecretRef: in.SecretRef, Purpose: in.TargetSlot, Fingerprint: in.Fingerprint, TargetSlot: in.TargetSlot})
	if err != nil {
		return SecretBindResult{}, err
	}
	return SecretBindResult{SecretRef: bound.SecretRef, Version: bound.Version, Fingerprint: bound.Fingerprint}, nil
}

func (d *tencentDispatcher) EnsureResources(ctx context.Context, in ResourceIntent) (*ResourceResult, error) {
	bound, err := d.provider.ResolveAcceptedResourcePlan(in.TenantID, in.Plan)
	if err != nil {
		return nil, err
	}
	// Capacity and price checks are read-only. They must both pass, on the same
	// prepaid monthly plan, before any provider mutation is attempted.
	if _, err = d.service.MonthlyPreflight(ctx, fabric.MonthlyPreflightInput{ResourceType: "compute", PackageID: bound.PackageID, Zone: bound.Zone}); err != nil {
		return nil, err
	}
	if _, err = d.service.MonthlyPreflight(ctx, fabric.MonthlyPreflightInput{ResourceType: "storage", PackageID: bound.PackageID, SizeGB: int(in.Plan.GetCapacityGib()), Zone: bound.Zone}); err != nil {
		return nil, err
	}

	// The compute allocation is idempotent by the original operation key. A
	// lost response or restart replays this same key and observes the same CVM.
	compute, err := d.service.CreateComputeAllocation(ctx, fabric.ComputeAllocationInput{ID: in.ComputeID, AccountID: bound.AccountID, WorkspaceID: in.WorkspaceID, PackageID: bound.PackageID, NodePoolID: bound.NodePoolID, IdempotencyKey: in.OperationID + ":compute"})
	if err != nil {
		return nil, err
	}
	current, ok := d.service.GetComputeAllocation(ctx, compute.ID)
	if !ok || !readyComputeStatus(current.Status) {
		return nil, errResourceDispatchPending
	}
	compute, err = d.provider.ReadComputeAllocation(ctx, current)
	if err != nil {
		return nil, err
	}
	if !readyComputeStatus(compute.Status) {
		return nil, errResourceDispatchPending
	}
	if err = validateTencentComputeReadback(bound, in, compute); err != nil {
		return nil, err
	}
	network, err := tencentNetworkFact(compute)
	if err != nil {
		return nil, err
	}

	storage, err := d.service.CreateStorageVolume(ctx, fabric.StorageVolumeInput{ID: in.StorageID, AccountID: bound.AccountID, WorkspaceID: in.WorkspaceID, ComputeID: compute.ID, Zone: compute.Zone, SizeGB: int(in.Plan.GetCapacityGib()), IdempotencyKey: in.OperationID + ":storage"})
	if err != nil {
		return nil, err
	}
	storage, err = d.provider.ReadStorageVolume(ctx, storage)
	if err != nil {
		return nil, err
	}
	if storage.Status != "ready" {
		return nil, errResourceDispatchPending
	}
	if err = validateTencentStorageReadback(bound, in, compute, storage); err != nil {
		return nil, err
	}

	attachment, err := d.service.CreateStorageAttachment(ctx, fabric.StorageAttachmentInput{WorkspaceID: in.WorkspaceID, ComputeID: compute.ID, VolumeID: storage.ID, IdempotencyKey: in.OperationID + ":attachment"})
	if err != nil {
		return nil, err
	}
	attachment, err = d.provider.ReadStorageAttachment(ctx, attachment, compute, storage)
	if err != nil {
		return nil, err
	}
	if attachment.Status != "attached" {
		return nil, errResourceDispatchPending
	}
	if err = validateTencentAttachmentReadback(in, compute, storage, attachment); err != nil {
		return nil, err
	}

	return &ResourceResult{
		Binding: &api.ResourceExecutionBinding{ComputeAllocationId: compute.ID, StorageVolumeId: storage.ID, DataAttachmentId: attachment.ID, DataAttachmentOperationId: attachment.OperationID, AccountId: bound.AccountID},
		Compute: compute, Storage: storage, Attachment: attachment, Network: network, ObservedAt: time.Now().UTC(),
	}, nil
}

func readyComputeStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running", "ready", "active":
		return true
	default:
		return false
	}
}

// validateTencentComputeReadback proves the CVM/TKE node identity and its prepaid
// monthly billing facts came from the provider, not from this Fabric process.
func validateTencentComputeReadback(bound fabric.AcceptedTencentResourcePlan, in ResourceIntent, compute fabric.ComputeAllocation) error {
	instanceID := firstNonEmptyValue(compute.InstanceID, compute.CVMInstanceID)
	if compute.ID != in.ComputeID || compute.AccountID != bound.AccountID || compute.WorkspaceID != in.WorkspaceID ||
		compute.Provider != providerTencentTKE || !readyComputeStatus(compute.Status) || compute.ProviderResourceID == "" ||
		compute.PackageID != bound.PackageID || compute.NodePoolID != bound.NodePoolID || compute.Zone != bound.Zone ||
		compute.MachineName == "" || compute.NodeName == "" || compute.PrivateIP == "" || !strings.HasPrefix(instanceID, "ins-") ||
		compute.ChargeType != "PREPAID" || compute.RenewFlag != "NOTIFY_AND_MANUAL_RENEW" || compute.Deadline == "" || compute.ProviderRequestID == "" {
		return fmt.Errorf("tencent_compute_readback_mismatch")
	}
	return nil
}

func validateTencentStorageReadback(bound fabric.AcceptedTencentResourcePlan, in ResourceIntent, compute fabric.ComputeAllocation, storage fabric.StorageVolume) error {
	if storage.ID != in.StorageID || storage.WorkspaceID != in.WorkspaceID || storage.AccountID != bound.AccountID ||
		storage.Provider != providerTencentTKE || storage.Status != "ready" || storage.ProviderResourceID == "" ||
		!strings.HasPrefix(storage.ProviderResourceID, "disk-") || storage.SizeGB != int(in.Plan.GetCapacityGib()) ||
		storage.Zone != compute.Zone || storage.DiskType == "" || storage.ProviderData["region"] != bound.Region {
		return fmt.Errorf("tencent_storage_readback_mismatch")
	}
	return nil
}

func validateTencentAttachmentReadback(in ResourceIntent, compute fabric.ComputeAllocation, storage fabric.StorageVolume, attachment fabric.StorageAttachment) error {
	if attachment.ID == "" || attachment.OperationID != in.OperationID+":attachment" || attachment.WorkspaceID != in.WorkspaceID ||
		attachment.ComputeID != compute.ID || attachment.VolumeID != storage.ID || attachment.Provider != providerTencentTKE ||
		attachment.Status != "attached" || !strings.HasPrefix(attachment.ProviderAttachmentID, "pv/") || attachment.ProviderRequestID == "" {
		return fmt.Errorf("tencent_attachment_readback_mismatch")
	}
	return nil
}

// tencentNetworkFact is the provider-authoritative TKE placement the CVM joined.
func tencentNetworkFact(compute fabric.ComputeAllocation) (*NetworkFact, error) {
	region := compute.ProviderData["region"]
	if compute.NodePoolID == "" || compute.Zone == "" || region == "" || region != strings.TrimSpace(region) {
		return nil, fmt.Errorf("tencent_network_readback_mismatch")
	}
	return &NetworkFact{ProviderReference: "tke-node-pool/" + compute.NodePoolID, Zone: compute.Zone, Region: region}, nil
}

func firstNonEmptyValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
