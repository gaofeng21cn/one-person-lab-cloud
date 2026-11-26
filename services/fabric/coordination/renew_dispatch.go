package coordination

import (
	"context"
	"fmt"
	"time"

	"opl-cloud/services/fabric/internal/fabric"
)

// renewResourceFromOwnSurface extends the prepaid period of one accepted
// resource set through the same internal Fabric service and provider the HTTP
// runtime routes use.
//
// The renewal reuses the original compute and storage identities and the
// original durable mutation claim, so a lost response can never buy a second
// resource. The internal service confirms the provider identity and the
// extended deadline itself; this adapter only carries the confirmed readback
// back to the coordination owner.
func renewResourceFromOwnSurface(ctx context.Context, service *fabric.Service, in ResourceRenewalIntent) (ResourceRenewalResult, error) {
	compute, err := service.RenewComputeAllocation(ctx, in.ComputeID, in.IdempotencyKey+":compute")
	if err != nil {
		return ResourceRenewalResult{}, err
	}
	storage, err := service.RenewStorageVolume(ctx, in.StorageID, in.IdempotencyKey+":storage")
	if err != nil {
		return ResourceRenewalResult{}, err
	}
	// The renewal extends the resources the Workspace already owns. A provider
	// that answered with a different handle is not a renewal of this set.
	if compute.ID != in.ComputeID || storage.ID != in.StorageID {
		return ResourceRenewalResult{}, fmt.Errorf("renewed_resources_identity_mismatch")
	}
	return ResourceRenewalResult{Compute: compute, Storage: storage, ObservedAt: time.Now().UTC()}, nil
}
