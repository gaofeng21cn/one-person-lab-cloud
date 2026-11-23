package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tcerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/internal/protectedresource"
)

var requiredTencentEnv = []string{
	"TENCENTCLOUD_SECRET_ID",
	"TENCENTCLOUD_SECRET_KEY",
	"TENCENTCLOUD_REGION",
	"TENCENT_DEPLOY_CLUSTER_ID",
}

var errCVMInstanceNotFound = errors.New("CVM instance not found")

var errTKEInstanceNotFound = errors.New("TKE instance not found")

const tkeListPageLimit int64 = 100

const nodePoolBootstrapMutationConfirmation = "CREATE_MISSING_WORKSPACE_NODEPOOLS"

const nodePoolImageGCMutationConfirmation = "RECONCILE_WORKSPACE_NODEPOOL_IMAGE_GC"

const protectedCheckNotChecked = "not_checked"

const protectedCheckPassed = "passed"

const protectedCheckFailed = "failed"

const protectedCheckNotApplicable = "not_applicable"

const predebitIAMProofMode = "production_runner_deployment_attestation"

// The accepted attestation shape is declared once for the source in
// packages/contracts/tencent-predebit-iam-contract.json. The installer reads
// that declaration from the exact Candidate source it installs, so the shape
// this provisioner accepts and the shape the installer produces cannot drift.
const predebitIAMAttestationSchemaVersion = 3

const tencentProviderProfileEnv = "OPL_FABRIC_TENCENT_TKE_PROVIDER_PROFILE_JSON"

const workspaceNodeImageGCHighThresholdEnv = "OPL_WORKSPACE_NODE_IMAGE_GC_HIGH_THRESHOLD_PERCENT"

const workspaceNodeImageGCLowThresholdEnv = "OPL_WORKSPACE_NODE_IMAGE_GC_LOW_THRESHOLD_PERCENT"

type tencentSKUProfile struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Compute   struct {
		ID           string `json:"id"`
		Server       string `json:"server"`
		CPU          uint64 `json:"cpu"`
		MemoryGB     uint64 `json:"memoryGb"`
		DiskGB       int    `json:"diskGb"`
		InstanceType string `json:"instanceType"`
	} `json:"compute"`
	NodePoolID  string `json:"nodePoolId"`
	MaxReplicas int64  `json:"maxReplicas"`
	Zone        string `json:"zone"`
	Storage     struct {
		SizeGB   int    `json:"sizeGb"`
		DiskType string `json:"diskType"`
	} `json:"storage"`
	Billing struct {
		ChargeType   string `json:"chargeType"`
		PeriodMonths int64  `json:"periodMonths"`
		RenewFlag    string `json:"renewFlag"`
	} `json:"billing"`
}

type tencentSKUProviderProfile struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Packages      []tencentSKUProfile `json:"packages"`
}

func configuredTencentSKUProfiles(env map[string]string) ([]tencentSKUProfile, error) {
	var profile tencentSKUProviderProfile
	raw := strings.TrimSpace(env[tencentProviderProfileEnv])
	if raw == "" || json.Unmarshal([]byte(raw), &profile) != nil || profile.SchemaVersion != 1 || len(profile.Packages) == 0 {
		return nil, fmt.Errorf("tencent_provider_profile_required")
	}
	seen := map[string]bool{}
	for index, item := range profile.Packages {
		item.ID, item.Name = strings.TrimSpace(item.ID), strings.TrimSpace(item.Name)
		item.Compute.ID, item.Compute.Server, item.Compute.InstanceType = strings.TrimSpace(item.Compute.ID), strings.TrimSpace(item.Compute.Server), strings.TrimSpace(item.Compute.InstanceType)
		item.NodePoolID, item.Zone = strings.TrimSpace(item.NodePoolID), strings.TrimSpace(item.Zone)
		item.Storage.DiskType = strings.TrimSpace(item.Storage.DiskType)
		if item.ID == "" || seen[item.ID] || item.Name == "" || item.Compute.ID == "" || item.Compute.Server == "" || item.Compute.CPU == 0 || item.Compute.MemoryGB == 0 || item.Compute.InstanceType == "" || item.Compute.DiskGB <= 0 ||
			item.MaxReplicas <= 0 || item.Zone == "" || item.Storage.SizeGB < 10 || item.Storage.SizeGB%10 != 0 || item.Storage.DiskType == "" || item.Compute.DiskGB != item.Storage.SizeGB ||
			item.Billing.ChargeType != "PREPAID" || item.Billing.PeriodMonths != 1 || item.Billing.RenewFlag != "NOTIFY_AND_MANUAL_RENEW" {
			return nil, fmt.Errorf("tencent_provider_profile_invalid")
		}
		seen[item.ID] = true
		profile.Packages[index] = item
	}
	return profile.Packages, nil
}

func validateTencentBootstrapProfiles(profiles []tencentSKUProfile) error {
	for _, item := range profiles {
		if !item.Available {
			continue
		}
		item.Compute.ID = strings.TrimSpace(item.Compute.ID)
		item.Compute.Server = strings.TrimSpace(item.Compute.Server)
		item.Compute.InstanceType = strings.TrimSpace(item.Compute.InstanceType)
		item.NodePoolID = strings.TrimSpace(item.NodePoolID)
		item.Zone = strings.TrimSpace(item.Zone)
		item.Storage.DiskType = strings.TrimSpace(item.Storage.DiskType)
		if item.Name == "" || item.Compute.ID == "" || item.Compute.Server == "" || item.Compute.InstanceType == "" ||
			item.Compute.DiskGB <= 0 || item.MaxReplicas <= 0 || item.Zone == "" || item.Storage.SizeGB <= 0 || item.Storage.DiskType == "" ||
			item.Compute.DiskGB != item.Storage.SizeGB || item.Billing.ChargeType != "PREPAID" || item.Billing.PeriodMonths != 1 || item.Billing.RenewFlag != "NOTIFY_AND_MANUAL_RENEW" {
			return fmt.Errorf("tencent_provider_profile_invalid")
		}
	}
	return nil
}

var predebitIAMRequiredActions = []string{"tag:TagResources", "tag:ModifyResourcesTagValue"}

var predebitIAMRequiredPolicies = []string{"QcloudCVMFinanceAccess"}

type Request struct {
	Action           string                 `json:"action"`
	DryRun           bool                   `json:"dryRun,omitempty"`
	AccountId        string                 `json:"accountId,omitempty"`
	UserId           string                 `json:"userId,omitempty"`
	PackageId        string                 `json:"packageId,omitempty"`
	Region           string                 `json:"region,omitempty"`
	Zone             string                 `json:"zone,omitempty"`
	StorageVolumeId  string                 `json:"storageVolumeId,omitempty"`
	RequiredCapacity int64                  `json:"requiredCapacity,omitempty"`
	Tags             map[string]string      `json:"tags,omitempty"`
	ComputeTags      map[string]string      `json:"computeTags,omitempty"`
	Pool             ComputePoolInput       `json:"pool,omitempty"`
	Allocation       ComputeAllocationInput `json:"allocation,omitempty"`
	Storage          StorageInput           `json:"storage,omitempty"`
}

type ComputePoolInput struct {
	Id                 string            `json:"id,omitempty"`
	ClusterId          string            `json:"clusterId,omitempty"`
	PackageId          string            `json:"packageId,omitempty"`
	InstanceType       string            `json:"instanceType,omitempty"`
	CPU                uint64            `json:"cpu,omitempty"`
	MemoryGB           uint64            `json:"memoryGb,omitempty"`
	NodePoolId         string            `json:"nodePoolId,omitempty"`
	DesiredNodeLabels  map[string]string `json:"desiredNodeLabels,omitempty"`
	DesiredReplicas    int64             `json:"desiredReplicas,omitempty"`
	MaxReplicas        int64             `json:"maxReplicas,omitempty"`
	BaselineReplicas   int64             `json:"baselineReplicas,omitempty"`
	TargetReplicas     int64             `json:"targetReplicas,omitempty"`
	BeforeMachineNames []string          `json:"beforeMachineNames,omitempty"`
}

type ComputeAllocationInput struct {
	Id          string `json:"id,omitempty"`
	InstanceId  string `json:"instanceId,omitempty"`
	MachineType string `json:"machineType,omitempty"`
	MachineName string `json:"machineName,omitempty"`
	NodeName    string `json:"nodeName,omitempty"`
	PrivateIp   string `json:"privateIp,omitempty"`
	PublicIp    string `json:"publicIp,omitempty"`
	Deadline    string `json:"deadline,omitempty"`
}

type StorageInput struct {
	Id                         string `json:"id,omitempty"`
	SizeGB                     uint64 `json:"sizeGb,omitempty"`
	Zone                       string `json:"zone,omitempty"`
	DiskType                   string `json:"diskType,omitempty"`
	Deadline                   string `json:"deadline,omitempty"`
	ExpectedState              string `json:"expectedState,omitempty"`
	ExpectedProviderResourceId string `json:"expectedProviderResourceId,omitempty"`
	AllowExistingExactReplay   bool   `json:"allowExistingExactReplay,omitempty"`
}

type Response struct {
	Observation              *contracts.ResourceObservation `json:"observation,omitempty"`
	Ok                       bool                           `json:"ok"`
	OperationId              string                         `json:"operationId,omitempty"`
	PoolId                   string                         `json:"poolId,omitempty"`
	NodePoolId               string                         `json:"nodePoolId,omitempty"`
	InstanceId               string                         `json:"instanceId,omitempty"`
	NodeName                 string                         `json:"nodeName,omitempty"`
	PrivateIp                string                         `json:"privateIp,omitempty"`
	MachinePresent           *bool                          `json:"machinePresent,omitempty"`
	StoragePresent           *bool                          `json:"storagePresent,omitempty"`
	CVMStatus                string                         `json:"cvmStatus,omitempty"`
	TKEStatus                string                         `json:"tkeStatus,omitempty"`
	CBSStatus                string                         `json:"cbsStatus,omitempty"`
	StorageVolumeId          string                         `json:"storageVolumeId,omitempty"`
	StorageState             string                         `json:"storageState,omitempty"`
	PublicIp                 string                         `json:"publicIp,omitempty"`
	Status                   string                         `json:"status,omitempty"`
	ProviderRequestId        string                         `json:"providerRequestId,omitempty"`
	ProviderRequestIDs       map[string]string              `json:"providerRequestIds,omitempty"`
	ProviderPriceCNY         float64                        `json:"providerPriceCny,omitempty"`
	ProviderData             map[string]string              `json:"providerData,omitempty"`
	ErrorCode                string                         `json:"errorCode,omitempty"`
	Message                  string                         `json:"message,omitempty"`
	Retryable                bool                           `json:"retryable,omitempty"`
	MissingEnv               []string                       `json:"missingEnv,omitempty"`
	Machines                 []MachineOutput                `json:"machines,omitempty"`
	InstanceType             string                         `json:"instanceType,omitempty"`
	InstanceAvailable        bool                           `json:"instanceAvailable,omitempty"`
	RequiredCapacity         int64                          `json:"requiredCapacity,omitempty"`
	RemainingQuota           uint64                         `json:"remainingQuota,omitempty"`
	CurrentReplicas          int64                          `json:"currentReplicas,omitempty"`
	ReadyReplicas            int64                          `json:"readyReplicas,omitempty"`
	MaxReplicas              int64                          `json:"maxReplicas,omitempty"`
	TargetReplicas           int64                          `json:"targetReplicas,omitempty"`
	MachineType              string                         `json:"machineType,omitempty"`
	Zones                    []string                       `json:"zones,omitempty"`
	PreflightStages          []PreflightStage               `json:"preflightStages,omitempty"`
	NodePools                []NodePoolBootstrapResult      `json:"nodePools,omitempty"`
	NodePoolImageGC          []NodePoolImageGCResult        `json:"nodePoolImageGc,omitempty"`
	SKUPackages              []WorkspaceSKUPackage          `json:"skuPackages,omitempty"`
	PrepaidQuotaRemaining    uint64                         `json:"prepaidQuotaRemaining,omitempty"`
	Subnets                  []WorkspaceSubnetFact          `json:"subnets,omitempty"`
	TKEClusterNodeLimit      uint64                         `json:"tkeClusterNodeLimit,omitempty"`
	TKECurrentNodeCount      uint64                         `json:"tkeCurrentNodeCount,omitempty"`
	TKEAvailableNodeCapacity uint64                         `json:"tkeAvailableNodeCapacity,omitempty"`
	TKECapacity              *TKEClusterCapacityFacts       `json:"tkeCapacity,omitempty"`
	ProtectedSystem          ProtectedSystemFacts           `json:"protectedSystem,omitempty"`
	NodePoolInventory        []string                       `json:"nodePoolInventoryBeforeMutation,omitempty"`
	MutationCount            int                            `json:"mutationCount"`
	FailureStage             string                         `json:"failureStage,omitempty"`
	ProviderErrorClass       string                         `json:"providerErrorClass,omitempty"`
	ProviderIdentityFailure  *ProviderIdentityFailure       `json:"providerIdentityFailure,omitempty"`
	MutationEvidence         *MutationEvidence              `json:"mutationEvidence,omitempty"`
}

type ProviderIdentityFailure struct {
	Predicate      string `json:"predicate"`
	ExpectedDigest string `json:"expectedDigest"`
	ActualDigest   string `json:"actualDigest"`
}

type MutationEvidence struct {
	Attempted int      `json:"attempted"`
	Confirmed int      `json:"confirmed"`
	Unknown   int      `json:"unknown"`
	Missing   []string `json:"missing,omitempty"`
}

type WorkspaceSKUCandidate struct {
	InstanceType    string  `json:"instanceType"`
	CPU             uint64  `json:"cpu"`
	MemoryGB        uint64  `json:"memoryGb"`
	MonthlyPriceCNY float64 `json:"monthlyPriceCny"`
	StockStatus     string  `json:"stockStatus"`
	StockCategory   string  `json:"stockCategory,omitempty"`
}

type WorkspaceSKUPackage struct {
	PackageID                  string                  `json:"packageId"`
	CPU                        uint64                  `json:"cpu"`
	MemoryGB                   uint64                  `json:"memoryGb"`
	RecommendedInstanceType    string                  `json:"recommendedInstanceType"`
	RecommendedMonthlyPriceCNY float64                 `json:"recommendedMonthlyPriceCny"`
	Candidates                 []WorkspaceSKUCandidate `json:"candidates"`
}

type WorkspaceSubnetFact struct {
	SubnetID             string `json:"subnetId"`
	VPCID                string `json:"vpcId"`
	Zone                 string `json:"zone"`
	AvailableIPAddresses uint64 `json:"availableIpAddresses"`
}

type TKEClusterCapacityFacts struct {
	ClusterLevel     string                `json:"clusterLevel"`
	CurrentNodeCount uint64                `json:"currentNodeCount"`
	LevelAttributes  []TKEClusterLevelFact `json:"levelAttributes"`
}

type TKEClusterLevelFact struct {
	Name      string  `json:"name"`
	Alias     string  `json:"alias,omitempty"`
	NodeCount *uint64 `json:"nodeCount,omitempty"`
	Enable    *bool   `json:"enable,omitempty"`
}

type ProtectedSystemFacts struct {
	NodePoolID         string `json:"nodePoolId,omitempty"`
	PoolCheckStatus    string `json:"poolCheckStatus"`
	MachineID          string `json:"machineId,omitempty"`
	MachineCheckStatus string `json:"machineCheckStatus"`
	NodeName           string `json:"nodeName,omitempty"`
	NodeCheckStatus    string `json:"nodeCheckStatus"`
	MachineType        string `json:"machineType,omitempty"`
	CVMApplicable      bool   `json:"cvmApplicable"`
	CVMID              string `json:"cvmId,omitempty"`
	CVMCheckStatus     string `json:"cvmCheckStatus"`
}

type NodePoolBootstrapResult struct {
	PackageID                 string              `json:"packageId"`
	PoolID                    string              `json:"poolId"`
	NodePoolID                string              `json:"nodePoolId,omitempty"`
	InstanceType              string              `json:"instanceType"`
	CPU                       uint64              `json:"cpu"`
	MemoryGB                  uint64              `json:"memoryGb"`
	MaxReplicas               int64               `json:"maxReplicas"`
	MaxReplicasSource         string              `json:"maxReplicasSource"`
	MaxReplicasDecision       string              `json:"maxReplicasDecision"`
	MaxReplicasConstraint     string              `json:"maxReplicasConstraint"`
	MaxReplicasRecommendation string              `json:"maxReplicasRecommendation"`
	Status                    string              `json:"status"`
	ErrorCode                 string              `json:"errorCode,omitempty"`
	TaintsBefore              []NodePoolTaintFact `json:"taintsBefore,omitempty"`
	TaintsAfter               []NodePoolTaintFact `json:"taintsAfter,omitempty"`
	EstimatedNodeCount        int64               `json:"estimatedNodeCount"`
	AffectedNodeNames         []string            `json:"affectedNodeNames,omitempty"`
}

type NodePoolTaintFact struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Effect string `json:"effect"`
}

type NodePoolImageGCResult struct {
	PackageID            string              `json:"packageId"`
	NodePoolID           string              `json:"nodePoolId"`
	Status               string              `json:"status"`
	HighThresholdPercent int                 `json:"highThresholdPercent"`
	LowThresholdPercent  int                 `json:"lowThresholdPercent"`
	UpdateExistingNodes  bool                `json:"updateExistingNodes"`
	TaintsBefore         []NodePoolTaintFact `json:"taintsBefore"`
	TaintsAfter          []NodePoolTaintFact `json:"taintsAfter"`
	KubeletArgsBefore    []string            `json:"kubeletArgsBefore"`
	KubeletArgsAfter     []string            `json:"kubeletArgsAfter"`
}

type PreflightStage struct {
	Stage      string         `json:"stage"`
	Status     string         `json:"status"`
	ErrorCode  string         `json:"errorCode,omitempty"`
	BlockedBy  []string       `json:"blockedBy"`
	DurationMS int64          `json:"durationMs"`
	SafeFacts  map[string]any `json:"safeFacts"`
}

type MachineOutput struct {
	MachineId    string `json:"machineId"`
	InstanceId   string `json:"instanceId,omitempty"`
	NodeName     string `json:"nodeName,omitempty"`
	PrivateIp    string `json:"privateIp,omitempty"`
	PublicIp     string `json:"publicIp,omitempty"`
	InstanceType string `json:"instanceType,omitempty"`
	Zone         string `json:"zone,omitempty"`
	ChargeType   string `json:"chargeType,omitempty"`
	RenewFlag    string `json:"renewFlag,omitempty"`
	Deadline     string `json:"deadline,omitempty"`
	Ready        bool   `json:"ready"`
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

var cbsOwnershipTagKeys = [...]string{"opl_account_id", "opl_workspace_id", "opl_resource_id", "opl_operation_id"}

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		writeResponse(Response{Ok: false, ErrorCode: "stdin_read_failed", Message: err.Error()})
		os.Exit(1)
	}
	var request Request
	if err := json.Unmarshal(raw, &request); err != nil {
		writeResponse(Response{Ok: false, ErrorCode: "invalid_json", Message: err.Error()})
		os.Exit(1)
	}
	env := envMap(os.Environ())
	client, setupFailure := newTencentSDKClient(env)
	if setupFailure != nil && request.Action != "readiness" && request.Action != "protected_resource_check" {
		writeResponse(*setupFailure)
		os.Exit(1)
	}
	var provisioner TencentClient = client
	if provisioner == nil {
		provisioner = unimplementedTencentClient{}
	}
	response := handleWithClient(request, env, provisioner)
	writeResponse(response)
	if !response.Ok {
		os.Exit(1)
	}
}

func writeResponse(response Response) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(response)
}

func envMap(values []string) map[string]string {
	result := map[string]string{}
	for _, item := range values {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			result[key] = value
		}
	}
	return result
}

func handle(request Request, env map[string]string) Response {
	return handleWithClient(request, env, unimplementedTencentClient{})
}

func handleWithClient(request Request, env map[string]string, client TencentClient) Response {
	if request.Action == "protected_resource_check" {
		guard := protectedresource.FromMap(env)
		if err := guard.Check(protectedresource.Target{PackageID: request.PackageId, NodePoolID: request.Pool.NodePoolId, MachineID: request.Allocation.MachineName, NodeName: request.Allocation.NodeName, CVMID: request.Allocation.InstanceId}); err != nil {
			return Response{Ok: false, ErrorCode: err.Error(), Message: "Protected resource guard rejected the target.", Retryable: false}
		}
		return Response{Ok: true, Status: "allowed"}
	}
	missing := missingEnv(env)
	if request.Action == "readiness" {
		if len(missing) > 0 {
			return Response{
				Ok:         false,
				ErrorCode:  "tencent_env_missing",
				Message:    "Tencent Cloud provisioner environment is incomplete.",
				MissingEnv: missing,
				Retryable:  false,
			}
		}
		return Response{Ok: true, Status: "ready"}
	}
	if len(missing) > 0 {
		return Response{
			Ok:         false,
			ErrorCode:  "tencent_env_missing",
			Message:    "Tencent Cloud provisioner environment is incomplete.",
			MissingEnv: missing,
			Retryable:  false,
		}
	}
	if request.Action == "workspace_sku_inventory" {
		return client.WorkspaceSKUInventory(request, env)
	}
	if request.Action == "bootstrap_compute_node_pools" {
		if !request.DryRun && strings.TrimSpace(env["RUN_TENCENT_NODE_POOL_BOOTSTRAP_CONFIRMATION"]) != nodePoolBootstrapMutationConfirmation {
			return Response{Ok: false, ErrorCode: "node_pool_bootstrap_confirmation_required", Message: "The exact node pool bootstrap mutation confirmation is required.", Retryable: false}
		}
		if !request.DryRun && strings.TrimSpace(env["RUN_TENCENT_CREATE_RELEASE_EXECUTION"]) != "1" {
			return Response{Ok: false, ErrorCode: "live_mutation_flag_required", Message: "Set RUN_TENCENT_CREATE_RELEASE_EXECUTION=1 to run live Tencent resource mutations.", Retryable: false}
		}
		if !request.DryRun && strings.TrimSpace(env["RUN_TENCENT_NODE_POOL_BOOTSTRAP"]) != "1" {
			return Response{Ok: false, ErrorCode: "node_pool_bootstrap_flag_required", Message: "Set RUN_TENCENT_NODE_POOL_BOOTSTRAP=1 only in the approved manual bootstrap workflow.", Retryable: false}
		}
		return client.BootstrapComputeNodePools(request, env)
	}
	if request.Action == "reconcile_workspace_node_pool_image_gc" {
		if !request.DryRun && strings.TrimSpace(env["RUN_TENCENT_NODE_POOL_IMAGE_GC_CONFIRMATION"]) != nodePoolImageGCMutationConfirmation {
			return Response{Ok: false, ErrorCode: "node_pool_image_gc_confirmation_required", Message: "The exact Workspace NodePool image GC reconciliation confirmation is required.", Retryable: false}
		}
		if !request.DryRun && strings.TrimSpace(env["RUN_TENCENT_CREATE_RELEASE_EXECUTION"]) != "1" {
			return Response{Ok: false, ErrorCode: "live_mutation_flag_required", Message: "Set RUN_TENCENT_CREATE_RELEASE_EXECUTION=1 to run live Tencent resource mutations.", Retryable: false}
		}
		if !request.DryRun && strings.TrimSpace(env["RUN_TENCENT_NODE_POOL_IMAGE_GC_RECONCILE"]) != "1" {
			return Response{Ok: false, ErrorCode: "node_pool_image_gc_flag_required", Message: "Set RUN_TENCENT_NODE_POOL_IMAGE_GC_RECONCILE=1 only in the approved manual reconciliation workflow.", Retryable: false}
		}
		reconciler, ok := client.(interface {
			ReconcileWorkspaceNodePoolImageGC(Request, map[string]string) Response
		})
		if !ok {
			return Response{Ok: false, ErrorCode: "tencent_live_not_implemented", Message: "Tencent live Workspace NodePool image GC reconciliation is not implemented in this build.", Retryable: false}
		}
		return reconciler.ReconcileWorkspaceNodePoolImageGC(request, env)
	}
	if isLiveMutation(request) && strings.TrimSpace(env["RUN_TENCENT_CREATE_RELEASE_EXECUTION"]) != "1" {
		return Response{
			Ok:        false,
			ErrorCode: "live_mutation_flag_required",
			Message:   "Set RUN_TENCENT_CREATE_RELEASE_EXECUTION=1 to run live Tencent resource mutations.",
			Retryable: false,
		}
	}
	if isLiveMutation(request) {
		guard := protectedresource.FromMap(env)
		if err := guard.Check(protectedresource.Target{
			PackageID: request.PackageId, NodePoolID: request.Pool.NodePoolId,
			MachineID: request.Allocation.MachineName, NodeName: request.Allocation.NodeName, CVMID: request.Allocation.InstanceId,
		}); err != nil {
			return Response{Ok: false, ErrorCode: err.Error(), Message: "Protected resource guard rejected the mutation target.", Retryable: false}
		}
	}

	switch request.Action {
	case "predebit_iam_gate":
		gate, ok := client.(interface {
			PredebitIAMGate(Request, map[string]string) Response
		})
		if !ok {
			return Response{Ok: false, ErrorCode: "tencent_live_not_implemented", Message: "Tencent pre-debit IAM gate is not implemented in this build.", Retryable: false}
		}
		return gate.PredebitIAMGate(request, env)
	case "capacity_preflight":
		return client.Capacity(request, env)
	case "storage_preflight":
		return client.StoragePreflight(request, env)
	case "provider_truth":
		return client.ProviderTruth(request, env)
	case "compute_claim_truth":
		return client.ComputeClaimTruth(request, env)
	case "discover_storage_volume":
		return client.DiscoverStorageVolume(request, env)
	case "claim_compute_machine":
		return client.ClaimComputeMachine(request, env)
	case "prepare_compute_allocation":
		if request.DryRun {
			return Response{Ok: true, PoolId: request.Pool.Id, NodePoolId: request.Pool.NodePoolId, Status: "prepared", ProviderRequestId: "dryrun-prepare-" + request.Pool.NodePoolId, CurrentReplicas: 0, TargetReplicas: 1, Machines: []MachineOutput{}}
		}
		return client.PrepareComputeAllocation(request, env)
	case "create_storage_volume":
		return client.CreateStorageVolume(request, env)
	case "sync_storage_volume", "read_storage_for_delete":
		return client.SyncStorageVolume(request, env)
	case "destroy_storage_volume":
		if request.DryRun {
			return dryRunDestroyStorageVolume(request)
		}
		return client.DestroyStorageVolume(request, env)
	case "renew_storage_volume":
		return client.RenewStorageVolume(request, env)
	case "renew_compute_allocation":
		return client.RenewComputeAllocation(request, env)
	case "create_compute_allocation":
		if request.DryRun {
			return dryRunCreateComputeAllocation(request, env)
		}
		return client.CreateComputeAllocation(request, env)
	case "read_compute_allocation":
		reader, ok := client.(interface {
			ReadComputeAllocation(Request, map[string]string) Response
		})
		if !ok {
			return Response{Ok: false, ErrorCode: "tencent_live_not_implemented", Message: "Tencent live compute allocation readback is not implemented in this build.", Retryable: false}
		}
		return reader.ReadComputeAllocation(request, env)
	case "tag_compute_machine":
		if request.DryRun {
			return Response{Ok: true, InstanceId: request.Allocation.InstanceId, Status: "tagged", ProviderRequestId: "dryrun-tag-" + request.Allocation.InstanceId}
		}
		return client.TagComputeMachine(request, env)
	case "sync_compute_allocation":
		if request.DryRun {
			return dryRunSyncComputeAllocation(request)
		}
		return client.SyncComputeAllocation(request, env)
	case "read_compute_destroy_status":
		return client.ReadComputeDestroyStatus(request, env)
	case "destroy_compute_allocation":
		if request.DryRun {
			return dryRunDestroyComputeAllocation(request)
		}
		return client.DestroyComputeAllocation(request, env)
	case storageFinalReleaseAction:
		releaser, ok := client.(interface {
			ReleaseRecycledStorageVolume(Request, map[string]string) Response
		})
		if !ok || request.DryRun {
			return Response{Ok: false, ErrorCode: "storage_final_release_unavailable", MutationCount: 0}
		}
		return releaser.ReleaseRecycledStorageVolume(request, env)
	case detachPrepaidComputeAction:
		detacher, ok := client.(interface {
			DetachPrepaidComputeAllocation(Request, map[string]string) Response
		})
		if !ok || request.DryRun {
			return Response{Ok: false, ErrorCode: "compute_prepaid_detach_unavailable", MutationCount: 0}
		}
		return detacher.DetachPrepaidComputeAllocation(request, env)
	case retirePrepaidComputeAction:
		retirer, ok := client.(interface {
			RetirePrepaidComputeAllocation(Request, map[string]string) Response
		})
		if !ok || request.DryRun {
			return Response{Ok: false, ErrorCode: "compute_prepaid_retire_unavailable", MutationCount: 0}
		}
		return retirer.RetirePrepaidComputeAllocation(request, env)
	case "release_recycled_compute_allocation":
		releaser, ok := client.(interface {
			ReleaseRecycledComputeAllocation(Request, map[string]string) Response
		})
		if !ok || request.DryRun {
			return Response{Ok: false, ErrorCode: "compute_final_release_unavailable", MutationCount: 0}
		}
		return releaser.ReleaseRecycledComputeAllocation(request, env)
	default:
		return Response{
			Ok:        false,
			ErrorCode: "unknown_action",
			Message:   fmt.Sprintf("Unknown provisioner action: %s", request.Action),
			Retryable: false,
		}
	}
}

func isLiveMutation(request Request) bool {
	if request.DryRun {
		return false
	}
	return request.Action == detachPrepaidComputeAction || request.Action == retirePrepaidComputeAction ||
		request.Action == "bootstrap_compute_node_pools" || request.Action == "create_compute_allocation" || request.Action == "tag_compute_machine" || request.Action == "claim_compute_machine" ||
		request.Action == storageFinalReleaseAction || request.Action == "destroy_compute_allocation" || request.Action == "release_recycled_compute_allocation" || request.Action == "renew_compute_allocation" || request.Action == "create_storage_volume" || request.Action == "destroy_storage_volume" || request.Action == "renew_storage_volume"
}

func missingEnv(env map[string]string) []string {
	var missing []string
	for _, key := range requiredTencentEnv {
		if strings.TrimSpace(env[key]) == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

func dryRunCreateComputeAllocation(request Request, env map[string]string) Response {
	stable := stableSuffix(request.AccountId, request.UserId, request.PackageId, request.Pool.Id, request.Allocation.Id)
	nodePoolId := request.Pool.NodePoolId
	if nodePoolId == "" {
		nodePoolId = "np-" + stable[:8]
	}
	instanceId := request.Allocation.InstanceId
	if instanceId == "" {
		instanceId = "ins-" + stable[:12]
	}
	nodeName := request.Allocation.NodeName
	if nodeName == "" {
		nodeName = "node-" + stable[:10]
	}
	providerData := map[string]string{
		"accountId":       request.AccountId,
		"userId":          request.UserId,
		"packageId":       request.PackageId,
		"clusterId":       env["TENCENT_DEPLOY_CLUSTER_ID"],
		"region":          env["TENCENTCLOUD_REGION"],
		"instanceType":    request.Pool.InstanceType,
		"provisionerMode": "dry-run",
	}
	for key, value := range request.Tags {
		if strings.TrimSpace(value) != "" {
			providerData[key] = value
		}
	}
	return Response{
		Ok:           true,
		OperationId:  "op-create-compute-" + stable[:12],
		PoolId:       request.Pool.Id,
		NodePoolId:   nodePoolId,
		InstanceId:   instanceId,
		NodeName:     nodeName,
		PrivateIp:    "10.0.0." + strconv.Itoa(len(stable)),
		Status:       "running",
		ProviderData: providerData,
	}
}

func dryRunDestroyComputeAllocation(request Request) Response {
	stable := stableSuffix(request.AccountId, request.Allocation.Id, request.Allocation.InstanceId)
	providerData := map[string]string{
		"accountId":       request.AccountId,
		"provisionerMode": "dry-run",
	}
	for key, value := range request.Tags {
		if strings.TrimSpace(value) != "" {
			providerData[key] = value
		}
	}
	return Response{
		Ok:           true,
		OperationId:  "op-destroy-compute-" + stable[:12],
		PoolId:       request.Pool.Id,
		NodePoolId:   request.Pool.NodePoolId,
		InstanceId:   request.Allocation.InstanceId,
		NodeName:     request.Allocation.NodeName,
		Status:       "destroyed",
		ProviderData: providerData,
	}
}

func dryRunDestroyStorageVolume(request Request) Response {
	stable := stableSuffix(request.AccountId, request.Storage.Id)
	providerData := map[string]string{
		"accountId":       request.AccountId,
		"provisionerMode": "dry-run",
	}
	for key, value := range request.Tags {
		if strings.TrimSpace(value) != "" {
			providerData[key] = value
		}
	}
	return Response{
		Ok:              true,
		OperationId:     "op-destroy-storage-" + stable[:12],
		StorageVolumeId: request.Storage.Id,
		Status:          "destroyed",
		ProviderData:    providerData,
	}
}

func dryRunSyncComputeAllocation(request Request) Response {
	stable := stableSuffix(request.AccountId, request.Allocation.Id, request.Allocation.MachineName, request.Allocation.PrivateIp)
	status := firstNonEmpty(request.Allocation.NodeName, request.Allocation.MachineName, request.Allocation.PrivateIp)
	if status != "" {
		status = "running"
	} else {
		status = "external_deleted"
	}
	return Response{
		Ok:           true,
		OperationId:  "op-sync-compute-" + stable[:12],
		PoolId:       request.Pool.Id,
		NodePoolId:   request.Pool.NodePoolId,
		InstanceId:   request.Allocation.InstanceId,
		NodeName:     request.Allocation.NodeName,
		PrivateIp:    request.Allocation.PrivateIp,
		PublicIp:     request.Allocation.PublicIp,
		Status:       status,
		ProviderData: map[string]string{"accountId": request.AccountId, "syncResult": status},
	}
}

func stableSuffix(parts ...string) string {
	hash := sha1.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func missingSpecificEnv(env map[string]string, keys []string) []string {
	var missing []string
	for _, key := range keys {
		if strings.TrimSpace(env[key]) == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

func intFromEnv(env map[string]string, key string, fallback int) int {
	if strings.TrimSpace(env[key]) == "" {
		return fallback
	}
	value, err := strconv.Atoi(env[key])
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func nonNegativeIntFromEnv(env map[string]string, key string, fallback int) int {
	if strings.TrimSpace(env[key]) == "" {
		return fallback
	}
	value, err := strconv.Atoi(env[key])
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func splitCsv(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func stringsToPtrs(values []string) []*string {
	if len(values) == 0 {
		return nil
	}
	result := make([]*string, 0, len(values))
	for _, value := range values {
		result = append(result, common.StringPtr(value))
	}
	return result
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// Tencent SDK codes are copied into redacted admission evidence. Keep only a
// bounded provider code; SDK messages and request IDs remain out of receipts.
func safeTencentProviderErrorCode(err error) string {
	var sdkErr *tcerrors.TencentCloudSDKError
	if err == nil || !errors.As(err, &sdkErr) || sdkErr == nil {
		return ""
	}
	code := strings.TrimSpace(sdkErr.Code)
	if code == "" || len(code) > 128 {
		return ""
	}
	for _, char := range code {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '.' && char != '_' && char != '-' {
			return ""
		}
	}
	return code
}

func firstString(values []*string) string {
	if len(values) == 0 || values[0] == nil {
		return ""
	}
	return *values[0]
}

func sdkErrorResponse(code string, err error) Response {
	message := err.Error()
	var readbackErr *nodePoolReadbackError
	if errors.As(err, &readbackErr) {
		code = string(readbackErr.code)
		message = readbackErr.Error()
	}
	response := Response{
		Ok:        false,
		ErrorCode: code,
		Message:   message,
		Retryable: true,
		ProviderData: map[string]string{
			"providerErrorCode":    code,
			"providerErrorMessage": message,
		},
	}
	if readbackErr != nil {
		if providerCode := safeTencentProviderErrorCode(err); providerCode != "" {
			response.ProviderData["providerErrorCode"] = providerCode
		}
	}
	var sdkErr *tcerrors.TencentCloudSDKError
	if errors.As(err, &sdkErr) && sdkErr != nil {
		response.ProviderRequestId = sdkErr.RequestId
		response.ProviderData["providerErrorRequestId"] = sdkErr.RequestId
	}
	return response
}
