package coordination

import (
	"context"
	"fmt"
	"time"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/fabric/internal/fabric"
)

type localDispatcher struct {
	service  *fabric.Service
	provider *fabric.LocalDockerProvider
}

// NewLocalDispatcher uses the exact same service and provider as Fabric's HTTP
// runtime routes, preserving their existing operation store and resource maps.
func NewLocalDispatcher(service *fabric.Service, provider *fabric.LocalDockerProvider) ResourceDispatcher {
	if service == nil || provider == nil {
		return nil
	}
	return &localDispatcher{service: service, provider: provider}
}

func (d *localDispatcher) Provider() string { return providerLocalDocker }

func (d *localDispatcher) BindSecret(ctx context.Context, in SecretBindIntent) (SecretBindResult, error) {
	bound, err := d.provider.BindWorkspaceApplicationSecret(ctx, fabric.SecretBindInput{AccountID: in.TenantID, WorkspaceID: in.WorkspaceID, RuntimeInstanceID: in.RuntimeInstanceID, SecretRef: in.SecretRef, Purpose: in.TargetSlot, Fingerprint: in.Fingerprint, TargetSlot: in.TargetSlot})
	if err != nil {
		return SecretBindResult{}, err
	}
	return SecretBindResult{SecretRef: bound.SecretRef, Version: bound.Version, Fingerprint: bound.Fingerprint}, nil
}

func (d *localDispatcher) EnsureResources(ctx context.Context, in ResourceIntent) (*ResourceResult, error) {
	bound, err := d.provider.ResolveAcceptedResourcePlan(in.TenantID, in.Plan)
	if err != nil {
		return nil, err
	}
	// The quota preflight must pass before even the free Docker network is
	// dispatched, so an unsupported host cannot leave a partial allocation.
	if _, err = d.service.MonthlyPreflight(ctx, fabric.MonthlyPreflightInput{ResourceType: "storage", PackageID: bound.PackageID, SizeGB: int(in.Plan.GetCapacityGib()), Zone: "local"}); err != nil {
		return nil, err
	}
	compute, err := d.service.CreateComputeAllocation(ctx, fabric.ComputeAllocationInput{ID: in.ComputeID, AccountID: bound.AccountID, WorkspaceID: in.WorkspaceID, PackageID: bound.PackageID, NodePoolID: bound.NodePoolID, IdempotencyKey: in.OperationID + ":compute"})
	if err != nil {
		return nil, err
	}
	// Compute uses the provider's existing asynchronous operation. Returning
	// pending lets the next original-order reconcile observe it without a second
	// create or a new workflow worker.
	compute, ok := d.service.GetComputeAllocation(ctx, compute.ID)
	if !ok || compute.Status != "running" {
		return nil, fmt.Errorf("local_compute_pending")
	}
	compute, err = d.provider.ReadComputeAllocation(ctx, compute)
	if err != nil {
		return nil, err
	}
	if compute.ID != in.ComputeID || compute.AccountID != bound.AccountID || compute.WorkspaceID != in.WorkspaceID || compute.Status != "running" || compute.ProviderResourceID == "" {
		return nil, fmt.Errorf("local_compute_readback_mismatch")
	}
	storage, err := d.service.CreateStorageVolume(ctx, fabric.StorageVolumeInput{ID: in.StorageID, AccountID: bound.AccountID, WorkspaceID: in.WorkspaceID, ComputeID: compute.ID, Zone: compute.Zone, SizeGB: int(in.Plan.GetCapacityGib()), IdempotencyKey: in.OperationID + ":storage"})
	if err != nil {
		return nil, err
	}
	storage, err = d.provider.ReadStorageVolume(ctx, storage)
	if err != nil {
		return nil, err
	}
	if storage.ID != in.StorageID || storage.AccountID != bound.AccountID || storage.WorkspaceID != in.WorkspaceID || storage.Status != "ready" || storage.ProviderResourceID == "" {
		return nil, fmt.Errorf("local_storage_readback_mismatch")
	}
	attachment, err := d.service.CreateStorageAttachment(ctx, fabric.StorageAttachmentInput{WorkspaceID: in.WorkspaceID, ComputeID: compute.ID, VolumeID: storage.ID, IdempotencyKey: in.OperationID + ":attachment"})
	if err != nil {
		return nil, err
	}
	attachment, err = d.provider.ReadStorageAttachment(ctx, attachment, compute, storage)
	if err != nil {
		return nil, err
	}
	if attachment.ID == "" || attachment.OperationID != in.OperationID+":attachment" || attachment.WorkspaceID != in.WorkspaceID || attachment.ComputeID != compute.ID || attachment.VolumeID != storage.ID || attachment.Status != "attached" || attachment.ProviderAttachmentID == "" {
		return nil, fmt.Errorf("local_attachment_readback_mismatch")
	}
	// The local compute's provider reference is the Docker network the order
	// joined; that is the provider-authoritative network fact for this set.
	network := &NetworkFact{ProviderReference: compute.ProviderResourceID, Zone: compute.Zone}
	return &ResourceResult{Binding: &api.ResourceExecutionBinding{ComputeAllocationId: compute.ID, StorageVolumeId: storage.ID, DataAttachmentId: attachment.ID, DataAttachmentOperationId: attachment.OperationID, AccountId: bound.AccountID}, Compute: compute, Storage: storage, Attachment: attachment, Network: network, ObservedAt: time.Now().UTC()}, nil
}
