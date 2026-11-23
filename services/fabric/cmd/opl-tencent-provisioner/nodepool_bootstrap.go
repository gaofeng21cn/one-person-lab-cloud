package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tke2022 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tke/v20220501"
)

func buildCreateNativeNodePoolRequest(request Request, env map[string]string) (*tke2022.CreateNodePoolRequest, *Response) {
	missing := missingSpecificEnv(env, []string{
		"TENCENT_DEPLOY_CLUSTER_ID",
		"TENCENT_CVM_SUBNET_ID",
		"TENCENT_CVM_SECURITY_GROUP_IDS",
		"TENCENT_CVM_SYSTEM_DISK_TYPE",
		"TENCENT_CVM_SYSTEM_DISK_SIZE_GB",
	})
	if len(missing) > 0 {
		return nil, &Response{
			Ok:         false,
			ErrorCode:  "tencent_node_pool_env_missing",
			Message:    "Tencent TKE node pool creation environment is incomplete.",
			MissingEnv: missing,
			Retryable:  false,
		}
	}
	nodePoolName := request.Pool.Id
	if strings.TrimSpace(nodePoolName) == "" {
		nodePoolName = "pool-" + request.PackageId + "-" + request.Pool.InstanceType
	}
	if strings.TrimSpace(request.Pool.InstanceType) == "" {
		return nil, &Response{Ok: false, ErrorCode: "instance_type_required", Message: "ComputePool instanceType is required.", Retryable: false}
	}
	if request.Pool.MaxReplicas <= 0 {
		return nil, &Response{Ok: false, ErrorCode: "max_replicas_required", Message: "ComputePool maxReplicas must be explicitly approved.", Retryable: false}
	}
	systemDiskSize, err := strconv.ParseInt(strings.TrimSpace(env["TENCENT_CVM_SYSTEM_DISK_SIZE_GB"]), 10, 64)
	if err != nil || systemDiskSize <= 0 {
		return nil, &Response{Ok: false, ErrorCode: "system_disk_configuration_invalid", Message: "TENCENT_CVM_SYSTEM_DISK_SIZE_GB must be an explicitly configured positive integer.", Retryable: false}
	}
	highThreshold, lowThreshold, gcFailure := workspaceNodeImageGCThresholds(env)
	if gcFailure != nil {
		return nil, gcFailure
	}
	createRequest := tke2022.NewCreateNodePoolRequest()
	createRequest.ClusterId = common.StringPtr(env["TENCENT_DEPLOY_CLUSTER_ID"])
	createRequest.Name = common.StringPtr(nodePoolName)
	createRequest.Type = common.StringPtr("Native")
	createRequest.DeletionProtection = common.BoolPtr(true)
	nodePoolLabels := []*tke2022.Label{
		{Name: common.StringPtr("oplcloud.cn/pool-id"), Value: common.StringPtr(request.Pool.Id)},
		{Name: common.StringPtr("oplcloud.cn/package-id"), Value: common.StringPtr(request.PackageId)},
		{Name: common.StringPtr("oplcloud.cn/instance-type"), Value: common.StringPtr(request.Pool.InstanceType)},
		{Name: common.StringPtr("medopl.cn/workload"), Value: common.StringPtr("workspace")},
	}
	createRequest.Labels = nodePoolLabels
	createRequest.Taints = []*tke2022.Taint{{Key: common.StringPtr("oplcloud.cn/package-id"), Value: common.StringPtr(request.PackageId), Effect: common.StringPtr("NoSchedule")}}
	createRequest.Native = &tke2022.CreateNativeNodePoolParam{
		Scaling: &tke2022.MachineSetScaling{
			MinReplicas:  common.Int64Ptr(0),
			MaxReplicas:  common.Int64Ptr(request.Pool.MaxReplicas),
			CreatePolicy: common.StringPtr("ZonePriority"),
		},
		SubnetIds:          stringsToPtrs(splitCsv(env["TENCENT_CVM_SUBNET_ID"])),
		InstanceChargeType: common.StringPtr("PREPAID"),
		InstanceChargePrepaid: &tke2022.InstanceChargePrepaid{
			Period: common.Uint64Ptr(1), RenewFlag: common.StringPtr("NOTIFY_AND_MANUAL_RENEW"),
		},
		SystemDisk: &tke2022.Disk{
			DiskType: common.StringPtr(strings.TrimSpace(env["TENCENT_CVM_SYSTEM_DISK_TYPE"])),
			DiskSize: common.Int64Ptr(systemDiskSize),
		},
		InstanceTypes:     []*string{common.StringPtr(request.Pool.InstanceType)},
		SecurityGroupIds:  stringsToPtrs(splitCsv(env["TENCENT_CVM_SECURITY_GROUP_IDS"])),
		AutoRepair:        common.BoolPtr(false),
		EnableAutoscaling: common.BoolPtr(false),
		Replicas:          common.Int64Ptr(0),
		MachineType:       common.StringPtr("NativeCVM"),
		AutomationService: common.BoolPtr(true),
		RuntimeRootDir:    common.StringPtr("/var/lib/containerd"),
		KubeletArgs: stringsToPtrs([]string{
			fmt.Sprintf("image-gc-high-threshold=%d", highThreshold),
			fmt.Sprintf("image-gc-low-threshold=%d", lowThreshold),
		}),
	}
	return createRequest, nil
}

func workspaceNodeImageGCThresholds(env map[string]string) (int, int, *Response) {
	high, highErr := strconv.Atoi(strings.TrimSpace(env[workspaceNodeImageGCHighThresholdEnv]))
	low, lowErr := strconv.Atoi(strings.TrimSpace(env[workspaceNodeImageGCLowThresholdEnv]))
	if highErr != nil || lowErr != nil || high < 1 || high > 100 || low < 1 || low >= high {
		return 0, 0, &Response{
			Ok: false, ErrorCode: "workspace_node_image_gc_configuration_invalid",
			Message: "Workspace node image GC thresholds must be integers with 1 <= low < high <= 100.", Retryable: false,
		}
	}
	return high, low, nil
}

func kubeletArgName(value string) string {
	value = strings.TrimLeft(strings.TrimSpace(value), "-")
	name, _, _ := strings.Cut(value, "=")
	return strings.TrimSpace(name)
}

func kubeletArgsValues(values []*string) ([]string, bool) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == nil || strings.TrimSpace(*value) == "" || kubeletArgName(*value) == "" {
			return nil, false
		}
		result = append(result, strings.TrimSpace(*value))
	}
	return result, true
}

func desiredWorkspaceNodeImageGCKubeletArgs(values []*string, high, low int) ([]string, bool) {
	current, ok := kubeletArgsValues(values)
	if !ok {
		return nil, false
	}
	result := make([]string, 0, len(current)+2)
	for _, value := range current {
		switch kubeletArgName(value) {
		case "image-gc-high-threshold", "image-gc-low-threshold":
			continue
		default:
			result = append(result, value)
		}
	}
	return append(result,
		fmt.Sprintf("image-gc-high-threshold=%d", high),
		fmt.Sprintf("image-gc-low-threshold=%d", low),
	), true
}

func workspaceNodeImageGCMatches(values []*string, high, low int) bool {
	highValue, lowValue, highCount, lowCount := "", "", 0, 0
	for _, value := range values {
		if value == nil {
			return false
		}
		name := kubeletArgName(*value)
		_, configured, hasValue := strings.Cut(strings.TrimLeft(strings.TrimSpace(*value), "-"), "=")
		switch name {
		case "image-gc-high-threshold":
			highValue, highCount = configured, highCount+1
		case "image-gc-low-threshold":
			lowValue, lowCount = configured, lowCount+1
		}
		if (name == "image-gc-high-threshold" || name == "image-gc-low-threshold") && !hasValue {
			return false
		}
	}
	return highCount == 1 && lowCount == 1 && highValue == strconv.Itoa(high) && lowValue == strconv.Itoa(low)
}

type bootstrapPackageSpec struct {
	PackageID          string
	PoolID             string
	InstanceType       string
	CPU                uint64
	MemoryGB           uint64
	MaxReplicas        int64
	ExpectedNodePoolID string
}

func bootstrapPackageSpecs(env map[string]string) ([]bootstrapPackageSpec, *Response) {
	profiles, err := configuredTencentSKUProfiles(env)
	if err != nil {
		return nil, &Response{Ok: false, ErrorCode: "tencent_provider_profile_invalid", Message: err.Error(), Retryable: false}
	}
	if err := validateTencentBootstrapProfiles(profiles); err != nil {
		return nil, &Response{Ok: false, ErrorCode: "tencent_provider_profile_invalid", Message: err.Error(), Retryable: false}
	}
	specs := make([]bootstrapPackageSpec, 0, len(profiles))
	for _, item := range profiles {
		if !item.Available {
			continue
		}
		specs = append(specs, bootstrapPackageSpec{
			PackageID: item.ID, PoolID: item.Compute.ID, InstanceType: item.Compute.InstanceType,
			CPU: item.Compute.CPU, MemoryGB: item.Compute.MemoryGB, MaxReplicas: item.MaxReplicas,
			ExpectedNodePoolID: item.NodePoolID,
		})
	}
	if len(specs) == 0 {
		return nil, &Response{Ok: false, ErrorCode: "tencent_provider_profile_invalid", Message: "Tencent Provider Profile has no available Workspace packages.", Retryable: false}
	}
	return specs, nil
}

func canonicalNativeMachineType(value string) (string, bool, bool) {
	switch {
	case strings.EqualFold(strings.TrimSpace(value), "NativeCVM"):
		return "NativeCVM", true, true
	case strings.EqualFold(strings.TrimSpace(value), "Native"):
		return "Native", false, true
	case strings.EqualFold(strings.TrimSpace(value), "CXM"):
		return "CXM", false, true
	default:
		return strings.TrimSpace(value), false, false
	}
}

func validTencentCVMID(value string) bool {
	value = strings.TrimSpace(value)
	return len(value) > len("ins-") && strings.HasPrefix(value, "ins-")
}

func validateBootstrapSystemConfig(env map[string]string, specs []bootstrapPackageSpec) *Response {
	for _, key := range []string{"OPL_SYSTEM_COMPUTE_NODE_POOL_ID", "OPL_SYSTEM_COMPUTE_MACHINE_ID", "OPL_SYSTEM_COMPUTE_NODE_NAME", "OPL_SYSTEM_COMPUTE_MACHINE_TYPE"} {
		if strings.TrimSpace(env[key]) == "" {
			return &Response{Ok: false, ErrorCode: "protected_resource_guard_configuration_invalid", Message: key + " is required for node pool bootstrap.", Retryable: false}
		}
	}
	machineType, cvmApplicable, supported := canonicalNativeMachineType(env["OPL_SYSTEM_COMPUTE_MACHINE_TYPE"])
	if !supported {
		return &Response{Ok: false, ErrorCode: "protected_resource_guard_configuration_invalid", Message: "System MachineType must be NativeCVM, Native, or CXM.", Retryable: false}
	}
	cvmID := strings.TrimSpace(env["OPL_SYSTEM_COMPUTE_CVM_ID"])
	if cvmApplicable && !validTencentCVMID(cvmID) {
		return &Response{Ok: false, ErrorCode: "protected_resource_guard_configuration_invalid", Message: "NativeCVM system identity requires a verified ins-* value.", Retryable: false}
	}
	if !cvmApplicable && cvmID != "" {
		return &Response{Ok: false, ErrorCode: "protected_resource_guard_configuration_invalid", Message: machineType + " system identity must not configure a CVM identity.", Retryable: false}
	}
	systemPoolID := strings.TrimSpace(env["OPL_SYSTEM_COMPUTE_NODE_POOL_ID"])
	seen := map[string]bool{systemPoolID: true}
	for _, spec := range specs {
		if spec.ExpectedNodePoolID == "" {
			continue
		}
		if seen[spec.ExpectedNodePoolID] {
			return &Response{Ok: false, ErrorCode: "protected_resource_guard_configuration_invalid", Message: "System, Basic, and Pro NodePool identities must be distinct.", Retryable: false}
		}
		seen[spec.ExpectedNodePoolID] = true
	}
	return nil
}

func (client *tencentSDKClient) bootstrapNodePoolInventory() ([]*tke2022.NodePool, error) {
	if client == nil || client.nativeTkeClient == nil || client.nativeCvmClient == nil {
		return nil, fmt.Errorf("Tencent TKE and CVM SDK clients are required")
	}
	pools := []*tke2022.NodePool{}
	totalCount := int64(-1)
	for offset := int64(0); ; {
		request := tke2022.NewDescribeNodePoolsRequest()
		request.ClusterId = common.StringPtr(client.clusterId)
		request.Offset = common.Int64Ptr(offset)
		request.Limit = common.Int64Ptr(tkeListPageLimit)
		response, err := client.nativeTkeClient.DescribeNodePools(request)
		if err != nil {
			return nil, err
		}
		if response == nil || response.Response == nil || response.Response.TotalCount == nil || len(response.Response.NodePools) > int(tkeListPageLimit) {
			return nil, fmt.Errorf("Tencent TKE NodePool inventory is missing or incomplete")
		}
		pageTotal := *response.Response.TotalCount
		if pageTotal < 0 || (totalCount >= 0 && pageTotal != totalCount) {
			return nil, fmt.Errorf("Tencent TKE NodePool inventory is missing or incomplete")
		}
		if totalCount < 0 {
			totalCount = pageTotal
		}
		pools = append(pools, response.Response.NodePools...)
		if int64(len(pools)) == totalCount {
			break
		}
		if len(response.Response.NodePools) == 0 || int64(len(pools)) > totalCount {
			return nil, fmt.Errorf("Tencent TKE NodePool inventory is missing or incomplete")
		}
		offset += int64(len(response.Response.NodePools))
	}
	ids := map[string]bool{}
	for _, pool := range pools {
		if pool == nil {
			return nil, fmt.Errorf("Tencent TKE NodePool inventory contains an empty identity")
		}
		id := strings.TrimSpace(stringValue(pool.NodePoolId))
		if len(id) <= len("np-") || !strings.HasPrefix(id, "np-") || ids[id] {
			return nil, fmt.Errorf("Tencent TKE NodePool inventory contains an empty or duplicate identity")
		}
		ids[id] = true
	}
	return pools, nil
}

func poolTouchesBootstrapSpec(pool *tke2022.NodePool, spec bootstrapPackageSpec) bool {
	if pool == nil {
		return false
	}
	labels := nodePoolLabels(pool)
	return stringValue(pool.Name) == spec.PoolID || labels["oplcloud.cn/pool-id"] == spec.PoolID ||
		labels["oplcloud.cn/package-id"] == spec.PackageID ||
		(spec.ExpectedNodePoolID != "" && stringValue(pool.NodePoolId) == spec.ExpectedNodePoolID)
}

func bootstrapNodePoolIDs(pools []*tke2022.NodePool) []string {
	ids := make([]string, 0, len(pools))
	for _, pool := range pools {
		ids = append(ids, strings.TrimSpace(stringValue(pool.NodePoolId)))
	}
	sort.Strings(ids)
	return ids
}

func bootstrapPackagePoolStatus(pool *tke2022.NodePool, spec bootstrapPackageSpec) string {
	if pool == nil || !isCVMNativeNodePool(pool) {
		return ""
	}
	lifeState := strings.ToLower(strings.TrimSpace(stringValue(pool.LifeState)))
	if lifeState != "running" && lifeState != "creating" {
		return ""
	}
	if spec.ExpectedNodePoolID != "" && stringValue(pool.NodePoolId) != spec.ExpectedNodePoolID {
		return ""
	}
	labels := nodePoolLabels(pool)
	if stringValue(pool.Name) != spec.PoolID || labels["oplcloud.cn/pool-id"] != spec.PoolID || labels["oplcloud.cn/package-id"] != spec.PackageID ||
		labels["oplcloud.cn/instance-type"] != spec.InstanceType || labels["medopl.cn/workload"] != "workspace" {
		return ""
	}
	native := pool.Native
	if native == nil || native.Scaling == nil || native.Scaling.MinReplicas == nil || *native.Scaling.MinReplicas != 0 ||
		native.Scaling.MaxReplicas == nil || *native.Scaling.MaxReplicas <= 0 || (spec.MaxReplicas > 0 && *native.Scaling.MaxReplicas != spec.MaxReplicas) || native.Replicas == nil || native.ReadyReplicas == nil ||
		*native.Replicas != *native.ReadyReplicas || native.EnableAutoscaling == nil || *native.EnableAutoscaling || native.AutoRepair == nil || *native.AutoRepair ||
		len(native.InstanceTypes) != 1 || stringValue(native.InstanceTypes[0]) != spec.InstanceType || len(native.SubnetIds) == 0 {
		return ""
	}
	if pool.DeletionProtection == nil || !*pool.DeletionProtection || bootstrapPackageTaintState(pool, spec) != "target" {
		return ""
	}
	if lifeState == "creating" {
		return "pending"
	}
	return "registered"
}

func bootstrapPackagePoolFailureStage(pool *tke2022.NodePool, spec bootstrapPackageSpec) string {
	if pool == nil || !isCVMNativeNodePool(pool) {
		return "package_native_contract_mismatch"
	}
	lifeState := strings.ToLower(strings.TrimSpace(stringValue(pool.LifeState)))
	if lifeState != "running" && lifeState != "creating" {
		return "package_lifecycle_mismatch"
	}
	if spec.ExpectedNodePoolID != "" && stringValue(pool.NodePoolId) != spec.ExpectedNodePoolID {
		return "package_node_pool_id_mismatch"
	}
	labels := nodePoolLabels(pool)
	switch {
	case stringValue(pool.Name) != spec.PoolID:
		return "package_pool_name_mismatch"
	case labels["oplcloud.cn/pool-id"] != spec.PoolID:
		return "package_pool_label_mismatch"
	case labels["oplcloud.cn/package-id"] != spec.PackageID:
		return "package_id_label_mismatch"
	case labels["oplcloud.cn/instance-type"] != spec.InstanceType:
		return "package_instance_type_label_mismatch"
	case labels["medopl.cn/workload"] != "workspace":
		return "package_workload_label_mismatch"
	}
	native := pool.Native
	switch {
	case native == nil || native.Scaling == nil:
		return "package_scaling_missing"
	case native.Scaling.MinReplicas == nil || *native.Scaling.MinReplicas != 0:
		return "package_min_replicas_mismatch"
	case native.Scaling.MaxReplicas == nil || *native.Scaling.MaxReplicas <= 0 || (spec.MaxReplicas > 0 && *native.Scaling.MaxReplicas != spec.MaxReplicas):
		return "package_max_replicas_mismatch"
	case native.Replicas == nil || native.ReadyReplicas == nil || *native.Replicas != *native.ReadyReplicas:
		return "package_replica_readiness_mismatch"
	case native.EnableAutoscaling == nil || *native.EnableAutoscaling:
		return "package_autoscaling_mismatch"
	case native.AutoRepair == nil || *native.AutoRepair:
		return "package_auto_repair_mismatch"
	case len(native.InstanceTypes) != 1 || stringValue(native.InstanceTypes[0]) != spec.InstanceType:
		return "package_instance_type_mismatch"
	case len(native.SubnetIds) == 0:
		return "package_subnet_missing"
	case pool.DeletionProtection == nil || !*pool.DeletionProtection:
		return "package_deletion_protection_mismatch"
	case bootstrapPackageTaintState(pool, spec) != "target":
		return "package_taint_mismatch"
	default:
		return "package_contract_mismatch"
	}
}

func bootstrapPackageTaintState(pool *tke2022.NodePool, spec bootstrapPackageSpec) string {
	if pool == nil || len(pool.Taints) != 1 || pool.Taints[0] == nil || stringValue(pool.Taints[0].Effect) != "NoSchedule" {
		return "conflict"
	}
	taint := pool.Taints[0]
	switch {
	case stringValue(taint.Key) == "oplcloud.cn/package-id" && stringValue(taint.Value) == spec.PackageID:
		return "target"
	case stringValue(taint.Key) == "oplcloud.cn/workspace-id" && stringValue(taint.Value) == "unallocated":
		return "legacy"
	default:
		return "conflict"
	}
}

func bootstrapPackageMigrationStatus(pool *tke2022.NodePool, spec bootstrapPackageSpec) string {
	if pool == nil {
		return ""
	}
	clone := *pool
	clone.Taints = []*tke2022.Taint{{Key: common.StringPtr("oplcloud.cn/package-id"), Value: common.StringPtr(spec.PackageID), Effect: common.StringPtr("NoSchedule")}}
	if bootstrapPackagePoolStatus(&clone, spec) == "" {
		return ""
	}
	return bootstrapPackageTaintState(pool, spec)
}

func nodePoolTaintFacts(pool *tke2022.NodePool) []NodePoolTaintFact {
	if pool == nil {
		return nil
	}
	result := make([]NodePoolTaintFact, 0, len(pool.Taints))
	for _, taint := range pool.Taints {
		if taint == nil {
			continue
		}
		result = append(result, NodePoolTaintFact{Key: stringValue(taint.Key), Value: stringValue(taint.Value), Effect: stringValue(taint.Effect)})
	}
	return result
}

func bootstrapInventoryMatches(pools []*tke2022.NodePool, env map[string]string, specs []bootstrapPackageSpec) (map[string]*tke2022.NodePool, *Response) {
	return bootstrapInventoryMatchesByStatus(pools, env, specs, bootstrapPackagePoolStatus)
}

func bootstrapInventoryMatchesByStatus(
	pools []*tke2022.NodePool,
	env map[string]string,
	specs []bootstrapPackageSpec,
	status func(*tke2022.NodePool, bootstrapPackageSpec) string,
) (map[string]*tke2022.NodePool, *Response) {
	systemPoolID := strings.TrimSpace(env["OPL_SYSTEM_COMPUTE_NODE_POOL_ID"])
	var systemPool *tke2022.NodePool
	for _, pool := range pools {
		if stringValue(pool.NodePoolId) == systemPoolID {
			if systemPool != nil {
				return nil, &Response{Ok: false, ErrorCode: "node_pool_bootstrap_inventory_conflict", Message: "System NodePool identity is duplicated.", FailureStage: "system_pool_duplicate", Retryable: false}
			}
			systemPool = pool
		}
	}
	if systemPool == nil {
		return nil, &Response{Ok: false, ErrorCode: "node_pool_bootstrap_inventory_conflict", Message: "Protected system NodePool was not found.", FailureStage: "system_pool_missing", Retryable: false}
	}
	for _, spec := range specs {
		if poolTouchesBootstrapSpec(systemPool, spec) {
			return nil, &Response{Ok: false, ErrorCode: "node_pool_bootstrap_inventory_conflict", Message: "Protected system NodePool conflicts with a customer package identity.", FailureStage: "system_pool_package_conflict", Retryable: false}
		}
	}
	for _, pool := range pools {
		if pool == systemPool {
			continue
		}
		matchCount := 0
		for _, spec := range specs {
			if poolTouchesBootstrapSpec(pool, spec) {
				matchCount++
			}
		}
		if matchCount != 1 {
			return nil, &Response{Ok: false, ErrorCode: "node_pool_bootstrap_inventory_conflict", Message: "NodePool inventory contains an unknown or ambiguous package identity.", FailureStage: "package_identity_ambiguous", Retryable: false}
		}
	}
	matches := map[string]*tke2022.NodePool{}
	for _, spec := range specs {
		for _, pool := range pools {
			if pool == systemPool || !poolTouchesBootstrapSpec(pool, spec) {
				continue
			}
			if matches[spec.PackageID] != nil {
				return nil, &Response{Ok: false, ErrorCode: "node_pool_bootstrap_inventory_conflict", Message: "Package NodePool inventory is duplicated.", FailureStage: "package_pool_duplicate", Retryable: false}
			}
			if status(pool, spec) == "" {
				return nil, &Response{Ok: false, ErrorCode: "node_pool_bootstrap_inventory_conflict", Message: "Package NodePool does not match the fixed contract.", FailureStage: bootstrapPackagePoolFailureStage(pool, spec), Retryable: false}
			}
			matches[spec.PackageID] = pool
		}
	}
	matchedNodePools := map[string]string{}
	for packageID, pool := range matches {
		nodePoolID := strings.TrimSpace(stringValue(pool.NodePoolId))
		if priorPackageID := matchedNodePools[nodePoolID]; nodePoolID != "" && priorPackageID != "" && priorPackageID != packageID {
			return nil, &Response{Ok: false, ErrorCode: "node_pool_bootstrap_inventory_conflict", Message: "Provider Profile packages cannot share a NodePool.", FailureStage: "package_node_pool_shared", Retryable: false}
		}
		matchedNodePools[nodePoolID] = packageID
	}
	return matches, nil
}

func (client *tencentSDKClient) ReconcileWorkspaceNodePoolImageGC(request Request, env map[string]string) Response {
	high, low, failure := workspaceNodeImageGCThresholds(env)
	if failure != nil {
		return *failure
	}
	specs, failure := bootstrapPackageSpecs(env)
	if failure != nil {
		return *failure
	}
	if failure = validateBootstrapSystemConfig(env, specs); failure != nil {
		return *failure
	}
	pools, err := client.bootstrapNodePoolInventory()
	if err != nil {
		return Response{Ok: false, Status: "unknown", ErrorCode: "node_pool_image_gc_inventory_unavailable", Message: "Tencent NodePool inventory is unavailable.", MutationCount: 0, Retryable: false}
	}
	matches, failure := bootstrapInventoryMatchesByStatus(pools, env, specs, bootstrapPackageMigrationStatus)
	if failure != nil {
		failure.Status = "conflict"
		failure.MutationCount = 0
		failure.NodePoolInventory = bootstrapNodePoolIDs(pools)
		return *failure
	}
	protectedSystem, err := client.verifyBootstrapSystemIdentity(pools, env)
	if err != nil {
		return Response{Ok: false, Status: "conflict", ErrorCode: "protected_system_identity_mismatch", Message: "Protected system identity is unavailable or inconsistent.", ProtectedSystem: protectedSystem, NodePoolInventory: bootstrapNodePoolIDs(pools), MutationCount: 0, Retryable: false}
	}

	results := make([]NodePoolImageGCResult, 0, len(specs))
	required := 0
	for _, spec := range specs {
		pool := matches[spec.PackageID]
		if pool == nil || pool.Native == nil {
			return Response{Ok: false, Status: "conflict", ErrorCode: "node_pool_image_gc_inventory_conflict", Message: "Workspace NodePool does not expose native kubelet configuration.", ProtectedSystem: protectedSystem, NodePoolInventory: bootstrapNodePoolIDs(pools), MutationCount: 0, Retryable: false}
		}
		before, valid := kubeletArgsValues(pool.Native.KubeletArgs)
		after, mergeable := desiredWorkspaceNodeImageGCKubeletArgs(pool.Native.KubeletArgs, high, low)
		if !valid || !mergeable {
			return Response{Ok: false, Status: "conflict", ErrorCode: "node_pool_image_gc_kubelet_args_invalid", Message: "Workspace NodePool kubelet arguments cannot be safely reconciled.", ProtectedSystem: protectedSystem, NodePoolInventory: bootstrapNodePoolIDs(pools), MutationCount: 0, Retryable: false}
		}
		taintState := bootstrapPackageMigrationStatus(pool, spec)
		status := "registered"
		if taintState != "target" || !workspaceNodeImageGCMatches(pool.Native.KubeletArgs, high, low) {
			status = "reconciliation_required"
			required++
		}
		results = append(results, NodePoolImageGCResult{
			PackageID: spec.PackageID, NodePoolID: stringValue(pool.NodePoolId), Status: status,
			HighThresholdPercent: high, LowThresholdPercent: low, UpdateExistingNodes: status != "registered",
			TaintsBefore:      nodePoolTaintFacts(pool),
			TaintsAfter:       []NodePoolTaintFact{{Key: "oplcloud.cn/package-id", Value: spec.PackageID, Effect: "NoSchedule"}},
			KubeletArgsBefore: before, KubeletArgsAfter: after,
		})
	}
	base := Response{Ok: true, Status: "registered", ProtectedSystem: protectedSystem, NodePoolInventory: bootstrapNodePoolIDs(pools), NodePoolImageGC: results, MutationCount: 0}
	if required == 0 {
		return base
	}
	if request.DryRun {
		base.Status = "reconciliation_required"
		return base
	}
	for index := range results {
		if results[index].Status != "reconciliation_required" {
			continue
		}
		modifyRequest := tke2022.NewModifyNodePoolRequest()
		modifyRequest.ClusterId = common.StringPtr(client.clusterId)
		modifyRequest.NodePoolId = common.StringPtr(results[index].NodePoolID)
		modifyRequest.Taints = []*tke2022.Taint{{Key: common.StringPtr("oplcloud.cn/package-id"), Value: common.StringPtr(results[index].PackageID), Effect: common.StringPtr("NoSchedule")}}
		modifyRequest.Native = &tke2022.UpdateNativeNodePoolParam{
			KubeletArgs:             stringsToPtrs(results[index].KubeletArgsAfter),
			UpdateExistedNode:       common.BoolPtr(true),
			UpdateMachineManagement: common.StringPtr("enable"),
		}
		base.MutationCount++
		modified, modifyErr := client.nativeTkeClient.ModifyNodePool(modifyRequest)
		if modifyErr != nil || modified == nil || modified.Response == nil || strings.TrimSpace(stringValue(modified.Response.RequestId)) == "" {
			base.Ok, base.Status, base.ErrorCode = false, "unknown", "node_pool_image_gc_reconcile_result_unknown"
			base.Message = "Tencent NodePool image GC reconciliation result is unknown; reconcile by GET only."
			base.FailureStage = "modify_node_pool"
			base.ProviderErrorClass = safeTencentProviderErrorCode(modifyErr)
			results[index].Status = "unknown"
			base.NodePoolImageGC = results
			return base
		}
		readback, _, readbackErr := client.describeNativeNodePool(results[index].NodePoolID)
		if readbackErr != nil || bootstrapPackagePoolStatus(readback, specs[index]) != "registered" || readback.Native == nil || !workspaceNodeImageGCMatches(readback.Native.KubeletArgs, high, low) {
			base.Ok, base.Status, base.ErrorCode = false, "unknown", "node_pool_image_gc_reconcile_readback_unknown"
			base.Message = "Tencent NodePool image GC reconciliation readback is unknown; reconcile by GET only."
			base.FailureStage = "node_pool_readback"
			base.ProviderErrorClass = safeTencentProviderErrorCode(readbackErr)
			results[index].Status = "unknown"
			base.NodePoolImageGC = results
			return base
		}
		results[index].Status = "reconciled"
		results[index].TaintsAfter = nodePoolTaintFacts(readback)
		results[index].KubeletArgsAfter, _ = kubeletArgsValues(readback.Native.KubeletArgs)
	}
	base.Status, base.NodePoolImageGC = "reconciled", results
	return base
}

func newProtectedSystemFacts() ProtectedSystemFacts {
	return ProtectedSystemFacts{
		PoolCheckStatus:    protectedCheckNotChecked,
		MachineCheckStatus: protectedCheckNotChecked,
		NodeCheckStatus:    protectedCheckNotChecked,
		CVMCheckStatus:     protectedCheckNotChecked,
	}
}

func (client *tencentSDKClient) verifyBootstrapSystemIdentity(pools []*tke2022.NodePool, env map[string]string) (ProtectedSystemFacts, error) {
	facts := newProtectedSystemFacts()
	systemPoolID := strings.TrimSpace(env["OPL_SYSTEM_COMPUTE_NODE_POOL_ID"])
	wantMachine := strings.TrimSpace(env["OPL_SYSTEM_COMPUTE_MACHINE_ID"])
	wantNode := strings.TrimSpace(env["OPL_SYSTEM_COMPUTE_NODE_NAME"])
	if systemPoolID == "" {
		facts.PoolCheckStatus = protectedCheckFailed
		return facts, fmt.Errorf("protected system NodePool identity is required")
	}
	var systemPool *tke2022.NodePool
	for _, pool := range pools {
		if pool != nil && stringValue(pool.NodePoolId) == systemPoolID {
			if systemPool != nil {
				facts.PoolCheckStatus = protectedCheckFailed
				return facts, fmt.Errorf("protected system NodePool identity is ambiguous")
			}
			systemPool = pool
		}
	}
	if systemPool == nil {
		facts.PoolCheckStatus = protectedCheckFailed
		return facts, fmt.Errorf("protected system NodePool identity is unavailable")
	}
	facts.NodePoolID = systemPoolID
	facts.PoolCheckStatus = protectedCheckPassed
	rawMachineType := ""
	if systemPool.Native != nil {
		rawMachineType = stringValue(systemPool.Native.MachineType)
	}
	machineType, cvmApplicable, supportedMachineType := canonicalNativeMachineType(rawMachineType)
	facts.MachineType = machineType
	facts.CVMApplicable = cvmApplicable
	if wantMachine == "" {
		facts.MachineCheckStatus = protectedCheckFailed
		return facts, fmt.Errorf("protected system Machine identity is required")
	}
	machines, _, err := client.describeClusterMachines(systemPoolID)
	if err != nil {
		facts.MachineCheckStatus = protectedCheckFailed
		return facts, err
	}
	var matched *tke2022.Machine
	for _, machine := range machines {
		if machine != nil && stringValue(machine.MachineName) == wantMachine {
			if matched != nil {
				facts.MachineCheckStatus = protectedCheckFailed
				return facts, fmt.Errorf("protected system Machine identity is ambiguous")
			}
			matched = machine
		}
	}
	if matched == nil {
		facts.MachineCheckStatus = protectedCheckFailed
		return facts, fmt.Errorf("protected system Machine identity does not match")
	}
	facts.MachineID = wantMachine
	facts.MachineCheckStatus = protectedCheckPassed
	if wantNode == "" {
		facts.NodeCheckStatus = protectedCheckFailed
		return facts, fmt.Errorf("protected system Node identity is required")
	}
	nodeMatches := 0
	for _, machine := range machines {
		if machine != nil && kubernetesNodeName(machine) == wantNode {
			nodeMatches++
		}
	}
	if kubernetesNodeName(matched) != wantNode || nodeMatches != 1 {
		facts.NodeCheckStatus = protectedCheckFailed
		return facts, fmt.Errorf("protected system Machine and Node identities do not match uniquely")
	}
	facts.NodeName = wantNode
	facts.NodeCheckStatus = protectedCheckPassed
	if !supportedMachineType {
		facts.CVMCheckStatus = protectedCheckFailed
		return facts, fmt.Errorf("protected system MachineType is unsupported")
	}
	configuredMachineType := strings.TrimSpace(env["OPL_SYSTEM_COMPUTE_MACHINE_TYPE"])
	if configuredMachineType != "" {
		expectedMachineType, _, configuredMachineTypeSupported := canonicalNativeMachineType(configuredMachineType)
		if !configuredMachineTypeSupported || expectedMachineType != machineType {
			facts.CVMCheckStatus = protectedCheckFailed
			return facts, fmt.Errorf("protected system MachineType does not match configured identity")
		}
	}
	configuredCVMID := strings.TrimSpace(env["OPL_SYSTEM_COMPUTE_CVM_ID"])
	if !cvmApplicable {
		if configuredCVMID != "" {
			facts.CVMCheckStatus = protectedCheckFailed
			return facts, fmt.Errorf("protected non-CVM system identity configured an unexpected CVM")
		}
		facts.CVMCheckStatus = protectedCheckNotApplicable
		return facts, nil
	}
	cvm, _, err := client.describeCvmInstanceByPrivateIp(stringValue(matched.LanIP))
	if err != nil || cvm == nil {
		facts.CVMCheckStatus = protectedCheckFailed
		return facts, fmt.Errorf("protected system CVM identity does not match")
	}
	resolvedCVMID := strings.TrimSpace(stringValue(cvm.InstanceId))
	if !validTencentCVMID(resolvedCVMID) || (configuredCVMID != "" && configuredCVMID != resolvedCVMID) {
		facts.CVMCheckStatus = protectedCheckFailed
		return facts, fmt.Errorf("protected system CVM identity does not match")
	}
	facts.CVMID = resolvedCVMID
	facts.CVMCheckStatus = protectedCheckPassed
	return facts, nil
}

func bootstrapInventoryCapacity(request Request, env map[string]string) (int64, *Response) {
	const immediateHeadroom int64 = 1
	if request.RequiredCapacity > 0 && request.RequiredCapacity != immediateHeadroom {
		return 0, &Response{Ok: false, ErrorCode: "workspace_capacity_input_mismatch", Message: "bootstrap requiredCapacity must be exactly one immediate launch headroom.", Retryable: false}
	}
	return immediateHeadroom, nil
}

func withBootstrapInventoryFacts(response Response, inventory Response, requiredCapacity int64) Response {
	response.RequiredCapacity = requiredCapacity
	response.PrepaidQuotaRemaining = inventory.PrepaidQuotaRemaining
	response.Subnets = inventory.Subnets
	response.TKEClusterNodeLimit = inventory.TKEClusterNodeLimit
	response.TKECurrentNodeCount = inventory.TKECurrentNodeCount
	response.TKEAvailableNodeCapacity = inventory.TKEAvailableNodeCapacity
	response.TKECapacity = inventory.TKECapacity
	response.ProtectedSystem = inventory.ProtectedSystem
	response.NodePoolInventory = inventory.NodePoolInventory
	return response
}

func selectedWorkspaceSKU(inventory Response, packageID, configured string) (string, error) {
	for _, current := range inventory.SKUPackages {
		if current.PackageID != packageID {
			continue
		}
		selected := strings.TrimSpace(configured)
		if selected == "" {
			selected = current.RecommendedInstanceType
		}
		for _, candidate := range current.Candidates {
			if candidate.InstanceType == selected {
				return selected, nil
			}
		}
		return "", fmt.Errorf("selected %s instance type is not an eligible current PREPAID candidate", packageID)
	}
	return "", fmt.Errorf("%s SKU inventory is missing", packageID)
}

func existingBootstrapPackageSKU(pool *tke2022.NodePool, spec bootstrapPackageSpec) string {
	if pool == nil || stringValue(pool.Name) != spec.PoolID {
		return ""
	}
	labels := nodePoolLabels(pool)
	if labels["oplcloud.cn/pool-id"] != spec.PoolID || labels["oplcloud.cn/package-id"] != spec.PackageID || pool.Native == nil || len(pool.Native.InstanceTypes) != 1 {
		return ""
	}
	labelSKU := strings.TrimSpace(labels["oplcloud.cn/instance-type"])
	nativeSKU := strings.TrimSpace(stringValue(pool.Native.InstanceTypes[0]))
	if labelSKU == "" || labelSKU != nativeSKU {
		return ""
	}
	return labelSKU
}

func preserveExistingBootstrapSKUs(pools []*tke2022.NodePool, specs []bootstrapPackageSpec, systemPoolID string) *Response {
	for index := range specs {
		var existing *tke2022.NodePool
		for _, pool := range pools {
			if pool == nil || stringValue(pool.NodePoolId) == systemPoolID || !poolTouchesBootstrapSpec(pool, specs[index]) {
				continue
			}
			if existing != nil {
				existing = nil
				break
			}
			existing = pool
		}
		if existing == nil {
			continue
		}
		existingSKU := existingBootstrapPackageSKU(existing, specs[index])
		if existingSKU == "" {
			continue
		}
		if existingSKU != specs[index].InstanceType {
			return &Response{Ok: false, ErrorCode: "workspace_sku_profile_conflict", Message: "Existing Tencent NodePool SKU differs from the configured Provider Profile.", Retryable: false, MutationCount: 0}
		}
	}
	return nil
}

func (client *tencentSDKClient) BootstrapComputeNodePools(request Request, env map[string]string) Response {
	requiredCapacity, failure := bootstrapInventoryCapacity(request, env)
	if failure != nil {
		return *failure
	}
	inventory := client.WorkspaceSKUInventory(Request{Action: "workspace_sku_inventory", Zone: request.Zone, RequiredCapacity: requiredCapacity}, env)
	if !inventory.Ok {
		return inventory
	}
	profiles, profileErr := configuredTencentSKUProfiles(env)
	if profileErr != nil {
		return Response{Ok: false, ErrorCode: "tencent_provider_profile_invalid", Message: profileErr.Error(), Retryable: false, MutationCount: 0}
	}
	for _, item := range profiles {
		if !item.Available {
			continue
		}
		if _, err := selectedWorkspaceSKU(inventory, item.ID, item.Compute.InstanceType); err != nil {
			return Response{Ok: false, ErrorCode: "workspace_sku_selection_invalid", Message: err.Error(), Retryable: false, MutationCount: 0}
		}
	}
	specs, failure := bootstrapPackageSpecs(env)
	if failure != nil {
		return *failure
	}
	if failure = validateBootstrapSystemConfig(env, specs); failure != nil {
		return *failure
	}
	pools, err := client.bootstrapNodePoolInventory()
	if err != nil {
		return Response{Ok: false, ErrorCode: "node_pool_bootstrap_inventory_unavailable", Message: "Tencent NodePool inventory is unavailable.", Retryable: false}
	}
	inventory.NodePoolInventory = bootstrapNodePoolIDs(pools)
	if failure = preserveExistingBootstrapSKUs(pools, specs, strings.TrimSpace(env["OPL_SYSTEM_COMPUTE_NODE_POOL_ID"])); failure != nil {
		failure.NodePoolInventory = inventory.NodePoolInventory
		return *failure
	}
	matches, failure := bootstrapInventoryMatches(pools, env, specs)
	if failure != nil {
		failure.NodePoolInventory = inventory.NodePoolInventory
		return *failure
	}
	protectedSystem, err := client.verifyBootstrapSystemIdentity(pools, env)
	if err != nil {
		return Response{Ok: false, ErrorCode: "protected_system_identity_mismatch", Message: "Protected system identity is unavailable or inconsistent.", ProtectedSystem: protectedSystem, NodePoolInventory: inventory.NodePoolInventory, MutationCount: 0, Retryable: false}
	}
	inventory.ProtectedSystem = protectedSystem
	results := make([]NodePoolBootstrapResult, 0, len(specs))
	missing, pendingCount := 0, 0
	for _, spec := range specs {
		result := NodePoolBootstrapResult{
			PackageID: spec.PackageID, PoolID: spec.PoolID, InstanceType: spec.InstanceType, CPU: spec.CPU, MemoryGB: spec.MemoryGB, MaxReplicas: spec.MaxReplicas,
			MaxReplicasSource: "provider_profile", MaxReplicasDecision: "deployment_profile_approved",
			MaxReplicasConstraint:     "positive_integer_subject_to_current_tencent_account_region_quota",
			MaxReplicasRecommendation: "deployment_owner_updates_provider_profile_after_inventory_and_quota_review",
		}
		if pool := matches[spec.PackageID]; pool != nil {
			result.NodePoolID = stringValue(pool.NodePoolId)
			result.Status = bootstrapPackagePoolStatus(pool, spec)
			if spec.MaxReplicas == 0 && pool.Native != nil && pool.Native.Scaling != nil && pool.Native.Scaling.MaxReplicas != nil {
				result.MaxReplicas = *pool.Native.Scaling.MaxReplicas
				result.MaxReplicasSource = "tencent_node_pool_readback"
				result.MaxReplicasDecision = "existing_pool_inventory"
			}
			if result.Status == "pending" {
				pendingCount++
			}
		} else {
			result.Status = "missing"
			missing++
		}
		results = append(results, result)
	}
	if request.DryRun {
		status := "registered"
		if missing > 0 {
			status = "missing"
		} else if pendingCount > 0 {
			status = "pending"
		}
		return withBootstrapInventoryFacts(Response{Ok: true, Status: status, NodePools: results, MutationCount: 0}, inventory, requiredCapacity)
	}
	mutationCount, createdCount, failedCount := 0, 0, 0
	for index, spec := range specs {
		if results[index].Status != "missing" {
			continue
		}
		createRequest, buildFailure := buildCreateNativeNodePoolRequest(Request{PackageId: spec.PackageID, Pool: ComputePoolInput{Id: spec.PoolID, InstanceType: spec.InstanceType, MaxReplicas: spec.MaxReplicas}}, env)
		if buildFailure != nil {
			results[index].Status = "failed"
			results[index].ErrorCode = buildFailure.ErrorCode
			failedCount++
			continue
		}
		mutationCount++
		created, createErr := client.nativeTkeClient.CreateNodePool(createRequest)
		if createErr != nil || created == nil || created.Response == nil || !strings.HasPrefix(strings.TrimSpace(stringValue(created.Response.NodePoolId)), "np-") {
			results[index].Status = "failed"
			results[index].ErrorCode = "node_pool_create_failed"
			failedCount++
			continue
		}
		createdID := strings.TrimSpace(stringValue(created.Response.NodePoolId))
		if createdID == strings.TrimSpace(env["OPL_SYSTEM_COMPUTE_NODE_POOL_ID"]) || (index > 0 && results[0].NodePoolID == createdID) {
			results[index].Status = "failed"
			results[index].ErrorCode = "node_pool_create_identity_conflict"
			failedCount++
			continue
		}
		readback, _, readbackErr := client.describeNativeNodePool(createdID)
		readbackStatus := bootstrapPackagePoolStatus(readback, bootstrapPackageSpec{
			PackageID: spec.PackageID, PoolID: spec.PoolID, InstanceType: spec.InstanceType, MaxReplicas: spec.MaxReplicas, ExpectedNodePoolID: createdID,
		})
		if readbackErr != nil || readbackStatus == "" {
			results[index].NodePoolID = createdID
			results[index].Status = "failed"
			results[index].ErrorCode = "node_pool_create_readback_failed"
			failedCount++
			continue
		}
		results[index].NodePoolID = createdID
		if readbackStatus == "pending" {
			results[index].Status = "pending"
			pendingCount++
		} else {
			results[index].Status = "created"
			createdCount++
		}
	}
	if failedCount > 0 || pendingCount > 0 {
		return withBootstrapInventoryFacts(Response{Ok: false, Status: "partial", ErrorCode: "node_pool_bootstrap_partial", Message: "One or more package NodePools were not created; existing successful pools were preserved.", NodePools: results, MutationCount: mutationCount, Retryable: false}, inventory, requiredCapacity)
	}
	status := "completed"
	if createdCount == len(specs) {
		status = "created"
	} else if createdCount == 0 {
		status = "registered"
	}
	return withBootstrapInventoryFacts(Response{Ok: true, Status: status, NodePools: results, MutationCount: mutationCount}, inventory, requiredCapacity)
}

func nativeReplicas(pool *tke2022.NodePool) int64 {
	if pool == nil || pool.Native == nil || pool.Native.Replicas == nil {
		return 0
	}
	return *pool.Native.Replicas
}
