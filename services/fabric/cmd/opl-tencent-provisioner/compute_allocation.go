package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	cbs2017 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cbs/v20170312"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tcerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	cvm2017 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm/v20170312"
	tke2022 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tke/v20220501"
	vpc2017 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc/v20170312"
	contracts "opl-cloud/packages/contracts/go"
)

func (client *tencentSDKClient) RenewComputeAllocation(request Request, _ map[string]string) Response {
	if client == nil || client.nativeCvmClient == nil {
		return Response{Ok: false, ErrorCode: "tencent_sdk_client_missing", Message: "Tencent CVM SDK client is missing.", Retryable: false}
	}
	allocation := request.Allocation
	allocation.Deadline = normalizeTencentDeadline(allocation.Deadline)
	request.Allocation = allocation
	if strings.TrimSpace(request.AccountId) == "" || strings.TrimSpace(allocation.Id) == "" || !strings.HasPrefix(allocation.InstanceId, "ins-") || allocation.Deadline == "" || strings.TrimSpace(request.Pool.InstanceType) == "" || strings.TrimSpace(request.Zone) == "" || !validOwnershipTags(request.Tags) || request.Tags["opl_account_id"] != request.AccountId || request.Tags["opl_resource_id"] != allocation.Id {
		return Response{Ok: false, ErrorCode: "tencent_cvm_renew_input_invalid", Message: "Exact compute resource, CVM instance, and previous deadline are required.", Retryable: false}
	}
	current := client.computeRenewalReadback(request)
	if !current.Ok || current.Status == "external_deleted" {
		return current
	}
	if current.ProviderData["deadline"] != allocation.Deadline {
		if !deadlineAfter(current.ProviderData["deadline"], allocation.Deadline) {
			return Response{Ok: false, ErrorCode: "tencent_cvm_renewal_deadline_invalid", Message: "Tencent CVM renewal deadline did not increase.", InstanceId: allocation.InstanceId, ProviderRequestId: current.ProviderRequestId, ProviderData: current.ProviderData, Retryable: true}
		}
		current.ProviderData["renewalResult"] = "already_renewed"
		return current
	}
	renew := cvm2017.NewRenewInstancesRequest()
	renew.InstanceIds = []*string{common.StringPtr(allocation.InstanceId)}
	renew.InstanceChargePrepaid = &cvm2017.InstanceChargePrepaid{Period: common.Int64Ptr(1), RenewFlag: common.StringPtr("NOTIFY_AND_MANUAL_RENEW")}
	renew.RenewPortableDataDisk = common.BoolPtr(false)
	renewed, err := client.nativeCvmClient.RenewInstances(renew)
	if err != nil {
		return sdkErrorResponse("tencent_renew_cvm_failed", err)
	}
	if renewed == nil || renewed.Response == nil {
		return Response{Ok: false, ErrorCode: "tencent_renew_cvm_response_missing", Message: "Tencent CVM renewal response is missing.", Retryable: true}
	}
	response := client.computeRenewalReadback(request)
	if !response.Ok || response.Status == "external_deleted" || !deadlineAfter(response.ProviderData["deadline"], allocation.Deadline) {
		return Response{
			Ok: false, ErrorCode: "tencent_cvm_renewal_unconfirmed", Message: "Tencent CVM renewal could not be confirmed by exact readback.",
			InstanceId: allocation.InstanceId, ProviderRequestId: stringValue(renewed.Response.RequestId), ProviderData: response.ProviderData, Retryable: true,
		}
	}
	response.ProviderData["renewCvmRequestId"] = stringValue(renewed.Response.RequestId)
	response.ProviderData["renewalResult"] = "renewed"
	response.ProviderRequestId = firstNonEmpty(stringValue(renewed.Response.RequestId), response.ProviderRequestId)
	return response
}

func (client *tencentSDKClient) computeRenewalReadback(request Request) Response {
	allocation := request.Allocation
	instance, requestID, err := client.describeCvmInstanceByID(allocation.InstanceId)
	if err != nil {
		response := sdkErrorResponse("tencent_describe_cvm_renewal_failed", err)
		response.ProviderRequestId = firstNonEmpty(response.ProviderRequestId, requestID)
		return response
	}
	if instance == nil {
		return Response{
			Ok: true, InstanceId: allocation.InstanceId, Status: "external_deleted", CVMStatus: "NOT_FOUND", ProviderRequestId: requestID,
			ProviderData: map[string]string{"instanceId": allocation.InstanceId, "cvmStatus": "NOT_FOUND", "describeCvmRequestId": requestID},
		}
	}
	zone := ""
	if instance.Placement != nil {
		zone = stringValue(instance.Placement.Zone)
	}
	deadline := normalizeTencentDeadline(stringValue(instance.ExpiredTime))
	actualTags, tagErr := cvmOwnershipTags(instance)
	if stringValue(instance.InstanceType) != request.Pool.InstanceType {
		return Response{
			Ok: false, ErrorCode: "compute_instance_type_mismatch", Message: "Tencent CVM instance type does not match the requested package.",
			InstanceId: allocation.InstanceId, ProviderRequestId: requestID, ProviderData: map[string]string{"instanceType": stringValue(instance.InstanceType)}, Retryable: true,
		}
	}
	if (request.Pool.CPU > 0 || request.Pool.MemoryGB > 0) && !cvmResourceShapeMatches(instance, request.Pool) {
		response := computeResourceShapeFailure(requestID)
		response.InstanceId = allocation.InstanceId
		return response
	}
	if stringValue(instance.InstanceId) != allocation.InstanceId || stringValue(instance.InstanceName) != allocation.Id ||
		stringValue(instance.InstanceChargeType) != "PREPAID" || stringValue(instance.RenewFlag) != "NOTIFY_AND_MANUAL_RENEW" ||
		deadline == "" || zone != request.Zone || tagErr != nil ||
		(strings.TrimSpace(allocation.PrivateIp) != "" && !containsString(instance.PrivateIpAddresses, allocation.PrivateIp)) {
		return Response{
			Ok: false, ErrorCode: "tencent_cvm_renewal_readback_mismatch", Message: "Tencent CVM billing, ownership, or deadline facts do not match the requested allocation.",
			InstanceId: allocation.InstanceId, ProviderRequestId: requestID, Retryable: true,
		}
	}
	for _, key := range cbsOwnershipTagKeys {
		if actualTags[key] != request.Tags[key] {
			return Response{Ok: false, ErrorCode: "tencent_cvm_renewal_readback_mismatch", Message: "Tencent CVM ownership tags do not match the requested allocation.", InstanceId: allocation.InstanceId, ProviderRequestId: requestID, Retryable: true}
		}
	}
	state := stringValue(instance.InstanceState)
	response := Response{
		Ok: true, InstanceId: allocation.InstanceId, Status: "provider_ready", CVMStatus: state, ProviderRequestId: requestID,
		ProviderData: map[string]string{
			"instanceId": allocation.InstanceId, "resourceId": allocation.Id, "cvmStatus": state,
			"instanceType": stringValue(instance.InstanceType),
			"chargeType":   stringValue(instance.InstanceChargeType), "renewFlag": stringValue(instance.RenewFlag),
			"deadline": deadline, "zone": zone, "privateIp": firstString(instance.PrivateIpAddresses), "describeCvmRequestId": requestID,
		},
	}
	if instance.CPU != nil && instance.Memory != nil {
		response.ProviderData["cpu"] = strconv.FormatInt(*instance.CPU, 10)
		response.ProviderData["memoryGb"] = strconv.FormatInt(*instance.Memory, 10)
	}
	for _, key := range cbsOwnershipTagKeys {
		response.ProviderData[key] = actualTags[key]
	}
	return response
}

func (client *tencentSDKClient) PrepareComputeAllocation(request Request, _ map[string]string) Response {
	if client == nil || client.nativeTkeClient == nil {
		return Response{Ok: false, ErrorCode: "tencent_sdk_client_missing", Message: "Tencent TKE SDK client is missing.", Retryable: false}
	}
	nodePoolId := strings.TrimSpace(request.Pool.NodePoolId)
	if nodePoolId == "" {
		return Response{Ok: false, ErrorCode: "compute_node_pool_id_required", Message: "An exact registered NodePool ID is required.", Retryable: false}
	}
	if request.Pool.MaxReplicas <= 0 {
		return Response{Ok: false, ErrorCode: "compute_allocation_plan_invalid", Message: "A configured NodePool maximum is required.", Retryable: false}
	}
	pool, describeRequestId, err := client.describeNativeNodePool(nodePoolId)
	if err != nil {
		return sdkErrorResponse("tencent_describe_node_pool_failed", err)
	}
	if failure := mutationNodePoolFailure(pool, request, describeRequestId); failure != nil {
		return *failure
	}
	currentReplicas := nativeReplicas(pool)
	beforeMachines, beforeMachinesRequestId, err := client.describeClusterMachines(nodePoolId)
	if err != nil {
		response := sdkErrorResponse("tencent_describe_cluster_machines_failed", err)
		response.ProviderRequestId = describeRequestId
		return response
	}
	if int64(len(beforeMachines)) != currentReplicas {
		return Response{Ok: false, ErrorCode: "compute_allocation_machine_inventory_incomplete", Message: "NodePool machine inventory is incomplete.", ProviderRequestId: beforeMachinesRequestId, Retryable: false}
	}
	machines := make([]MachineOutput, 0, len(beforeMachines))
	for _, machine := range beforeMachines {
		if machine == nil || strings.TrimSpace(stringValue(machine.MachineName)) == "" {
			return Response{Ok: false, ErrorCode: "compute_allocation_machine_inventory_incomplete", Message: "NodePool machine inventory contains an unknown machine.", ProviderRequestId: beforeMachinesRequestId, Retryable: false}
		}
		machines = append(machines, MachineOutput{MachineId: stringValue(machine.MachineName)})
	}
	return Response{Ok: true, PoolId: request.Pool.Id, NodePoolId: nodePoolId, Status: "prepared", ProviderRequestId: firstNonEmpty(beforeMachinesRequestId, describeRequestId), CurrentReplicas: currentReplicas, MaxReplicas: request.Pool.MaxReplicas, TargetReplicas: currentReplicas + 1, Machines: machines}
}

func (client *tencentSDKClient) CreateComputeAllocation(request Request, _ map[string]string) Response {
	return client.resolveComputeAllocation(request, true)
}

func (client *tencentSDKClient) ReadComputeAllocation(request Request, _ map[string]string) Response {
	return client.resolveComputeAllocation(request, false)
}

func (client *tencentSDKClient) resolveComputeAllocation(request Request, allowScale bool) Response {
	if client == nil || client.nativeTkeClient == nil || client.nativeCvmClient == nil || client.nativeVpcClient == nil {
		return Response{Ok: false, ErrorCode: "tencent_sdk_client_missing", Message: "Tencent TKE, CVM, and VPC SDK clients are required.", Retryable: false}
	}
	nodePoolId := strings.TrimSpace(request.Pool.NodePoolId)
	if nodePoolId == "" || request.Pool.CPU == 0 || request.Pool.MemoryGB == 0 || request.Pool.MaxReplicas <= 0 || request.Pool.BaselineReplicas < 0 || request.Pool.TargetReplicas != request.Pool.BaselineReplicas+1 || request.Pool.TargetReplicas > request.Pool.MaxReplicas || int64(len(request.Pool.BeforeMachineNames)) != request.Pool.BaselineReplicas {
		return Response{Ok: false, ErrorCode: "compute_allocation_plan_invalid", Message: "A persisted allocation plan with an exact NodePool ID is required.", Retryable: false}
	}
	pool, describeRequestId, err := client.describeNativeNodePool(nodePoolId)
	if err != nil {
		return sdkErrorResponse("tencent_describe_node_pool_failed", err)
	}
	if failure := mutationNodePoolFailure(pool, request, describeRequestId); failure != nil {
		return *failure
	}
	machineType, cvmApplicable, supportedMachineType := canonicalNativeMachineType(stringValue(pool.Native.MachineType))
	if !supportedMachineType {
		return Response{Ok: false, ErrorCode: "compute_machine_type_unverified", Message: "Tencent NodePool MachineType could not be verified.", ProviderRequestId: describeRequestId, Retryable: false}
	}
	currentReplicas := nativeReplicas(pool)
	if currentReplicas < request.Pool.BaselineReplicas || currentReplicas > request.Pool.TargetReplicas {
		return Response{Ok: false, ErrorCode: "compute_allocation_replica_state_ambiguous", Message: "NodePool replicas do not match the persisted allocation plan.", ProviderRequestId: describeRequestId, Retryable: false}
	}
	scaleRequestId := ""
	if currentReplicas == request.Pool.BaselineReplicas {
		if !allowScale {
			machines, machineRequestID, machineErr := client.describeClusterMachines(nodePoolId)
			if machineErr != nil {
				return sdkErrorResponse("tencent_describe_cluster_machines_failed", machineErr)
			}
			if !exactMachineInventoryMatches(machines, request.Pool.BeforeMachineNames) {
				return Response{Ok: false, ErrorCode: "compute_allocation_baseline_inventory_conflict", Message: "NodePool machine inventory does not exactly match the persisted baseline.", ProviderRequestId: firstNonEmpty(machineRequestID, describeRequestId), Retryable: false}
			}
			present := false
			return Response{
				Ok: true, PoolId: request.Pool.Id, NodePoolId: nodePoolId, Status: "absent", MachinePresent: &present,
				ProviderRequestId: firstNonEmpty(machineRequestID, describeRequestId), CurrentReplicas: currentReplicas,
				MaxReplicas: request.Pool.MaxReplicas, TargetReplicas: request.Pool.TargetReplicas, MutationCount: 0,
				ProviderData: map[string]string{
					"machineType": machineType, "cvmApplicable": strconv.FormatBool(cvmApplicable),
					"describeNodePoolRequestId": describeRequestId, "describeMachinesReadyReqId": machineRequestID,
				},
			}
		}
		scaleRequest := tke2022.NewScaleNodePoolRequest()
		scaleRequest.ClusterId = common.StringPtr(client.clusterId)
		scaleRequest.NodePoolId = common.StringPtr(nodePoolId)
		scaleRequest.Replicas = common.Int64Ptr(request.Pool.TargetReplicas)
		scaleResponse, scaleErr := client.nativeTkeClient.ScaleNodePool(scaleRequest)
		if scaleErr != nil {
			return Response{Ok: false, ErrorCode: "tencent_scale_node_pool_result_unknown", Message: "Tencent ScaleNodePool result is unknown; read back the persisted absolute target.", ProviderRequestId: describeRequestId, Retryable: true}
		}
		if scaleResponse != nil && scaleResponse.Response != nil {
			scaleRequestId = stringValue(scaleResponse.Response.RequestId)
		}
	}
	afterMachines, machineRequestId, err := client.describeClusterMachines(nodePoolId)
	if err != nil {
		return sdkErrorResponse("tencent_describe_cluster_machines_failed", err)
	}
	machine, differenceCode := exactNewReadyMachine(afterMachines, request.Pool.BeforeMachineNames, request.Pool.InstanceType)
	if differenceCode != "" {
		return Response{Ok: false, ErrorCode: differenceCode, Message: differenceCode, ProviderRequestId: firstNonEmpty(machineRequestId, scaleRequestId, describeRequestId), Retryable: differenceCode == "compute_allocation_pending"}
	}
	machineName := stringValue(machine.MachineName)
	privateIp := stringValue(machine.LanIP)
	if !machineResourceShapeMatches(machine, request.Pool) {
		response := computeResourceShapeFailure(firstNonEmpty(machineRequestId, scaleRequestId, describeRequestId))
		return response
	}
	tkeInstance, tkeRequestID, err := client.describeNativeTkeClusterInstanceByMachineName(machineName, nodePoolId)
	if err != nil {
		response := sdkErrorResponse("tencent_describe_tke_instance_failed", err)
		response.ProviderRequestId = firstNonEmpty(tkeRequestID, machineRequestId, scaleRequestId, describeRequestId)
		response.Retryable = true
		return response
	}
	if failure := validateNativeTkeAllocationIdentity(tkeInstance, machine, request); failure != nil {
		failure.ProviderRequestId = firstNonEmpty(tkeRequestID, machineRequestId, scaleRequestId, describeRequestId)
		return *failure
	}
	nativeIdentity := tkeInstance.Native
	if !nativeResourceShapeMatches(nativeIdentity, request.Pool) {
		response := computeResourceShapeFailure(firstNonEmpty(tkeRequestID, machineRequestId, scaleRequestId, describeRequestId))
		return response
	}
	targetVpcID, subnetRequestID, err := client.describeNodePoolSubnetVpc(pool, stringValue(nativeIdentity.SubnetId))
	if err != nil {
		response := sdkErrorResponse("tencent_describe_allocation_subnet_failed", err)
		response.ProviderRequestId = firstNonEmpty(subnetRequestID, tkeRequestID, machineRequestId, scaleRequestId, describeRequestId)
		response.Retryable = true
		return response
	}
	if stringValue(nativeIdentity.VpcId) != targetVpcID {
		return Response{Ok: false, ErrorCode: "compute_tke_network_identity_mismatch", Message: "Tencent TKE native identity does not match the exact NodePool VPC and subnet.", ProviderRequestId: firstNonEmpty(subnetRequestID, tkeRequestID, machineRequestId, scaleRequestId, describeRequestId), Retryable: true}
	}
	instanceId := ""
	publicIp := ""
	instanceIdentitySource := ""
	cvmInstance, cvmRequestId, err := client.describeCvmInstanceByPrivateIp(privateIp)
	if err == nil {
		instanceId = stringValue(cvmInstance.InstanceId)
		publicIp = firstString(cvmInstance.PublicIpAddresses)
		instanceIdentitySource = "cvm"
	} else if errors.Is(err, errCVMInstanceNotFound) {
		return Response{Ok: false, ErrorCode: "compute_cvm_identity_required", Message: "NativeCVM allocation did not resolve to a Tencent CVM instance.", ProviderRequestId: firstNonEmpty(cvmRequestId, machineRequestId, scaleRequestId), Retryable: true}
	} else {
		response := sdkErrorResponse("tencent_describe_cvm_instance_failed", err)
		response.ProviderRequestId = firstNonEmpty(cvmRequestId, machineRequestId, scaleRequestId)
		return response
	}
	if cvmInstance.VirtualPrivateCloud == nil || instanceId != stringValue(nativeIdentity.InstanceId) ||
		!containsString(cvmInstance.PrivateIpAddresses, privateIp) ||
		stringValue(cvmInstance.VirtualPrivateCloud.VpcId) != targetVpcID ||
		stringValue(cvmInstance.VirtualPrivateCloud.SubnetId) != stringValue(nativeIdentity.SubnetId) {
		return Response{Ok: false, ErrorCode: "compute_cvm_identity_mismatch", Message: "Tencent CVM identity does not match the exact TKE Machine, VPC, and subnet.", ProviderRequestId: firstNonEmpty(cvmRequestId, subnetRequestID, tkeRequestID, machineRequestId, scaleRequestId), Retryable: true}
	}
	if stringValue(cvmInstance.InstanceType) != request.Pool.InstanceType {
		return Response{Ok: false, ErrorCode: "compute_instance_type_mismatch", Message: "Tencent NodePool, Machine, Native instance, and CVM instance types must exactly match.", ProviderRequestId: firstNonEmpty(cvmRequestId, tkeRequestID, machineRequestId, scaleRequestId, describeRequestId), Retryable: true}
	}
	if !cvmResourceShapeMatches(cvmInstance, request.Pool) {
		response := computeResourceShapeFailure(firstNonEmpty(cvmRequestId, tkeRequestID, machineRequestId, scaleRequestId, describeRequestId))
		return response
	}
	if !strings.EqualFold(strings.TrimSpace(stringValue(cvmInstance.InstanceState)), "running") {
		return Response{Ok: false, ErrorCode: "compute_cvm_not_ready", Message: "Tencent CVM has not explicitly reported RUNNING.", ProviderRequestId: firstNonEmpty(cvmRequestId, subnetRequestID, tkeRequestID, machineRequestId, scaleRequestId), Retryable: true}
	}
	zone := ""
	if cvmInstance.Placement != nil {
		zone = stringValue(cvmInstance.Placement.Zone)
	}
	chargeType, renewFlag, deadline := stringValue(cvmInstance.InstanceChargeType), stringValue(cvmInstance.RenewFlag), normalizeTencentDeadline(stringValue(cvmInstance.ExpiredTime))
	if strings.TrimSpace(zone) == "" || chargeType != "PREPAID" || renewFlag != "NOTIFY_AND_MANUAL_RENEW" || strings.TrimSpace(deadline) == "" {
		return Response{Ok: false, ErrorCode: "compute_cvm_billing_facts_required", Message: "NativeCVM allocation did not return exact PREPAID manual-renew billing facts.", ProviderRequestId: firstNonEmpty(cvmRequestId, machineRequestId, scaleRequestId), Retryable: true}
	}
	nodeName := kubernetesNodeName(machine)
	if nodeName == "" && machineName == "" {
		return Response{
			Ok:                false,
			ErrorCode:         "compute_allocation_node_identity_required",
			Message:           "Tencent TKE did not return a node identity for this compute allocation.",
			ProviderRequestId: firstNonEmpty(cvmRequestId, machineRequestId, scaleRequestId),
			Retryable:         true,
		}
	}
	return Response{
		Ok:                true,
		OperationId:       "op-create-compute-" + stableSuffix(request.AccountId, request.Allocation.Id, nodePoolId, fmt.Sprintf("%d", request.Pool.TargetReplicas))[:12],
		PoolId:            request.Pool.Id,
		NodePoolId:        nodePoolId,
		InstanceId:        instanceId,
		NodeName:          nodeName,
		PrivateIp:         privateIp,
		PublicIp:          publicIp,
		Status:            "running",
		ProviderRequestId: firstNonEmpty(scaleRequestId, machineRequestId, describeRequestId),
		ProviderData: map[string]string{
			"clusterId":                  client.clusterId,
			"region":                     client.region,
			"describeNodePoolRequestId":  describeRequestId,
			"describeMachinesReadyReqId": machineRequestId,
			"describeCvmRequestId":       cvmRequestId,
			"describeTkeInstanceReqId":   tkeRequestID,
			"describeSubnetRequestId":    subnetRequestID,
			"scaleNodePoolRequestId":     scaleRequestId,
			"instanceType":               request.Pool.InstanceType,
			"machineType":                machineType,
			"cvmApplicable":              strconv.FormatBool(cvmApplicable),
			"cpu":                        strconv.FormatUint(request.Pool.CPU, 10),
			"memoryGb":                   strconv.FormatUint(request.Pool.MemoryGB, 10),
			"replicasBefore":             fmt.Sprintf("%d", request.Pool.BaselineReplicas),
			"replicasAfter":              fmt.Sprintf("%d", request.Pool.TargetReplicas),
			"instanceId":                 instanceId,
			"instanceIdentitySource":     instanceIdentitySource,
			"machineName":                machineName,
			"machineState":               strings.ToLower(strings.TrimSpace(stringValue(machine.MachineState))),
			"tkeInstanceState":           strings.ToLower(strings.TrimSpace(stringValue(tkeInstance.InstanceState))),
			"nodeName":                   nodeName,
			"privateIp":                  privateIp,
			"publicIp":                   publicIp,
			"zone":                       zone,
			"vpcId":                      targetVpcID,
			"subnetId":                   stringValue(nativeIdentity.SubnetId),
			"chargeType":                 chargeType,
			"periodMonths":               "1",
			"renewFlag":                  renewFlag,
			"deadline":                   deadline,
		},
	}
}

func exactMachineInventoryMatches(machines []*tke2022.Machine, expectedNames []string) bool {
	if len(machines) != len(expectedNames) {
		return false
	}
	expected := make(map[string]struct{}, len(expectedNames))
	for _, name := range expectedNames {
		name = strings.TrimSpace(name)
		if name == "" {
			return false
		}
		if _, duplicate := expected[name]; duplicate {
			return false
		}
		expected[name] = struct{}{}
	}
	observed := make(map[string]struct{}, len(machines))
	for _, machine := range machines {
		if machine == nil {
			return false
		}
		name := strings.TrimSpace(stringValue(machine.MachineName))
		if _, allowed := expected[name]; !allowed || name == "" {
			return false
		}
		if _, duplicate := observed[name]; duplicate {
			return false
		}
		observed[name] = struct{}{}
	}
	return len(observed) == len(expected)
}

func (client *tencentSDKClient) ComputeClaimTruth(request Request, _ map[string]string) Response {
	if client == nil || client.nativeTkeClient == nil || client.nativeCvmClient == nil || client.nativeVpcClient == nil {
		return computeClaimTruthFailure("provider_describe")
	}
	if request.Pool.CPU == 0 || request.Pool.MemoryGB == 0 ||
		strings.TrimSpace(request.AccountId) == "" || strings.TrimSpace(request.Zone) == "" || strings.TrimSpace(request.Pool.Id) == "" ||
		strings.TrimSpace(request.Pool.NodePoolId) == "" || strings.TrimSpace(request.Pool.InstanceType) == "" || request.Pool.MaxReplicas <= 0 ||
		request.Pool.BaselineReplicas < 0 || request.Pool.TargetReplicas != request.Pool.BaselineReplicas+1 ||
		int64(len(request.Pool.BeforeMachineNames)) != request.Pool.BaselineReplicas || strings.TrimSpace(request.Allocation.Id) == "" ||
		!strings.HasPrefix(request.Allocation.InstanceId, "ins-") || strings.TrimSpace(request.Allocation.MachineName) == "" ||
		strings.TrimSpace(request.Allocation.NodeName) == "" || strings.TrimSpace(request.Allocation.PrivateIp) == "" ||
		strings.TrimSpace(request.Allocation.Deadline) == "" || !validOwnershipTags(request.Tags) ||
		request.Tags["opl_account_id"] != request.AccountId || request.Tags["opl_resource_id"] != request.Allocation.Id {
		return computeClaimProviderIdentityFailure("compute_claim.request_contract", map[string]any{
			"knownPackage": true, "shapeMatches": true, "accountPresent": true, "zonePresent": true, "poolIdentityPresent": true,
			"capacityPlanValid": true, "allocationIdentityPresent": true, "ownershipTagsValid": true,
		}, map[string]any{
			"knownPackage": strings.TrimSpace(request.PackageId) != "", "shapeMatches": request.Pool.CPU > 0 && request.Pool.MemoryGB > 0,
			"accountPresent": strings.TrimSpace(request.AccountId) != "", "zonePresent": strings.TrimSpace(request.Zone) != "",
			"poolIdentityPresent":       strings.TrimSpace(request.Pool.Id) != "" && strings.TrimSpace(request.Pool.NodePoolId) != "" && strings.TrimSpace(request.Pool.InstanceType) != "",
			"capacityPlanValid":         request.Pool.MaxReplicas > 0 && request.Pool.BaselineReplicas >= 0 && request.Pool.TargetReplicas == request.Pool.BaselineReplicas+1 && int64(len(request.Pool.BeforeMachineNames)) == request.Pool.BaselineReplicas,
			"allocationIdentityPresent": strings.TrimSpace(request.Allocation.Id) != "" && strings.HasPrefix(request.Allocation.InstanceId, "ins-") && strings.TrimSpace(request.Allocation.MachineName) != "" && strings.TrimSpace(request.Allocation.NodeName) != "" && strings.TrimSpace(request.Allocation.PrivateIp) != "" && strings.TrimSpace(request.Allocation.Deadline) != "",
			"ownershipTagsValid":        validOwnershipTags(request.Tags) && request.Tags["opl_account_id"] == request.AccountId && request.Tags["opl_resource_id"] == request.Allocation.Id,
		})
	}
	pool, _, err := client.describeNativeNodePool(request.Pool.NodePoolId)
	if err != nil {
		return computeClaimTruthFailure(computeClaimDescribeReason(err))
	}
	machines, _, err := client.describeClusterMachines(request.Pool.NodePoolId)
	if err != nil {
		return computeClaimTruthFailure(computeClaimDescribeReason(err))
	}
	machine, selectionCode := exactPersistedReadyMachine(machines, request.Allocation.MachineName)
	if selectionCode != "" {
		return computeClaimProviderIdentityFailure("compute_claim.machine_selection", map[string]any{"selectionCode": "", "machineSelected": true}, map[string]any{"selectionCode": selectionCode, "machineSelected": machine != nil})
	}
	if !computeClaimNodePoolMatches(pool, request) {
		return computeClaimProviderIdentityFailure("compute_claim.node_pool_identity", request.Pool, pool)
	}
	machineName, privateIP := stringValue(machine.MachineName), stringValue(machine.LanIP)
	if machineName != request.Allocation.MachineName || kubernetesNodeName(machine) != request.Allocation.NodeName || privateIP != request.Allocation.PrivateIp ||
		!machineResourceShapeMatches(machine, request.Pool) {
		return computeClaimProviderIdentityFailure("compute_claim.machine_identity", map[string]any{
			"machineName": request.Allocation.MachineName, "nodeName": request.Allocation.NodeName, "privateIp": request.Allocation.PrivateIp,
			"instanceType": request.Pool.InstanceType, "cpu": request.Pool.CPU, "memoryGb": request.Pool.MemoryGB,
		}, map[string]any{
			"machineName": machineName, "nodeName": kubernetesNodeName(machine), "privateIp": privateIP,
			"instanceType": stringValue(machine.InstanceType), "cpu": machine.CPU, "memory": machine.Memory,
		})
	}
	tkeInstance, _, err := client.describeNativeTkeClusterInstanceByMachineName(machineName, request.Pool.NodePoolId)
	if err != nil {
		return computeClaimTruthFailure(computeClaimDescribeReason(err))
	}
	if validateNativeTkeAllocationIdentity(tkeInstance, machine, request) != nil || !nativeResourceShapeMatches(tkeInstance.Native, request.Pool) {
		return computeClaimProviderIdentityFailure("compute_claim.tke_instance_identity", map[string]any{
			"machineName": machineName, "nodePoolId": request.Pool.NodePoolId, "nodeName": request.Allocation.NodeName,
			"privateIp": privateIP, "instanceType": request.Pool.InstanceType,
		}, tkeInstance)
	}
	targetVPCID, _, err := client.describeNodePoolSubnetVpc(pool, stringValue(tkeInstance.Native.SubnetId))
	if err != nil {
		return computeClaimTruthFailure(computeClaimDescribeReason(err))
	}
	if stringValue(tkeInstance.Native.VpcId) != targetVPCID {
		return computeClaimProviderIdentityFailure("compute_claim.network_identity", map[string]string{"vpcId": targetVPCID}, map[string]string{"vpcId": stringValue(tkeInstance.Native.VpcId)})
	}
	cvmInstance, _, err := client.describeCvmInstanceByPrivateIp(privateIP)
	if err != nil {
		return computeClaimTruthFailure(computeClaimDescribeReason(err))
	}
	if cvmInstance == nil || cvmInstance.VirtualPrivateCloud == nil || stringValue(cvmInstance.InstanceId) != request.Allocation.InstanceId ||
		!containsString(cvmInstance.PrivateIpAddresses, privateIP) || stringValue(cvmInstance.VirtualPrivateCloud.VpcId) != targetVPCID ||
		stringValue(cvmInstance.VirtualPrivateCloud.SubnetId) != stringValue(tkeInstance.Native.SubnetId) ||
		stringValue(cvmInstance.InstanceType) != request.Pool.InstanceType || !cvmResourceShapeMatches(cvmInstance, request.Pool) ||
		!strings.EqualFold(strings.TrimSpace(stringValue(cvmInstance.InstanceState)), "running") {
		return computeClaimProviderIdentityFailure("compute_claim.cvm_identity", map[string]any{
			"instanceId": request.Allocation.InstanceId, "privateIp": privateIP, "vpcId": targetVPCID,
			"subnetId": stringValue(tkeInstance.Native.SubnetId), "instanceType": request.Pool.InstanceType, "state": "running",
		}, cvmInstance)
	}
	zone := ""
	if cvmInstance.Placement != nil {
		zone = stringValue(cvmInstance.Placement.Zone)
	}
	deadline := normalizeTencentDeadline(stringValue(cvmInstance.ExpiredTime))
	if zone != request.Zone || stringValue(cvmInstance.InstanceChargeType) != "PREPAID" ||
		stringValue(cvmInstance.RenewFlag) != "NOTIFY_AND_MANUAL_RENEW" || deadline == "" || deadline != request.Allocation.Deadline {
		return computeClaimProviderIdentityFailure("compute_claim.cvm_billing", map[string]any{
			"zone": request.Zone, "chargeType": "PREPAID", "renewFlag": "NOTIFY_AND_MANUAL_RENEW", "deadline": request.Allocation.Deadline,
		}, map[string]any{
			"zone": zone, "chargeType": stringValue(cvmInstance.InstanceChargeType), "renewFlag": stringValue(cvmInstance.RenewFlag), "deadline": deadline,
		})
	}
	ownershipState, ok, ownershipPredicate := computeClaimCVMOwnershipState(cvmInstance, request)
	if !ok {
		actualTags, _ := cvmOwnershipTags(cvmInstance)
		return computeClaimProviderIdentityFailure(ownershipPredicate, map[string]any{
			"instanceNames": []string{machineName, request.Allocation.Id}, "tags": request.Tags,
		}, map[string]any{"instanceName": stringValue(cvmInstance.InstanceName), "tags": actualTags})
	}
	return Response{
		Ok: true, Status: "proven", PoolId: request.Pool.Id, NodePoolId: request.Pool.NodePoolId,
		InstanceId: request.Allocation.InstanceId, NodeName: request.Allocation.NodeName, PrivateIp: privateIP,
		PublicIp: firstString(cvmInstance.PublicIpAddresses), InstanceType: request.Pool.InstanceType, CVMStatus: "RUNNING", TKEStatus: "RUNNING",
		ProviderData: map[string]string{
			"machineName": machineName, "zone": zone, "chargeType": "PREPAID", "periodMonths": "1",
			"renewFlag": "NOTIFY_AND_MANUAL_RENEW", "deadline": deadline, "cvmOwnershipState": ownershipState,
		},
	}
}

func computeClaimNodePoolMatches(pool *tke2022.NodePool, request Request) bool {
	return pool != nil && pool.Native != nil && pool.Native.Scaling != nil && isCVMNativeNodePool(pool) &&
		strings.EqualFold(strings.TrimSpace(stringValue(pool.LifeState)), "running") && matchesCapacityNodePool(pool, request) &&
		pool.Native.Replicas != nil && *pool.Native.Replicas == request.Pool.TargetReplicas &&
		pool.Native.ReadyReplicas != nil && *pool.Native.ReadyReplicas == request.Pool.TargetReplicas &&
		pool.Native.Scaling.MinReplicas != nil && *pool.Native.Scaling.MinReplicas == 0 && pool.Native.Scaling.MaxReplicas != nil &&
		*pool.Native.Scaling.MaxReplicas == request.Pool.MaxReplicas && pool.Native.EnableAutoscaling != nil && !*pool.Native.EnableAutoscaling &&
		pool.Native.AutoRepair != nil && !*pool.Native.AutoRepair && len(pool.Native.InstanceTypes) == 1 &&
		stringValue(pool.Native.InstanceTypes[0]) == request.Pool.InstanceType && len(pool.Native.SubnetIds) > 0
}

func computeClaimCVMOwnershipState(instance *cvm2017.Instance, request Request) (string, bool, string) {
	tags, err := cvmOwnershipTags(instance)
	if err != nil {
		return "", false, "compute_claim.cvm_ownership_shape"
	}
	targetCount := 0
	for _, key := range cbsOwnershipTagKeys {
		actual := strings.TrimSpace(tags[key])
		if actual == "" {
			continue
		}
		if actual != request.Tags[key] {
			return "", false, computeClaimOwnershipPredicate(key)
		}
		targetCount++
	}
	instanceName := strings.TrimSpace(stringValue(instance.InstanceName))
	if instanceName == request.Allocation.Id && targetCount == len(cbsOwnershipTagKeys) {
		return "target_owned", true, ""
	}
	return "recoverable", true, ""
}

func computeClaimOwnershipPredicate(key string) string {
	switch key {
	case "opl_account_id", "opl_workspace_id", "opl_resource_id", "opl_operation_id":
		return "compute_claim.cvm_ownership." + key
	default:
		return "compute_claim.cvm_ownership_shape"
	}
}

type cvmOwnershipTarget struct {
	InstanceID          string
	CurrentName         string
	ProviderCurrentName string
	TargetName          string
	Tags                map[string]string
}

type cvmOwnershipConvergence struct {
	Attempted       int
	Confirmed       int
	Unknown         int
	ProviderData    map[string]string
	ProviderRequest string
}

const cvmOwnershipSnapshotField = "cvm_ownership_snapshot"

func (client *tencentSDKClient) convergeCVMOwnership(target cvmOwnershipTarget) (cvmOwnershipConvergence, *Response) {
	result := cvmOwnershipConvergence{ProviderData: map[string]string{}}
	if client == nil || client.nativeCvmClient == nil {
		failure := computeClaimConvergenceFailure("provider_describe", "cvm_pre_read", "client_unavailable", result, nil)
		return result, &failure
	}
	instance, requestID, err := client.describeCvmInstanceByID(target.InstanceID)
	result.ProviderRequest = requestID
	if err != nil || instance == nil || stringValue(instance.InstanceId) != target.InstanceID {
		missing := []string(nil)
		if err == nil {
			missing = []string{"instance"}
		}
		failure := computeClaimConvergenceFailure(computeClaimDescribeReason(err), "cvm_pre_read", computeClaimProviderErrorClass(err), result, missing)
		return result, &failure
	}
	states, tags, conflict, err := classifyCVMOwnership(instance, target)
	if err != nil {
		failure := computeClaimConvergenceFailure("identity_mismatch", "cvm_pre_read", "malformed_readback", result, nil)
		return result, &failure
	}
	currentName := strings.TrimSpace(stringValue(instance.InstanceName))
	if target.CurrentName == "" {
		target.CurrentName = currentName
		states, tags, conflict, err = classifyCVMOwnership(instance, target)
		if err != nil {
			failure := computeClaimConvergenceFailure("identity_mismatch", "cvm_pre_read", "malformed_readback", result, nil)
			return result, &failure
		}
	}
	if conflict != "" {
		failure := computeClaimConvergenceFailure("identity_mismatch", "cvm_conflict_check", "ownership_conflict", result, []string{conflict})
		return result, &failure
	}
	missingTags := []string{}
	for _, key := range cbsOwnershipTagKeys {
		if states[key] == "missing" {
			missingTags = append(missingTags, key)
		}
	}
	if len(missingTags) > 0 && client.nativeTagClient == nil {
		failure := computeClaimConvergenceFailure("provider_describe", "cvm_mutation_precondition", "client_unavailable", result, missingTags)
		return result, &failure
	}
	fields := append([]string{"instance_name"}, cbsOwnershipTagKeys[:]...)
	for _, field := range fields {
		if states[field] == "target_owned" {
			continue
		}
		var mutationErr error
		result.Attempted++
		stage := "cvm_tag_readback"
		if field == "instance_name" {
			stage = "cvm_rename_readback"
			modify := cvm2017.NewModifyInstancesAttributeRequest()
			modify.InstanceIds = []*string{common.StringPtr(target.InstanceID)}
			modify.InstanceName = common.StringPtr(target.TargetName)
			modified, modifyErr := client.nativeCvmClient.ModifyInstancesAttribute(modify)
			mutationErr = modifyErr
			if modified != nil && modified.Response != nil {
				result.ProviderData["modifyInstanceRequestId"] = stringValue(modified.Response.RequestId)
			}
		} else {
			_, attached := tags[field]
			tagRequestID, tagErr := client.nativeTagClient.SetCVMTag(target.InstanceID, field, target.Tags[field], attached)
			mutationErr = tagErr
			if tagRequestID != "" {
				result.ProviderData["tagRequestId:"+field] = tagRequestID
			}
		}
		state, readbackStates, readbackTags, readbackRequestID, readbackErr := client.readCVMOwnershipField(target, field)
		result.ProviderRequest = firstNonEmpty(readbackRequestID, result.ProviderRequest)
		if state == "target_owned" {
			result.Confirmed++
			states, tags = readbackStates, readbackTags
			continue
		}
		if state == "unknown" {
			result.Unknown++
		}
		reason, providerClass := "provider_describe", "readback_mismatch"
		failureErr := readbackErr
		if mutationErr != nil {
			failureErr = mutationErr
		}
		if state == "conflict" {
			reason, providerClass = "identity_mismatch", "ownership_conflict"
		} else if failureErr != nil {
			reason, providerClass = computeClaimDescribeReason(failureErr), computeClaimProviderErrorClass(failureErr)
		}
		failure := computeClaimConvergenceFailure(reason, stage, providerClass, result, []string{field})
		return result, &failure
	}
	state, finalStates, finalTags, readbackRequestID, readbackErr := client.readCVMOwnershipField(target, cvmOwnershipSnapshotField)
	result.ProviderRequest = firstNonEmpty(readbackRequestID, result.ProviderRequest)
	if state != "target_owned" {
		reason, providerClass := "provider_describe", "readback_mismatch"
		if state == "unknown" {
			result.Unknown, result.Confirmed = result.Attempted, 0
		}
		if state == "conflict" {
			reason, providerClass = "identity_mismatch", "ownership_conflict"
		} else if readbackErr != nil {
			reason, providerClass = computeClaimDescribeReason(readbackErr), computeClaimProviderErrorClass(readbackErr)
		}
		failure := computeClaimConvergenceFailure(reason, "cvm_final_readback", providerClass, result, missingCVMOwnershipFields(finalStates))
		return result, &failure
	}
	tags = finalTags
	for _, key := range cbsOwnershipTagKeys {
		result.ProviderData[key] = tags[key]
	}
	return result, nil
}

func (client *tencentSDKClient) readCVMOwnershipField(target cvmOwnershipTarget, field string) (string, map[string]string, map[string]string, string, error) {
	ctx := client.convergenceContext
	if ctx == nil {
		ctx = context.Background()
	}
	var requestID string
	lastState := "unknown"
	var lastErr error
	var lastStates map[string]string
	var lastTags map[string]string
	for attempt := 0; attempt < 6; attempt++ {
		if attempt > 0 && client.convergenceWait != nil {
			if err := client.convergenceWait(ctx, attempt); err != nil {
				return "unknown", nil, nil, requestID, err
			}
		}
		instance, currentRequestID, err := client.describeCvmInstanceByID(target.InstanceID)
		requestID = firstNonEmpty(currentRequestID, requestID)
		if err != nil || instance == nil || stringValue(instance.InstanceId) != target.InstanceID {
			lastState, lastErr = "unknown", err
			if lastErr == nil {
				lastErr = fmt.Errorf("Tencent CVM readback instance is missing")
			}
			continue
		}
		states, tags, conflict, classifyErr := classifyCVMOwnership(instance, target)
		if classifyErr != nil {
			return "conflict", nil, nil, requestID, classifyErr
		}
		if conflict != "" {
			return "conflict", states, tags, requestID, nil
		}
		lastStates, lastTags, lastErr = states, tags, nil
		lastState = cvmOwnershipReadbackState(states, field)
		if lastState == "target_owned" {
			return lastState, states, tags, requestID, nil
		}
	}
	return lastState, lastStates, lastTags, requestID, lastErr
}

func cvmOwnershipReadbackState(states map[string]string, field string) string {
	if field != cvmOwnershipSnapshotField {
		return states[field]
	}
	if len(missingCVMOwnershipFields(states)) == 0 {
		return "target_owned"
	}
	return "missing"
}

func missingCVMOwnershipFields(states map[string]string) []string {
	fields := append([]string{"instance_name"}, cbsOwnershipTagKeys[:]...)
	missing := make([]string, 0, len(fields))
	for _, field := range fields {
		if states[field] != "target_owned" {
			missing = append(missing, field)
		}
	}
	return missing
}

func classifyCVMOwnership(instance *cvm2017.Instance, target cvmOwnershipTarget) (map[string]string, map[string]string, string, error) {
	states := map[string]string{}
	if instance == nil || stringValue(instance.InstanceId) != target.InstanceID {
		return states, nil, "instance", fmt.Errorf("Tencent CVM instance identity is missing")
	}
	tags, err := cvmOwnershipTags(instance)
	if err != nil {
		return states, nil, "ownership_tags", err
	}
	actualName := strings.TrimSpace(stringValue(instance.InstanceName))
	switch {
	case actualName == target.TargetName:
		states["instance_name"] = "target_owned"
	case actualName == "" || actualName == target.CurrentName ||
		(target.ProviderCurrentName != "" && actualName == target.ProviderCurrentName):
		states["instance_name"] = "missing"
	default:
		return states, tags, "instance_name", nil
	}
	for legacyKey, canonicalKey := range cvmOwnershipTagAliases {
		actual := strings.TrimSpace(tags[legacyKey])
		if actual != "" && actual != target.Tags[canonicalKey] {
			return states, tags, legacyKey, nil
		}
	}
	for _, key := range cbsOwnershipTagKeys {
		actual := strings.TrimSpace(tags[key])
		switch {
		case actual == target.Tags[key]:
			states[key] = "target_owned"
		case actual == "":
			states[key] = "missing"
		default:
			return states, tags, key, nil
		}
	}
	return states, tags, "", nil
}

func computeClaimProviderErrorClass(err error) string {
	if err == nil {
		return "readback_mismatch"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}
	if computeClaimDescribeReason(err) == "iam_rbac" {
		return "iam_rbac"
	}
	return "provider_error"
}

func computeClaimConvergenceFailure(reason, stage, providerClass string, evidence cvmOwnershipConvergence, missing []string) Response {
	response := computeClaimTruthFailure(reason)
	response.MutationCount = evidence.Attempted
	response.FailureStage = stage
	response.ProviderErrorClass = providerClass
	response.ProviderRequestId = evidence.ProviderRequest
	response.MutationEvidence = &MutationEvidence{
		Attempted: evidence.Attempted,
		Confirmed: evidence.Confirmed,
		Unknown:   evidence.Unknown,
		Missing:   append([]string(nil), missing...),
	}
	return response
}

func computeClaimDescribeReason(err error) string {
	if err == nil {
		return "provider_describe"
	}
	// Preserve this gate's existing rejection reasons through the readback wrapper.
	var readbackErr *nodePoolReadbackError
	if errors.As(err, &readbackErr) && readbackErr.cause != nil {
		err = readbackErr.cause
	}
	value := strings.ToLower(err.Error())
	var sdkErr *tcerrors.TencentCloudSDKError
	if errors.As(err, &sdkErr) && sdkErr != nil {
		value += " " + strings.ToLower(sdkErr.Code)
	}
	for _, marker := range []string{"authfailure", "unauthorized", "forbidden", "permission", "accessdenied", "invalidcredential", "secretid"} {
		if strings.Contains(value, marker) {
			return "iam_rbac"
		}
	}
	return "provider_describe"
}

func computeClaimTruthFailure(reason string) Response {
	switch reason {
	case "provider_describe", "iam_rbac", "multiple_candidate", "identity_mismatch":
	default:
		reason = "provider_describe"
	}
	return Response{Ok: false, ErrorCode: reason, Message: "Compute claim truth could not be proven.", Retryable: false}
}

func computeClaimProviderIdentityFailure(predicate string, expected, actual any) Response {
	if !validComputeClaimProviderIdentityPredicate(predicate) {
		predicate = "compute_claim.provider_response_identity"
	}
	response := computeClaimTruthFailure("identity_mismatch")
	expectedDigest, expectedOK := computeClaimProviderIdentityDigest(expected)
	actualDigest, actualOK := computeClaimProviderIdentityDigest(actual)
	if !expectedOK || !actualOK || expectedDigest == actualDigest {
		return response
	}
	response.FailureStage = "cvm_pre_read"
	response.ProviderErrorClass = "readback_mismatch"
	response.ProviderIdentityFailure = &ProviderIdentityFailure{
		Predicate: predicate, ExpectedDigest: expectedDigest, ActualDigest: actualDigest,
	}
	return response
}

func computeClaimProviderIdentityDigest(value any) (string, bool) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", false
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), true
}

func validComputeClaimProviderIdentityPredicate(value string) bool {
	switch value {
	case "compute_claim.request_contract", "compute_claim.machine_selection", "compute_claim.node_pool_identity",
		"compute_claim.machine_identity", "compute_claim.tke_instance_identity", "compute_claim.network_identity",
		"compute_claim.cvm_identity", "compute_claim.cvm_billing", "compute_claim.cvm_ownership_shape",
		"compute_claim.cvm_ownership.instance_name", "compute_claim.cvm_ownership.opl_account_id",
		"compute_claim.cvm_ownership.opl_workspace_id", "compute_claim.cvm_ownership.opl_resource_id",
		"compute_claim.cvm_ownership.opl_operation_id", "compute_claim.provider_response_identity",
		"compute_claim.kubernetes_node_identity":
		return true
	default:
		return false
	}
}

func (client *tencentSDKClient) ClaimComputeMachine(request Request, _ map[string]string) Response {
	truth := client.ComputeClaimTruth(request, nil)
	if !truth.Ok {
		return truth
	}
	converged, failure := client.convergeCVMOwnership(cvmOwnershipTarget{
		InstanceID: request.Allocation.InstanceId,
		TargetName: request.Allocation.Id, Tags: request.Tags,
	})
	if failure != nil {
		return *failure
	}
	converged.ProviderData["cvmOwnershipState"] = "target_owned"
	return Response{
		Ok: true, Status: "claimed", InstanceId: request.Allocation.InstanceId, ProviderRequestId: converged.ProviderRequest,
		MutationCount: converged.Attempted, ProviderData: converged.ProviderData,
		MutationEvidence: &MutationEvidence{Attempted: converged.Attempted, Confirmed: converged.Confirmed},
	}
}

func exactNewReadyMachine(after []*tke2022.Machine, before []string, instanceType string) (*tke2022.Machine, string) {
	beforeSet := map[string]bool{}
	for _, name := range before {
		name = strings.TrimSpace(name)
		if name == "" || beforeSet[name] {
			return nil, "compute_allocation_plan_invalid"
		}
		beforeSet[name] = true
	}
	seenBefore := map[string]bool{}
	newMachines := []*tke2022.Machine{}
	for _, machine := range after {
		if machine == nil || strings.TrimSpace(stringValue(machine.MachineName)) == "" {
			return nil, "compute_allocation_machine_difference_incomplete"
		}
		name := stringValue(machine.MachineName)
		if beforeSet[name] {
			seenBefore[name] = true
			continue
		}
		newMachines = append(newMachines, machine)
	}
	if len(seenBefore) != len(beforeSet) {
		return nil, "compute_allocation_machine_difference_incomplete"
	}
	if len(newMachines) == 0 {
		return nil, "compute_allocation_pending"
	}
	if len(newMachines) != 1 {
		return nil, "compute_allocation_machine_difference_ambiguous"
	}
	machine := newMachines[0]
	state := strings.ToLower(strings.TrimSpace(stringValue(machine.MachineState)))
	if !explicitReadyState(state) {
		return nil, "compute_allocation_pending"
	}
	if stringValue(machine.InstanceType) != instanceType {
		return nil, "compute_allocation_machine_identity_mismatch"
	}
	return machine, ""
}

func exactPersistedReadyMachine(machines []*tke2022.Machine, machineName string) (*tke2022.Machine, string) {
	if machineName == "" || machineName != strings.TrimSpace(machineName) {
		return nil, "compute_claim_persisted_machine_invalid"
	}
	matches := []*tke2022.Machine{}
	for _, machine := range machines {
		if machine == nil || strings.TrimSpace(stringValue(machine.MachineName)) == "" {
			return nil, "compute_claim_machine_inventory_incomplete"
		}
		if stringValue(machine.MachineName) == machineName {
			matches = append(matches, machine)
		}
	}
	if len(matches) == 0 {
		return nil, "compute_claim_persisted_machine_missing"
	}
	if len(matches) != 1 {
		return nil, "compute_claim_machine_inventory_ambiguous"
	}
	if !explicitReadyState(stringValue(matches[0].MachineState)) {
		return nil, "compute_claim_persisted_machine_not_ready"
	}
	return matches[0], ""
}

func explicitReadyState(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "ready", "running":
		return true
	default:
		return false
	}
}

func validateNativeTkeAllocationIdentity(instance *tke2022.Instance, machine *tke2022.Machine, request Request) *Response {
	if failure := validateNativeTkeAllocationBinding(instance, machine, request); failure != nil {
		return failure
	}
	if !strings.EqualFold(strings.TrimSpace(stringValue(instance.InstanceState)), "running") || !explicitReadyState(stringValue(instance.Native.MachineState)) {
		return &Response{Ok: false, ErrorCode: "compute_tke_identity_mismatch", Message: "Tencent TKE instance does not match the exact Ready NativeCVM Machine identity.", Retryable: true}
	}
	return nil
}

func validateNativeTkeAllocationBinding(instance *tke2022.Instance, machine *tke2022.Machine, request Request) *Response {
	machineName := stringValue(machine.MachineName)
	privateIP := stringValue(machine.LanIP)
	if instance == nil {
		return &Response{Ok: false, ErrorCode: "compute_tke_identity_mismatch", Message: "Tencent TKE instance does not match the exact Ready NativeCVM Machine identity.", Retryable: true}
	}
	native := instance.Native
	valid := instance != nil && native != nil && stringValue(instance.InstanceId) == machineName &&
		stringValue(instance.NodePoolId) == request.Pool.NodePoolId && strings.EqualFold(stringValue(instance.NodeType), "Native") &&
		stringValue(instance.LanIP) == privateIP &&
		stringValue(native.MachineName) == machineName &&
		stringValue(native.LanIp) == privateIP && stringValue(native.InstanceType) == request.Pool.InstanceType &&
		strings.EqualFold(stringValue(native.MachineType), "NativeCVM") && strings.HasPrefix(stringValue(native.InstanceId), "ins-") &&
		strings.TrimSpace(stringValue(native.VpcId)) != "" && strings.TrimSpace(stringValue(native.SubnetId)) != ""
	if valid {
		return nil
	}
	return &Response{Ok: false, ErrorCode: "compute_tke_identity_mismatch", Message: "Tencent TKE instance does not match the exact Ready NativeCVM Machine identity.", Retryable: true}
}

func (client *tencentSDKClient) describeNodePoolSubnetVpc(pool *tke2022.NodePool, subnetID string) (string, string, error) {
	if client == nil || client.nativeVpcClient == nil || pool == nil || pool.Native == nil || strings.TrimSpace(subnetID) == "" {
		return "", "", fmt.Errorf("exact NodePool subnet identity is required")
	}
	allowed := false
	seen := map[string]bool{}
	for _, raw := range pool.Native.SubnetIds {
		candidate := strings.TrimSpace(stringValue(raw))
		if candidate == "" || seen[candidate] {
			return "", "", fmt.Errorf("NodePool subnet inventory is invalid")
		}
		seen[candidate] = true
		allowed = allowed || candidate == subnetID
	}
	if !allowed {
		return "", "", fmt.Errorf("native instance subnet is not registered on the exact NodePool")
	}
	request := vpc2017.NewDescribeSubnetsRequest()
	request.SubnetIds = []*string{common.StringPtr(subnetID)}
	response, err := client.nativeVpcClient.DescribeSubnets(request)
	if err != nil {
		return "", "", err
	}
	if response == nil || response.Response == nil {
		return "", "", fmt.Errorf("Tencent VPC DescribeSubnets response is missing")
	}
	requestID := stringValue(response.Response.RequestId)
	if response.Response.TotalCount == nil || *response.Response.TotalCount != 1 || len(response.Response.SubnetSet) != 1 || response.Response.SubnetSet[0] == nil ||
		stringValue(response.Response.SubnetSet[0].SubnetId) != subnetID || strings.TrimSpace(stringValue(response.Response.SubnetSet[0].VpcId)) == "" {
		return "", requestID, fmt.Errorf("Tencent VPC subnet identity is missing or ambiguous")
	}
	return stringValue(response.Response.SubnetSet[0].VpcId), requestID, nil
}

func (client *tencentSDKClient) TagComputeMachine(request Request, _ map[string]string) Response {
	if client == nil || client.nativeCvmClient == nil {
		return Response{Ok: false, ErrorCode: "tencent_sdk_client_missing", Message: "Tencent CVM SDK client is missing.", Retryable: false}
	}
	instanceID := strings.TrimSpace(request.Allocation.InstanceId)
	resourceID := strings.TrimSpace(request.Tags["opl_resource_id"])
	if instanceID == "" || resourceID == "" || len(resourceID) > 60 || !validOwnershipTags(request.Tags) {
		return Response{Ok: false, ErrorCode: "compute_machine_identity_required", Message: "Machine instance id and a resource id of at most 60 characters are required.", Retryable: false}
	}
	if !strings.HasPrefix(instanceID, "ins-") {
		return Response{Ok: false, ErrorCode: "compute_machine_identity_unverified", Message: "Tencent machine identity source is unsupported.", Retryable: false}
	}
	converged, failure := client.convergeCVMOwnership(cvmOwnershipTarget{
		InstanceID: instanceID, CurrentName: request.Allocation.MachineName,
		ProviderCurrentName: nativeCVMInstanceName(client.clusterId, request.Allocation.MachineName),
		TargetName:          resourceID, Tags: request.Tags,
	})
	if failure != nil {
		return tagComputeMachineFailure(*failure)
	}
	return Response{
		Ok: true, InstanceId: instanceID, Status: "tagged", ProviderRequestId: converged.ProviderRequest,
		ProviderData: converged.ProviderData, MutationCount: converged.Attempted,
		MutationEvidence: &MutationEvidence{Attempted: converged.Attempted, Confirmed: converged.Confirmed},
	}
}

func nativeCVMInstanceName(clusterID, machineName string) string {
	clusterID = strings.TrimSpace(clusterID)
	machineName = strings.TrimSpace(machineName)
	if clusterID == "" || machineName == "" {
		return ""
	}
	return clusterID + "_" + machineName
}

func tagComputeMachineFailure(response Response) Response {
	switch response.FailureStage {
	case "cvm_pre_read":
		if response.MutationEvidence != nil && len(response.MutationEvidence.Missing) == 1 && response.MutationEvidence.Missing[0] == "instance" {
			response.ErrorCode = "compute_machine_identity_unverified"
		} else {
			response.ErrorCode = "tencent_verify_compute_machine_failed"
		}
	case "cvm_conflict_check":
		response.ErrorCode = "compute_machine_tag_unverified"
	case "cvm_mutation_precondition":
		response.ErrorCode = "tencent_sdk_client_missing"
	case "cvm_final_readback":
		response.ErrorCode = "tencent_verify_compute_machine_tag_failed"
	case "cvm_rename_readback", "cvm_tag_readback":
		if response.MutationEvidence != nil && response.MutationEvidence.Unknown > 0 {
			response.ErrorCode = "tencent_verify_compute_machine_tag_failed"
		} else {
			response.ErrorCode = "tencent_tag_compute_machine_failed"
		}
	default:
		response.ErrorCode = "tencent_tag_compute_machine_failed"
	}
	response.Message = "Tencent CVM ownership convergence could not be confirmed."
	response.Retryable = response.ErrorCode != "tencent_sdk_client_missing"
	return response
}

func validOwnershipTags(tags map[string]string) bool {
	for _, key := range cbsOwnershipTagKeys {
		if strings.TrimSpace(tags[key]) == "" {
			return false
		}
	}
	return true
}

func cvmOwnershipTags(instance *cvm2017.Instance) (map[string]string, error) {
	tags := map[string]string{}
	if instance == nil {
		return tags, fmt.Errorf("Tencent CVM instance is missing")
	}
	for _, tag := range instance.Tags {
		if tag == nil {
			continue
		}
		key := strings.TrimSpace(stringValue(tag.Key))
		if key == "" {
			continue
		}
		if _, duplicate := tags[key]; duplicate {
			return nil, fmt.Errorf("Tencent CVM ownership tag is duplicated")
		}
		tags[key] = stringValue(tag.Value)
	}
	return tags, nil
}

func (client *tencentSDKClient) withComputeDeleteEvidence(response Response, request Request, machineType string, cvmApplicable bool, describeNodePoolRequestID, modifySelfProvisioningRequestID, verifyMachineRequestID, describeCVMRequestID string, machinePresent *bool, cvmStatus string) Response {
	if response.ProviderData == nil {
		response.ProviderData = map[string]string{}
	}
	response.InstanceId = request.Allocation.InstanceId
	response.NodeName = request.Allocation.NodeName
	response.NodePoolId = request.Pool.NodePoolId
	response.ProviderData["clusterId"] = client.clusterId
	response.ProviderData["region"] = client.region
	response.ProviderData["deleteMethod"] = "DeleteClusterMachines"
	response.ProviderData["scaleDown"] = "true"
	response.ProviderData["deleteMode"] = "terminate"
	response.ProviderData["describeNodePoolRequestId"] = describeNodePoolRequestID
	response.ProviderData["modifySelfProvisioningReqId"] = modifySelfProvisioningRequestID
	response.ProviderData["verifyMachineDeletedReqId"] = verifyMachineRequestID
	response.ProviderData["machineType"] = machineType
	response.ProviderData["cvmApplicable"] = strconv.FormatBool(cvmApplicable)
	if machinePresent != nil {
		response.MachinePresent = machinePresent
		response.ProviderData["machinePresent"] = strconv.FormatBool(*machinePresent)
		if !*machinePresent {
			response.TKEStatus = "NOT_FOUND"
			response.ProviderData["tkeStatus"] = "NOT_FOUND"
		}
	}
	if cvmApplicable {
		response.ProviderData["describeCvmRequestId"] = describeCVMRequestID
		if cvmStatus != "" {
			response.CVMStatus = cvmStatus
			response.ProviderData["cvmStatus"] = cvmStatus
		}
	}
	return response
}

func (client *tencentSDKClient) withComputeDeleteAttemptEvidence(response Response, request Request, machineType string, cvmApplicable bool, describeNodePoolRequestID, modifySelfProvisioningRequestID, verifyMachineRequestID, describeCVMRequestID string, machinePresent *bool, cvmStatus string) Response {
	if response.MutationCount != 1 {
		return response
	}
	return client.withComputeDeleteEvidence(response, request, machineType, cvmApplicable, describeNodePoolRequestID, modifySelfProvisioningRequestID, verifyMachineRequestID, describeCVMRequestID, machinePresent, cvmStatus)
}

func persistedComputeDestroyMachineType(allocation ComputeAllocationInput) (string, bool, *Response) {
	rawMachineType := strings.TrimSpace(allocation.MachineType)
	if rawMachineType == "" {
		return "", false, &Response{Ok: false, ErrorCode: "compute_destroy_machine_type_required", Message: "Persisted ComputeAllocation machineType is required for compute deletion.", Retryable: false, MutationCount: 0}
	}
	machineType, cvmApplicable, supported := canonicalNativeMachineType(rawMachineType)
	if !supported || allocation.MachineType != rawMachineType || machineType != rawMachineType {
		return "", false, &Response{Ok: false, ErrorCode: "compute_destroy_machine_type_invalid", Message: "Persisted ComputeAllocation machineType must be exactly NativeCVM, Native, or CXM.", Retryable: false, MutationCount: 0}
	}
	return machineType, cvmApplicable, nil
}

func (client *tencentSDKClient) DestroyComputeAllocation(request Request, env map[string]string) Response {
	persistedMachineType, persistedCVMApplicable, machineTypeFailure := persistedComputeDestroyMachineType(request.Allocation)
	if machineTypeFailure != nil {
		return *machineTypeFailure
	}
	if client == nil || client.nativeTkeClient == nil {
		return Response{Ok: false, ErrorCode: "tencent_sdk_client_missing", Message: "Tencent TKE SDK client is missing.", Retryable: false}
	}
	if identityFailure := client.computeDestroyProviderIdentityFailure(request); identityFailure != nil {
		return *identityFailure
	}
	if strings.TrimSpace(request.Pool.NodePoolId) == "" {
		return Response{Ok: false, ErrorCode: "node_pool_id_required", Message: "ComputePool nodePoolId is required.", Retryable: false}
	}
	if strings.TrimSpace(request.Allocation.MachineName) == "" {
		return Response{Ok: false, ErrorCode: "compute_allocation_machine_identity_required", Message: "ComputeAllocation machineName is required to destroy a dedicated Tencent node.", Retryable: false}
	}
	if strings.TrimSpace(request.Allocation.NodeName) == "" || strings.TrimSpace(request.Allocation.PrivateIp) == "" || strings.TrimSpace(request.Allocation.InstanceId) == "" {
		return Response{Ok: false, ErrorCode: "compute_machine_identity_unverified", Message: "ComputeAllocation nodeName, privateIp, and instanceId are required to destroy a dedicated Tencent node.", Retryable: false}
	}
	describeRequestId := ""
	modifySelfProvisioningRequestId := ""
	pool, requestId, err := client.describeNativeNodePool(request.Pool.NodePoolId)
	if err != nil {
		response := sdkErrorResponse("tencent_describe_node_pool_failed", err)
		response.ProviderData = map[string]string{
			"clusterId":   client.clusterId,
			"region":      client.region,
			"nodePoolId":  request.Pool.NodePoolId,
			"machineName": request.Allocation.MachineName,
			"nodeName":    request.Allocation.NodeName,
			"instanceId":  request.Allocation.InstanceId,
		}
		return response
	}
	describeRequestId = requestId
	if strings.TrimSpace(describeRequestId) == "" {
		return Response{Ok: false, ErrorCode: "compute_machine_identity_unverified", Message: "Tencent NodePool readback requestId is required before compute deletion.", Retryable: false}
	}
	machineType := ""
	if pool != nil && pool.Native != nil {
		machineType = stringValue(pool.Native.MachineType)
	}
	machineType, cvmApplicable, supportedMachineType := canonicalNativeMachineType(machineType)
	if supportedMachineType && (machineType != persistedMachineType || cvmApplicable != persistedCVMApplicable) {
		return Response{
			Ok: false, ErrorCode: "compute_destroy_machine_type_mismatch", Message: "Persisted ComputeAllocation machineType does not match the live Tencent NodePool.",
			ProviderRequestId: describeRequestId, Retryable: false, MutationCount: 0,
			ProviderData: map[string]string{
				"persistedMachineType": persistedMachineType, "persistedCvmApplicable": strconv.FormatBool(persistedCVMApplicable),
				"liveMachineType": machineType, "liveCvmApplicable": strconv.FormatBool(cvmApplicable),
			},
		}
	}
	if !supportedMachineType || cvmApplicable && !validTencentCVMID(request.Allocation.InstanceId) {
		return Response{
			Ok: false, ErrorCode: "compute_machine_identity_unverified", Message: "Tencent MachineType and instance identity are inconsistent.",
			ProviderRequestId: describeRequestId, Retryable: false,
			ProviderData: map[string]string{"machineType": machineType, "cvmApplicable": strconv.FormatBool(cvmApplicable)},
		}
	}
	if machineType == "NativeCVM" {
		// Prepaid retirement has two separately fenced provider actions. The
		// generic path must not terminate or reconfigure their shared NodePool.
		chargeType := stringValue(pool.Native.InstanceChargeType)
		if chargeType == "PREPAID" {
			return Response{
				Ok: false, ErrorCode: "compute_prepaid_retirement_required",
				Message:    "Prepaid compute requires the separate detach_prepaid_compute_allocation and retire_prepaid_compute_allocation actions.",
				InstanceId: request.Allocation.InstanceId, ProviderRequestId: describeRequestId, Retryable: false, MutationCount: 0,
			}
		}
		if chargeType != "POSTPAID_BY_HOUR" {
			return Response{
				Ok: false, ErrorCode: "compute_destroy_charge_type_unverified",
				Message:    "Generic NativeCVM destruction requires an explicit live POSTPAID_BY_HOUR billing type.",
				InstanceId: request.Allocation.InstanceId, ProviderRequestId: describeRequestId, Retryable: false, MutationCount: 0,
			}
		}
	}
	deleteAttempts := intFromEnv(env, "TENCENT_TKE_NODE_DELETE_ATTEMPTS", 30)
	if deleteAttempts < 1 {
		deleteAttempts = 1
	}
	deleteDelayMs := nonNegativeIntFromEnv(env, "TENCENT_TKE_NODE_DELETE_DELAY_MS", 10000)
	machinePresent, identityRequestId, err := client.verifyDestroyMachineOwnership(pool, request)
	if err != nil {
		return Response{
			Ok: false, ErrorCode: "compute_machine_identity_unverified", Message: err.Error(),
			ProviderRequestId: firstNonEmpty(identityRequestId, describeRequestId), Retryable: false,
			ProviderData: map[string]string{
				"clusterId": client.clusterId, "region": client.region, "nodePoolId": request.Pool.NodePoolId,
				"machineName": request.Allocation.MachineName, "nodeName": request.Allocation.NodeName,
				"privateIp": request.Allocation.PrivateIp, "instanceId": request.Allocation.InstanceId,
			},
		}
	}
	if strings.TrimSpace(identityRequestId) == "" {
		return Response{
			Ok: false, ErrorCode: "compute_machine_identity_unverified", Message: "Tencent pre-delete identity readback requestId is required.",
			ProviderRequestId: describeRequestId, Retryable: false,
			ProviderData: map[string]string{
				"clusterId": client.clusterId, "region": client.region, "nodePoolId": request.Pool.NodePoolId,
				"machineName": request.Allocation.MachineName, "nodeName": request.Allocation.NodeName,
				"privateIp": request.Allocation.PrivateIp, "instanceId": request.Allocation.InstanceId,
			},
		}
	}
	if machinePresent && nativeSelfProvisioningEnabled(pool) {
		requestId, err := client.disableNativeNodePoolSelfProvisioning(request.Pool.NodePoolId)
		if err != nil {
			response := sdkErrorResponse("tencent_disable_node_pool_self_provisioning_failed", err)
			response.ProviderRequestId = describeRequestId
			response.ProviderData = map[string]string{
				"clusterId":                    client.clusterId,
				"region":                       client.region,
				"nodePoolId":                   request.Pool.NodePoolId,
				"machineName":                  request.Allocation.MachineName,
				"nodeName":                     request.Allocation.NodeName,
				"instanceId":                   request.Allocation.InstanceId,
				"describeNodePoolRequestId":    describeRequestId,
				"selfProvisioningDisableError": "true",
			}
			return response
		}
		modifySelfProvisioningRequestId = requestId
	}
	providerRequestId := ""
	mutationCount := 0
	var deleteErr error
	if machinePresent {
		deleteRequest := tke2022.NewDeleteClusterMachinesRequest()
		deleteRequest.ClusterId = common.StringPtr(client.clusterId)
		deleteRequest.MachineNames = []*string{common.StringPtr(request.Allocation.MachineName)}
		deleteRequest.EnableScaleDown = common.BoolPtr(true)
		deleteRequest.InstanceDeleteMode = common.StringPtr("terminate")
		deleteResponse, err := client.nativeTkeClient.DeleteClusterMachines(deleteRequest)
		deleteErr = err
		mutationCount = 1
		if deleteResponse != nil && deleteResponse.Response != nil {
			providerRequestId = stringValue(deleteResponse.Response.RequestId)
		}
	}
	deleteVerifiedRequestID := identityRequestId
	if machinePresent {
		for attempt := 1; attempt <= deleteAttempts; attempt++ {
			machine, verifyRequestID, verifyErr := client.readDestroyMachine(request)
			deleteVerifiedRequestID = verifyRequestID
			if verifyErr != nil {
				response := sdkErrorResponse("tencent_verify_compute_machine_delete_failed", verifyErr)
				response.ProviderRequestId = firstNonEmpty(providerRequestId, verifyRequestID, describeRequestId)
				response.MutationCount = mutationCount
				return client.withComputeDeleteAttemptEvidence(response, request, machineType, cvmApplicable, describeRequestId, modifySelfProvisioningRequestId, verifyRequestID, "", nil, "")
			}
			if machine == nil {
				machinePresent = false
				break
			}
			if attempt == deleteAttempts {
				message := "Tencent TKE still reports the deleted Machine."
				if deleteErr != nil {
					message = deleteErr.Error()
				}
				response := Response{Ok: false, ErrorCode: "compute_machine_delete_unverified", Message: message, ProviderRequestId: firstNonEmpty(providerRequestId, verifyRequestID, describeRequestId), Retryable: true, MutationCount: mutationCount}
				return client.withComputeDeleteAttemptEvidence(response, request, machineType, cvmApplicable, describeRequestId, modifySelfProvisioningRequestId, verifyRequestID, "", &machinePresent, "")
			}
			if deleteDelayMs > 0 {
				time.Sleep(time.Duration(deleteDelayMs) * time.Millisecond)
			}
		}
	}
	cvmVerifiedRequestID := ""
	cvmStatus := ""
	if cvmApplicable {
		for attempt := 1; attempt <= deleteAttempts; attempt++ {
			instance, verifyRequestID, verifyErr := client.describeCvmInstanceByID(request.Allocation.InstanceId)
			cvmVerifiedRequestID = verifyRequestID
			if verifyErr != nil {
				response := sdkErrorResponse("tencent_verify_cvm_delete_failed", verifyErr)
				response.ProviderRequestId = firstNonEmpty(providerRequestId, cvmVerifiedRequestID, deleteVerifiedRequestID, describeRequestId)
				response.MutationCount = mutationCount
				return client.withComputeDeleteAttemptEvidence(response, request, machineType, cvmApplicable, describeRequestId, modifySelfProvisioningRequestId, deleteVerifiedRequestID, cvmVerifiedRequestID, &machinePresent, "")
			}
			if instance == nil {
				cvmStatus = "NOT_FOUND"
				break
			}
			if stringValue(instance.InstanceId) != request.Allocation.InstanceId {
				response := Response{
					Ok: false, ErrorCode: "compute_cvm_delete_identity_mismatch", Message: "Tencent CVM delete readback returned a different instance identity.",
					InstanceId: request.Allocation.InstanceId, CVMStatus: stringValue(instance.InstanceState), ProviderRequestId: firstNonEmpty(providerRequestId, cvmVerifiedRequestID, deleteVerifiedRequestID, describeRequestId), Retryable: false, MutationCount: mutationCount,
					ProviderData: map[string]string{"verifyMachineDeletedReqId": deleteVerifiedRequestID, "describeCvmRequestId": cvmVerifiedRequestID},
				}
				return client.withComputeDeleteAttemptEvidence(response, request, machineType, cvmApplicable, describeRequestId, modifySelfProvisioningRequestId, deleteVerifiedRequestID, cvmVerifiedRequestID, &machinePresent, stringValue(instance.InstanceState))
			}
			if attempt == deleteAttempts {
				response := Response{
					Ok: false, ErrorCode: "compute_cvm_delete_unverified", Message: "Tencent CVM still reports the terminated instance.",
					InstanceId: request.Allocation.InstanceId, CVMStatus: stringValue(instance.InstanceState), ProviderRequestId: firstNonEmpty(providerRequestId, cvmVerifiedRequestID, deleteVerifiedRequestID, describeRequestId), Retryable: true, MutationCount: mutationCount,
					ProviderData: map[string]string{"verifyMachineDeletedReqId": deleteVerifiedRequestID, "describeCvmRequestId": cvmVerifiedRequestID},
				}
				return client.withComputeDeleteAttemptEvidence(response, request, machineType, cvmApplicable, describeRequestId, modifySelfProvisioningRequestId, deleteVerifiedRequestID, cvmVerifiedRequestID, &machinePresent, stringValue(instance.InstanceState))
			}
			if deleteDelayMs > 0 {
				time.Sleep(time.Duration(deleteDelayMs) * time.Millisecond)
			}
		}
	}
	response := Response{
		Ok:                true,
		OperationId:       "op-destroy-compute-" + stableSuffix(request.AccountId, request.Allocation.Id, request.Pool.NodePoolId, request.Allocation.NodeName)[:12],
		Status:            "external_deleted",
		ProviderRequestId: firstNonEmpty(providerRequestId, cvmVerifiedRequestID, deleteVerifiedRequestID, describeRequestId),
		MutationCount:     mutationCount,
	}
	return client.withComputeDeleteEvidence(response, request, machineType, cvmApplicable, describeRequestId, modifySelfProvisioningRequestId, deleteVerifiedRequestID, cvmVerifiedRequestID, &machinePresent, cvmStatus)
}

func (client *tencentSDKClient) ReadComputeDestroyStatus(request Request, _ map[string]string) Response {
	machineType, cvmApplicable, machineTypeFailure := persistedComputeDestroyMachineType(request.Allocation)
	if machineTypeFailure != nil {
		return *machineTypeFailure
	}
	if client == nil || client.nativeTkeClient == nil || cvmApplicable && client.nativeCvmClient == nil {
		return Response{Ok: false, ErrorCode: "tencent_sdk_client_missing", Message: "The Tencent clients required by the persisted machineType are missing.", Retryable: false, MutationCount: 0}
	}
	if identityFailure := client.computeDestroyProviderIdentityFailure(request); identityFailure != nil {
		return *identityFailure
	}
	if strings.TrimSpace(request.Pool.NodePoolId) == "" || strings.TrimSpace(request.Allocation.Id) == "" || strings.TrimSpace(request.Allocation.MachineName) == "" ||
		strings.TrimSpace(request.Allocation.NodeName) == "" || strings.TrimSpace(request.Allocation.PrivateIp) == "" || strings.TrimSpace(request.Allocation.InstanceId) == "" ||
		cvmApplicable && !validTencentCVMID(request.Allocation.InstanceId) {
		return Response{Ok: false, ErrorCode: "compute_destroy_status_identity_required", Message: "Exact persisted compute identity is required for delete status readback.", Retryable: false, MutationCount: 0}
	}

	machine, machineRequestID, machineErr := client.readDestroyMachine(request)
	if machineErr != nil || strings.TrimSpace(machineRequestID) == "" {
		if machineErr == nil {
			machineErr = errors.New("Tencent TKE delete status readback requestId is missing")
		}
		response := sdkErrorResponse("tencent_read_compute_destroy_machine_failed", machineErr)
		response.ProviderRequestId = machineRequestID
		response.MutationCount = 0
		response.ProviderData["machineType"] = machineType
		response.ProviderData["cvmApplicable"] = strconv.FormatBool(cvmApplicable)
		return response
	}
	machinePresent := machine != nil
	tkeStatus := "NOT_FOUND"
	if machinePresent {
		tkeStatus = strings.ToUpper(strings.TrimSpace(stringValue(machine.MachineState)))
		if tkeStatus == "" {
			tkeStatus = "UNKNOWN"
		}
	}
	providerData := map[string]string{
		"clusterId":                  client.clusterId,
		"region":                     client.region,
		"nodePoolId":                 request.Pool.NodePoolId,
		"machineName":                request.Allocation.MachineName,
		"nodeName":                   request.Allocation.NodeName,
		"privateIp":                  request.Allocation.PrivateIp,
		"machineType":                machineType,
		"cvmApplicable":              strconv.FormatBool(cvmApplicable),
		"machinePresent":             strconv.FormatBool(machinePresent),
		"tkeStatus":                  tkeStatus,
		"describeClusterMachinesReq": machineRequestID,
	}
	if machinePresent {
		providerData["syncResult"] = "found"
		return Response{
			Ok: true, Status: "present", InstanceId: request.Allocation.InstanceId, MachinePresent: &machinePresent, TKEStatus: tkeStatus,
			ProviderRequestId: machineRequestID, ProviderData: providerData, MutationCount: 0,
		}
	}
	if !cvmApplicable {
		providerData["syncResult"] = "missing"
		return Response{
			Ok: true, Status: "external_deleted", InstanceId: request.Allocation.InstanceId, MachinePresent: &machinePresent, TKEStatus: "NOT_FOUND",
			ProviderRequestId: machineRequestID, ProviderData: providerData, MutationCount: 0,
		}
	}

	instance, cvmRequestID, cvmErr := client.describeCvmInstanceByID(request.Allocation.InstanceId)
	if cvmErr != nil || strings.TrimSpace(cvmRequestID) == "" {
		if cvmErr == nil {
			cvmErr = errors.New("Tencent CVM delete status readback requestId is missing")
		}
		response := sdkErrorResponse("tencent_read_compute_destroy_cvm_failed", cvmErr)
		response.ProviderRequestId = firstNonEmpty(cvmRequestID, machineRequestID)
		response.InstanceId = request.Allocation.InstanceId
		response.MachinePresent = &machinePresent
		response.TKEStatus = "NOT_FOUND"
		response.MutationCount = 0
		for key, value := range providerData {
			response.ProviderData[key] = value
		}
		if cvmRequestID != "" {
			response.ProviderData["describeCvmRequestId"] = cvmRequestID
		}
		return response
	}
	providerData["describeCvmRequestId"] = cvmRequestID
	if instance != nil {
		cvmStatus := strings.ToUpper(strings.TrimSpace(stringValue(instance.InstanceState)))
		if cvmStatus == "" {
			cvmStatus = "UNKNOWN"
		}
		providerData["syncResult"] = "found"
		providerData["cvmStatus"] = cvmStatus
		providerData["isolatedSource"] = stringValue(instance.IsolatedSource)
		return Response{
			Ok: true, Status: "present", InstanceId: request.Allocation.InstanceId, MachinePresent: &machinePresent, TKEStatus: "NOT_FOUND", CVMStatus: cvmStatus,
			ProviderRequestId: firstNonEmpty(cvmRequestID, machineRequestID), ProviderData: providerData, MutationCount: 0,
		}
	}
	providerData["syncResult"] = "missing"
	providerData["cvmStatus"] = "NOT_FOUND"
	return Response{
		Ok: true, Status: "external_deleted", InstanceId: request.Allocation.InstanceId, MachinePresent: &machinePresent, TKEStatus: "NOT_FOUND", CVMStatus: "NOT_FOUND",
		ProviderRequestId: firstNonEmpty(cvmRequestID, machineRequestID), ProviderData: providerData, MutationCount: 0,
	}
}

func (client *tencentSDKClient) computeDestroyProviderIdentityFailure(request Request) *Response {
	if request.Pool.ClusterId == "" || request.Region == "" || request.Pool.ClusterId != client.clusterId || request.Region != client.region {
		return &Response{
			Ok: false, ErrorCode: "compute_provider_config_identity_mismatch",
			Message:   "Persisted compute cluster and region must exactly match the active Tencent provider configuration.",
			Retryable: false, MutationCount: 0,
		}
	}
	return nil
}

func (client *tencentSDKClient) classifyMissingComputeMachine(request Request) (string, string, error) {
	const base = "compute_provider_partial_identity_machine_missing"
	instances, requestID, err := client.describeClusterInstancePages(nil)
	if err != nil {
		return "", requestID, err
	}
	matches := []*tke2022.Instance{}
	for _, instance := range instances {
		if instance == nil || !strings.EqualFold(stringValue(instance.NodeType), "Native") {
			continue
		}
		native := instance.Native
		nativeMachineName, nativeInstanceID, nativePrivateIP := "", "", ""
		if native != nil {
			nativeMachineName = stringValue(native.MachineName)
			nativeInstanceID = stringValue(native.InstanceId)
			nativePrivateIP = stringValue(native.LanIp)
		}
		machineMatches := stringValue(instance.InstanceId) == request.Allocation.MachineName || nativeMachineName == request.Allocation.MachineName
		cvmMatches := nativeInstanceID == request.Allocation.InstanceId
		privateIPMatches := stringValue(instance.LanIP) == request.Allocation.PrivateIp || nativePrivateIP == request.Allocation.PrivateIp
		if machineMatches || cvmMatches || privateIPMatches {
			matches = append(matches, instance)
		}
	}
	if len(matches) == 0 {
		return base + "_tke_instance_missing", requestID, nil
	}
	if len(matches) != 1 {
		return base + "_tke_instance_ambiguous", requestID, nil
	}
	instance := matches[0]
	native := instance.Native
	if native == nil || stringValue(instance.InstanceId) != request.Allocation.MachineName ||
		stringValue(native.MachineName) != request.Allocation.MachineName || stringValue(native.InstanceId) != request.Allocation.InstanceId ||
		stringValue(instance.LanIP) != request.Allocation.PrivateIp || stringValue(native.LanIp) != request.Allocation.PrivateIp {
		return base + "_tke_instance_identity_mismatch", requestID, nil
	}
	if stringValue(instance.NodePoolId) != request.Pool.NodePoolId {
		return base + "_tke_instance_wrong_pool", requestID, nil
	}
	return base + "_machine_inventory_missing", requestID, nil
}

func (client *tencentSDKClient) SyncComputeAllocation(request Request, _ map[string]string) (result Response) {
	var legacyReadinessFailure *Response
	defer func() {
		if legacyReadinessFailure != nil {
			if result.Observation == nil {
				result.Observation = &contracts.ResourceObservation{State: contracts.ResourceObservedUnknown, ReasonCode: result.ErrorCode}
			}
			legacyReadinessFailure.Observation = result.Observation
			result = *legacyReadinessFailure
		}
	}()
	if client == nil || client.nativeTkeClient == nil || client.nativeCvmClient == nil {
		return Response{Ok: false, ErrorCode: "tencent_sdk_client_missing", Message: "Tencent TKE and CVM SDK clients are required.", Retryable: false}
	}
	if strings.TrimSpace(request.Pool.InstanceType) == "" || request.Pool.CPU == 0 || request.Pool.MemoryGB == 0 {
		return Response{Ok: false, ErrorCode: "compute_resource_shape_required", Message: "The exact package instance type, CPU, and memory are required for compute sync.", Retryable: false}
	}
	if strings.TrimSpace(request.AccountId) == "" || strings.TrimSpace(request.PackageId) == "" || strings.TrimSpace(request.Pool.Id) == "" || strings.TrimSpace(request.Pool.NodePoolId) == "" || strings.TrimSpace(request.Allocation.Id) == "" || !strings.HasPrefix(request.Allocation.InstanceId, "ins-") || strings.TrimSpace(request.Zone) == "" || !validOwnershipTags(request.Tags) || request.Tags["opl_account_id"] != request.AccountId || request.Tags["opl_resource_id"] != request.Allocation.Id {
		return Response{Ok: false, ErrorCode: "compute_sync_identity_required", Message: "Exact compute account, resource, CVM instance, Zone, and ownership tags are required for compute sync.", Retryable: false}
	}
	machine, requestId, err := client.readDestroyMachine(request)
	if err != nil {
		response := sdkErrorResponse("tencent_describe_cluster_machines_failed", err)
		response.ProviderData = map[string]string{"nodePoolId": request.Pool.NodePoolId, "machineName": request.Allocation.MachineName, "nodeName": request.Allocation.NodeName, "privateIp": request.Allocation.PrivateIp}
		return response
	}
	cvm := client.computeRenewalReadback(request)
	if !cvm.Ok {
		if cvm.ErrorCode != "compute_instance_type_mismatch" && cvm.ErrorCode != "compute_resource_shape_mismatch" {
			cvm.ErrorCode = "tencent_sync_cvm_readback_failed"
		}
		return cvm
	}
	if machine == nil && cvm.Status == "external_deleted" {
		return Response{
			Ok:                true,
			OperationId:       "op-sync-compute-" + stableSuffix(request.AccountId, request.Allocation.Id, request.Pool.NodePoolId, request.Allocation.MachineName, request.Allocation.PrivateIp)[:12],
			PoolId:            request.Pool.Id,
			NodePoolId:        request.Pool.NodePoolId,
			InstanceId:        request.Allocation.InstanceId,
			NodeName:          request.Allocation.NodeName,
			PrivateIp:         request.Allocation.PrivateIp,
			PublicIp:          request.Allocation.PublicIp,
			Status:            "external_deleted",
			Observation:       &contracts.ResourceObservation{Available: true, State: contracts.ResourceObservedAbsent},
			CVMStatus:         "NOT_FOUND",
			TKEStatus:         "NOT_FOUND",
			ProviderRequestId: firstNonEmpty(cvm.ProviderRequestId, requestId),
			ProviderData: map[string]string{
				"clusterId":                  client.clusterId,
				"region":                     client.region,
				"nodePoolId":                 request.Pool.NodePoolId,
				"machineName":                request.Allocation.MachineName,
				"nodeName":                   request.Allocation.NodeName,
				"privateIp":                  request.Allocation.PrivateIp,
				"syncResult":                 "missing",
				"cvmStatus":                  "NOT_FOUND",
				"tkeStatus":                  "NOT_FOUND",
				"describeClusterMachinesReq": requestId,
				"describeCvmRequestId":       cvm.ProviderData["describeCvmRequestId"],
			},
		}
	}
	if machine == nil || cvm.Status == "external_deleted" {
		code := "compute_provider_partial_identity_machine_missing"
		diagnosticRequestID := ""
		var observation *contracts.ResourceObservation
		if machine != nil {
			code = "compute_provider_partial_identity_cvm_missing"
		} else {
			var diagnosticErr error
			code, diagnosticRequestID, diagnosticErr = client.classifyMissingComputeMachine(request)
			if diagnosticErr != nil {
				failure := sdkErrorResponse("tencent_describe_tke_instance_failed", diagnosticErr)
				code = failure.ErrorCode
				diagnosticRequestID = firstNonEmpty(failure.ProviderRequestId, diagnosticRequestID)
				failure.ProviderData["providerErrorMessage"] = code
				if providerCode := safeTencentProviderErrorCode(diagnosticErr); providerCode != "" {
					failure.ProviderData["providerErrorCode"] = providerCode
				}
				for key, value := range failure.ProviderData {
					cvm.ProviderData[key] = value
				}
			}
			// CVM ownership is verified above even when its TKE binding is gone.
			observation = computeCVMObservation(request, cvm)
			observation.ReasonCode = code
		}
		return Response{
			Ok: false, ErrorCode: code, Message: "Tencent compute identity is only partially present.",
			Observation: observation, InstanceId: cvm.InstanceId, CVMStatus: cvm.CVMStatus, ProviderData: cvm.ProviderData,
			ProviderRequestId: firstNonEmpty(diagnosticRequestID, cvm.ProviderRequestId, requestId), Retryable: true,
		}
	}
	// Retained Machine/CVM absence and owned orphan CVMs are observable after
	// pool retirement. A live allocation still needs the original pool checks.
	pool, poolRequestID, err := client.describeNativeNodePool(request.Pool.NodePoolId)
	if err != nil {
		response := sdkErrorResponse("tencent_describe_node_pool_failed", err)
		if poolRequestID != "" {
			response.ProviderRequestId = poolRequestID
		}
		return response
	}
	if failure := readbackNodePoolFailure(pool, request, poolRequestID); failure != nil {
		return *failure
	}
	privateIP := firstNonEmpty(stringValue(machine.LanIP), request.Allocation.PrivateIp)
	nodeName := firstNonEmpty(kubernetesNodeName(machine), request.Allocation.NodeName)
	machineName := firstNonEmpty(stringValue(machine.MachineName), request.Allocation.MachineName)
	status := strings.ToLower(strings.TrimSpace(stringValue(machine.MachineState)))
	if !explicitReadyState(status) {
		legacyReadinessFailure = &Response{Ok: false, ErrorCode: "compute_machine_state_unready", Message: "Tencent Machine has not explicitly reported Ready or Running.", ProviderRequestId: firstNonEmpty(cvm.ProviderRequestId, requestId, poolRequestID), Retryable: true}
	}
	if !machineResourceShapeMatches(machine, request.Pool) {
		response := computeResourceShapeFailure(firstNonEmpty(cvm.ProviderRequestId, requestId, poolRequestID))
		return response
	}
	tkeInstance, tkeRequestID, err := client.describeNativeTkeClusterInstanceByMachineName(machineName, request.Pool.NodePoolId)
	if err != nil {
		response := sdkErrorResponse("tencent_describe_tke_instance_failed", err)
		response.ProviderRequestId = firstNonEmpty(tkeRequestID, cvm.ProviderRequestId, requestId, poolRequestID)
		response.Retryable = true
		return response
	}
	if failure := validateNativeTkeAllocationBinding(tkeInstance, machine, request); failure != nil {
		failure.ProviderRequestId = firstNonEmpty(tkeRequestID, cvm.ProviderRequestId, requestId, poolRequestID)
		return *failure
	}
	if failure := validateNativeTkeAllocationIdentity(tkeInstance, machine, request); failure != nil && legacyReadinessFailure == nil {
		failure.ProviderRequestId = firstNonEmpty(tkeRequestID, cvm.ProviderRequestId, requestId, poolRequestID)
		legacyReadinessFailure = failure
	}
	if !nativeResourceShapeMatches(tkeInstance.Native, request.Pool) {
		response := computeResourceShapeFailure(firstNonEmpty(tkeRequestID, cvm.ProviderRequestId, requestId, poolRequestID))
		return response
	}
	if !strings.EqualFold(strings.TrimSpace(cvm.CVMStatus), "running") && legacyReadinessFailure == nil {
		legacyReadinessFailure = &Response{Ok: false, ErrorCode: "compute_cvm_not_ready", Message: "Tencent CVM has not explicitly reported RUNNING.", ProviderRequestId: firstNonEmpty(cvm.ProviderRequestId, requestId), Retryable: true}
	}
	if stringValue(machine.InstanceType) != request.Pool.InstanceType || cvm.ProviderData["instanceType"] != request.Pool.InstanceType {
		cvm.ProviderData["tkeInstanceType"] = stringValue(machine.InstanceType)
		return Response{Ok: false, ErrorCode: "compute_instance_type_mismatch", Message: "Tencent TKE and CVM instance types must exactly match the requested package.", InstanceId: request.Allocation.InstanceId, NodeName: nodeName, PrivateIp: privateIP, InstanceType: firstNonEmpty(cvm.ProviderData["instanceType"], stringValue(machine.InstanceType)), ProviderRequestId: firstNonEmpty(cvm.ProviderRequestId, requestId), ProviderData: cvm.ProviderData, Retryable: true}
	}
	if cvm.ProviderData["privateIp"] != privateIP {
		return Response{Ok: false, ErrorCode: "compute_cvm_identity_mismatch", Message: "Tencent CVM and TKE machine do not share the requested private IP.", ProviderRequestId: firstNonEmpty(cvm.ProviderRequestId, requestId), Retryable: true}
	}
	providerData := cvm.ProviderData
	providerData["clusterId"] = client.clusterId
	providerData["region"] = client.region
	providerData["nodePoolId"] = request.Pool.NodePoolId
	providerData["machineName"] = machineName
	providerData["nodeName"] = nodeName
	providerData["privateIp"] = privateIP
	providerData["syncResult"] = "found"
	providerData["describeClusterMachinesReq"] = requestId
	providerData["describeNodePoolRequestId"] = poolRequestID
	providerData["describeTkeInstanceReqId"] = tkeRequestID
	providerData["cpu"] = strconv.FormatUint(request.Pool.CPU, 10)
	providerData["memoryGb"] = strconv.FormatUint(request.Pool.MemoryGB, 10)
	observation := computeCVMObservation(request, cvm)
	if stringValue(tkeInstance.Native.InstanceId) != request.Allocation.InstanceId {
		observation = &contracts.ResourceObservation{State: contracts.ResourceObservedUnknown, ReasonCode: "compute_cvm_identity_mismatch"}
	}
	return Response{Observation: observation,
		Ok:                true,
		OperationId:       "op-sync-compute-" + stableSuffix(request.AccountId, request.Allocation.Id, request.Pool.NodePoolId, machineName, privateIP)[:12],
		PoolId:            request.Pool.Id,
		NodePoolId:        request.Pool.NodePoolId,
		InstanceId:        request.Allocation.InstanceId,
		NodeName:          nodeName,
		PrivateIp:         privateIP,
		PublicIp:          request.Allocation.PublicIp,
		InstanceType:      request.Pool.InstanceType,
		Status:            status,
		CVMStatus:         cvm.CVMStatus,
		TKEStatus:         strings.ToUpper(status),
		ProviderRequestId: firstNonEmpty(cvm.ProviderRequestId, requestId),
		ProviderData:      providerData,
	}
}

func computeCVMObservation(request Request, cvm Response) *contracts.ResourceObservation {
	observation := &contracts.ResourceObservation{State: contracts.ResourceObservedUnknown, ReasonCode: "provider_state_unrecognized"}
	switch strings.ToUpper(strings.TrimSpace(cvm.CVMStatus)) {
	case "RUNNING":
		observation.State = contracts.ResourceObservedRunning
	case "STOPPED":
		observation.State = contracts.ResourceObservedStopped
	case "SHUTDOWN":
		observation.State = contracts.ResourceObservedPendingDeletion
	case "TERMINATING":
		observation.State = contracts.ResourceObservedDeleting
	case "STARTING", "STOPPING", "REBOOTING", "PENDING", "CREATING":
		observation.State = contracts.ResourceObservedPending
	}
	if observation.State != contracts.ResourceObservedUnknown {
		observation.Available, observation.ReasonCode = true, ""
		observation.ProviderID, observation.PackageOrSpec, observation.Zone = request.Allocation.InstanceId, request.Pool.InstanceType, request.Zone
		observation.ExpiresAt = cvm.ProviderData["deadline"]
	}
	return observation
}

func (client *tencentSDKClient) ProviderTruth(request Request, _ map[string]string) Response {
	if client == nil || client.nativeTkeClient == nil || client.nativeCvmClient == nil || client.nativeCbsClient == nil {
		return Response{Ok: false, ErrorCode: "tencent_sdk_client_missing", Message: "Tencent TKE, CVM, and CBS SDK clients are required.", Retryable: false}
	}
	for field, value := range map[string]string{
		"accountId": request.AccountId, "resourceId": request.Allocation.Id, "clusterId": request.Pool.ClusterId,
		"nodePoolId": request.Pool.NodePoolId, "instanceType": request.Pool.InstanceType, "machineName": request.Allocation.MachineName,
		"instanceId": request.Allocation.InstanceId, "privateIp": request.Allocation.PrivateIp, "storageVolumeId": request.StorageVolumeId,
	} {
		if strings.TrimSpace(value) == "" {
			return Response{Ok: false, ErrorCode: "provider_truth_identity_required", Message: field + " is required for exact provider truth.", Retryable: false}
		}
	}
	if request.Pool.ClusterId != client.clusterId {
		return Response{Ok: false, ErrorCode: "provider_truth_cluster_mismatch", Message: "The supplied cluster does not match the configured cluster.", Retryable: false}
	}
	if !strings.HasPrefix(request.Allocation.InstanceId, "ins-") {
		return Response{Ok: false, ErrorCode: "provider_truth_cvm_instance_required", Message: "A Tencent CVM ins-* instance ID is required.", Retryable: false}
	}
	if !strings.HasPrefix(request.StorageVolumeId, "disk-") {
		return Response{Ok: false, ErrorCode: "provider_truth_cbs_volume_required", Message: "A Tencent CBS disk-* volume ID is required.", Retryable: false}
	}
	request.Storage.Id = request.StorageVolumeId
	if request.Tags["opl_account_id"] != request.AccountId || !validCBSReadbackInput(request.Storage, request.Tags) {
		return Response{Ok: false, ErrorCode: "provider_truth_cbs_ownership_required", Message: "Exact CBS ownership tags and storage facts are required for provider truth.", Retryable: false}
	}
	if !validOwnershipTags(request.ComputeTags) || request.ComputeTags["opl_account_id"] != request.AccountId || request.ComputeTags["opl_workspace_id"] != request.Tags["opl_workspace_id"] || request.ComputeTags["opl_resource_id"] != request.Allocation.Id {
		return Response{Ok: false, ErrorCode: "provider_truth_compute_ownership_required", Message: "Exact CVM ownership tags are required for provider truth.", Retryable: false}
	}
	storagePresent, cbsStatus, cbsRequestID, cbsFacts, err := client.cbsVolumeTruth(request.Storage, request.Tags)
	if err != nil {
		response := sdkErrorResponse("tencent_provider_truth_cbs_probe_failed", err)
		response.ProviderRequestId = firstNonEmpty(response.ProviderRequestId, cbsRequestID)
		return response
	}
	providerData := map[string]string{
		"resourceId": request.Allocation.Id, "clusterId": request.Pool.ClusterId,
		"nodePoolId": request.Pool.NodePoolId, "machineName": request.Allocation.MachineName,
		"storageVolumeId": request.StorageVolumeId, "storagePresent": strconv.FormatBool(storagePresent), "cbsStatus": cbsStatus,
		"describeCbsRequestId": cbsRequestID,
	}
	for key, value := range cbsFacts {
		providerData[key] = value
	}
	withKnownStorage := func(response Response) Response {
		response.StoragePresent = &storagePresent
		response.CBSStatus = cbsStatus
		response.ProviderRequestId = firstNonEmpty(response.ProviderRequestId, cbsRequestID)
		for key, value := range response.ProviderData {
			providerData[key] = value
		}
		response.ProviderData = providerData
		return response
	}

	pool, poolRequestID, err := client.describeNativeNodePool(request.Pool.NodePoolId)
	if err != nil {
		response := sdkErrorResponse("tencent_provider_truth_node_pool_failed", err)
		response.ProviderRequestId = firstNonEmpty(response.ProviderRequestId, poolRequestID)
		return withKnownStorage(response)
	}
	if !isCVMNativeNodePool(pool) {
		return withKnownStorage(Response{Ok: false, ErrorCode: "tencent_cvm_node_pool_required", Message: "Provider truth requires a one-month PREPAID NativeCVM node pool with manual renewal.", ProviderRequestId: poolRequestID, Retryable: false})
	}

	machines, machineRequestID, err := client.describeClusterMachines(request.Pool.NodePoolId)
	if err != nil {
		response := sdkErrorResponse("tencent_provider_truth_machine_probe_failed", err)
		response.ProviderRequestId = firstNonEmpty(response.ProviderRequestId, machineRequestID)
		return withKnownStorage(response)
	}
	var machine *tke2022.Machine
	for _, candidate := range machines {
		if candidate != nil && stringValue(candidate.MachineName) == request.Allocation.MachineName {
			if machine != nil {
				return withKnownStorage(Response{Ok: false, ErrorCode: "provider_truth_machine_ambiguous", Message: "The supplied machine name is not unique in the exact node pool.", ProviderRequestId: machineRequestID, Retryable: false})
			}
			machine = candidate
		}
	}
	if machine != nil && stringValue(machine.LanIP) != request.Allocation.PrivateIp {
		return withKnownStorage(Response{Ok: false, ErrorCode: "provider_truth_machine_ip_mismatch", Message: "The supplied machine and private IP do not identify the same TKE machine.", ProviderRequestId: machineRequestID, Retryable: false})
	}

	tkeInstance, tkeRequestID, tkeErr := client.describeTkeClusterInstanceByPrivateIp(request.Allocation.PrivateIp, request.Pool.NodePoolId)
	if tkeErr != nil && !errors.Is(tkeErr, errTKEInstanceNotFound) {
		response := sdkErrorResponse("tencent_provider_truth_tke_instance_probe_failed", tkeErr)
		response.ProviderRequestId = firstNonEmpty(response.ProviderRequestId, tkeRequestID)
		return withKnownStorage(response)
	}
	cvmInstance, cvmRequestID, err := client.describeCvmInstanceByID(request.Allocation.InstanceId)
	if err != nil {
		response := sdkErrorResponse("tencent_provider_truth_cvm_probe_failed", err)
		response.ProviderRequestId = firstNonEmpty(response.ProviderRequestId, cvmRequestID)
		return withKnownStorage(response)
	}

	machinePresent, tkePresent, cvmPresent := machine != nil, tkeInstance != nil, cvmInstance != nil
	providerData["machinePresent"] = strconv.FormatBool(machinePresent)
	providerData["describeNodePoolRequestId"] = poolRequestID
	providerData["describeMachineRequestId"] = machineRequestID
	providerData["describeTkeRequestId"] = tkeRequestID
	providerData["describeCvmRequestId"] = cvmRequestID
	if !storagePresent && !machinePresent && !tkePresent && !cvmPresent {
		providerData["tkeStatus"] = "NOT_FOUND"
		providerData["cvmStatus"] = "NOT_FOUND"
		return Response{Ok: true, PoolId: request.Pool.Id, NodePoolId: request.Pool.NodePoolId, InstanceId: request.Allocation.InstanceId, PrivateIp: request.Allocation.PrivateIp, MachinePresent: &machinePresent, StoragePresent: &storagePresent, CVMStatus: "NOT_FOUND", TKEStatus: "NOT_FOUND", CBSStatus: cbsStatus, Status: "absent", MachineType: "NativeCVM", ProviderRequestId: firstNonEmpty(cbsRequestID, cvmRequestID, tkeRequestID, machineRequestID), ProviderData: providerData}
	}
	if !machinePresent && !tkePresent && !cvmPresent {
		providerData["tkeStatus"] = "NOT_FOUND"
		providerData["cvmStatus"] = "NOT_FOUND"
		return withKnownStorage(Response{Ok: false, ErrorCode: "provider_truth_partial_identity", Message: "Tencent provider identity is only partially present.", MachinePresent: &machinePresent, CVMStatus: "NOT_FOUND", TKEStatus: "NOT_FOUND", ProviderRequestId: firstNonEmpty(cvmRequestID, tkeRequestID, machineRequestID), Retryable: true})
	}
	if !machinePresent || !tkePresent || !cvmPresent {
		return withKnownStorage(Response{Ok: false, ErrorCode: "provider_truth_partial_identity", Message: "Tencent provider identity is only partially present.", ProviderRequestId: firstNonEmpty(cvmRequestID, tkeRequestID, machineRequestID), Retryable: true})
	}
	if stringValue(tkeInstance.InstanceId) != request.Allocation.MachineName {
		return withKnownStorage(Response{Ok: false, ErrorCode: "provider_truth_node_mismatch", Message: "The supplied machine does not identify the TKE cluster instance at the private IP.", ProviderRequestId: tkeRequestID, Retryable: false})
	}
	if stringValue(machine.InstanceType) != request.Pool.InstanceType || stringValue(cvmInstance.InstanceType) != request.Pool.InstanceType {
		return withKnownStorage(Response{Ok: false, ErrorCode: "provider_truth_compute_sku_mismatch", Message: "The TKE machine and CVM instance type must exactly match the requested package.", ProviderRequestId: firstNonEmpty(cvmRequestID, machineRequestID), Retryable: false})
	}
	if stringValue(cvmInstance.InstanceId) != request.Allocation.InstanceId || stringValue(cvmInstance.InstanceName) != request.Allocation.Id ||
		!containsString(cvmInstance.PrivateIpAddresses, request.Allocation.PrivateIp) {
		return withKnownStorage(Response{Ok: false, ErrorCode: "provider_truth_cvm_identity_mismatch", Message: "The supplied resource, instance, machine, and node do not identify the same CVM.", ProviderRequestId: cvmRequestID, Retryable: false})
	}
	zone := ""
	if cvmInstance.Placement != nil {
		zone = stringValue(cvmInstance.Placement.Zone)
	}
	deadline := normalizeTencentDeadline(stringValue(cvmInstance.ExpiredTime))
	cvmTags, tagErr := cvmOwnershipTags(cvmInstance)
	if tagErr != nil {
		return withKnownStorage(Response{Ok: false, ErrorCode: "provider_truth_cvm_ownership_mismatch", Message: tagErr.Error(), ProviderRequestId: cvmRequestID, Retryable: false})
	}
	for _, key := range cbsOwnershipTagKeys {
		if cvmTags[key] != request.ComputeTags[key] {
			return withKnownStorage(Response{Ok: false, ErrorCode: "provider_truth_cvm_ownership_mismatch", Message: "The exact CVM ownership tags do not match the compute allocation.", ProviderRequestId: cvmRequestID, Retryable: false})
		}
	}
	if stringValue(cvmInstance.InstanceChargeType) != "PREPAID" || stringValue(cvmInstance.RenewFlag) != "NOTIFY_AND_MANUAL_RENEW" || deadline == "" || strings.TrimSpace(zone) == "" || zone != request.Storage.Zone {
		return withKnownStorage(Response{Ok: false, ErrorCode: "provider_truth_cvm_billing_mismatch", Message: "The exact CVM does not have required PREPAID manual-renew billing facts.", ProviderRequestId: cvmRequestID, Retryable: false})
	}
	tkeStatus := strings.ToUpper(strings.TrimSpace(stringValue(tkeInstance.InstanceState)))
	cvmStatus := strings.ToUpper(strings.TrimSpace(stringValue(cvmInstance.InstanceState)))
	if tkeStatus == "" || cvmStatus == "" {
		return withKnownStorage(Response{Ok: false, ErrorCode: "provider_truth_status_unknown", Message: "Tencent returned a present resource without an exact state.", ProviderRequestId: firstNonEmpty(cvmRequestID, tkeRequestID), Retryable: true})
	}
	providerData["tkeStatus"] = tkeStatus
	providerData["cvmStatus"] = cvmStatus
	providerData["chargeType"] = stringValue(cvmInstance.InstanceChargeType)
	providerData["renewFlag"] = stringValue(cvmInstance.RenewFlag)
	providerData["deadline"] = deadline
	providerData["zone"] = zone
	providerData["instanceType"] = request.Pool.InstanceType
	for _, key := range cbsOwnershipTagKeys {
		providerData["computeTag:"+key] = cvmTags[key]
	}
	if !storagePresent {
		return withKnownStorage(Response{
			Ok: false, ErrorCode: "provider_truth_partial_identity", Message: "Tencent provider identity is only partially present.", Retryable: true,
			PoolId: request.Pool.Id, NodePoolId: request.Pool.NodePoolId, InstanceId: request.Allocation.InstanceId, PrivateIp: request.Allocation.PrivateIp,
			MachinePresent: &machinePresent, CVMStatus: cvmStatus, TKEStatus: tkeStatus, Status: "present", MachineType: "NativeCVM", InstanceType: request.Pool.InstanceType,
			ProviderRequestId: firstNonEmpty(cvmRequestID, tkeRequestID, machineRequestID),
		})
	}
	return Response{Ok: true, PoolId: request.Pool.Id, NodePoolId: request.Pool.NodePoolId, InstanceId: request.Allocation.InstanceId, PrivateIp: request.Allocation.PrivateIp, MachinePresent: &machinePresent, StoragePresent: &storagePresent, CVMStatus: cvmStatus, TKEStatus: tkeStatus, CBSStatus: cbsStatus, Status: "present", MachineType: "NativeCVM", InstanceType: request.Pool.InstanceType, ProviderRequestId: firstNonEmpty(cbsRequestID, cvmRequestID), ProviderData: providerData}
}

func (client *tencentSDKClient) cbsVolumeTruth(storage StorageInput, tags map[string]string) (bool, string, string, map[string]string, error) {
	request := cbs2017.NewDescribeDisksRequest()
	request.DiskIds = []*string{common.StringPtr(storage.Id)}
	response, err := client.nativeCbsClient.DescribeDisks(request)
	if err != nil {
		if sdkErr, ok := err.(*tcerrors.TencentCloudSDKError); ok && sdkErr.Code == cbs2017.INVALIDDISKID_NOTFOUND {
			return false, "NOT_FOUND", sdkErr.RequestId, nil, nil
		}
		return false, "", "", nil, err
	}
	if response == nil || response.Response == nil || response.Response.TotalCount == nil ||
		*response.Response.TotalCount != uint64(len(response.Response.DiskSet)) || len(response.Response.DiskSet) > 1 {
		return false, "", "", nil, fmt.Errorf("Tencent CBS DescribeDisks response is missing or incomplete")
	}
	requestID := stringValue(response.Response.RequestId)
	if len(response.Response.DiskSet) == 0 {
		return false, "NOT_FOUND", requestID, nil, nil
	}
	disk := response.Response.DiskSet[0]
	facts, err := validateCBSVolume(disk, storage, tags, false)
	if err != nil {
		return false, "", requestID, nil, err
	}
	truthFacts := map[string]string{
		"storageChargeType": facts["diskChargeType"], "storageRenewFlag": facts["renewFlag"], "storageDeadline": facts["deadline"],
		"storageDiskType": facts["diskType"], "storageDiskName": facts["diskName"], "storageDiskUsage": facts["diskUsage"],
		"storageSizeGb": facts["sizeGb"], "storageZone": facts["zone"],
	}
	for _, key := range cbsOwnershipTagKeys {
		truthFacts[key] = facts[key]
	}
	return true, strings.ToUpper(strings.TrimSpace(stringValue(disk.DiskState))), requestID, truthFacts, nil
}

func (client *tencentSDKClient) describeClusterMachinePages(filters []*tke2022.Filter) ([]*tke2022.Machine, string, error) {
	machines := []*tke2022.Machine{}
	requestID := ""
	totalCount := int64(-1)
	for offset := int64(0); ; {
		request := tke2022.NewDescribeClusterMachinesRequest()
		request.ClusterId = common.StringPtr(client.clusterId)
		request.Limit = common.Int64Ptr(tkeListPageLimit)
		request.Offset = common.Int64Ptr(offset)
		request.Filters = filters
		response, err := client.nativeTkeClient.DescribeClusterMachines(request)
		if err != nil {
			return nil, requestID, err
		}
		if response == nil || response.Response == nil || response.Response.TotalCount == nil {
			return nil, requestID, fmt.Errorf("Tencent TKE DescribeClusterMachines response is missing or incomplete")
		}
		pageTotal := *response.Response.TotalCount
		if pageTotal < 0 || (totalCount >= 0 && pageTotal != totalCount) || int64(len(response.Response.Machines)) > tkeListPageLimit {
			return nil, requestID, fmt.Errorf("Tencent TKE DescribeClusterMachines response is missing or incomplete")
		}
		if totalCount < 0 {
			totalCount = pageTotal
		}
		requestID = firstNonEmpty(requestID, stringValue(response.Response.RequestId))
		for _, machine := range response.Response.Machines {
			if machine == nil || strings.TrimSpace(stringValue(machine.MachineName)) == "" {
				return nil, requestID, fmt.Errorf("Tencent TKE DescribeClusterMachines returned an unidentified machine")
			}
		}
		machines = append(machines, response.Response.Machines...)
		if int64(len(machines)) == totalCount {
			return machines, requestID, nil
		}
		if len(response.Response.Machines) == 0 || int64(len(machines)) > totalCount {
			return nil, requestID, fmt.Errorf("Tencent TKE DescribeClusterMachines response is missing or incomplete")
		}
		offset += int64(len(response.Response.Machines))
	}
}

func (client *tencentSDKClient) describeClusterInstancePages(filters []*tke2022.Filter) ([]*tke2022.Instance, string, error) {
	instances := []*tke2022.Instance{}
	requestID := ""
	var totalCount *uint64
	for offset := int64(0); ; {
		request := tke2022.NewDescribeClusterInstancesRequest()
		request.ClusterId = common.StringPtr(client.clusterId)
		request.Limit = common.Int64Ptr(tkeListPageLimit)
		request.Offset = common.Int64Ptr(offset)
		request.Filters = filters
		response, err := client.nativeTkeClient.DescribeClusterInstances(request)
		if err != nil {
			return nil, requestID, err
		}
		if response == nil || response.Response == nil || response.Response.TotalCount == nil || len(response.Response.InstanceSet) > int(tkeListPageLimit) {
			return nil, requestID, fmt.Errorf("Tencent TKE DescribeClusterInstances response is missing or incomplete")
		}
		pageTotal := *response.Response.TotalCount
		if totalCount != nil && pageTotal != *totalCount {
			return nil, requestID, fmt.Errorf("Tencent TKE DescribeClusterInstances response is missing or incomplete")
		}
		if totalCount == nil {
			totalCount = common.Uint64Ptr(pageTotal)
		}
		requestID = firstNonEmpty(requestID, stringValue(response.Response.RequestId))
		instances = append(instances, response.Response.InstanceSet...)
		if uint64(len(instances)) == pageTotal {
			return instances, requestID, nil
		}
		if len(response.Response.InstanceSet) == 0 || uint64(len(instances)) > pageTotal {
			return nil, requestID, fmt.Errorf("Tencent TKE DescribeClusterInstances response is missing or incomplete")
		}
		offset += int64(len(response.Response.InstanceSet))
	}
}

func (client *tencentSDKClient) describeClusterMachines(nodePoolId string) ([]*tke2022.Machine, string, error) {
	var filters []*tke2022.Filter
	if strings.TrimSpace(nodePoolId) != "" {
		filters = []*tke2022.Filter{{Name: common.StringPtr("NodePoolsId"), Values: []*string{common.StringPtr(nodePoolId)}}}
	}
	machines, requestID, err := client.describeClusterMachinePages(filters)
	if err == nil || strings.TrimSpace(nodePoolId) == "" || !isInvalidMachineFilterError(err) {
		return machines, requestID, err
	}
	machines, requestID, err = client.describeClusterMachinePages(nil)
	if err != nil {
		return nil, requestID, err
	}
	instances, _, err := client.describeClusterInstancePages([]*tke2022.Filter{{Name: common.StringPtr("NodePoolIds"), Values: []*string{common.StringPtr(nodePoolId)}}})
	if err != nil {
		return nil, requestID, err
	}
	poolIPs := map[string]bool{}
	for _, instance := range instances {
		if instance != nil && stringValue(instance.NodePoolId) == nodePoolId {
			poolIPs[stringValue(instance.LanIP)] = true
		}
	}
	filtered := machines[:0]
	for _, machine := range machines {
		if machine != nil && poolIPs[stringValue(machine.LanIP)] {
			filtered = append(filtered, machine)
		}
	}
	return filtered, requestID, nil
}

func (client *tencentSDKClient) readDestroyMachine(request Request) (*tke2022.Machine, string, error) {
	if strings.TrimSpace(request.Allocation.MachineName) == "" {
		return nil, "", fmt.Errorf("the retained machine name is required for exact readback")
	}
	poolMachines, requestID, err := client.describeClusterMachines(request.Pool.NodePoolId)
	if err != nil {
		return nil, requestID, err
	}
	machineName := strings.TrimSpace(request.Allocation.MachineName)
	matches := []*tke2022.Machine{}
	for _, machine := range poolMachines {
		if machine != nil && stringValue(machine.MachineName) == machineName {
			matches = append(matches, machine)
		}
	}
	if len(matches) > 1 {
		return nil, requestID, fmt.Errorf("machine name is not unique in the exact node pool")
	}
	allMachines, globalRequestID, err := client.describeClusterMachines("")
	if err != nil {
		return nil, firstNonEmpty(globalRequestID, requestID), err
	}
	globalMatches := 0
	for _, machine := range allMachines {
		if machine != nil && stringValue(machine.MachineName) == machineName {
			globalMatches++
		}
	}
	if len(matches) == 0 {
		if globalMatches != 0 {
			return nil, globalRequestID, fmt.Errorf("machine name exists outside the exact node pool")
		}
		return nil, firstNonEmpty(globalRequestID, requestID), nil
	}
	if globalMatches != 1 {
		return nil, globalRequestID, fmt.Errorf("machine name is not globally unique")
	}
	machine := matches[0]
	privateIP := stringValue(machine.LanIP)
	if privateIP == "" || (request.Allocation.PrivateIp != "" && request.Allocation.PrivateIp != privateIP) ||
		(request.Allocation.NodeName != "" && request.Allocation.NodeName != kubernetesNodeName(machine)) {
		return nil, globalRequestID, fmt.Errorf("machine name, node name, and private IP do not identify the same machine")
	}
	return machine, firstNonEmpty(globalRequestID, requestID), nil
}

func (client *tencentSDKClient) verifyDestroyMachineOwnership(pool *tke2022.NodePool, request Request) (bool, string, error) {
	machine, requestID, err := client.readDestroyMachine(request)
	if err != nil || machine == nil {
		return false, requestID, err
	}
	privateIP := stringValue(machine.LanIP)
	machineType := ""
	if pool != nil && pool.Native != nil {
		machineType = stringValue(pool.Native.MachineType)
	}
	switch {
	case strings.EqualFold(machineType, "NativeCVM"):
		instance, providerRequestID, err := client.describeCvmInstanceByPrivateIp(privateIP)
		if err != nil {
			return false, firstNonEmpty(providerRequestID, requestID), err
		}
		instanceID := strings.TrimSpace(request.Allocation.InstanceId)
		if !strings.HasPrefix(instanceID, "ins-") || stringValue(instance.InstanceId) != instanceID {
			return false, providerRequestID, fmt.Errorf("CVM instance does not match the supplied instance ID")
		}
		return true, providerRequestID, nil
	case strings.EqualFold(machineType, "Native"), strings.EqualFold(machineType, "CXM"):
		instance, providerRequestID, err := client.describeTkeClusterInstanceByPrivateIp(privateIP, request.Pool.NodePoolId)
		if err != nil {
			return false, firstNonEmpty(providerRequestID, requestID), err
		}
		if request.Allocation.InstanceId == "" || stringValue(instance.InstanceId) != request.Allocation.InstanceId ||
			stringValue(instance.NodePoolId) != request.Pool.NodePoolId || stringValue(instance.LanIP) != privateIP {
			return false, providerRequestID, fmt.Errorf("TKE instance does not match the supplied machine identity")
		}
		return true, providerRequestID, nil
	default:
		return false, requestID, fmt.Errorf("unsupported Tencent machine provider: %s", machineType)
	}
}

func nativeSelfProvisioningEnabled(pool *tke2022.NodePool) bool {
	return pool != nil && pool.Native != nil && ((pool.Native.EnableAutoscaling != nil && *pool.Native.EnableAutoscaling) ||
		(pool.Native.AutoRepair != nil && *pool.Native.AutoRepair))
}

func (client *tencentSDKClient) disableNativeNodePoolSelfProvisioning(nodePoolId string) (string, error) {
	modifyRequest := tke2022.NewModifyNodePoolRequest()
	modifyRequest.ClusterId = common.StringPtr(client.clusterId)
	modifyRequest.NodePoolId = common.StringPtr(nodePoolId)
	modifyRequest.Native = &tke2022.UpdateNativeNodePoolParam{
		EnableAutoscaling: common.BoolPtr(false),
		AutoRepair:        common.BoolPtr(false),
	}
	modifyResponse, err := client.nativeTkeClient.ModifyNodePool(modifyRequest)
	if err != nil {
		return "", err
	}
	return stringValue(modifyResponse.Response.RequestId), nil
}

func isInvalidMachineFilterError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "invalid filter name") || strings.Contains(message, "invalidparameter")
}

func kubernetesNodeName(machine *tke2022.Machine) string {
	if machine == nil {
		return ""
	}
	if lanIp := stringValue(machine.LanIP); lanIp != "" {
		return lanIp
	}
	return stringValue(machine.MachineName)
}

func (client *tencentSDKClient) describeCvmInstanceByPrivateIp(privateIp string) (*cvm2017.Instance, string, error) {
	if strings.TrimSpace(privateIp) == "" {
		return nil, "", fmt.Errorf("private IP is required to resolve CVM instance identity")
	}
	if client == nil || client.nativeCvmClient == nil {
		return nil, "", fmt.Errorf("Tencent CVM SDK client is missing")
	}
	describeRequest := cvm2017.NewDescribeInstancesRequest()
	describeRequest.Filters = []*cvm2017.Filter{{
		Name:   common.StringPtr("private-ip-address"),
		Values: []*string{common.StringPtr(privateIp)},
	}}
	describeRequest.Limit = common.Int64Ptr(100)
	describeResponse, err := client.nativeCvmClient.DescribeInstances(describeRequest)
	if err != nil {
		return nil, "", err
	}
	if describeResponse == nil || describeResponse.Response == nil {
		return nil, "", fmt.Errorf("Tencent CVM DescribeInstances response is missing")
	}
	requestId := stringValue(describeResponse.Response.RequestId)
	if describeResponse.Response.TotalCount == nil || *describeResponse.Response.TotalCount != int64(len(describeResponse.Response.InstanceSet)) {
		return nil, requestId, fmt.Errorf("Tencent CVM DescribeInstances response is incomplete")
	}
	if *describeResponse.Response.TotalCount > 1 {
		return nil, requestId, fmt.Errorf("Tencent CVM private IP identity is ambiguous")
	}
	if *describeResponse.Response.TotalCount == 1 {
		instance := describeResponse.Response.InstanceSet[0]
		if instance == nil || !containsString(instance.PrivateIpAddresses, privateIp) {
			return nil, requestId, fmt.Errorf("Tencent CVM DescribeInstances returned a mismatched instance")
		}
		return instance, requestId, nil
	}
	return nil, requestId, fmt.Errorf("%w for private IP %s", errCVMInstanceNotFound, privateIp)
}

func (client *tencentSDKClient) describeCvmInstanceByID(instanceID string) (*cvm2017.Instance, string, error) {
	describeRequest := cvm2017.NewDescribeInstancesRequest()
	describeRequest.InstanceIds = []*string{common.StringPtr(instanceID)}
	describeRequest.Limit = common.Int64Ptr(1)
	describeResponse, err := client.nativeCvmClient.DescribeInstances(describeRequest)
	if err != nil {
		return nil, "", err
	}
	if describeResponse == nil || describeResponse.Response == nil || describeResponse.Response.TotalCount == nil ||
		*describeResponse.Response.TotalCount != int64(len(describeResponse.Response.InstanceSet)) || len(describeResponse.Response.InstanceSet) > 1 {
		return nil, "", fmt.Errorf("Tencent CVM DescribeInstances response is missing or incomplete")
	}
	requestID := stringValue(describeResponse.Response.RequestId)
	if len(describeResponse.Response.InstanceSet) == 0 {
		return nil, requestID, nil
	}
	if describeResponse.Response.InstanceSet[0] == nil {
		return nil, requestID, fmt.Errorf("Tencent CVM DescribeInstances returned an empty instance")
	}
	return describeResponse.Response.InstanceSet[0], requestID, nil
}

func (client *tencentSDKClient) describeTkeClusterInstanceByPrivateIp(privateIp string, nodePoolId string) (*tke2022.Instance, string, error) {
	if strings.TrimSpace(privateIp) == "" {
		return nil, "", fmt.Errorf("private IP is required to resolve TKE instance identity")
	}
	if client == nil || client.nativeTkeClient == nil {
		return nil, "", fmt.Errorf("Tencent TKE SDK client is missing")
	}
	if strings.TrimSpace(nodePoolId) == "" {
		return nil, "", fmt.Errorf("node pool ID is required to resolve native TKE instance identity")
	}
	describeRequest := tke2022.NewDescribeClusterInstancesRequest()
	describeRequest.ClusterId = common.StringPtr(client.clusterId)
	describeRequest.Limit = common.Int64Ptr(100)
	describeRequest.Filters = []*tke2022.Filter{
		{Name: common.StringPtr("VagueIpAddress"), Values: []*string{common.StringPtr(privateIp)}},
	}
	describeRequest.Filters = append(describeRequest.Filters,
		&tke2022.Filter{Name: common.StringPtr("NodePoolIds"), Values: []*string{common.StringPtr(nodePoolId)}})
	describeResponse, err := client.nativeTkeClient.DescribeClusterInstances(describeRequest)
	if err != nil {
		return nil, "", err
	}
	if describeResponse == nil || describeResponse.Response == nil || describeResponse.Response.TotalCount == nil ||
		*describeResponse.Response.TotalCount != uint64(len(describeResponse.Response.InstanceSet)) {
		return nil, "", fmt.Errorf("Tencent TKE DescribeClusterInstances response is missing or incomplete")
	}
	requestId := stringValue(describeResponse.Response.RequestId)
	var matched *tke2022.Instance
	for _, instance := range describeResponse.Response.InstanceSet {
		if instance == nil {
			return nil, requestId, fmt.Errorf("Tencent TKE DescribeClusterInstances returned an empty instance")
		}
		if stringValue(instance.LanIP) == privateIp && stringValue(instance.NodePoolId) == nodePoolId && strings.EqualFold(stringValue(instance.NodeType), "Native") {
			if matched != nil {
				return nil, requestId, fmt.Errorf("Tencent TKE instance identity is ambiguous")
			}
			matched = instance
		}
	}
	if matched != nil {
		return matched, requestId, nil
	}
	return nil, requestId, fmt.Errorf("%w for private IP %s", errTKEInstanceNotFound, privateIp)
}

func (client *tencentSDKClient) describeNativeTkeClusterInstanceByMachineName(machineName string, nodePoolId string) (*tke2022.Instance, string, error) {
	if strings.TrimSpace(machineName) == "" || strings.TrimSpace(nodePoolId) == "" {
		return nil, "", fmt.Errorf("machine name and node pool ID are required to resolve native TKE instance identity")
	}
	if client == nil || client.nativeTkeClient == nil {
		return nil, "", fmt.Errorf("Tencent TKE SDK client is missing")
	}
	describeRequest := tke2022.NewDescribeClusterInstancesRequest()
	describeRequest.ClusterId = common.StringPtr(client.clusterId)
	describeRequest.Limit = common.Int64Ptr(100)
	describeRequest.Filters = []*tke2022.Filter{
		{Name: common.StringPtr("InstanceIds"), Values: []*string{common.StringPtr(machineName)}},
		{Name: common.StringPtr("NodePoolIds"), Values: []*string{common.StringPtr(nodePoolId)}},
	}
	describeResponse, err := client.nativeTkeClient.DescribeClusterInstances(describeRequest)
	if err != nil {
		return nil, "", err
	}
	if describeResponse == nil || describeResponse.Response == nil || describeResponse.Response.TotalCount == nil ||
		*describeResponse.Response.TotalCount != uint64(len(describeResponse.Response.InstanceSet)) {
		return nil, "", fmt.Errorf("Tencent TKE DescribeClusterInstances response is missing")
	}
	requestID := stringValue(describeResponse.Response.RequestId)
	var matched *tke2022.Instance
	for _, instance := range describeResponse.Response.InstanceSet {
		if instance == nil {
			return nil, requestID, fmt.Errorf("Tencent TKE DescribeClusterInstances returned an empty instance")
		}
		if stringValue(instance.InstanceId) == machineName && stringValue(instance.NodePoolId) == nodePoolId && strings.EqualFold(stringValue(instance.NodeType), "Native") {
			if matched != nil {
				return nil, requestID, fmt.Errorf("Tencent TKE Machine identity is ambiguous")
			}
			matched = instance
		}
	}
	if matched == nil {
		return nil, requestID, fmt.Errorf("%w for Machine %s", errTKEInstanceNotFound, machineName)
	}
	return matched, requestID, nil
}

type nodePoolReadbackErrorCode string

const (
	nodePoolReadbackSDKFailed        nodePoolReadbackErrorCode = "tencent_describe_node_pool_failed"
	nodePoolReadbackResponseInvalid  nodePoolReadbackErrorCode = "tencent_node_pool_readback_response_invalid"
	nodePoolReadbackEmptyOrAmbiguous nodePoolReadbackErrorCode = "tencent_node_pool_readback_empty_or_ambiguous"
	nodePoolReadbackIDMismatch       nodePoolReadbackErrorCode = "tencent_node_pool_readback_id_mismatch"
)

// Keep provider diagnostics out of the fixed classification while retaining the
// cause for SDK evidence and existing mutation admission checks.
type nodePoolReadbackError struct {
	code  nodePoolReadbackErrorCode
	cause error
}

func (err *nodePoolReadbackError) Error() string { return string(err.code) }

func (err *nodePoolReadbackError) Unwrap() error { return err.cause }

func (client *tencentSDKClient) describeNativeNodePool(nodePoolId string) (*tke2022.NodePool, string, error) {
	describeRequest := tke2022.NewDescribeNodePoolsRequest()
	describeRequest.ClusterId = common.StringPtr(client.clusterId)
	describeRequest.Limit = common.Int64Ptr(100)
	describeRequest.Filters = []*tke2022.Filter{
		{Name: common.StringPtr("NodePoolsId"), Values: []*string{common.StringPtr(nodePoolId)}},
	}
	describeResponse, err := client.nativeTkeClient.DescribeNodePools(describeRequest)
	if err != nil {
		return nil, "", &nodePoolReadbackError{code: nodePoolReadbackSDKFailed, cause: err}
	}
	if describeResponse == nil || describeResponse.Response == nil {
		return nil, "", &nodePoolReadbackError{code: nodePoolReadbackResponseInvalid}
	}
	requestID := stringValue(describeResponse.Response.RequestId)
	if describeResponse.Response.TotalCount == nil {
		return nil, requestID, &nodePoolReadbackError{code: nodePoolReadbackResponseInvalid}
	}
	if *describeResponse.Response.TotalCount != 1 || len(describeResponse.Response.NodePools) != 1 {
		return nil, requestID, &nodePoolReadbackError{code: nodePoolReadbackEmptyOrAmbiguous}
	}
	if describeResponse.Response.NodePools[0] == nil {
		return nil, requestID, &nodePoolReadbackError{code: nodePoolReadbackResponseInvalid}
	}
	if stringValue(describeResponse.Response.NodePools[0].NodePoolId) != nodePoolId {
		return nil, requestID, &nodePoolReadbackError{code: nodePoolReadbackIDMismatch}
	}
	return describeResponse.Response.NodePools[0], requestID, nil
}

func nodePoolLabels(pool *tke2022.NodePool) map[string]string {
	labels := map[string]string{}
	for _, label := range pool.Labels {
		if label != nil && label.Name != nil && label.Value != nil {
			labels[*label.Name] = *label.Value
		}
	}
	return labels
}

func deadlineAfter(current, previous string) bool {
	currentTime, currentErr := time.Parse(time.RFC3339, normalizeTencentDeadline(current))
	previousTime, previousErr := time.Parse(time.RFC3339, normalizeTencentDeadline(previous))
	return currentErr == nil && previousErr == nil && currentTime.After(previousTime)
}

func normalizeTencentDeadline(value string) string {
	parsed, err := parseTencentTime(value)
	if err != nil {
		return ""
	}
	return parsed.UTC().Format(time.RFC3339)
}

func parseTencentTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	return time.ParseInLocation("2006-01-02 15:04:05", value, time.FixedZone("UTC+8", 8*60*60))
}

var cvmOwnershipTagAliases = map[string]string{
	"oplcloud.cn/account-id":   "opl_account_id",
	"oplcloud.cn/workspace-id": "opl_workspace_id",
	"oplcloud.cn/resource-id":  "opl_resource_id",
	"oplcloud.cn/operation-id": "opl_operation_id",
}
