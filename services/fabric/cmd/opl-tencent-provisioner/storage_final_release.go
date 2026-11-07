package main

import (
	"strings"
	"time"

	cbs2017 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cbs/v20170312"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
)

const storageFinalReleaseAction = "release_recycled_storage_volume"

// ReleaseRecycledStorageVolume issues exactly one final TerminateDisks for an
// original prepaid disk in the recycle bin. Fabric owns the durable send fence;
// an ambiguous SDK result never causes a retry or another termination here.
func (client *tencentSDKClient) ReleaseRecycledStorageVolume(request Request, _ map[string]string) Response {
	refuse := func() Response {
		return Response{Ok: false, ErrorCode: "storage_final_release_identity_or_state_mismatch", MutationCount: 0}
	}
	if request.DryRun || client == nil || client.nativeCbsClient == nil {
		return refuse()
	}
	if mismatch := client.storageProviderRegionFailure(request); mismatch != nil {
		return *mismatch
	}
	storage := request.Storage
	originalDeadline, deadlineErr := time.Parse(time.RFC3339Nano, storage.Deadline)
	if request.Region != strings.TrimSpace(request.Region) || request.AccountId == "" || request.AccountId != strings.TrimSpace(request.AccountId) ||
		request.Tags["opl_account_id"] != request.AccountId || len(request.Tags) != len(cbsOwnershipTagKeys) || !validCBSReadbackInput(storage, request.Tags) ||
		storage.Id == "disk-" || storage.Id != strings.TrimSpace(storage.Id) || storage.DiskType != strings.TrimSpace(storage.DiskType) ||
		storage.Zone != strings.TrimSpace(storage.Zone) || request.Zone != "" && request.Zone != storage.Zone || deadlineErr != nil {
		return refuse()
	}
	for _, key := range cbsOwnershipTagKeys {
		if request.Tags[key] != strings.TrimSpace(request.Tags[key]) {
			return refuse()
		}
	}
	// Reuse deletion readback so both manual-renew flags are validated without
	// changing the admission rules of ordinary reads or the original destroy.
	read := request
	read.Action = "read_storage_for_delete"
	before := client.storageVolumeReadback(read, true)
	before.Retryable = false
	if !before.Ok {
		return before
	}
	if strings.TrimSpace(before.ProviderRequestId) == "" || before.StorageVolumeId != storage.Id {
		return refuse()
	}
	if before.CBSStatus == "NOT_FOUND" && before.Status == "external_deleted" {
		return before
	}
	deadline, err := time.Parse(time.RFC3339Nano, before.ProviderData["deadline"])
	if before.CBSStatus != "TORECYCLE" || err != nil || deadline.After(originalDeadline) {
		return refuse()
	}
	// The official Terraform CBS force_delete implementation at
	// da4ffb7a7275152bb8070abcb07326fa27ca9c2a uses the same TerminateDisks
	// RPC a second time for a disk already observed in TORECYCLE.
	terminate := cbs2017.NewTerminateDisksRequest()
	terminate.DiskIds = []*string{common.StringPtr(storage.Id)}
	result, callErr := client.nativeCbsClient.TerminateDisks(terminate)
	requestID := ""
	if result != nil && result.Response != nil {
		requestID = stringValue(result.Response.RequestId)
	}
	if callErr != nil || strings.TrimSpace(requestID) == "" || requestID != strings.TrimSpace(requestID) {
		return Response{Ok: false, ErrorCode: "storage_final_release_dispatch_unconfirmed", StorageVolumeId: storage.Id, ProviderRequestId: requestID, MutationCount: 1}
	}
	return Response{Ok: true, Status: "accepted", StorageVolumeId: storage.Id, ProviderRequestId: requestID, MutationCount: 1}
}
