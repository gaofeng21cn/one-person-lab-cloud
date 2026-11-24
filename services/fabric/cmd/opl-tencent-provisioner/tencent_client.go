package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	cbs2017 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cbs/v20170312"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tchttp "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/http"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	cvm2017 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm/v20170312"
	tke2018 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tke/v20180525"
	tke2022 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tke/v20220501"
	vpc2017 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc/v20170312"
	contracts "opl-cloud/packages/contracts/go"
)

type TencentClient interface {
	WorkspaceSKUInventory(request Request, env map[string]string) Response
	Capacity(request Request, env map[string]string) Response
	StoragePreflight(request Request, env map[string]string) Response
	ProviderTruth(request Request, env map[string]string) Response
	ComputeClaimTruth(request Request, env map[string]string) Response
	ClaimComputeMachine(request Request, env map[string]string) Response
	PrepareComputeAllocation(request Request, env map[string]string) Response
	CreateComputeAllocation(request Request, env map[string]string) Response
	TagComputeMachine(request Request, env map[string]string) Response
	SyncComputeAllocation(request Request, env map[string]string) Response
	ReadComputeDestroyStatus(request Request, env map[string]string) Response
	DestroyComputeAllocation(request Request, env map[string]string) Response
	RenewComputeAllocation(request Request, env map[string]string) Response
	DiscoverStorageVolume(request Request, env map[string]string) Response
	CreateStorageVolume(request Request, env map[string]string) Response
	SyncStorageVolume(request Request, env map[string]string) Response
	DestroyStorageVolume(request Request, env map[string]string) Response
	RenewStorageVolume(request Request, env map[string]string) Response
	BootstrapComputeNodePools(request Request, env map[string]string) Response
}

type unimplementedTencentClient struct{}

type tencentSDKClient struct {
	region                string
	clusterId             string
	nativeTkeClient       tkeNativeAPI
	nativeLegacyTkeClient tkeLegacyAPI
	nativeCvmClient       cvmNativeAPI
	nativeCbsClient       cbsNativeAPI
	nativeVpcClient       vpcNativeAPI
	nativeTagClient       tagNativeAPI
	convergenceContext    context.Context
	convergenceWait       func(context.Context, int) error
}

type tagNativeAPI interface {
	SetCVMTag(instanceID, key, value string, attached bool) (string, error)
}

type tencentTagClient struct {
	client         *common.Client
	identityClient *common.Client
	policyClient   *common.Client
	region         string
}

type tagResourceRequest struct {
	*tchttp.BaseRequest
	ServiceType    *string   `json:"ServiceType,omitempty" name:"ServiceType"`
	ResourceIds    []*string `json:"ResourceIds,omitempty" name:"ResourceIds"`
	TagKey         *string   `json:"TagKey,omitempty" name:"TagKey"`
	TagValue       *string   `json:"TagValue,omitempty" name:"TagValue"`
	ResourceRegion *string   `json:"ResourceRegion,omitempty" name:"ResourceRegion"`
	ResourcePrefix *string   `json:"ResourcePrefix,omitempty" name:"ResourcePrefix"`
}

type tagResourceResponse struct {
	*tchttp.BaseResponse
	Response *struct {
		RequestId *string `json:"RequestId,omitempty" name:"RequestId"`
	} `json:"Response"`
}

type getCallerIdentityRequest struct {
	*tchttp.BaseRequest
}

type getCallerIdentityResponse struct {
	*tchttp.BaseResponse
	Response *struct {
		Type        *string `json:"Type,omitempty" name:"Type"`
		PrincipalId *string `json:"PrincipalId,omitempty" name:"PrincipalId"`
		AccountId   *string `json:"AccountId,omitempty" name:"AccountId"`
		UserId      *string `json:"UserId,omitempty" name:"UserId"`
		RequestId   *string `json:"RequestId,omitempty" name:"RequestId"`
	} `json:"Response"`
}

type listAttachedUserPoliciesRequest struct {
	*tchttp.BaseRequest
	TargetUin *uint64 `json:"TargetUin,omitempty" name:"TargetUin"`
	Page      *uint64 `json:"Page,omitempty" name:"Page"`
	Rp        *uint64 `json:"Rp,omitempty" name:"Rp"`
}

type attachedUserPolicy struct {
	PolicyID   *uint64 `json:"PolicyId,omitempty" name:"PolicyId"`
	PolicyName *string `json:"PolicyName,omitempty" name:"PolicyName"`
	PolicyType *string `json:"PolicyType,omitempty" name:"PolicyType"`
	Deactived  *uint64 `json:"Deactived,omitempty" name:"Deactived"`
}

type listAttachedUserPoliciesResponse struct {
	*tchttp.BaseResponse
	Response *struct {
		TotalNum  *uint64               `json:"TotalNum,omitempty" name:"TotalNum"`
		List      []*attachedUserPolicy `json:"List,omitempty" name:"List"`
		RequestID *string               `json:"RequestId,omitempty" name:"RequestId"`
	} `json:"Response"`
}

type predebitIAMIdentity struct {
	Type        string `json:"type"`
	PrincipalID string `json:"principalId"`
	AccountID   string `json:"accountId"`
	UserID      string `json:"userId"`
}

type predebitIAMAttestation struct {
	SchemaVersion    int                 `json:"schemaVersion"`
	ProofMode        string              `json:"proofMode"`
	ReleaseSHA       string              `json:"releaseSha"`
	Identity         predebitIAMIdentity `json:"identity"`
	RequiredActions  []string            `json:"requiredActions"`
	RequiredPolicies []string            `json:"requiredPolicies"`
	PolicyDigest     string              `json:"policyDigest"`
}

type callerIdentityNativeAPI interface {
	CallerIdentity() (map[string]string, string, error)
}

type financePolicyNativeAPI interface {
	ActiveFinancePolicy(string) (bool, string, error)
}

type createAndBindTag struct {
	TagKey   *string `json:"TagKey,omitempty" name:"TagKey"`
	TagValue *string `json:"TagValue,omitempty" name:"TagValue"`
}

type createAndBindTagRequest struct {
	*tchttp.BaseRequest
	ResourceList []*string           `json:"ResourceList,omitempty" name:"ResourceList"`
	Tags         []*createAndBindTag `json:"Tags,omitempty" name:"Tags"`
}

type createAndBindTagResponse struct {
	*tchttp.BaseResponse
	Response *struct {
		RequestId *string `json:"RequestId,omitempty" name:"RequestId"`
	} `json:"Response"`
}

func (client *tencentTagClient) CallerIdentity() (map[string]string, string, error) {
	identityClient := client.identityClient
	if identityClient == nil {
		identityClient = client.client
	}
	identityRequest := &getCallerIdentityRequest{BaseRequest: &tchttp.BaseRequest{}}
	identityRequest.Init().WithApiInfo("sts", "2018-08-13", "GetCallerIdentity")
	identityResponse := &getCallerIdentityResponse{BaseResponse: &tchttp.BaseResponse{}}
	if err := identityClient.Send(identityRequest, identityResponse); err != nil {
		return nil, "", err
	}
	if identityResponse.Response == nil {
		return nil, "", fmt.Errorf("Tencent STS GetCallerIdentity response is missing")
	}
	identity := map[string]string{
		"type":        strings.TrimSpace(stringValue(identityResponse.Response.Type)),
		"principalId": strings.TrimSpace(stringValue(identityResponse.Response.PrincipalId)),
		"accountId":   strings.TrimSpace(stringValue(identityResponse.Response.AccountId)),
		"userId":      strings.TrimSpace(stringValue(identityResponse.Response.UserId)),
	}
	requestID := strings.TrimSpace(stringValue(identityResponse.Response.RequestId))
	for _, field := range []string{"type", "principalId", "accountId", "userId"} {
		if identity[field] == "" {
			return nil, "", fmt.Errorf("Tencent STS GetCallerIdentity response is missing %s", field)
		}
	}
	if requestID == "" {
		return nil, "", fmt.Errorf("Tencent STS GetCallerIdentity response is missing requestId")
	}
	return identity, requestID, nil
}

func (client *tencentTagClient) ActiveFinancePolicy(userID string) (bool, string, error) {
	policyClient := client.policyClient
	if policyClient == nil {
		policyClient = client.client
	}
	if policyClient == nil {
		return false, "", fmt.Errorf("Tencent CAM client is missing")
	}
	targetUIN, err := strconv.ParseUint(strings.TrimSpace(userID), 10, 64)
	if err != nil || targetUIN == 0 {
		return false, "", fmt.Errorf("Tencent CAM user identity is invalid")
	}
	const pageSize uint64 = 200
	var observed uint64
	activePolicyIDs := map[uint64]struct{}{}
	lastRequestID := ""
	for page := uint64(1); page <= 200; page++ {
		request := &listAttachedUserPoliciesRequest{
			BaseRequest: &tchttp.BaseRequest{}, TargetUin: common.Uint64Ptr(targetUIN), Page: common.Uint64Ptr(page), Rp: common.Uint64Ptr(pageSize),
		}
		request.Init().WithApiInfo("cam", "2019-01-16", "ListAttachedUserPolicies")
		response := &listAttachedUserPoliciesResponse{BaseResponse: &tchttp.BaseResponse{}}
		if err := policyClient.Send(request, response); err != nil {
			return false, "", err
		}
		if response.Response == nil || response.Response.TotalNum == nil || strings.TrimSpace(stringValue(response.Response.RequestID)) == "" {
			return false, "", fmt.Errorf("Tencent CAM ListAttachedUserPolicies response is incomplete")
		}
		lastRequestID = strings.TrimSpace(stringValue(response.Response.RequestID))
		observed += uint64(len(response.Response.List))
		if observed > *response.Response.TotalNum {
			return false, "", fmt.Errorf("Tencent CAM ListAttachedUserPolicies count is invalid")
		}
		for _, policy := range response.Response.List {
			if policy == nil || strings.TrimSpace(stringValue(policy.PolicyName)) != predebitIAMRequiredPolicies[0] ||
				strings.TrimSpace(stringValue(policy.PolicyType)) != "QCS" || policy.PolicyID == nil || *policy.PolicyID == 0 ||
				policy.Deactived == nil || *policy.Deactived != 0 {
				continue
			}
			activePolicyIDs[*policy.PolicyID] = struct{}{}
		}
		if observed == *response.Response.TotalNum {
			if len(activePolicyIDs) > 1 {
				return false, "", fmt.Errorf("Tencent CAM finance policy attachment is ambiguous")
			}
			return len(activePolicyIDs) == 1, lastRequestID, nil
		}
		if len(response.Response.List) == 0 {
			return false, "", fmt.Errorf("Tencent CAM ListAttachedUserPolicies pagination did not advance")
		}
	}
	return false, "", fmt.Errorf("Tencent CAM ListAttachedUserPolicies pagination exceeded the limit")
}

func (client *tencentTagClient) SetCVMTag(instanceID, key, value string, attached bool) (string, error) {
	if !attached {
		identity, _, err := client.CallerIdentity()
		if err != nil {
			return "", err
		}
		accountID := identity["accountId"]
		resource := fmt.Sprintf("qcs::cvm:%s:uin/%s:instance/%s", client.region, accountID, instanceID)
		request := &createAndBindTagRequest{
			BaseRequest:  &tchttp.BaseRequest{},
			ResourceList: []*string{common.StringPtr(resource)},
			Tags: []*createAndBindTag{{
				TagKey: common.StringPtr(key), TagValue: common.StringPtr(value),
			}},
		}
		request.Init().WithApiInfo("tag", "2018-08-13", "TagResources")
		response := &createAndBindTagResponse{BaseResponse: &tchttp.BaseResponse{}}
		if err := client.client.Send(request, response); err != nil {
			return "", err
		}
		if response.Response == nil || strings.TrimSpace(stringValue(response.Response.RequestId)) == "" {
			return "", fmt.Errorf("Tencent Tag TagResources response is missing")
		}
		return stringValue(response.Response.RequestId), nil
	}
	action := "ModifyResourcesTagValue"
	request := &tagResourceRequest{
		BaseRequest: &tchttp.BaseRequest{}, ServiceType: common.StringPtr("cvm"), ResourceIds: []*string{common.StringPtr(instanceID)},
		TagKey: common.StringPtr(key), TagValue: common.StringPtr(value), ResourceRegion: common.StringPtr(client.region), ResourcePrefix: common.StringPtr("instance"),
	}
	request.Init().WithApiInfo("tag", "2018-08-13", action)
	response := &tagResourceResponse{BaseResponse: &tchttp.BaseResponse{}}
	if err := client.client.Send(request, response); err != nil {
		return "", err
	}
	if response.Response == nil || strings.TrimSpace(stringValue(response.Response.RequestId)) == "" {
		return "", fmt.Errorf("Tencent Tag %s response is missing", action)
	}
	return stringValue(response.Response.RequestId), nil
}

func (client *tencentSDKClient) PredebitIAMGate(_ Request, env map[string]string) Response {
	raw := strings.TrimSpace(env["OPL_TENCENT_PREDEBIT_IAM_ATTESTATION"])
	if raw == "" {
		return Response{Ok: false, ErrorCode: "predebit_iam_attestation_missing", Message: "Tencent pre-debit IAM deployment attestation is required.", Retryable: false, MutationCount: 0}
	}
	var attestation predebitIAMAttestation
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&attestation); err != nil {
		return Response{Ok: false, ErrorCode: "predebit_iam_attestation_invalid", Message: "Tencent pre-debit IAM deployment attestation is invalid.", Retryable: false, MutationCount: 0}
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Response{Ok: false, ErrorCode: "predebit_iam_attestation_invalid", Message: "Tencent pre-debit IAM deployment attestation is invalid.", Retryable: false, MutationCount: 0}
	}
	if !validPredebitIAMAttestation(attestation) {
		return Response{Ok: false, ErrorCode: "predebit_iam_attestation_invalid", Message: "Tencent pre-debit IAM deployment attestation is invalid.", Retryable: false, MutationCount: 0}
	}
	if releaseSHA := strings.TrimSpace(env["OPL_RELEASE_SHA"]); !validLowerHex(releaseSHA, 40) || attestation.ReleaseSHA != releaseSHA {
		return Response{Ok: false, ErrorCode: "predebit_iam_release_mismatch", Message: "Tencent pre-debit IAM attestation release does not match the running release.", Retryable: false, MutationCount: 0}
	}
	// The attestation binds a release the deployment claimed. Only a binary
	// that states the same revision itself may act on that binding, so a
	// deployment that points at an image other than the one it claims cannot
	// mutate the customer's cloud resources.
	if !contracts.ProductSourceSHAIsCurrent(env["OPL_RELEASE_SHA"]) {
		return Response{Ok: false, ErrorCode: "predebit_iam_release_mismatch", Message: "Tencent pre-debit IAM attestation release does not match the running binary.", Retryable: false, MutationCount: 0}
	}
	identityClient, ok := client.nativeTagClient.(callerIdentityNativeAPI)
	if !ok {
		return Response{Ok: false, ErrorCode: "predebit_iam_sts_unavailable", Message: "Tencent STS identity client is unavailable.", Retryable: false, MutationCount: 0}
	}
	live, requestID, err := identityClient.CallerIdentity()
	if err != nil {
		return Response{Ok: false, ErrorCode: "predebit_iam_sts_failed", Message: "Tencent live STS identity could not be verified.", Retryable: true, MutationCount: 0}
	}
	if live["type"] != attestation.Identity.Type || live["principalId"] != attestation.Identity.PrincipalID || live["accountId"] != attestation.Identity.AccountID || live["userId"] != attestation.Identity.UserID {
		return Response{Ok: false, ErrorCode: "predebit_iam_identity_mismatch", Message: "Tencent live STS identity does not match the deployment attestation.", Retryable: false, MutationCount: 0}
	}
	policyClient, ok := client.nativeTagClient.(financePolicyNativeAPI)
	if !ok {
		return Response{Ok: false, ErrorCode: "predebit_iam_finance_policy_unavailable", Message: "Tencent live CAM policy client is unavailable.", Retryable: false, MutationCount: 0}
	}
	active, policyRequestID, err := policyClient.ActiveFinancePolicy(live["userId"])
	if err != nil {
		return Response{Ok: false, ErrorCode: "predebit_iam_finance_policy_read_failed", Message: "Tencent live CAM finance policy could not be verified.", Retryable: true, MutationCount: 0}
	}
	if !active {
		return Response{Ok: false, ErrorCode: "predebit_iam_finance_policy_missing", Message: "Tencent live CAM finance policy is not active.", Retryable: false, MutationCount: 0}
	}
	return Response{
		Ok: true, Status: "ready", ProviderRequestId: policyRequestID, MutationCount: 0,
		ProviderData: map[string]string{
			"proofMode": attestation.ProofMode, "releaseSha": attestation.ReleaseSHA,
			"requiredActions": strings.Join(attestation.RequiredActions, ","), "policyDigest": attestation.PolicyDigest,
			"requiredPolicies": strings.Join(attestation.RequiredPolicies, ","), "identityRequestId": requestID,
		},
	}
}

func validPredebitIAMAttestation(attestation predebitIAMAttestation) bool {
	if attestation.SchemaVersion != predebitIAMAttestationSchemaVersion || attestation.ProofMode != predebitIAMProofMode || !validLowerHex(attestation.ReleaseSHA, 40) ||
		len(attestation.RequiredActions) != len(predebitIAMRequiredActions) || !strings.HasPrefix(attestation.PolicyDigest, "sha256:") || !validLowerHex(strings.TrimPrefix(attestation.PolicyDigest, "sha256:"), 64) {
		return false
	}
	for index, action := range predebitIAMRequiredActions {
		if attestation.RequiredActions[index] != action {
			return false
		}
	}
	if len(attestation.RequiredPolicies) != len(predebitIAMRequiredPolicies) {
		return false
	}
	for index, policy := range predebitIAMRequiredPolicies {
		if attestation.RequiredPolicies[index] != policy {
			return false
		}
	}
	if attestation.Identity.Type != "CAMUser" {
		return false
	}
	if userID, err := strconv.ParseUint(attestation.Identity.UserID, 10, 64); err != nil || userID == 0 {
		return false
	}
	for _, value := range []string{attestation.Identity.PrincipalID, attestation.Identity.AccountID} {
		if value == "" || value != strings.TrimSpace(value) {
			return false
		}
	}
	return true
}

func validLowerHex(value string, size int) bool {
	if len(value) != size {
		return false
	}
	for _, current := range value {
		if (current < '0' || current > '9') && (current < 'a' || current > 'f') {
			return false
		}
	}
	return true
}

type tkeNativeAPI interface {
	CreateNodePool(request *tke2022.CreateNodePoolRequest) (*tke2022.CreateNodePoolResponse, error)
	DescribeClusterInstances(request *tke2022.DescribeClusterInstancesRequest) (*tke2022.DescribeClusterInstancesResponse, error)
	DescribeClusterMachines(request *tke2022.DescribeClusterMachinesRequest) (*tke2022.DescribeClusterMachinesResponse, error)
	DescribeNodePools(request *tke2022.DescribeNodePoolsRequest) (*tke2022.DescribeNodePoolsResponse, error)
	ModifyNodePool(request *tke2022.ModifyNodePoolRequest) (*tke2022.ModifyNodePoolResponse, error)
	ScaleNodePool(request *tke2022.ScaleNodePoolRequest) (*tke2022.ScaleNodePoolResponse, error)
	DeleteClusterMachines(request *tke2022.DeleteClusterMachinesRequest) (*tke2022.DeleteClusterMachinesResponse, error)
}

type tkeLegacyAPI interface {
	DescribeClusters(request *tke2018.DescribeClustersRequest) (*tke2018.DescribeClustersResponse, error)
	DescribeClusterLevelAttribute(request *tke2018.DescribeClusterLevelAttributeRequest) (*tke2018.DescribeClusterLevelAttributeResponse, error)
}

type cvmNativeAPI interface {
	DescribeAccountQuota(request *cvm2017.DescribeAccountQuotaRequest) (*cvm2017.DescribeAccountQuotaResponse, error)
	DescribeInstances(request *cvm2017.DescribeInstancesRequest) (*cvm2017.DescribeInstancesResponse, error)
	DescribeZoneInstanceConfigInfos(request *cvm2017.DescribeZoneInstanceConfigInfosRequest) (*cvm2017.DescribeZoneInstanceConfigInfosResponse, error)
	ModifyInstancesAttribute(request *cvm2017.ModifyInstancesAttributeRequest) (*cvm2017.ModifyInstancesAttributeResponse, error)
	RenewInstances(request *cvm2017.RenewInstancesRequest) (*cvm2017.RenewInstancesResponse, error)
}

type cbsNativeAPI interface {
	CreateDisks(request *cbs2017.CreateDisksRequest) (*cbs2017.CreateDisksResponse, error)
	DescribeDiskConfigQuota(request *cbs2017.DescribeDiskConfigQuotaRequest) (*cbs2017.DescribeDiskConfigQuotaResponse, error)
	DescribeDisks(request *cbs2017.DescribeDisksRequest) (*cbs2017.DescribeDisksResponse, error)
	InquiryPriceCreateDisks(request *cbs2017.InquiryPriceCreateDisksRequest) (*cbs2017.InquiryPriceCreateDisksResponse, error)
	RenewDisk(request *cbs2017.RenewDiskRequest) (*cbs2017.RenewDiskResponse, error)
	TerminateDisks(request *cbs2017.TerminateDisksRequest) (*cbs2017.TerminateDisksResponse, error)
}

type vpcNativeAPI interface {
	DescribeSubnets(request *vpc2017.DescribeSubnetsRequest) (*vpc2017.DescribeSubnetsResponse, error)
}

func (unimplementedTencentClient) WorkspaceSKUInventory(_ Request, _ map[string]string) Response {
	return Response{Ok: false, ErrorCode: "tencent_live_not_implemented", Message: "Tencent live Workspace SKU inventory is not implemented in this build.", Retryable: false}
}

func (unimplementedTencentClient) Capacity(_ Request, _ map[string]string) Response {
	return Response{Ok: false, ErrorCode: "tencent_live_not_implemented", Message: "Tencent live capacity preflight is not implemented in this build.", Retryable: false}
}

func (unimplementedTencentClient) StoragePreflight(_ Request, _ map[string]string) Response {
	return Response{Ok: false, ErrorCode: "tencent_live_not_implemented", Message: "Tencent live storage preflight is not implemented in this build.", Retryable: false}
}

func (unimplementedTencentClient) ProviderTruth(_ Request, _ map[string]string) Response {
	return Response{Ok: false, ErrorCode: "tencent_live_not_implemented", Message: "Tencent live provider truth is not implemented in this build.", Retryable: false}
}

func (unimplementedTencentClient) ComputeClaimTruth(_ Request, _ map[string]string) Response {
	return Response{Ok: false, ErrorCode: "provider_describe", Message: "Compute claim truth is unavailable.", Retryable: false}
}

func (unimplementedTencentClient) ClaimComputeMachine(_ Request, _ map[string]string) Response {
	return Response{Ok: false, ErrorCode: "provider_describe", Message: "Compute claim convergence is unavailable.", Retryable: false}
}

func (unimplementedTencentClient) PrepareComputeAllocation(_ Request, _ map[string]string) Response {
	return Response{Ok: false, ErrorCode: "tencent_live_not_implemented", Message: "Tencent live compute allocation preparation is not implemented in this build.", Retryable: false}
}

func (unimplementedTencentClient) CreateComputeAllocation(_ Request, _ map[string]string) Response {
	return Response{
		Ok:        false,
		ErrorCode: "tencent_live_not_implemented",
		Message:   "Tencent live compute allocation is not implemented in this build.",
		Retryable: false,
	}
}

func (unimplementedTencentClient) ReadComputeAllocation(_ Request, _ map[string]string) Response {
	return Response{Ok: false, ErrorCode: "tencent_live_not_implemented", Message: "Tencent live compute allocation readback is not implemented in this build.", Retryable: false}
}

func (unimplementedTencentClient) TagComputeMachine(_ Request, _ map[string]string) Response {
	return Response{Ok: false, ErrorCode: "tencent_live_not_implemented", Message: "Tencent live compute machine tagging is not implemented in this build.", Retryable: false}
}

func (unimplementedTencentClient) DestroyComputeAllocation(_ Request, _ map[string]string) Response {
	return Response{
		Ok:        false,
		ErrorCode: "tencent_live_not_implemented",
		Message:   "Tencent live compute allocation destroy is not implemented in this build.",
		Retryable: false,
	}
}

func (unimplementedTencentClient) SyncComputeAllocation(_ Request, _ map[string]string) Response {
	return Response{
		Ok:        false,
		ErrorCode: "tencent_live_not_implemented",
		Message:   "Tencent live compute allocation sync is not implemented in this build.",
		Retryable: false,
	}
}

func (unimplementedTencentClient) ReadComputeDestroyStatus(_ Request, _ map[string]string) Response {
	return Response{
		Ok:        false,
		ErrorCode: "tencent_live_not_implemented",
		Message:   "Tencent live compute destroy status readback is not implemented in this build.",
		Retryable: false,
	}
}

func (unimplementedTencentClient) CreateStorageVolume(_ Request, _ map[string]string) Response {
	return Response{Ok: false, ErrorCode: "tencent_live_not_implemented", Message: "Tencent live CBS creation is not implemented in this build.", Retryable: false}
}

func (unimplementedTencentClient) DiscoverStorageVolume(_ Request, _ map[string]string) Response {
	return Response{Ok: false, StorageState: "unknown", ErrorCode: "tencent_live_not_implemented", Message: "Tencent live CBS discovery is not implemented in this build.", Retryable: false}
}

func (unimplementedTencentClient) SyncStorageVolume(_ Request, _ map[string]string) Response {
	return Response{Ok: false, ErrorCode: "tencent_live_not_implemented", Message: "Tencent live CBS sync is not implemented in this build.", Retryable: false}
}

func (unimplementedTencentClient) DestroyStorageVolume(_ Request, _ map[string]string) Response {
	return Response{Ok: false, ErrorCode: "tencent_live_not_implemented", Message: "Tencent live CBS destroy is not implemented in this build.", Retryable: false}
}

func (unimplementedTencentClient) RenewStorageVolume(_ Request, _ map[string]string) Response {
	return Response{Ok: false, ErrorCode: "tencent_live_not_implemented", Message: "Tencent live CBS renewal is not implemented in this build.", Retryable: false}
}

func (unimplementedTencentClient) RenewComputeAllocation(_ Request, _ map[string]string) Response {
	return Response{Ok: false, ErrorCode: "tencent_live_not_implemented", Message: "Tencent live CVM renewal is not implemented in this build.", Retryable: false}
}

func (unimplementedTencentClient) BootstrapComputeNodePools(_ Request, _ map[string]string) Response {
	return Response{Ok: false, ErrorCode: "tencent_live_not_implemented", Message: "Tencent live node pool bootstrap is not implemented in this build.", Retryable: false}
}

func newTencentSDKClient(env map[string]string) (*tencentSDKClient, *Response) {
	missing := missingEnv(env)
	if len(missing) > 0 {
		return nil, &Response{
			Ok:         false,
			ErrorCode:  "tencent_env_missing",
			Message:    "Tencent Cloud provisioner environment is incomplete.",
			MissingEnv: missing,
			Retryable:  false,
		}
	}
	credential := common.NewCredential(env["TENCENTCLOUD_SECRET_ID"], env["TENCENTCLOUD_SECRET_KEY"])

	tkeProfile := profile.NewClientProfile()
	tkeProfile.HttpProfile.Endpoint = "tke.tencentcloudapi.com"
	tkeClient, err := tke2022.NewClient(credential, env["TENCENTCLOUD_REGION"], tkeProfile)
	if err != nil {
		return nil, &Response{
			Ok:        false,
			ErrorCode: "tencent_sdk_client_failed",
			Message:   err.Error(),
			Retryable: false,
		}
	}
	legacyTkeProfile := profile.NewClientProfile()
	legacyTkeProfile.HttpProfile.Endpoint = "tke.tencentcloudapi.com"
	legacyTkeClient, err := tke2018.NewClient(credential, env["TENCENTCLOUD_REGION"], legacyTkeProfile)
	if err != nil {
		return nil, &Response{Ok: false, ErrorCode: "tencent_sdk_client_failed", Message: err.Error(), Retryable: false}
	}
	cvmProfile := profile.NewClientProfile()
	cvmProfile.HttpProfile.Endpoint = "cvm.tencentcloudapi.com"
	cvmClient, err := cvm2017.NewClient(credential, env["TENCENTCLOUD_REGION"], cvmProfile)
	if err != nil {
		return nil, &Response{
			Ok:        false,
			ErrorCode: "tencent_sdk_client_failed",
			Message:   err.Error(),
			Retryable: false,
		}
	}
	cbsProfile := profile.NewClientProfile()
	cbsProfile.HttpProfile.Endpoint = "cbs.tencentcloudapi.com"
	cbsClient, err := cbs2017.NewClient(credential, env["TENCENTCLOUD_REGION"], cbsProfile)
	if err != nil {
		return nil, &Response{Ok: false, ErrorCode: "tencent_sdk_client_failed", Message: err.Error(), Retryable: false}
	}
	vpcProfile := profile.NewClientProfile()
	vpcProfile.HttpProfile.Endpoint = "vpc.tencentcloudapi.com"
	vpcClient, err := vpc2017.NewClient(credential, env["TENCENTCLOUD_REGION"], vpcProfile)
	if err != nil {
		return nil, &Response{Ok: false, ErrorCode: "tencent_sdk_client_failed", Message: err.Error(), Retryable: false}
	}
	tagProfile := profile.NewClientProfile()
	tagProfile.HttpProfile.Endpoint = "tag.tencentcloudapi.com"
	tagClient := &common.Client{}
	tagClient.Init(env["TENCENTCLOUD_REGION"]).WithCredential(credential).WithProfile(tagProfile)
	stsProfile := profile.NewClientProfile()
	stsProfile.HttpProfile.Endpoint = "sts.tencentcloudapi.com"
	stsClient := &common.Client{}
	stsClient.Init(env["TENCENTCLOUD_REGION"]).WithCredential(credential).WithProfile(stsProfile)
	camProfile := profile.NewClientProfile()
	camProfile.HttpProfile.Endpoint = "cam.tencentcloudapi.com"
	camClient := &common.Client{}
	camClient.Init(env["TENCENTCLOUD_REGION"]).WithCredential(credential).WithProfile(camProfile)

	return &tencentSDKClient{
		region:                env["TENCENTCLOUD_REGION"],
		clusterId:             env["TENCENT_DEPLOY_CLUSTER_ID"],
		nativeTkeClient:       tkeClient,
		nativeLegacyTkeClient: legacyTkeClient,
		nativeCvmClient:       cvmClient,
		nativeCbsClient:       cbsClient,
		nativeVpcClient:       vpcClient,
		nativeTagClient:       &tencentTagClient{client: tagClient, identityClient: stsClient, policyClient: camClient, region: env["TENCENTCLOUD_REGION"]},
		convergenceContext:    context.Background(),
		convergenceWait:       boundedConvergenceWait,
	}, nil
}

func boundedConvergenceWait(ctx context.Context, attempt int) error {
	delay := cvmOwnershipReadbackDelay(attempt)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func cvmOwnershipReadbackDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 500 * time.Millisecond
	case 2:
		return time.Second
	case 3:
		return 2 * time.Second
	case 4:
		return 4 * time.Second
	case 5:
		return 8 * time.Second
	default:
		return 0
	}
}
