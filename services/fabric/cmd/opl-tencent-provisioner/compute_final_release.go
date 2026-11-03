package main

import (
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	cvm2017 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm/v20170312"
)

// This explicit action is not part of an ordinary destroy retry. Fabric admits
// it only behind its durable, exact-resource final-release dispatch fence.
func (client *tencentSDKClient) ReleaseRecycledComputeAllocation(request Request, env map[string]string) Response {
	refuse := func() Response {
		return Response{Ok: false, ErrorCode: "compute_final_release_identity_or_state_mismatch", MutationCount: 0}
	}
	if request.DryRun || request.Allocation.MachineType != "NativeCVM" || request.Zone == "" || request.Pool.InstanceType == "" || !validOwnershipTags(request.Tags) || request.Tags["opl_account_id"] != request.AccountId || request.Tags["opl_resource_id"] != request.Allocation.Id {
		return refuse()
	}
	before := client.ReadComputeDestroyStatus(request, env)
	if !before.Ok {
		return before
	}
	if before.MachinePresent == nil || *before.MachinePresent || before.TKEStatus != "NOT_FOUND" {
		return refuse()
	}
	if before.CVMStatus == "NOT_FOUND" {
		return before
	}
	if before.CVMStatus != "SHUTDOWN" || before.ProviderData["isolatedSource"] != "MANMADE" {
		return refuse()
	}
	instance, readID, err := client.describeCvmInstanceByID(request.Allocation.InstanceId)
	if err != nil || readID == "" {
		return refuse()
	}
	if instance == nil {
		return client.ReadComputeDestroyStatus(request, env)
	}
	tags, err := cvmOwnershipTags(instance)
	if err != nil || stringValue(instance.InstanceState) != "SHUTDOWN" || stringValue(instance.IsolatedSource) != "MANMADE" || stringValue(instance.InstanceChargeType) != "PREPAID" || instance.Placement == nil || stringValue(instance.Placement.Zone) != request.Zone || stringValue(instance.InstanceType) != request.Pool.InstanceType || !containsString(instance.PrivateIpAddresses, request.Allocation.PrivateIp) {
		return refuse()
	}
	for _, key := range cbsOwnershipTagKeys {
		if tags[key] != request.Tags[key] {
			return refuse()
		}
	}
	terminator, ok := client.nativeCvmClient.(interface {
		TerminateInstances(*cvm2017.TerminateInstancesRequest) (*cvm2017.TerminateInstancesResponse, error)
	})
	if !ok {
		return Response{Ok: false, ErrorCode: "compute_final_release_unavailable", MutationCount: 0}
	}
	terminate := cvm2017.NewTerminateInstancesRequest()
	terminate.InstanceIds = []*string{common.StringPtr(request.Allocation.InstanceId)}
	result, callErr := terminator.TerminateInstances(terminate)
	requestID := ""
	if result != nil && result.Response != nil {
		requestID = stringValue(result.Response.RequestId)
	}
	// Never retry the irreversible call. Even an error may mean it was accepted;
	// Fabric's durable fence and later exact reads preserve that uncertainty.
	if callErr != nil || requestID == "" {
		return Response{Ok: false, ErrorCode: "compute_final_release_dispatch_unconfirmed", InstanceId: request.Allocation.InstanceId, MutationCount: 1}
	}
	return Response{Ok: true, Status: "accepted", InstanceId: request.Allocation.InstanceId, ProviderRequestId: requestID, MutationCount: 1}
}
