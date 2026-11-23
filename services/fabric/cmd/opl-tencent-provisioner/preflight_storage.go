package main

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	cbs2017 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cbs/v20170312"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tcerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	cvm2017 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm/v20170312"
	tke2018 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tke/v20180525"
	tke2022 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tke/v20220501"
	vpc2017 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc/v20170312"
)

type cbsReadbackValidationError struct {
	code string
}

func (err cbsReadbackValidationError) Error() string {
	return err.code
}

func workspaceSKUInventoryFailure(code string, err error) Response {
	message := code
	if err != nil {
		message = err.Error()
	}
	return Response{Ok: false, ErrorCode: code, Message: message, Retryable: false, MutationCount: 0}
}

func workspaceSKUPackage(items []*cvm2017.InstanceTypeQuotaItem, packageID, zone string, cpu, memoryGB uint64) (WorkspaceSKUPackage, error) {
	result := WorkspaceSKUPackage{PackageID: packageID, CPU: cpu, MemoryGB: memoryGB, Candidates: []WorkspaceSKUCandidate{}}
	seen := map[string]bool{}
	for _, item := range items {
		if item == nil || stringValue(item.Zone) != zone || stringValue(item.InstanceChargeType) != "PREPAID" || stringValue(item.Status) != "SELL" ||
			!exactInt64(item.Cpu, cpu) || !exactInt64(item.Memory, memoryGB) || item.Price == nil || item.Price.OriginalPrice == nil || *item.Price.OriginalPrice <= 0 ||
			item.Price.DiscountPrice == nil || *item.Price.DiscountPrice <= 0 {
			continue
		}
		instanceType := strings.TrimSpace(stringValue(item.InstanceType))
		if instanceType == "" || len(instanceType) > 64 || strings.ContainsAny(instanceType, " \t\r\n") || seen[instanceType] {
			return result, fmt.Errorf("eligible %s SKU inventory contains an empty or duplicate instance type", packageID)
		}
		seen[instanceType] = true
		result.Candidates = append(result.Candidates, WorkspaceSKUCandidate{
			InstanceType: instanceType, CPU: cpu, MemoryGB: memoryGB, MonthlyPriceCNY: *item.Price.DiscountPrice,
			StockStatus: stringValue(item.Status), StockCategory: stringValue(item.StatusCategory),
		})
	}
	sort.Slice(result.Candidates, func(i, j int) bool {
		if result.Candidates[i].MonthlyPriceCNY != result.Candidates[j].MonthlyPriceCNY {
			return result.Candidates[i].MonthlyPriceCNY < result.Candidates[j].MonthlyPriceCNY
		}
		return result.Candidates[i].InstanceType < result.Candidates[j].InstanceType
	})
	if len(result.Candidates) == 0 {
		return result, fmt.Errorf("no eligible %s PREPAID SKU matches the package shape in %s", packageID, zone)
	}
	result.RecommendedInstanceType = result.Candidates[0].InstanceType
	result.RecommendedMonthlyPriceCNY = result.Candidates[0].MonthlyPriceCNY
	return result, nil
}

func (client *tencentSDKClient) workspacePrepaidQuota(zone string) (uint64, error) {
	response, err := client.nativeCvmClient.DescribeAccountQuota(cvm2017.NewDescribeAccountQuotaRequest())
	if err != nil || response == nil || response.Response == nil || strings.TrimSpace(stringValue(response.Response.RequestId)) == "" ||
		response.Response.AccountQuotaOverview == nil || response.Response.AccountQuotaOverview.AccountQuota == nil {
		return 0, fmt.Errorf("Tencent PREPAID quota inventory is unavailable")
	}
	var matched *cvm2017.PrePaidQuota
	for _, quota := range response.Response.AccountQuotaOverview.AccountQuota.PrePaidQuotaSet {
		if quota == nil || strings.TrimSpace(stringValue(quota.Zone)) == "" {
			return 0, fmt.Errorf("Tencent PREPAID quota inventory is incomplete")
		}
		if stringValue(quota.Zone) == zone {
			if matched != nil {
				return 0, fmt.Errorf("Tencent PREPAID quota inventory is ambiguous")
			}
			matched = quota
		}
	}
	if matched == nil || matched.RemainingQuota == nil {
		return 0, fmt.Errorf("Tencent PREPAID quota inventory is unavailable for %s", zone)
	}
	return *matched.RemainingQuota, nil
}

func (client *tencentSDKClient) workspaceSubnetFacts(env map[string]string, zone string) ([]WorkspaceSubnetFact, uint64, error) {
	configured := splitCsv(env["TENCENT_CVM_SUBNET_ID"])
	seen := map[string]bool{}
	for _, subnetID := range configured {
		if seen[subnetID] {
			return nil, 0, fmt.Errorf("Workspace subnet configuration contains duplicate identities")
		}
		seen[subnetID] = true
	}
	if len(configured) == 0 {
		return nil, 0, fmt.Errorf("Workspace subnet configuration is missing")
	}
	request := vpc2017.NewDescribeSubnetsRequest()
	request.SubnetIds = stringsToPtrs(configured)
	response, err := client.nativeVpcClient.DescribeSubnets(request)
	if err != nil || response == nil || response.Response == nil || response.Response.TotalCount == nil ||
		strings.TrimSpace(stringValue(response.Response.RequestId)) == "" || *response.Response.TotalCount != uint64(len(configured)) || len(response.Response.SubnetSet) != len(configured) {
		return nil, 0, fmt.Errorf("Tencent Workspace subnet inventory is unavailable")
	}
	facts := make([]WorkspaceSubnetFact, 0, len(configured))
	matched := map[string]bool{}
	vpcID := ""
	available := uint64(0)
	for _, subnet := range response.Response.SubnetSet {
		if subnet == nil || subnet.AvailableIpAddressCount == nil || !seen[stringValue(subnet.SubnetId)] || matched[stringValue(subnet.SubnetId)] ||
			stringValue(subnet.Zone) != zone || strings.TrimSpace(stringValue(subnet.VpcId)) == "" {
			return nil, 0, fmt.Errorf("Tencent Workspace subnet inventory does not match the configured Zone and VPC")
		}
		if vpcID == "" {
			vpcID = stringValue(subnet.VpcId)
		} else if vpcID != stringValue(subnet.VpcId) {
			return nil, 0, fmt.Errorf("Workspace subnets span multiple VPCs")
		}
		if ^uint64(0)-available < *subnet.AvailableIpAddressCount {
			return nil, 0, fmt.Errorf("Workspace subnet capacity overflow")
		}
		available += *subnet.AvailableIpAddressCount
		matched[stringValue(subnet.SubnetId)] = true
		facts = append(facts, WorkspaceSubnetFact{SubnetID: stringValue(subnet.SubnetId), VPCID: vpcID, Zone: zone, AvailableIPAddresses: *subnet.AvailableIpAddressCount})
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].SubnetID < facts[j].SubnetID })
	return facts, available, nil
}

func (client *tencentSDKClient) workspaceTKECapacity() (uint64, uint64, uint64, TKEClusterCapacityFacts, error) {
	if client == nil || client.nativeLegacyTkeClient == nil || strings.TrimSpace(client.clusterId) == "" {
		return 0, 0, 0, TKEClusterCapacityFacts{}, fmt.Errorf("Tencent TKE cluster inventory is unavailable")
	}
	clustersRequest := tke2018.NewDescribeClustersRequest()
	clustersRequest.ClusterIds = []*string{common.StringPtr(client.clusterId)}
	clustersRequest.Limit = common.Int64Ptr(1)
	clusters, err := client.nativeLegacyTkeClient.DescribeClusters(clustersRequest)
	if err != nil || clusters == nil || clusters.Response == nil || clusters.Response.TotalCount == nil || *clusters.Response.TotalCount != 1 ||
		len(clusters.Response.Clusters) != 1 || clusters.Response.Clusters[0] == nil || strings.TrimSpace(stringValue(clusters.Response.RequestId)) == "" {
		return 0, 0, 0, TKEClusterCapacityFacts{}, fmt.Errorf("Tencent TKE cluster inventory is unavailable")
	}
	cluster := clusters.Response.Clusters[0]
	level := strings.TrimSpace(stringValue(cluster.ClusterLevel))
	if stringValue(cluster.ClusterId) != client.clusterId || !strings.EqualFold(stringValue(cluster.ClusterStatus), "Running") || level == "" || cluster.ClusterNodeNum == nil {
		return 0, 0, 0, TKEClusterCapacityFacts{}, fmt.Errorf("Tencent TKE cluster identity or level is unavailable")
	}
	facts := TKEClusterCapacityFacts{ClusterLevel: level, CurrentNodeCount: *cluster.ClusterNodeNum, LevelAttributes: []TKEClusterLevelFact{}}
	levelRequest := tke2018.NewDescribeClusterLevelAttributeRequest()
	levelRequest.ClusterID = common.StringPtr(client.clusterId)
	levels, err := client.nativeLegacyTkeClient.DescribeClusterLevelAttribute(levelRequest)
	if err != nil || levels == nil || levels.Response == nil || levels.Response.TotalCount == nil ||
		*levels.Response.TotalCount != int64(len(levels.Response.Items)) || strings.TrimSpace(stringValue(levels.Response.RequestId)) == "" {
		return 0, 0, 0, facts, fmt.Errorf("Tencent TKE cluster level inventory is unavailable")
	}
	for _, item := range levels.Response.Items {
		if item == nil {
			continue
		}
		attribute := TKEClusterLevelFact{Name: strings.TrimSpace(stringValue(item.Name)), Alias: strings.TrimSpace(stringValue(item.Alias))}
		if item.NodeCount != nil {
			nodeCount := *item.NodeCount
			attribute.NodeCount = &nodeCount
		}
		if item.Enable != nil {
			enable := *item.Enable
			attribute.Enable = &enable
		}
		facts.LevelAttributes = append(facts.LevelAttributes, attribute)
	}
	sort.Slice(facts.LevelAttributes, func(i, j int) bool {
		return facts.LevelAttributes[i].Name < facts.LevelAttributes[j].Name
	})
	matchCount := 0
	var nodeLimit *uint64
	for _, item := range levels.Response.Items {
		// The running cluster's matching NodeCount is the capacity fact; Enable is unrelated metadata.
		if item == nil || (strings.TrimSpace(stringValue(item.Name)) != level && strings.TrimSpace(stringValue(item.Alias)) != level) {
			continue
		}
		matchCount++
		if matchCount > 1 {
			return 0, 0, 0, facts, fmt.Errorf("Tencent TKE cluster level inventory is ambiguous")
		}
		if item.NodeCount != nil {
			nodeLimit = item.NodeCount
		}
	}
	if matchCount != 1 || nodeLimit == nil || *cluster.ClusterNodeNum > *nodeLimit {
		return 0, 0, 0, facts, fmt.Errorf("Tencent TKE cluster node limit is unavailable")
	}
	return *nodeLimit, *cluster.ClusterNodeNum, *nodeLimit - *cluster.ClusterNodeNum, facts, nil
}

func (client *tencentSDKClient) bootstrapSystemFacts(env map[string]string) (ProtectedSystemFacts, error) {
	facts := newProtectedSystemFacts()
	pools, err := client.bootstrapNodePoolInventory()
	if err != nil {
		facts.PoolCheckStatus = protectedCheckFailed
		return facts, err
	}
	return client.verifyBootstrapSystemIdentity(pools, env)
}

func (client *tencentSDKClient) WorkspaceSKUInventory(request Request, env map[string]string) Response {
	zone := strings.TrimSpace(request.Zone)
	configuredZone := strings.TrimSpace(env["OPL_TENCENT_ZONE"])
	if zone == "" {
		zone = configuredZone
	}
	if zone == "" || (configuredZone != "" && configuredZone != zone) || request.RequiredCapacity <= 0 {
		return workspaceSKUInventoryFailure("workspace_sku_inventory_invalid", nil)
	}
	if client == nil || client.nativeTkeClient == nil || client.nativeLegacyTkeClient == nil || client.nativeCvmClient == nil || client.nativeVpcClient == nil {
		return workspaceSKUInventoryFailure("workspace_sku_inventory_unavailable", fmt.Errorf("Tencent inventory clients are incomplete"))
	}
	availabilityRequest := cvm2017.NewDescribeZoneInstanceConfigInfosRequest()
	availabilityRequest.Filters = []*cvm2017.Filter{
		{Name: common.StringPtr("zone"), Values: []*string{common.StringPtr(zone)}},
		{Name: common.StringPtr("instance-charge-type"), Values: []*string{common.StringPtr("PREPAID")}},
	}
	availability, err := client.nativeCvmClient.DescribeZoneInstanceConfigInfos(availabilityRequest)
	if err != nil || availability == nil || availability.Response == nil || strings.TrimSpace(stringValue(availability.Response.RequestId)) == "" {
		return workspaceSKUInventoryFailure("workspace_sku_inventory_unavailable", fmt.Errorf("Tencent Workspace SKU inventory is unavailable"))
	}
	profiles, profileErr := configuredTencentSKUProfiles(env)
	if profileErr != nil {
		return workspaceSKUInventoryFailure("workspace_sku_inventory_invalid", profileErr)
	}
	packages := make([]WorkspaceSKUPackage, 0, len(profiles))
	for _, profile := range profiles {
		if !profile.Available {
			continue
		}
		current, packageErr := workspaceSKUPackage(availability.Response.InstanceTypeQuotaSet, profile.ID, zone, profile.Compute.CPU, profile.Compute.MemoryGB)
		if packageErr != nil {
			response := workspaceSKUInventoryFailure("workspace_sku_unavailable", packageErr)
			response.SKUPackages = packages
			return response
		}
		packages = append(packages, current)
	}
	quota, err := client.workspacePrepaidQuota(zone)
	if err != nil {
		response := workspaceSKUInventoryFailure("workspace_sku_inventory_unavailable", err)
		response.SKUPackages = packages
		return response
	}
	subnets, subnetCapacity, err := client.workspaceSubnetFacts(env, zone)
	if err != nil {
		response := workspaceSKUInventoryFailure("workspace_sku_inventory_unavailable", err)
		response.SKUPackages, response.PrepaidQuotaRemaining = packages, quota
		return response
	}
	nodeLimit, currentNodes, availableNodes, tkeCapacity, err := client.workspaceTKECapacity()
	if err != nil {
		response := workspaceSKUInventoryFailure("workspace_sku_inventory_unavailable", err)
		response.SKUPackages, response.PrepaidQuotaRemaining, response.Subnets = packages, quota, subnets
		if tkeCapacity.ClusterLevel != "" {
			response.TKECapacity = &tkeCapacity
		}
		return response
	}
	protectedSystem, err := client.bootstrapSystemFacts(env)
	if err != nil {
		response := workspaceSKUInventoryFailure("protected_system_identity_mismatch", fmt.Errorf("Protected system identity inventory is unavailable or inconsistent"))
		response.SKUPackages, response.PrepaidQuotaRemaining, response.Subnets = packages, quota, subnets
		response.TKEClusterNodeLimit, response.TKECurrentNodeCount, response.TKEAvailableNodeCapacity = nodeLimit, currentNodes, availableNodes
		response.ProtectedSystem = protectedSystem
		return response
	}
	response := Response{
		Ok: true, Status: "ready", RequiredCapacity: request.RequiredCapacity, SKUPackages: packages,
		PrepaidQuotaRemaining: quota, Subnets: subnets, TKEClusterNodeLimit: nodeLimit,
		TKECurrentNodeCount: currentNodes, TKEAvailableNodeCapacity: availableNodes, TKECapacity: &tkeCapacity,
		ProtectedSystem: protectedSystem, MutationCount: 0,
	}
	if quota < uint64(request.RequiredCapacity) || subnetCapacity < uint64(request.RequiredCapacity) || availableNodes < uint64(request.RequiredCapacity) {
		response.Ok = false
		response.Status = "blocked"
		response.ErrorCode = "workspace_capacity_insufficient"
		response.Message = "Approved Workspace NodePool capacity exceeds current PREPAID quota, subnet IP, or TKE node limits."
	}
	return response
}

func completedPreflightStage(stage, status, code string, started time.Time, blockedBy []string, facts map[string]any) PreflightStage {
	if blockedBy == nil {
		blockedBy = []string{}
	}
	if facts == nil {
		facts = map[string]any{}
	}
	return PreflightStage{Stage: stage, Status: status, ErrorCode: code, BlockedBy: blockedBy, DurationMS: time.Since(started).Milliseconds(), SafeFacts: facts}
}

func failedPreflightResponse(stages []PreflightStage) Response {
	code := "monthly_preflight_unavailable"
	for _, stage := range stages {
		if stage.Status == "failed" && stage.ErrorCode != "" {
			code = stage.ErrorCode
			break
		}
	}
	return Response{Ok: false, ErrorCode: code, Message: code, Retryable: false, PreflightStages: stages}
}

func allPreflightStagesPassed(stages []PreflightStage) bool {
	for _, stage := range stages {
		if stage.Status != "passed" {
			return false
		}
	}
	return true
}

func containsString(values []*string, expected string) bool {
	for _, current := range values {
		if stringValue(current) == expected {
			return true
		}
	}
	return false
}

func isCVMNativeNodePool(pool *tke2022.NodePool) bool {
	if pool == nil || pool.Native == nil || pool.Native.InstanceChargePrepaid == nil {
		return false
	}
	prepaid := pool.Native.InstanceChargePrepaid
	return stringValue(pool.Type) == "Native" && stringValue(pool.Native.MachineType) == "NativeCVM" &&
		stringValue(pool.Native.InstanceChargeType) == "PREPAID" &&
		prepaid.Period != nil && *prepaid.Period == 1 && stringValue(prepaid.RenewFlag) == "NOTIFY_AND_MANUAL_RENEW"
}

func mutationNodePoolFailure(pool *tke2022.NodePool, request Request, requestID string) *Response {
	if !isCVMNativeNodePool(pool) {
		return &Response{Ok: false, ErrorCode: "tencent_cvm_node_pool_required", Message: "Fabric compute requires a one-month PREPAID NativeCVM node pool with manual renewal.", ProviderRequestId: requestID, Retryable: false}
	}
	lifeState := strings.TrimSpace(stringValue(pool.LifeState))
	if strings.EqualFold(lifeState, "Creating") {
		return &Response{Ok: false, ErrorCode: "tencent_node_pool_not_ready", Message: "Tencent node pool is still being created.", ProviderRequestId: requestID, Retryable: true}
	}
	if !strings.EqualFold(lifeState, "Running") || !matchesCapacityNodePool(pool, request) {
		return &Response{Ok: false, ErrorCode: "tencent_node_pool_ownership_mismatch", Message: "Node pool does not match the requested ownership labels.", ProviderRequestId: requestID, Retryable: false}
	}
	requiredReplicas := request.Pool.DesiredReplicas
	if request.Pool.TargetReplicas > 0 {
		requiredReplicas = request.Pool.TargetReplicas
	} else if request.Allocation.Id != "" {
		requiredReplicas = nativeReplicas(pool) + 1
	}
	if len(pool.Native.InstanceTypes) != 1 || stringValue(pool.Native.InstanceTypes[0]) != request.Pool.InstanceType ||
		pool.Native.EnableAutoscaling == nil || *pool.Native.EnableAutoscaling ||
		pool.Native.AutoRepair == nil || *pool.Native.AutoRepair || pool.Native.Replicas == nil ||
		pool.Native.Scaling == nil || pool.Native.Scaling.MinReplicas == nil || *pool.Native.Scaling.MinReplicas != 0 || pool.Native.Scaling.MaxReplicas == nil ||
		request.Pool.MaxReplicas <= 0 || *pool.Native.Scaling.MaxReplicas != request.Pool.MaxReplicas || *pool.Native.Scaling.MaxReplicas < requiredReplicas {
		return &Response{Ok: false, ErrorCode: "tencent_node_pool_configuration_mismatch", Message: "Node pool runtime configuration does not match the requested Fabric mutation.", ProviderRequestId: requestID, Retryable: false}
	}
	return nil
}

func readbackNodePoolFailure(pool *tke2022.NodePool, request Request, requestID string) *Response {
	if !isCVMNativeNodePool(pool) || !strings.EqualFold(strings.TrimSpace(stringValue(pool.LifeState)), "running") ||
		!matchesCapacityNodePool(pool, request) || pool.Native == nil || len(pool.Native.InstanceTypes) != 1 ||
		stringValue(pool.Native.InstanceTypes[0]) != request.Pool.InstanceType {
		return &Response{Ok: false, ErrorCode: "compute_instance_type_mismatch", Message: "Tencent NodePool instance type does not match the requested package.", ProviderRequestId: requestID, Retryable: true}
	}
	return nil
}

func exactUint64(value *uint64, expected uint64) bool {
	return expected > 0 && value != nil && *value == expected
}

func exactInt64(value *int64, expected uint64) bool {
	return expected > 0 && value != nil && *value > 0 && uint64(*value) == expected
}

func machineResourceShapeMatches(machine *tke2022.Machine, pool ComputePoolInput) bool {
	return machine != nil && exactUint64(machine.CPU, pool.CPU) && exactUint64(machine.Memory, pool.MemoryGB)
}

func nativeResourceShapeMatches(native *tke2022.NativeNodeInfo, pool ComputePoolInput) bool {
	return native != nil && exactUint64(native.CPU, pool.CPU) && exactUint64(native.Memory, pool.MemoryGB)
}

func cvmResourceShapeMatches(instance *cvm2017.Instance, pool ComputePoolInput) bool {
	return instance != nil && exactInt64(instance.CPU, pool.CPU) && exactInt64(instance.Memory, pool.MemoryGB)
}

func computeResourceShapeFailure(requestID string) Response {
	return Response{Ok: false, ErrorCode: "compute_resource_shape_mismatch", Message: "Tencent Machine, Native instance, and CVM CPU and memory must exactly match the package resource contract.", ProviderRequestId: requestID, Retryable: true}
}

func matchesCapacityNodePool(pool *tke2022.NodePool, request Request) bool {
	labels := nodePoolLabels(pool)
	return request.Pool.Id != "" && request.PackageId != "" && request.Pool.InstanceType != "" &&
		labels["oplcloud.cn/pool-id"] == request.Pool.Id &&
		labels["oplcloud.cn/package-id"] == request.PackageId &&
		labels["oplcloud.cn/instance-type"] == request.Pool.InstanceType
}

func (client *tencentSDKClient) capacityNodePool(request Request) (*tke2022.NodePool, string, error) {
	if strings.TrimSpace(request.Pool.NodePoolId) == "" {
		return nil, "", fmt.Errorf("explicit node pool id required")
	}
	describe := tke2022.NewDescribeNodePoolsRequest()
	describe.ClusterId = common.StringPtr(client.clusterId)
	describe.Limit = common.Int64Ptr(1)
	describe.Filters = []*tke2022.Filter{{Name: common.StringPtr("NodePoolsId"), Values: []*string{common.StringPtr(request.Pool.NodePoolId)}}}
	response, err := client.nativeTkeClient.DescribeNodePools(describe)
	if err != nil {
		return nil, "", err
	}
	if response == nil || response.Response == nil {
		return nil, "", fmt.Errorf("explicit node pool ownership mismatch")
	}
	requestID := stringValue(response.Response.RequestId)
	if response.Response.TotalCount == nil || *response.Response.TotalCount != 1 ||
		len(response.Response.NodePools) != 1 || response.Response.NodePools[0] == nil ||
		stringValue(response.Response.NodePools[0].NodePoolId) != request.Pool.NodePoolId || !matchesCapacityNodePool(response.Response.NodePools[0], request) {
		return nil, requestID, fmt.Errorf("explicit node pool ownership mismatch")
	}
	return response.Response.NodePools[0], requestID, nil
}

func (client *tencentSDKClient) Capacity(request Request, _ map[string]string) Response {
	required := request.Pool.DesiredReplicas
	if required <= 0 || request.Pool.MaxReplicas <= 0 || strings.TrimSpace(request.Pool.InstanceType) == "" || strings.TrimSpace(request.Pool.NodePoolId) == "" || strings.TrimSpace(request.Pool.Id) == "" || request.Pool.CPU == 0 || request.Pool.MemoryGB == 0 {
		stages := []PreflightStage{}
		for _, stage := range []string{"node_pool_discovery", "tke_cluster_capacity", "node_pool_contract", "subnet", "zone", "cvm_prepaid_quota", "cvm_sku_price"} {
			stages = append(stages, completedPreflightStage(stage, "failed", "tencent_capacity_input_invalid", time.Now(), nil, nil))
		}
		return failedPreflightResponse(stages)
	}
	stages := make([]PreflightStage, 0, 7)
	clientReady := client != nil && client.nativeTkeClient != nil && client.nativeLegacyTkeClient != nil && client.nativeCvmClient != nil && client.nativeVpcClient != nil
	if !clientReady {
		for _, stage := range []string{"node_pool_discovery", "tke_cluster_capacity", "node_pool_contract", "subnet", "zone", "cvm_prepaid_quota", "cvm_sku_price"} {
			stages = append(stages, completedPreflightStage(stage, "failed", "tencent_capacity_client_missing", time.Now(), nil, nil))
		}
		return failedPreflightResponse(stages)
	}

	discoveryStarted := time.Now()
	pool, nodePoolRequestId, err := client.capacityNodePool(request)
	if err != nil {
		stages = append(stages, completedPreflightStage("node_pool_discovery", "failed", "tencent_capacity_node_pool_unavailable", discoveryStarted, nil, map[string]any{"matchCount": 0}))
	} else {
		stages = append(stages, completedPreflightStage("node_pool_discovery", "passed", "", discoveryStarted, nil, map[string]any{"matchCount": 1}))
	}

	clusterCapacityStarted := time.Now()
	clusterLimit, currentNodes, availableNodes, clusterFacts, clusterCapacityErr := client.workspaceTKECapacity()
	clusterCapacityPassed := clusterCapacityErr == nil && availableNodes >= uint64(required)
	clusterCapacityStatus, clusterCapacityCode := "passed", ""
	if clusterCapacityErr != nil {
		clusterCapacityStatus, clusterCapacityCode = "failed", "tencent_capacity_cluster_inventory_unavailable"
	} else if !clusterCapacityPassed {
		clusterCapacityStatus, clusterCapacityCode = "failed", "tencent_capacity_cluster_headroom_unavailable"
	}
	clusterSafeFacts := map[string]any{"clusterLevel": clusterFacts.ClusterLevel, "nodeLimit": clusterLimit, "currentNodes": currentNodes, "availableNodes": availableNodes, "requiredReplicas": required}
	stages = append(stages, completedPreflightStage("tke_cluster_capacity", clusterCapacityStatus, clusterCapacityCode, clusterCapacityStarted, nil, clusterSafeFacts))

	var native *tke2022.NativeNodePoolInfo
	contractPassed := false
	contractStarted := time.Now()
	if !clusterCapacityPassed {
		stages = append(stages, completedPreflightStage("node_pool_contract", "blocked", "preflight_dependency_blocked", contractStarted, []string{"tke_cluster_capacity"}, nil))
	} else if pool == nil {
		stages = append(stages, completedPreflightStage("node_pool_contract", "blocked", "preflight_dependency_blocked", contractStarted, []string{"node_pool_discovery"}, nil))
	} else {
		native = pool.Native
		contractPassed = isCVMNativeNodePool(pool) && strings.TrimSpace(stringValue(pool.LifeState)) == "Running" && native.Scaling != nil && native.Scaling.MinReplicas != nil && *native.Scaling.MinReplicas == 0 && native.Scaling.MaxReplicas != nil && *native.Scaling.MaxReplicas == request.Pool.MaxReplicas &&
			native.Replicas != nil && native.ReadyReplicas != nil && native.EnableAutoscaling != nil && native.AutoRepair != nil &&
			!*native.EnableAutoscaling && !*native.AutoRepair && *native.ReadyReplicas == *native.Replicas &&
			*native.Scaling.MaxReplicas >= *native.Replicas+required && len(native.InstanceTypes) == 1 && stringValue(native.InstanceTypes[0]) == request.Pool.InstanceType && len(native.SubnetIds) > 0
		status, code := "passed", ""
		if !contractPassed {
			status, code = "failed", "tencent_capacity_node_pool_unavailable"
		}
		facts := nodePoolSafeFacts(pool, request, required)
		if native != nil && native.Replicas != nil && native.ReadyReplicas != nil && native.Scaling != nil && native.Scaling.MaxReplicas != nil {
			facts["currentReplicas"], facts["readyReplicas"], facts["maxReplicas"] = *native.Replicas, *native.ReadyReplicas, *native.Scaling.MaxReplicas
		}
		stages = append(stages, completedPreflightStage("node_pool_contract", status, code, contractStarted, nil, facts))
	}

	subnetIds := []string{}
	zones := []string{}
	subnetRequestID := ""
	subnetPassed := false
	subnetStarted := time.Now()
	if !contractPassed {
		stages = append(stages, completedPreflightStage("subnet", "blocked", "preflight_dependency_blocked", subnetStarted, []string{"node_pool_contract"}, nil))
	} else {
		seenSubnetIds := map[string]bool{}
		validSubnetIDs := true
		for _, raw := range native.SubnetIds {
			subnetID := strings.TrimSpace(stringValue(raw))
			if subnetID == "" || seenSubnetIds[subnetID] {
				validSubnetIDs = false
				break
			}
			seenSubnetIds[subnetID] = true
			subnetIds = append(subnetIds, subnetID)
		}
		if validSubnetIDs {
			subnetRequest := vpc2017.NewDescribeSubnetsRequest()
			subnetRequest.SubnetIds = stringsToPtrs(subnetIds)
			subnetResponse, describeErr := client.nativeVpcClient.DescribeSubnets(subnetRequest)
			if describeErr == nil && subnetResponse != nil && subnetResponse.Response != nil {
				subnetRequestID = stringValue(subnetResponse.Response.RequestId)
				seenZones, foundSubnets := map[string]bool{}, map[string]bool{}
				for _, subnet := range subnetResponse.Response.SubnetSet {
					if subnet == nil || !seenSubnetIds[stringValue(subnet.SubnetId)] || foundSubnets[stringValue(subnet.SubnetId)] || subnet.AvailableIpAddressCount == nil || int64(*subnet.AvailableIpAddressCount) < required || strings.TrimSpace(stringValue(subnet.Zone)) == "" {
						validSubnetIDs = false
						break
					}
					foundSubnets[stringValue(subnet.SubnetId)] = true
					zone := strings.TrimSpace(stringValue(subnet.Zone))
					if !seenZones[zone] {
						seenZones[zone], zones = true, append(zones, zone)
					}
				}
				subnetPassed = validSubnetIDs && len(foundSubnets) == len(subnetIds) && subnetRequestID != "" && nodePoolRequestId != ""
			}
		}
		status, code := "passed", ""
		if !subnetPassed {
			status, code = "failed", "tencent_capacity_subnet_unavailable"
		}
		stages = append(stages, completedPreflightStage("subnet", status, code, subnetStarted, nil, map[string]any{"subnetCount": len(subnetIds)}))
	}
	zoneStarted := time.Now()
	zonePassed := subnetPassed && len(zones) == 1 && (strings.TrimSpace(request.Zone) == "" || zones[0] == request.Zone)
	if !subnetPassed {
		stages = append(stages, completedPreflightStage("zone", "blocked", "preflight_dependency_blocked", zoneStarted, []string{"subnet"}, nil))
	} else {
		status, code := "passed", ""
		if !zonePassed {
			status, code = "failed", "tencent_capacity_zone_unavailable"
		}
		stages = append(stages, completedPreflightStage("zone", status, code, zoneStarted, nil, map[string]any{"zone": firstNonEmpty(request.Zone, firstString(stringsToPtrs(zones)))}))
	}
	targetZone := strings.TrimSpace(request.Zone)
	if targetZone == "" && len(zones) == 1 {
		targetZone = zones[0]
	}

	remainingQuota := uint64(0)
	quotaRequestID := ""
	quotaStarted := time.Now()
	if targetZone == "" {
		stages = append(stages, completedPreflightStage("cvm_prepaid_quota", "blocked", "preflight_dependency_blocked", quotaStarted, []string{"zone"}, nil))
	} else {
		quotaRequest := cvm2017.NewDescribeAccountQuotaRequest()
		quotaResponse, err := client.nativeCvmClient.DescribeAccountQuota(quotaRequest)
		quotaPassed := false
		code := "tencent_capacity_prepaid_quota_describe_failed"
		if err == nil && quotaResponse != nil && quotaResponse.Response != nil && quotaResponse.Response.AccountQuotaOverview != nil && quotaResponse.Response.AccountQuotaOverview.AccountQuota != nil {
			quotaRequestID = stringValue(quotaResponse.Response.RequestId)
			matchingQuotas, ambiguous := []*cvm2017.PrePaidQuota{}, false
			for _, quota := range quotaResponse.Response.AccountQuotaOverview.AccountQuota.PrePaidQuotaSet {
				if quota == nil || strings.TrimSpace(stringValue(quota.Zone)) == "" {
					ambiguous = true
					continue
				}
				if stringValue(quota.Zone) == targetZone {
					matchingQuotas = append(matchingQuotas, quota)
				}
			}
			if !ambiguous && len(matchingQuotas) == 1 && matchingQuotas[0].RemainingQuota != nil {
				remainingQuota = *matchingQuotas[0].RemainingQuota
				quotaPassed = remainingQuota >= uint64(required) && quotaRequestID != ""
				code = "tencent_capacity_prepaid_quota_unavailable"
			}
		}
		status := "passed"
		if !quotaPassed {
			status = "failed"
		}
		stages = append(stages, completedPreflightStage("cvm_prepaid_quota", status, map[bool]string{true: "", false: code}[quotaPassed], quotaStarted, nil, map[string]any{"zone": targetZone, "remainingQuota": remainingQuota, "requiredCapacity": required}))
	}

	providerPriceCNY := 0.0
	availabilityRequestID := ""
	priceStarted := time.Now()
	if targetZone == "" {
		stages = append(stages, completedPreflightStage("cvm_sku_price", "blocked", "preflight_dependency_blocked", priceStarted, []string{"zone"}, nil))
	} else {
		zone := targetZone
		availabilityRequest := cvm2017.NewDescribeZoneInstanceConfigInfosRequest()
		availabilityRequest.Filters = []*cvm2017.Filter{
			{Name: common.StringPtr("zone"), Values: []*string{common.StringPtr(zone)}},
			{Name: common.StringPtr("instance-type"), Values: []*string{common.StringPtr(request.Pool.InstanceType)}},
			{Name: common.StringPtr("instance-charge-type"), Values: []*string{common.StringPtr("PREPAID")}},
		}
		availability, err := client.nativeCvmClient.DescribeZoneInstanceConfigInfos(availabilityRequest)
		exactAvailability := 0
		sell := false
		zonePriceCNY := 0.0
		shapeMatches := false
		var actualCPU any
		var actualMemoryGB any
		expectedCPU, expectedMemoryGB := request.Pool.CPU, request.Pool.MemoryGB
		if err == nil && availability != nil && availability.Response != nil {
			for _, item := range availability.Response.InstanceTypeQuotaSet {
				if item != nil && stringValue(item.Zone) == zone && stringValue(item.InstanceType) == request.Pool.InstanceType && stringValue(item.InstanceChargeType) == "PREPAID" {
					exactAvailability++
					sell = stringValue(item.Status) == "SELL"
					if item.Cpu != nil {
						actualCPU = *item.Cpu
					}
					if item.Memory != nil {
						actualMemoryGB = *item.Memory
					}
					shapeMatches = exactInt64(item.Cpu, expectedCPU) && exactInt64(item.Memory, expectedMemoryGB)
					if item.Price != nil && item.Price.DiscountPrice != nil {
						zonePriceCNY = *item.Price.DiscountPrice
					}
				}
			}
			availabilityRequestID = stringValue(availability.Response.RequestId)
		}
		pricePassed := exactAvailability == 1 && shapeMatches && sell && zonePriceCNY > 0 && availabilityRequestID != ""
		status, code := "passed", ""
		if !pricePassed {
			status, code = "failed", "tencent_capacity_instance_unavailable"
			if exactAvailability == 1 && !shapeMatches {
				code = "tencent_capacity_instance_shape_mismatch"
			}
		}
		providerPriceCNY = zonePriceCNY
		stages = append(stages, completedPreflightStage("cvm_sku_price", status, code, priceStarted, nil, map[string]any{
			"zone": targetZone, "instanceType": request.Pool.InstanceType, "cpu": actualCPU, "memoryGb": actualMemoryGB,
			"available": sell, "providerPriceCny": zonePriceCNY,
		}))
	}
	if !allPreflightStagesPassed(stages) {
		return failedPreflightResponse(stages)
	}
	providerRequestIDs := map[string]string{"nodePool": nodePoolRequestId, "subnets": subnetRequestID, "quota": quotaRequestID, "availability": availabilityRequestID}
	return Response{
		Ok: true, Status: "ready", ProviderRequestId: nodePoolRequestId, NodePoolId: stringValue(pool.NodePoolId),
		ProviderRequestIDs: providerRequestIDs, ProviderPriceCNY: providerPriceCNY,
		ProviderData: map[string]string{"chargeType": "PREPAID", "periodMonths": "1", "renewFlag": "NOTIFY_AND_MANUAL_RENEW", "zone": targetZone},
		InstanceType: request.Pool.InstanceType, InstanceAvailable: true, RequiredCapacity: required,
		RemainingQuota:  remainingQuota,
		CurrentReplicas: *native.Replicas, ReadyReplicas: *native.ReadyReplicas,
		MaxReplicas: *native.Scaling.MaxReplicas, TargetReplicas: *native.Replicas + required, MachineType: stringValue(native.MachineType), Zones: zones, PreflightStages: stages,
		TKEClusterNodeLimit: clusterLimit, TKECurrentNodeCount: currentNodes, TKEAvailableNodeCapacity: availableNodes, TKECapacity: &clusterFacts,
	}
}

func nodePoolSafeFacts(pool *tke2022.NodePool, request Request, required int64) map[string]any {
	facts := map[string]any{
		"nodePoolId": request.Pool.NodePoolId, "poolId": request.Pool.Id, "packageId": request.PackageId,
		"expectedInstanceType": request.Pool.InstanceType, "requiredCapacity": required,
	}
	if pool == nil {
		return facts
	}
	facts["labels"] = nodePoolLabels(pool)
	facts["type"] = stringValue(pool.Type)
	facts["lifeState"] = stringValue(pool.LifeState)
	taints := make([]map[string]string, 0, len(pool.Taints))
	for _, taint := range pool.Taints {
		if taint != nil {
			taints = append(taints, map[string]string{"key": stringValue(taint.Key), "value": stringValue(taint.Value), "effect": stringValue(taint.Effect)})
		}
	}
	facts["taints"] = taints
	if pool.Native == nil {
		return facts
	}
	native := pool.Native
	facts["machineType"] = stringValue(native.MachineType)
	facts["chargeType"] = stringValue(native.InstanceChargeType)
	facts["autoscaling"] = boolValue(native.EnableAutoscaling)
	facts["autoRepair"] = boolValue(native.AutoRepair)
	facts["actualInstanceTypes"] = ptrsToStrings(native.InstanceTypes)
	facts["zones"] = []string{}
	if native.Replicas != nil {
		facts["currentReplicas"] = *native.Replicas
	}
	if native.ReadyReplicas != nil {
		facts["readyReplicas"] = *native.ReadyReplicas
	}
	if native.Scaling != nil {
		if native.Scaling.MinReplicas != nil {
			facts["minReplicas"] = *native.Scaling.MinReplicas
		}
		if native.Scaling.MaxReplicas != nil {
			facts["maxReplicas"] = *native.Scaling.MaxReplicas
		}
	}
	if native.InstanceChargePrepaid != nil {
		if native.InstanceChargePrepaid.Period != nil {
			facts["periodMonths"] = *native.InstanceChargePrepaid.Period
		}
		facts["renewFlag"] = stringValue(native.InstanceChargePrepaid.RenewFlag)
	}
	return facts
}

func ptrsToStrings(values []*string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, stringValue(value))
	}
	return result
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func (client *tencentSDKClient) StoragePreflight(request Request, env map[string]string) Response {
	storage := request.Storage
	storage.DiskType = firstNonEmpty(storage.DiskType, env["TENCENT_CBS_DISK_TYPE"])
	if storage.SizeGB == 0 || strings.TrimSpace(storage.Zone) == "" || strings.TrimSpace(storage.DiskType) == "" {
		return failedPreflightResponse([]PreflightStage{
			completedPreflightStage("cbs_prepaid_quota", "failed", "tencent_storage_preflight_input_invalid", time.Now(), nil, nil),
			completedPreflightStage("cbs_price", "failed", "tencent_storage_preflight_input_invalid", time.Now(), nil, nil),
		})
	}
	if client == nil || client.nativeCbsClient == nil {
		return failedPreflightResponse([]PreflightStage{
			completedPreflightStage("cbs_prepaid_quota", "failed", "tencent_storage_preflight_client_missing", time.Now(), nil, nil),
			completedPreflightStage("cbs_price", "failed", "tencent_storage_preflight_client_missing", time.Now(), nil, nil),
		})
	}
	stages := make([]PreflightStage, 0, 2)
	quotaStarted := time.Now()
	quotaRequest := cbs2017.NewDescribeDiskConfigQuotaRequest()
	quotaRequest.InquiryType = common.StringPtr("INQUIRY_CBS_CONFIG")
	quotaRequest.DiskChargeType = common.StringPtr("PREPAID")
	quotaRequest.DiskTypes = []*string{common.StringPtr(storage.DiskType)}
	quotaRequest.Zones = []*string{common.StringPtr(storage.Zone)}
	quotaRequest.DiskUsage = common.StringPtr("DATA_DISK")
	quota, err := client.nativeCbsClient.DescribeDiskConfigQuota(quotaRequest)
	quotaRequestID := ""
	matches := 0
	available := false
	if err == nil && quota != nil && quota.Response != nil {
		quotaRequestID = stringValue(quota.Response.RequestId)
		for _, config := range quota.Response.DiskConfigSet {
			if config == nil || stringValue(config.DiskChargeType) != "PREPAID" || stringValue(config.Zone) != storage.Zone || stringValue(config.DiskType) != storage.DiskType || stringValue(config.DiskUsage) != "DATA_DISK" {
				continue
			}
			matches++
			available = config.Available != nil && *config.Available && config.MinDiskSize != nil && config.MaxDiskSize != nil && config.StepSize != nil && *config.StepSize > 0 &&
				storage.SizeGB >= *config.MinDiskSize && storage.SizeGB <= *config.MaxDiskSize && (storage.SizeGB-*config.MinDiskSize)%*config.StepSize == 0
		}
	}
	quotaPassed := quotaRequestID != "" && matches == 1 && available
	quotaStatus, quotaCode := "passed", ""
	if !quotaPassed {
		quotaStatus, quotaCode = "failed", "tencent_storage_quota_unavailable"
	}
	stages = append(stages, completedPreflightStage("cbs_prepaid_quota", quotaStatus, quotaCode, quotaStarted, nil, map[string]any{"zone": storage.Zone, "diskType": storage.DiskType, "sizeGb": storage.SizeGB, "available": available}))

	priceStarted := time.Now()
	priceRequest := cbs2017.NewInquiryPriceCreateDisksRequest()
	priceRequest.DiskChargeType = common.StringPtr("PREPAID")
	priceRequest.DiskType = common.StringPtr(storage.DiskType)
	priceRequest.DiskSize = common.Uint64Ptr(storage.SizeGB)
	priceRequest.DiskCount = common.Uint64Ptr(1)
	priceRequest.DiskChargePrepaid = &cbs2017.DiskChargePrepaid{Period: common.Uint64Ptr(1), RenewFlag: common.StringPtr("NOTIFY_AND_MANUAL_RENEW")}
	price, err := client.nativeCbsClient.InquiryPriceCreateDisks(priceRequest)
	providerPriceCNY := 0.0
	priceRequestID := ""
	priceFacts := map[string]any{"zone": storage.Zone, "diskType": storage.DiskType, "sizeGb": storage.SizeGB, "providerPriceCny": providerPriceCNY}
	priceStatus, priceCode := "passed", ""
	switch {
	case err != nil:
		priceStatus, priceCode = "failed", "tencent_storage_price_sdk_error"
		priceFacts["providerErrorClass"] = "sdk_error"
		if providerCode := safeTencentProviderErrorCode(err); providerCode != "" {
			priceFacts["providerErrorCode"] = providerCode
		}
	case price == nil || price.Response == nil:
		priceStatus, priceCode = "failed", "tencent_storage_price_response_invalid"
		priceFacts["providerErrorClass"] = "invalid_response"
	case strings.TrimSpace(stringValue(price.Response.RequestId)) == "":
		priceStatus, priceCode = "failed", "tencent_storage_price_request_id_missing"
		priceFacts["providerErrorClass"] = "missing_request_id"
	case price.Response.DiskPrice == nil || price.Response.DiskPrice.DiscountPrice == nil:
		priceStatus, priceCode = "failed", "tencent_storage_price_missing"
		priceFacts["providerErrorClass"] = "missing_price"
	default:
		providerPriceCNY = *price.Response.DiskPrice.DiscountPrice
		priceRequestID = stringValue(price.Response.RequestId)
		priceFacts["providerPriceCny"] = providerPriceCNY
		if providerPriceCNY <= 0 {
			priceStatus, priceCode = "failed", "tencent_storage_price_non_positive"
			priceFacts["providerErrorClass"] = "non_positive_price"
		}
	}
	stages = append(stages, completedPreflightStage("cbs_price", priceStatus, priceCode, priceStarted, nil, priceFacts))
	if !allPreflightStagesPassed(stages) {
		return failedPreflightResponse(stages)
	}
	return Response{
		Ok: true, Status: "ready", ProviderRequestId: priceRequestID,
		ProviderRequestIDs: map[string]string{"quota": quotaRequestID, "price": priceRequestID}, ProviderPriceCNY: providerPriceCNY,
		ProviderData:    map[string]string{"chargeType": "PREPAID", "periodMonths": "1", "renewFlag": "NOTIFY_AND_MANUAL_RENEW", "zone": storage.Zone, "diskType": storage.DiskType, "sizeGb": strconv.FormatUint(storage.SizeGB, 10)},
		PreflightStages: stages,
	}
}

func (client *tencentSDKClient) CreateStorageVolume(request Request, env map[string]string) Response {
	if client == nil || client.nativeCbsClient == nil {
		return Response{Ok: false, ErrorCode: "tencent_sdk_client_missing", Message: "Tencent CBS SDK client is missing.", Retryable: false}
	}
	storage := request.Storage
	storage.DiskType = firstNonEmpty(storage.DiskType, env["TENCENT_CBS_DISK_TYPE"])
	if strings.TrimSpace(request.AccountId) == "" || strings.TrimSpace(storage.Id) == "" || storage.SizeGB == 0 || strings.TrimSpace(storage.Zone) == "" || strings.TrimSpace(storage.DiskType) == "" ||
		!validCBSOwnershipTags(request.Tags) || request.Tags["opl_account_id"] != request.AccountId || request.Tags["opl_resource_id"] != storage.Id {
		return Response{Ok: false, ErrorCode: "tencent_cbs_input_invalid", Message: "Exact CBS account, resource, size, compute Zone, and ownership tags are required.", Retryable: false}
	}
	if identityFailure := client.storageProviderRegionFailure(request); identityFailure != nil {
		identityFailure.StorageState = "unknown"
		return *identityFailure
	}
	discoveryRequest := request
	discoveryRequest.Storage = storage
	discovery := client.DiscoverStorageVolume(discoveryRequest, env)
	if !discovery.Ok {
		return discovery
	}
	switch storage.ExpectedState {
	case "":
		if discovery.StorageState == "storage_existing_exact" {
			return discovery
		}
		if storage.AllowExistingExactReplay {
			return Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_cbs_replay_unconfirmed", Message: "A reserved CBS create attempt cannot be repeated without exact disk readback.", ProviderRequestId: discovery.ProviderRequestId, Retryable: false}
		}
	case "storage_not_started":
		if storage.ExpectedProviderResourceId != "" {
			return Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_cbs_approval_invalid", Message: "Approved absent CBS state cannot bind a disk identity.", ProviderRequestId: discovery.ProviderRequestId, Retryable: false}
		}
		if storage.AllowExistingExactReplay {
			if discovery.StorageState == "storage_existing_exact" {
				return discovery
			}
			return Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_cbs_replay_unconfirmed", Message: "A reserved CBS create attempt cannot be repeated without exact disk readback.", ProviderRequestId: discovery.ProviderRequestId, Retryable: false}
		}
		if discovery.StorageState != "storage_not_started" {
			return Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_cbs_approval_drift", Message: "Tencent CBS state changed after approval.", ProviderRequestId: discovery.ProviderRequestId, Retryable: false}
		}
	case "storage_existing_exact":
		if discovery.StorageState != "storage_existing_exact" || !strings.HasPrefix(storage.ExpectedProviderResourceId, "disk-") ||
			discovery.StorageVolumeId != storage.ExpectedProviderResourceId {
			return Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_cbs_approval_drift", Message: "Tencent CBS identity changed after approval.", ProviderRequestId: discovery.ProviderRequestId, Retryable: false}
		}
		return discovery
	default:
		return Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_cbs_approval_invalid", Message: "Approved CBS state is invalid.", Retryable: false}
	}
	create := cbs2017.NewCreateDisksRequest()
	create.Placement = &cbs2017.Placement{Zone: common.StringPtr(storage.Zone)}
	create.DiskChargeType = common.StringPtr("PREPAID")
	create.DiskType = common.StringPtr(storage.DiskType)
	create.DiskName = common.StringPtr(storage.Id)
	create.Tags = cbsTags(request.Tags)
	create.DiskCount = common.Uint64Ptr(1)
	create.DiskSize = common.Uint64Ptr(storage.SizeGB)
	create.ClientToken = common.StringPtr("opl-cbs-" + stableSuffix(request.AccountId, storage.Id, request.Tags["opl_operation_id"]))
	create.DiskChargePrepaid = &cbs2017.DiskChargePrepaid{Period: common.Uint64Ptr(1), RenewFlag: common.StringPtr("NOTIFY_AND_MANUAL_RENEW")}
	created, err := client.nativeCbsClient.CreateDisks(create)
	if err != nil {
		response := sdkErrorResponse("tencent_create_cbs_failed", err)
		if providerCode := safeTencentProviderErrorCode(err); providerCode != "" {
			response.Message = providerCode
			response.ProviderData["providerErrorCode"] = providerCode
			response.ProviderData["providerErrorMessage"] = providerCode
		}
		response.StorageState = "unknown"
		response.MutationCount = 1
		return response
	}
	if created == nil || created.Response == nil || len(created.Response.DiskIdSet) != 1 || !strings.HasPrefix(stringValue(created.Response.DiskIdSet[0]), "disk-") {
		return Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_create_cbs_identity_missing", Message: "Tencent CBS did not return exactly one disk identity.", Retryable: true, MutationCount: 1}
	}
	storage.Id = stringValue(created.Response.DiskIdSet[0])
	request.Storage = storage
	response := client.storageVolumeReadback(request, false)
	response.StorageVolumeId = storage.Id
	response.StorageState = "storage_existing_exact"
	if response.ProviderData == nil {
		response.ProviderData = map[string]string{}
	}
	response.ProviderData["createCbsRequestId"] = stringValue(created.Response.RequestId)
	response.ProviderRequestId = firstNonEmpty(stringValue(created.Response.RequestId), response.ProviderRequestId)
	response.MutationCount = 1
	return response
}

func (client *tencentSDKClient) DiscoverStorageVolume(request Request, _ map[string]string) Response {
	if client == nil || client.nativeCbsClient == nil {
		return Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_sdk_client_missing", Message: "Tencent CBS SDK client is missing.", Retryable: false}
	}
	if request.Action == "discover_storage_volume" {
		if identityFailure := client.storageProviderRegionFailure(request); identityFailure != nil {
			identityFailure.StorageState = "unknown"
			return *identityFailure
		}
	}
	storage := request.Storage
	storage.DiskType = strings.TrimSpace(storage.DiskType)
	if strings.TrimSpace(request.AccountId) == "" || storage.Id == "" || storage.SizeGB == 0 || storage.Zone == "" || storage.DiskType == "" ||
		!validCBSOwnershipTags(request.Tags) || request.Tags["opl_account_id"] != request.AccountId || request.Tags["opl_resource_id"] != storage.Id {
		return Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_cbs_input_invalid", Message: "Exact CBS account, resource, size, compute Zone, and ownership tags are required.", Retryable: false}
	}
	filters := make([]*cbs2017.Filter, 0, len(cbsOwnershipTagKeys))
	for _, key := range cbsOwnershipTagKeys {
		filters = append(filters, &cbs2017.Filter{Name: common.StringPtr("tag:" + key), Values: []*string{common.StringPtr(request.Tags[key])}})
	}
	exactDisks, exactRequestID, failure := client.describeRecoveryDisks(filters)
	if failure != nil {
		return *failure
	}
	if len(exactDisks) > 1 {
		return Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_cbs_multiple_candidate", Message: "Tencent CBS discovery must return at most one exact disk.", ProviderRequestId: exactRequestID, Retryable: false}
	}
	nameDisks, nameRequestID, failure := client.describeRecoveryDisks([]*cbs2017.Filter{{Name: common.StringPtr("disk-name"), Values: []*string{common.StringPtr(storage.Id)}}})
	if failure != nil {
		return *failure
	}
	requestID := firstNonEmpty(nameRequestID, exactRequestID)
	if len(exactDisks) == 0 && len(nameDisks) == 0 {
		return Response{Ok: true, StorageState: "storage_not_started", Status: "absent", ProviderRequestId: requestID, ProviderData: map[string]string{"region": client.region}, MutationCount: 0}
	}
	if len(nameDisks) > 1 {
		return Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_cbs_multiple_candidate", Message: "Tencent CBS discovery must return at most one logical disk.", ProviderRequestId: requestID, Retryable: false}
	}
	var disk *cbs2017.Disk
	switch {
	case len(exactDisks) == 1 && len(nameDisks) == 1:
		if exactDisks[0] == nil || nameDisks[0] == nil || stringValue(exactDisks[0].DiskId) != stringValue(nameDisks[0].DiskId) {
			return Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_cbs_identity_mismatch", Message: "Tencent CBS name and ownership tags do not identify the same unique disk.", ProviderRequestId: requestID, Retryable: false}
		}
		disk = nameDisks[0]
	case len(exactDisks) == 1 && exactDisks[0] != nil:
		disk = exactDisks[0]
	case len(nameDisks) == 1 && nameDisks[0] != nil:
		disk = nameDisks[0]
	default:
		return Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_cbs_identity_mismatch", Message: "Tencent CBS name and ownership tags do not identify the same unique disk.", ProviderRequestId: requestID, Retryable: false}
	}
	diskID := stringValue(disk.DiskId)
	exactStorage := storage
	exactStorage.Id = diskID
	facts, err := validateCBSVolume(disk, exactStorage, request.Tags, false)
	if err != nil || !oneMonthCBSPeriod(stringValue(disk.CreateTime), stringValue(disk.DeadlineTime)) {
		return Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_cbs_identity_mismatch", Message: "Tencent CBS billing or identity facts do not match the original launch.", ProviderRequestId: requestID, Retryable: false}
	}
	facts["periodMonths"] = "1"
	facts["describeCbsRequestId"] = requestID
	facts["region"] = client.region
	status := "pending"
	if state := strings.ToUpper(strings.TrimSpace(stringValue(disk.DiskState))); state == "UNATTACHED" || state == "ATTACHED" {
		status = "provider_ready"
	}
	return Response{
		Ok: true, StorageState: "storage_existing_exact", StorageVolumeId: diskID, CBSStatus: stringValue(disk.DiskState), Status: status,
		ProviderRequestId: requestID, ProviderData: facts, MutationCount: 0,
	}
}

func (client *tencentSDKClient) describeRecoveryDisks(filters []*cbs2017.Filter) ([]*cbs2017.Disk, string, *Response) {
	const pageLimit uint64 = 100
	disks := []*cbs2017.Disk{}
	requestID := ""
	totalCount := uint64(0)
	for offset := uint64(0); ; {
		describe := cbs2017.NewDescribeDisksRequest()
		describe.Filters = filters
		describe.Limit = common.Uint64Ptr(pageLimit)
		describe.Offset = common.Uint64Ptr(offset)
		result, err := client.nativeCbsClient.DescribeDisks(describe)
		if err != nil {
			response := sdkErrorResponse("tencent_describe_cbs_failed", err)
			response.StorageState = "unknown"
			response.MutationCount = 0
			return nil, requestID, &response
		}
		if result == nil || result.Response == nil || result.Response.TotalCount == nil || len(result.Response.DiskSet) > int(pageLimit) {
			response := Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_cbs_describe_incomplete", Message: "Tencent CBS DescribeDisks response is missing or incomplete.", ProviderRequestId: requestID, Retryable: true}
			return nil, requestID, &response
		}
		requestID = firstNonEmpty(stringValue(result.Response.RequestId), requestID)
		if offset == 0 {
			totalCount = *result.Response.TotalCount
		} else if *result.Response.TotalCount != totalCount {
			response := Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_cbs_describe_incomplete", Message: "Tencent CBS DescribeDisks total changed during pagination.", ProviderRequestId: requestID, Retryable: true}
			return nil, requestID, &response
		}
		disks = append(disks, result.Response.DiskSet...)
		if uint64(len(disks)) >= totalCount {
			break
		}
		if len(result.Response.DiskSet) == 0 {
			response := Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_cbs_describe_incomplete", Message: "Tencent CBS DescribeDisks pagination ended early.", ProviderRequestId: requestID, Retryable: true}
			return nil, requestID, &response
		}
		offset += uint64(len(result.Response.DiskSet))
	}
	if uint64(len(disks)) != totalCount {
		response := Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_cbs_describe_incomplete", Message: "Tencent CBS DescribeDisks result count is inconsistent.", ProviderRequestId: requestID, Retryable: true}
		return nil, requestID, &response
	}
	return disks, requestID, nil
}

func oneMonthCBSPeriod(created, deadline string) bool {
	createdAt, createdErr := parseTencentTime(created)
	deadlineAt, deadlineErr := parseTencentTime(deadline)
	if createdErr != nil || deadlineErr != nil {
		return false
	}
	delta := deadlineAt.Sub(createdAt.AddDate(0, 1, 0))
	return delta >= -time.Minute && delta <= time.Minute
}

func (client *tencentSDKClient) SyncStorageVolume(request Request, _ map[string]string) Response {
	if client == nil || client.nativeCbsClient == nil {
		return Response{Ok: false, ErrorCode: "tencent_sdk_client_missing", Message: "Tencent CBS SDK client is missing.", Retryable: false}
	}
	if identityFailure := client.storageProviderRegionFailure(request); identityFailure != nil {
		return *identityFailure
	}
	return client.storageVolumeReadback(request, true)
}

func (client *tencentSDKClient) RenewStorageVolume(request Request, _ map[string]string) Response {
	if client == nil || client.nativeCbsClient == nil {
		return Response{Ok: false, ErrorCode: "tencent_sdk_client_missing", Message: "Tencent CBS SDK client is missing.", Retryable: false}
	}
	if identityFailure := client.storageProviderRegionFailure(request); identityFailure != nil {
		return *identityFailure
	}
	request.Storage.Deadline = normalizeTencentDeadline(request.Storage.Deadline)
	if request.Storage.Deadline == "" {
		return Response{Ok: false, ErrorCode: "tencent_cbs_deadline_required", Message: "The current CBS deadline is required for idempotent renewal.", Retryable: false}
	}
	current := client.storageVolumeReadback(request, true)
	if !current.Ok || current.Status == "external_deleted" {
		return current
	}
	if current.ProviderData["deadline"] != request.Storage.Deadline {
		if !deadlineAfter(current.ProviderData["deadline"], request.Storage.Deadline) {
			return Response{Ok: false, ErrorCode: "tencent_cbs_renewal_deadline_invalid", Message: "Tencent CBS renewal deadline did not increase.", ProviderRequestId: current.ProviderRequestId, ProviderData: current.ProviderData, Retryable: true}
		}
		current.ProviderData["renewalResult"] = "already_renewed"
		return current
	}
	renew := cbs2017.NewRenewDiskRequest()
	renew.DiskId = common.StringPtr(request.Storage.Id)
	renew.DiskChargePrepaid = &cbs2017.DiskChargePrepaid{Period: common.Uint64Ptr(1), RenewFlag: common.StringPtr("NOTIFY_AND_MANUAL_RENEW")}
	renewed, err := client.nativeCbsClient.RenewDisk(renew)
	if err != nil {
		return sdkErrorResponse("tencent_renew_cbs_failed", err)
	}
	if renewed == nil || renewed.Response == nil {
		return Response{Ok: false, ErrorCode: "tencent_renew_cbs_response_missing", Message: "Tencent CBS renewal response is missing.", Retryable: true}
	}
	response := client.storageVolumeReadback(request, false)
	if response.Ok && !deadlineAfter(response.ProviderData["deadline"], request.Storage.Deadline) {
		return Response{Ok: false, ErrorCode: "tencent_cbs_renewal_unconfirmed", Message: "Tencent CBS renewal deadline did not increase.", ProviderRequestId: stringValue(renewed.Response.RequestId), ProviderData: response.ProviderData, Retryable: true}
	}
	if response.ProviderData == nil {
		response.ProviderData = map[string]string{}
	}
	response.ProviderData["renewCbsRequestId"] = stringValue(renewed.Response.RequestId)
	response.ProviderData["renewalResult"] = "renewed"
	response.ProviderRequestId = firstNonEmpty(stringValue(renewed.Response.RequestId), response.ProviderRequestId)
	return response
}

func (client *tencentSDKClient) storageVolumeReadback(request Request, allowAbsent bool) Response {
	storage := request.Storage
	storage.DiskType = strings.TrimSpace(storage.DiskType)
	if !validCBSReadbackInput(storage, request.Tags) {
		return Response{Ok: false, ErrorCode: "tencent_cbs_input_invalid", Message: "Exact CBS disk identity, size, compute Zone, and ownership tags are required.", Retryable: false}
	}
	describe := cbs2017.NewDescribeDisksRequest()
	describe.DiskIds = []*string{common.StringPtr(storage.Id)}
	result, err := client.nativeCbsClient.DescribeDisks(describe)
	if err != nil {
		if sdkErr, ok := err.(*tcerrors.TencentCloudSDKError); allowAbsent && ok && sdkErr.Code == cbs2017.INVALIDDISKID_NOTFOUND {
			return storageDestroyResponseEvidence(Response{Ok: true, StorageVolumeId: storage.Id, CBSStatus: "NOT_FOUND", Status: "external_deleted", ProviderRequestId: sdkErr.RequestId, ProviderData: map[string]string{"storageVolumeId": storage.Id, "cbsStatus": "NOT_FOUND", "describeCbsRequestId": sdkErr.RequestId, "region": client.region}}, "absence_confirmed", 0)
		}
		return sdkErrorResponse("tencent_describe_cbs_failed", err)
	}
	requestID := ""
	if result != nil && result.Response != nil {
		requestID = stringValue(result.Response.RequestId)
	}
	if allowAbsent && result != nil && result.Response != nil && result.Response.TotalCount != nil && *result.Response.TotalCount == 0 && len(result.Response.DiskSet) == 0 {
		return storageDestroyResponseEvidence(Response{Ok: true, StorageVolumeId: storage.Id, CBSStatus: "NOT_FOUND", Status: "external_deleted", ProviderRequestId: requestID, ProviderData: map[string]string{"storageVolumeId": storage.Id, "cbsStatus": "NOT_FOUND", "describeCbsRequestId": requestID, "region": client.region}}, "absence_confirmed", 0)
	}
	if result == nil || result.Response == nil || result.Response.TotalCount == nil || *result.Response.TotalCount != 1 || len(result.Response.DiskSet) != 1 || result.Response.DiskSet[0] == nil {
		return Response{Ok: false, ErrorCode: "tencent_cbs_readback_cardinality_mismatch", Message: "Tencent CBS readback must return exactly one disk.", ProviderRequestId: requestID, Retryable: true}
	}
	disk := result.Response.DiskSet[0]
	deleting := request.Action == "read_storage_for_delete" || request.Action == "destroy_storage_volume"
	facts, err := validateCBSVolume(disk, storage, request.Tags, deleting)
	if err != nil {
		return Response{Ok: false, ErrorCode: cbsReadbackValidationErrorCode(err), Message: "Tencent CBS billing or identity facts do not match the requested volume.", ProviderRequestId: requestID, Retryable: true}
	}
	state := stringValue(disk.DiskState)
	facts["describeCbsRequestId"] = requestID
	facts["region"] = client.region
	status := "pending"
	if state == "UNATTACHED" || state == "ATTACHED" {
		status = "provider_ready"
	}
	return Response{
		Ok: true, StorageVolumeId: storage.Id, CBSStatus: state, Status: status, ProviderRequestId: requestID,
		ProviderData: facts,
	}
}

func storageDestroyResponseEvidence(response Response, phase string, mutationCount int) Response {
	response.MutationCount = mutationCount
	if response.ProviderData == nil {
		response.ProviderData = map[string]string{}
	}
	response.ProviderData["storageDestroyPhase"] = phase
	response.ProviderData["storageDestroyMutationCount"] = strconv.Itoa(mutationCount)
	if response.Status != "" {
		response.ProviderData["status"] = response.Status
	}
	return response
}

func (client *tencentSDKClient) DestroyStorageVolume(request Request, env map[string]string) Response {
	if client == nil || client.nativeCbsClient == nil {
		return Response{Ok: false, ErrorCode: "tencent_sdk_client_missing", Message: "Tencent CBS SDK client is missing.", Retryable: false}
	}
	if identityFailure := client.storageProviderRegionFailure(request); identityFailure != nil {
		return *identityFailure
	}
	storage := request.Storage
	storage.DiskType = strings.TrimSpace(storage.DiskType)
	request.Storage = storage
	if strings.TrimSpace(request.AccountId) == "" || request.Tags["opl_account_id"] != request.AccountId || !validCBSReadbackInput(storage, request.Tags) {
		return Response{Ok: false, ErrorCode: "tencent_cbs_input_invalid", Message: "Exact CBS account, disk identity, size, compute Zone, and ownership tags are required.", Retryable: false}
	}
	detachAttempts := intFromEnv(env, "TENCENT_CBS_DETACH_ATTEMPTS", 30)
	detachDelayMS := nonNegativeIntFromEnv(env, "TENCENT_CBS_DETACH_DELAY_MS", 10000)
	current := Response{}
	for attempt := 1; attempt <= detachAttempts; attempt++ {
		current = client.storageVolumeReadback(request, true)
		if !current.Ok {
			if strings.HasPrefix(current.ErrorCode, "tencent_cbs_readback_") {
				current.Retryable = false
			}
			return storageDestroyResponseEvidence(current, "precondition_unconfirmed", 0)
		}
		if current.Status == "external_deleted" {
			return current
		}
		switch strings.ToUpper(strings.TrimSpace(current.CBSStatus)) {
		case "UNATTACHED":
		case "ATTACHED":
			if attempt == detachAttempts {
				return storageDestroyResponseEvidence(Response{
					Ok: false, StorageVolumeId: storage.Id, CBSStatus: current.CBSStatus, ErrorCode: "storage_volume_detach_unverified",
					Message: "Tencent CBS is still ATTACHED after bounded detach reconciliation.", ProviderRequestId: current.ProviderRequestId, ProviderData: current.ProviderData, Retryable: true,
				}, "terminate_not_attempted", 0)
			}
			if detachDelayMS > 0 {
				time.Sleep(time.Duration(detachDelayMS) * time.Millisecond)
			}
			continue
		default:
			return storageDestroyResponseEvidence(Response{
				Ok: false, StorageVolumeId: storage.Id, CBSStatus: current.CBSStatus, ErrorCode: "storage_volume_detach_state_unverified",
				Message: "Tencent CBS detach state is unknown.", ProviderRequestId: current.ProviderRequestId, ProviderData: current.ProviderData, Retryable: false,
			}, "precondition_unconfirmed", 0)
		}
		break
	}

	terminate := cbs2017.NewTerminateDisksRequest()
	terminate.DiskIds = []*string{common.StringPtr(storage.Id)}
	terminated, terminateErr := client.nativeCbsClient.TerminateDisks(terminate)
	terminateRequestID := ""
	if terminated != nil && terminated.Response != nil {
		terminateRequestID = stringValue(terminated.Response.RequestId)
	}

	attempts := intFromEnv(env, "TENCENT_CBS_DELETE_ATTEMPTS", 30)
	if attempts < 1 {
		attempts = 1
	}
	delayMS := nonNegativeIntFromEnv(env, "TENCENT_CBS_DELETE_DELAY_MS", 10000)
	for attempt := 1; attempt <= attempts; attempt++ {
		readback := client.storageVolumeReadback(request, true)
		if readback.ProviderData == nil {
			readback.ProviderData = map[string]string{}
		}
		if terminateRequestID != "" {
			readback.ProviderData["terminateCbsRequestId"] = terminateRequestID
		}
		if !readback.Ok {
			return storageDestroyResponseEvidence(readback, "terminate_attempted", 1)
		}
		if readback.Status == "external_deleted" && readback.CBSStatus == "NOT_FOUND" {
			readback.ProviderRequestId = firstNonEmpty(terminateRequestID, readback.ProviderRequestId)
			return storageDestroyResponseEvidence(readback, "absence_confirmed", 1)
		}
		if attempt == attempts {
			message := "Tencent CBS is still present after TerminateDisks."
			if terminateErr != nil {
				message = terminateErr.Error()
			}
			return storageDestroyResponseEvidence(Response{
				Ok: false, StorageVolumeId: storage.Id, CBSStatus: readback.CBSStatus, ErrorCode: "storage_volume_delete_unverified", Message: message,
				ProviderRequestId: firstNonEmpty(terminateRequestID, readback.ProviderRequestId), ProviderData: readback.ProviderData, Retryable: true, MutationCount: 1,
			}, "terminate_attempted", 1)
		}
		if delayMS > 0 {
			time.Sleep(time.Duration(delayMS) * time.Millisecond)
		}
	}
	return storageDestroyResponseEvidence(Response{Ok: false, StorageVolumeId: storage.Id, ErrorCode: "storage_volume_delete_unverified", Message: "Tencent CBS deletion did not reach an authoritative terminal state.", Retryable: true}, "terminate_attempted", 1)
}

func (client *tencentSDKClient) storageProviderRegionFailure(request Request) *Response {
	if request.Region == "" || request.Region != client.region {
		return &Response{
			Ok: false, ErrorCode: "storage_provider_config_identity_mismatch",
			Message:   "Persisted storage region must exactly match the active Tencent provider configuration.",
			Retryable: false, MutationCount: 0,
		}
	}
	return nil
}

func validCBSReadbackInput(storage StorageInput, tags map[string]string) bool {
	if !strings.HasPrefix(storage.Id, "disk-") || storage.SizeGB == 0 || strings.TrimSpace(storage.Zone) == "" || strings.TrimSpace(storage.DiskType) == "" {
		return false
	}
	return validCBSOwnershipTags(tags)
}

func validCBSOwnershipTags(tags map[string]string) bool {
	for _, key := range cbsOwnershipTagKeys {
		if strings.TrimSpace(tags[key]) == "" {
			return false
		}
	}
	return true
}

func validateCBSVolume(disk *cbs2017.Disk, storage StorageInput, expectedTags map[string]string, deleting bool) (map[string]string, error) {
	if disk == nil || !validCBSReadbackInput(storage, expectedTags) {
		return nil, cbsReadbackValidationError{code: "tencent_cbs_readback_identity_missing"}
	}
	zone := ""
	if disk.Placement != nil {
		zone = stringValue(disk.Placement.Zone)
	}
	deadline := normalizeTencentDeadline(stringValue(disk.DeadlineTime))
	actualTags := map[string]string{}
	for _, tag := range disk.Tags {
		if tag == nil || strings.TrimSpace(stringValue(tag.Key)) == "" {
			continue
		}
		key := stringValue(tag.Key)
		if _, duplicate := actualTags[key]; duplicate {
			return nil, cbsReadbackValidationError{code: "tencent_cbs_readback_tag_duplicate"}
		}
		actualTags[key] = stringValue(tag.Value)
	}
	switch {
	case stringValue(disk.DiskId) != storage.Id:
		return nil, cbsReadbackValidationError{code: "tencent_cbs_readback_disk_id_mismatch"}
	case stringValue(disk.DiskName) != expectedTags["opl_resource_id"]:
		return nil, cbsReadbackValidationError{code: "tencent_cbs_readback_disk_name_mismatch"}
	case stringValue(disk.DiskUsage) != "DATA_DISK":
		return nil, cbsReadbackValidationError{code: "tencent_cbs_readback_disk_usage_mismatch"}
	case strings.TrimSpace(stringValue(disk.DiskState)) == "":
		return nil, cbsReadbackValidationError{code: "tencent_cbs_readback_disk_state_missing"}
	case stringValue(disk.DiskChargeType) != "PREPAID":
		return nil, cbsReadbackValidationError{code: "tencent_cbs_readback_charge_type_mismatch"}
	case stringValue(disk.RenewFlag) != "NOTIFY_AND_MANUAL_RENEW" && !(deleting && stringValue(disk.RenewFlag) == "DISABLE_NOTIFY_AND_MANUAL_RENEW"):
		return nil, cbsReadbackValidationError{code: cbsRenewFlagMismatchCode(stringValue(disk.RenewFlag))}
	case stringValue(disk.DiskType) != storage.DiskType:
		return nil, cbsReadbackValidationError{code: "tencent_cbs_readback_disk_type_mismatch"}
	case disk.DiskSize == nil || *disk.DiskSize != storage.SizeGB:
		return nil, cbsReadbackValidationError{code: "tencent_cbs_readback_size_mismatch"}
	case zone != storage.Zone:
		return nil, cbsReadbackValidationError{code: "tencent_cbs_readback_zone_mismatch"}
	case deadline == "":
		return nil, cbsReadbackValidationError{code: "tencent_cbs_readback_deadline_missing"}
	}
	for _, key := range cbsOwnershipTagKeys {
		if actualTags[key] != expectedTags[key] {
			return nil, cbsReadbackValidationError{code: "tencent_cbs_readback_" + key + "_tag_mismatch"}
		}
	}
	facts := map[string]string{
		"storageVolumeId": storage.Id, "cbsStatus": stringValue(disk.DiskState), "diskName": stringValue(disk.DiskName), "diskUsage": stringValue(disk.DiskUsage),
		"diskChargeType": stringValue(disk.DiskChargeType), "diskType": stringValue(disk.DiskType), "renewFlag": stringValue(disk.RenewFlag),
		"sizeGb": strconv.FormatUint(*disk.DiskSize, 10), "zone": zone, "deadline": deadline,
	}
	for _, key := range cbsOwnershipTagKeys {
		facts[key] = actualTags[key]
	}
	return facts, nil
}

func cbsReadbackValidationErrorCode(err error) string {
	var mismatch cbsReadbackValidationError
	if errors.As(err, &mismatch) && strings.HasPrefix(mismatch.code, "tencent_cbs_readback_") {
		return mismatch.code
	}
	return "tencent_cbs_readback_mismatch"
}

func cbsRenewFlagMismatchCode(renewFlag string) string {
	switch strings.TrimSpace(renewFlag) {
	case "":
		return "tencent_cbs_readback_renew_flag_missing"
	case "NOTIFY_AND_AUTO_RENEW":
		return "tencent_cbs_readback_renew_flag_auto_renew"
	case "DISABLE_NOTIFY_AND_MANUAL_RENEW":
		return "tencent_cbs_readback_renew_flag_manual_renew_notify_disabled"
	default:
		return "tencent_cbs_readback_renew_flag_unknown"
	}
}

func cbsTags(tags map[string]string) []*cbs2017.Tag {
	items := []*cbs2017.Tag{}
	for _, key := range cbsOwnershipTagKeys {
		if value := strings.TrimSpace(tags[key]); value != "" {
			items = append(items, &cbs2017.Tag{Key: common.StringPtr(key), Value: common.StringPtr(value)})
		}
	}
	return items
}
