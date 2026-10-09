package coordination

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/fabric/internal/fabric"
)

// deleteResourceFromOwnSurface releases one deletion kind through the same
// internal Fabric capabilities the legacy HTTP lifecycle drives today, then
// classifies the outcome from the owning surface's readback. Every step is
// idempotent: the internal owner keeps the durable dispatch claim, so a repeated
// call reconciles by readback instead of dispatching a second provider mutation.
func deleteResourceFromOwnSurface(ctx context.Context, service *fabric.Service, in ResourceDeletionIntent, kind string) (ResourceDeletionFact, error) {
	switch kind {
	case DeletionKindRuntime:
		return deleteRuntimeFromOwnSurface(ctx, service, in)
	case DeletionKindSecret:
		return deleteSecretFromOwnSurface(ctx, service, in)
	case DeletionKindAttachment:
		return deleteAttachmentFromOwnSurface(ctx, service, in)
	case DeletionKindStorage:
		return deleteStorageFromOwnSurface(ctx, service, in)
	case DeletionKindCompute:
		return deleteComputeFromOwnSurface(ctx, service, in)
	default:
		return ResourceDeletionFact{Kind: kind}, fmt.Errorf("unknown resource deletion kind")
	}
}

// deleteRuntimeFromOwnSurface retires the Workspace runtime only when its own
// observation still reports owned objects, and confirms absence from that same
// observation surface. A resource-only purchase has no runtime, so its first
// readback already reports absent and no destroy is dispatched.
func deleteRuntimeFromOwnSurface(ctx context.Context, service *fabric.Service, in ResourceDeletionIntent) (ResourceDeletionFact, error) {
	fact := ResourceDeletionFact{Kind: DeletionKindRuntime, ResourceID: in.WorkspaceID}
	observation := service.ObserveWorkspaceRuntimeDelete(ctx, in.WorkspaceID)
	if observation.State != fabric.WorkspaceOwnerObservationAbsent {
		if _, err := service.DestroyWorkspaceRuntime(ctx, in.WorkspaceID, in.OperationID+":runtime"); err != nil {
			return fact, err
		}
		observation = service.ObserveWorkspaceRuntimeDelete(ctx, in.WorkspaceID)
	}
	fact.ProviderStatus = observation.State
	fact.EvidenceRef = observation.ReadbackID
	fact.ObservedAt = deletionObservedAt(observation.ObservedAt)
	switch observation.State {
	case fabric.WorkspaceOwnerObservationAbsent:
		fact.State = DeletionStateAbsent
	case fabric.WorkspaceRuntimeDeleteObservationPresent, fabric.WorkspaceOwnerObservationPending:
		fact.State = DeletionStatePending
	default:
		return fact, fmt.Errorf("workspace runtime deletion readback is %s", observation.State)
	}
	return fact, nil
}

// deleteSecretFromOwnSurface removes the Workspace's owned Gateway Secret
// binding through the runtime provider and confirms absence from the same
// gateway-secret observation surface. A runtime that owned no Secret reports
// absent on the first readback.
func deleteSecretFromOwnSurface(ctx context.Context, service *fabric.Service, in ResourceDeletionIntent) (ResourceDeletionFact, error) {
	secretRef := contracts.WorkspaceGatewaySecretRef(in.WorkspaceID)
	fact := ResourceDeletionFact{Kind: DeletionKindSecret, ResourceID: secretRef}
	observation := service.ObserveWorkspaceRuntimeGatewaySecret(ctx, in.WorkspaceID)
	if observation.State != fabric.WorkspaceOwnerObservationAbsent {
		err := service.RemoveWorkspaceApplicationGatewaySecret(ctx, fabric.WorkspaceApplicationGatewaySecretCleanupInput{
			AccountID: in.AccountID, WorkspaceID: in.WorkspaceID, SecretRef: secretRef, IdempotencyKey: in.OperationID + ":secret",
		})
		if err != nil {
			return fact, err
		}
		observation = service.ObserveWorkspaceRuntimeGatewaySecret(ctx, in.WorkspaceID)
	}
	fact.ProviderStatus = observation.State
	switch observation.State {
	case fabric.WorkspaceOwnerObservationAbsent:
		fact.State = DeletionStateAbsent
	case fabric.WorkspaceOwnerObservationReady, fabric.WorkspaceOwnerObservationPending:
		fact.State = DeletionStatePending
	default:
		return fact, fmt.Errorf("gateway secret binding readback is %s", observation.State)
	}
	return fact, nil
}

// deleteAttachmentFromOwnSurface releases the mount binding. Releasing the
// binding is a local transition of the owning storage-attachment surface; the
// physical disk detach is proven later by the storage readback. An attachment
// identity the owner never recorded cannot be proven absent.
func deleteAttachmentFromOwnSurface(ctx context.Context, service *fabric.Service, in ResourceDeletionIntent) (ResourceDeletionFact, error) {
	fact := ResourceDeletionFact{Kind: DeletionKindAttachment, ResourceID: in.AttachmentID}
	if strings.TrimSpace(in.AttachmentID) == "" {
		fact.ProviderStatus = "attachment_identity_unknown"
		return fact, fmt.Errorf("the resource set has no provider-confirmed attachment identity")
	}
	detached, err := service.DetachStorageAttachment(ctx, in.AttachmentID)
	if err != nil {
		return fact, err
	}
	if detached.ID != in.AttachmentID || detached.WorkspaceID != in.WorkspaceID ||
		(detached.ComputeID != "" && detached.ComputeID != in.ComputeID) || (detached.VolumeID != "" && detached.VolumeID != in.StorageID) {
		return fact, fmt.Errorf("storage attachment readback differs from the resource set")
	}
	fact.ProviderReference = detached.ProviderAttachmentID
	fact.ProviderStatus = detached.Status
	fact.EvidenceRef = detached.ProviderRequestID
	switch detached.Status {
	case "detached":
		fact.State = DeletionStateAbsent
	default:
		fact.State = DeletionStatePending
	}
	return fact, nil
}

// deleteStorageFromOwnSurface destroys the prepaid volume and then re-reads the
// provider's own volume facts. Absence is reported only from that readback; a
// destroy call returning, an unfinished recycle state or an unreadable volume
// stays pending or unknown.
func deleteStorageFromOwnSurface(ctx context.Context, service *fabric.Service, in ResourceDeletionIntent) (ResourceDeletionFact, error) {
	fact := ResourceDeletionFact{Kind: DeletionKindStorage, ResourceID: in.StorageID}
	destroyErr := error(nil)
	if _, err := service.DestroyStorageVolume(ctx, in.StorageID); err != nil {
		if !errors.Is(err, fabric.ErrWorkspaceLaunchPending) {
			return fact, err
		}
		destroyErr = err
	}
	readback, err := service.ReadStorageVolume(ctx, in.StorageID)
	if err != nil {
		return fact, errors.Join(destroyErr, err)
	}
	fact.ProviderReference = readback.ProviderResourceID
	fact.ProviderStatus = readback.Status
	fact.DestroyState = readback.DestroyState
	fact.EvidenceRef = readback.ReadbackID
	fact.ObservedAt = deletionObservedAt(readback.ObservedAt)
	if fabric.StorageDeletionAbsent(readback) {
		fact.State = DeletionStateAbsent
		return fact, nil
	}
	fact.State = DeletionStatePending
	return fact, nil
}

// deleteComputeFromOwnSurface destroys the compute allocation and then re-reads
// the compute destroy status, the provider's read-only absence surface. A
// dispatched but not yet reclaimed machine stays pending; an unreadable readback
// stays unknown.
func deleteComputeFromOwnSurface(ctx context.Context, service *fabric.Service, in ResourceDeletionIntent) (ResourceDeletionFact, error) {
	fact := ResourceDeletionFact{Kind: DeletionKindCompute, ResourceID: in.ComputeID}
	if _, err := service.DestroyComputeAllocation(ctx, in.ComputeID); err != nil {
		if !errors.Is(err, fabric.ErrWorkspaceLaunchPending) {
			return fact, err
		}
	}
	readback, err := service.ReadComputeDestroyStatus(ctx, in.ComputeID)
	if err != nil {
		return fact, err
	}
	fact.ProviderReference = firstNonBlank(readback.ProviderResourceID, readback.CVMInstanceID, readback.InstanceID, readback.MachineName)
	fact.ProviderStatus = readback.Status
	fact.DestroyState = readback.DestroyState
	fact.EvidenceRef = readback.ReadbackID
	fact.ObservedAt = deletionObservedAt(readback.ObservedAt)
	if fabric.ComputeDeletionAbsent(readback) {
		fact.State = DeletionStateAbsent
		return fact, nil
	}
	fact.State = DeletionStatePending
	return fact, nil
}

// deletionObservedAt keeps the owning surface's own observation time. An
// unparsable or absent stamp is left zero rather than replaced by the caller's
// clock.
func deletionObservedAt(value string) time.Time {
	observed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return observed.UTC()
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
