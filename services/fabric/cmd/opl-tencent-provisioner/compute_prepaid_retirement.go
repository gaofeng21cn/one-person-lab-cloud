package main

import (
	"strings"
	"unicode"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	cvm2017 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm/v20170312"
	tke2022 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tke/v20220501"
)

const (
	detachPrepaidComputeAction = "detach_prepaid_compute_allocation"
	retirePrepaidComputeAction = "retire_prepaid_compute_allocation"
)

// Each action is admitted by its own durable Fabric atomicClaim fence. These
// methods send at most one mutation and never turn an uncertain reply into a retry.
func (client *tencentSDKClient) DetachPrepaidComputeAllocation(request Request, _ map[string]string) Response {
	refuse := func() Response {
		return Response{Ok: false, ErrorCode: "compute_prepaid_detach_identity_or_state_mismatch", MutationCount: 0}
	}
	if !client.prepaidComputeRetirementInput(request) {
		return refuse()
	}
	pool, poolReadID, err := client.describeNativeNodePool(request.Pool.NodePoolId)
	if err != nil || !prepaidComputeRequestIDSafe(poolReadID) || pool == nil || pool.Native == nil ||
		stringValue(pool.Type) != "Native" || stringValue(pool.Native.MachineType) != "NativeCVM" || nativeSelfProvisioningEnabled(pool) {
		return refuse()
	}
	// Reuse the established Machine/node/IP/pool-to-CVM identity rules, then
	// independently read the exact CVM ID to verify the original owner and charge.
	present, ownershipReadID, err := client.verifyDestroyMachineOwnership(pool, request)
	if err != nil || !present || !prepaidComputeRequestIDSafe(ownershipReadID) {
		return refuse()
	}
	instance, cvmReadID, err := client.describeCvmInstanceByID(request.Allocation.InstanceId)
	if err != nil || !prepaidComputeRequestIDSafe(cvmReadID) || !exactPrepaidRetirementCVM(instance, request) || !activePrepaidRetirementCVM(instance) {
		return refuse()
	}

	detach := tke2022.NewDeleteClusterMachinesRequest()
	detach.ClusterId = common.StringPtr(client.clusterId)
	detach.MachineNames = []*string{common.StringPtr(request.Allocation.MachineName)}
	detach.InstanceDeleteMode = common.StringPtr("retain")
	detach.EnableScaleDown = common.BoolPtr(true)
	result, callErr := client.nativeTkeClient.DeleteClusterMachines(detach)
	requestID := ""
	if result != nil && result.Response != nil {
		requestID = stringValue(result.Response.RequestId)
	}
	return prepaidComputeDispatchResponse(request, "compute_prepaid_detach_dispatch_unconfirmed", requestID, callErr)
}

func (client *tencentSDKClient) RetirePrepaidComputeAllocation(request Request, env map[string]string) Response {
	refuse := func() Response {
		return Response{Ok: false, ErrorCode: "compute_prepaid_retire_identity_or_state_mismatch", MutationCount: 0}
	}
	if !client.prepaidComputeRetirementInput(request) {
		return refuse()
	}
	before := client.ReadComputeDestroyStatus(request, env)
	if !before.Ok || before.MachinePresent == nil || *before.MachinePresent || before.TKEStatus != "NOT_FOUND" ||
		!prepaidComputeRequestIDSafe(before.ProviderRequestId) {
		return refuse()
	}
	if before.CVMStatus == "NOT_FOUND" {
		before.Status = "already_absent"
		return before
	}
	instance, cvmReadID, err := client.describeCvmInstanceByID(request.Allocation.InstanceId)
	if err != nil || !prepaidComputeRequestIDSafe(cvmReadID) {
		return refuse()
	}
	if instance != nil && (!exactPrepaidRetirementCVM(instance, request) || !(activePrepaidRetirementCVM(instance) ||
		stringValue(instance.InstanceState) == "SHUTDOWN" && stringValue(instance.IsolatedSource) == "MANMADE")) {
		return refuse()
	}

	// These are stage observations only. Final release and deletion completion
	// remain separate owner decisions; in particular, never terminate SHUTDOWN here.
	if instance == nil || stringValue(instance.InstanceState) == "SHUTDOWN" {
		present := false
		out := Response{Ok: true, InstanceId: request.Allocation.InstanceId, MachinePresent: &present, TKEStatus: "NOT_FOUND", ProviderRequestId: cvmReadID, MutationCount: 0}
		if instance == nil {
			out.Status, out.CVMStatus = "already_absent", "NOT_FOUND"
		} else {
			out.Status, out.CVMStatus = "already_recycled", "SHUTDOWN"
			out.ProviderData = map[string]string{"isolatedSource": "MANMADE"}
		}
		return out
	}
	terminator, ok := client.nativeCvmClient.(interface {
		TerminateInstances(*cvm2017.TerminateInstancesRequest) (*cvm2017.TerminateInstancesResponse, error)
	})
	if !ok {
		return Response{Ok: false, ErrorCode: "compute_prepaid_retire_unavailable", MutationCount: 0}
	}
	retire := cvm2017.NewTerminateInstancesRequest()
	retire.InstanceIds = []*string{common.StringPtr(request.Allocation.InstanceId)}
	result, callErr := terminator.TerminateInstances(retire)
	requestID := ""
	if result != nil && result.Response != nil {
		requestID = stringValue(result.Response.RequestId)
	}
	return prepaidComputeDispatchResponse(request, "compute_prepaid_retire_dispatch_unconfirmed", requestID, callErr)
}

func (client *tencentSDKClient) prepaidComputeRetirementInput(request Request) bool {
	if request.DryRun || client == nil || client.nativeTkeClient == nil || client.nativeCvmClient == nil ||
		request.Allocation.MachineType != "NativeCVM" || client.computeDestroyProviderIdentityFailure(request) != nil ||
		!validTencentCVMID(request.Allocation.InstanceId) || !validOwnershipTags(request.Tags) ||
		request.Tags["opl_account_id"] != request.AccountId || request.Tags["opl_resource_id"] != request.Allocation.Id {
		return false
	}
	for _, value := range []string{request.AccountId, request.Region, request.Zone,
		request.Pool.ClusterId, request.Pool.NodePoolId, request.Pool.InstanceType, request.Allocation.Id,
		request.Allocation.InstanceId, request.Allocation.MachineName, request.Allocation.NodeName, request.Allocation.PrivateIp} {
		if value == "" || value != strings.TrimSpace(value) {
			return false
		}
	}
	for _, key := range cbsOwnershipTagKeys {
		if request.Tags[key] != strings.TrimSpace(request.Tags[key]) {
			return false
		}
	}
	return true
}

func exactPrepaidRetirementCVM(instance *cvm2017.Instance, request Request) bool {
	if instance == nil || stringValue(instance.InstanceId) != request.Allocation.InstanceId || stringValue(instance.InstanceName) != request.Allocation.Id ||
		instance.Placement == nil || stringValue(instance.Placement.Zone) != request.Zone || stringValue(instance.InstanceType) != request.Pool.InstanceType ||
		!containsString(instance.PrivateIpAddresses, request.Allocation.PrivateIp) || stringValue(instance.InstanceChargeType) != "PREPAID" ||
		stringValue(instance.RenewFlag) != "NOTIFY_AND_MANUAL_RENEW" {
		return false
	}
	tags, err := cvmOwnershipTags(instance)
	if err != nil {
		return false
	}
	for _, key := range cbsOwnershipTagKeys {
		if tags[key] != request.Tags[key] {
			return false
		}
	}
	return true
}

func activePrepaidRetirementCVM(instance *cvm2017.Instance) bool {
	return instance != nil && (stringValue(instance.InstanceState) == "RUNNING" || stringValue(instance.InstanceState) == "STOPPED") &&
		stringValue(instance.IsolatedSource) == "NOTISOLATED"
}

func prepaidComputeRequestIDSafe(value string) bool {
	return value != "" && strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) == -1
}

func prepaidComputeDispatchResponse(request Request, code, requestID string, callErr error) Response {
	out := Response{InstanceId: request.Allocation.InstanceId, MutationCount: 1}
	if callErr != nil || !prepaidComputeRequestIDSafe(requestID) {
		out.ErrorCode = code
		return out
	}
	out.Ok, out.Status, out.ProviderRequestId = true, "accepted", requestID
	return out
}
