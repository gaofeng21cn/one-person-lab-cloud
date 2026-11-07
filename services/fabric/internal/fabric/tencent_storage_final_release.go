package fabric

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ReleaseRecycledStorageVolume is a separate, explicitly fenced second stage.
// Only the original destroy owner may supply the frozen volume and its phase.
// Neither the original journal nor any binding is changed by this adapter.
func (p *TencentProvider) ReleaseRecycledStorageVolume(ctx context.Context, volume StorageVolume) (string, error) {
	owner, owned := ctx.Value(workspaceStorageDeleteOwnerContextKey{}).(StorageVolume)
	_, _, valid := tencentStorageDestroyBindingIdentity(volume)
	if !owned || !valid || owner.ProviderData["storageDestroyPhase"] == "" || !sameStorageDestroyStableIdentity(owner, volume) {
		return "", fmt.Errorf("storage_final_release_identity_mismatch")
	}
	if _, err := time.Parse(time.RFC3339Nano, owner.Deadline); err != nil {
		return "", fmt.Errorf("storage_final_release_identity_mismatch")
	}
	// Read the journal owner's phase, never an observation supplied by the caller.
	frozen := cloneStorageVolume(owner)
	// Erase the old observation values without removing their identity keys.
	// A sparse response must not inherit recycled-disk proof from the journal.
	readInput := cloneStorageVolume(frozen)
	for _, key := range []string{"cbsStatus", "storageVolumeId", "describeCbsRequestId"} {
		if _, present := readInput.ProviderData[key]; present {
			readInput.ProviderData[key] = ""
		}
	}
	readInput.ProviderRequestID, readInput.BindingPresent = "", nil
	observed, err := p.ReadStorageVolumeStatus(ctx, readInput)
	if err != nil {
		return "", err
	}
	if !sameStorageDestroyStableIdentity(frozen, observed) {
		return "", fmt.Errorf("storage_final_release_identity_mismatch")
	}
	if observed.BindingPresent == nil || *observed.BindingPresent {
		return "", fmt.Errorf("storage_final_release_binding_absence_required")
	}
	if observed.CBSStatus != "TORECYCLE" || observed.ProviderData["cbsStatus"] != "TORECYCLE" ||
		observed.ProviderData["storageVolumeId"] != frozen.ProviderResourceID || strings.TrimSpace(observed.ProviderData["describeCbsRequestId"]) == "" {
		return "", fmt.Errorf("storage_final_release_recycled_cbs_required")
	}
	response, err := p.provision(ctx, provisionerRequest{
		Action: storageFinalReleaseAction, AccountID: frozen.AccountID, Region: frozen.ProviderData["region"], Zone: frozen.Zone, Tags: cloneStorageVolume(frozen).CostTags,
		Storage: provisionerStorage{ID: frozen.ProviderResourceID, SizeGB: uint64(frozen.SizeGB), Zone: frozen.Zone, DiskType: frozen.DiskType, Deadline: frozen.Deadline},
	})
	if err != nil {
		return response.ProviderRequestID, err
	}
	if !response.OK {
		return response.ProviderRequestID, provisionerError(response)
	}
	if response.StorageVolumeID != frozen.ProviderResourceID || response.ProviderRequestID == "" || response.ProviderRequestID != strings.TrimSpace(response.ProviderRequestID) ||
		!(response.MutationCount == 1 && response.Status == "accepted" || response.MutationCount == 0 && response.Status == "external_deleted" && response.CBSStatus == "NOT_FOUND") {
		return "", fmt.Errorf("storage_final_release_response_invalid")
	}
	return response.ProviderRequestID, nil
}
