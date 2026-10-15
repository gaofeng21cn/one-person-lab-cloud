package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tcerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	tke2022 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tke/v20220501"
)

func nodePoolReadbackSyncRequest() Request {
	return Request{
		Action: "sync_compute_allocation", AccountId: "acct-alpha", PackageId: "basic", Zone: "ap-guangzhou-3",
		Region: "ap-guangzhou", Tags: computeOwnershipTags(),
		Pool: ComputePoolInput{Id: "pool-basic-2c4g", ClusterId: "cls-123", NodePoolId: "np-historical",
			InstanceType: "SA5.MEDIUM4", CPU: 2, MemoryGB: 4, MaxReplicas: 10, TargetReplicas: 1},
		Allocation: ComputeAllocationInput{Id: "compute-alpha", InstanceId: "ins-basic-1", MachineType: "NativeCVM",
			MachineName: "node-basic-1", NodeName: "10.0.0.11", PrivateIp: "10.0.0.11", Deadline: "2026-08-16T00:00:00Z"},
	}
}

func assertNodePoolReadbackFailure(t *testing.T, response Response, code, requestID string) {
	t.Helper()
	if response.Ok || response.ErrorCode != code || response.ProviderRequestId != requestID || !response.Retryable ||
		response.Message != code || response.ProviderData["providerErrorMessage"] != code {
		t.Errorf("response=%#v wantCode=%q wantRequestID=%q", response, code, requestID)
	}
	if response.Status == "absent" || response.Status == "external_deleted" || response.MutationCount != 0 ||
		response.MachinePresent != nil || response.NodePoolId != "" || response.InstanceId != "" {
		t.Errorf("unverified readback became a resource fact: %#v", response)
	}
	// IDs have dedicated evidence fields. Diagnostic text must stay fixed and redacted.
	publicDiagnostic, err := json.Marshal(struct{ Code, Message string }{response.ErrorCode, response.Message})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"provider-secret", "req-sensitive", "req-readback", "np-historical", "np-foreign", "ins-basic-1", "10.0.0.11"} {
		if strings.Contains(string(publicDiagnostic), secret) || strings.Contains(response.ProviderData["providerErrorMessage"], secret) {
			t.Errorf("diagnostic leaked %q: %s data=%#v", secret, publicDiagnostic, response.ProviderData)
		}
	}
}

func TestNodePoolReadbackSDKProbe(t *testing.T) {
	for _, tc := range []struct{ name, payload, code, requestID string }{
		{"nil envelope", `{"Response":null}`, "tencent_node_pool_readback_response_invalid", ""},
		{"empty", `{"Response":{"TotalCount":0,"NodePools":[],"RequestId":"req-readback"}}`, "tencent_node_pool_readback_empty_or_ambiguous", "req-readback"},
		{"count ambiguity", `{"Response":{"TotalCount":2,"NodePools":[{"NodePoolId":"np-historical"}],"RequestId":"req-readback"}}`, "tencent_node_pool_readback_empty_or_ambiguous", "req-readback"},
		{"ID mismatch", `{"Response":{"TotalCount":1,"NodePools":[{"NodePoolId":"np-foreign"}],"RequestId":"req-readback"}}`, "tencent_node_pool_readback_id_mismatch", "req-readback"},
		{"SDK error", `{"Response":{"Error":{"Code":"AuthFailure.UnauthorizedOperation","Message":"provider-secret np-historical 10.0.0.11"},"RequestId":"req-sensitive"}}`, "tencent_describe_node_pool_failed", "req-sensitive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var captured []*tke2022.DescribeNodePoolsRequest
			api := &fakeNativeTkeAPI{nodePoolId: "np-historical", replicas: 1}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Header.Get("X-TC-Action") == "DescribeClusterMachines" {
					request := tke2022.NewDescribeClusterMachinesRequest()
					if err := json.NewDecoder(r.Body).Decode(request); err != nil {
						t.Error(err)
					}
					response, err := api.DescribeClusterMachines(request)
					if err != nil {
						t.Error(err)
					}
					_ = json.NewEncoder(w).Encode(response)
					return
				}
				if r.Header.Get("X-TC-Action") != "DescribeNodePools" {
					t.Errorf("unexpected provider action: %s", r.Header.Get("X-TC-Action"))
				}
				request := tke2022.NewDescribeNodePoolsRequest()
				if err := json.NewDecoder(r.Body).Decode(request); err != nil {
					t.Error(err)
				}
				captured = append(captured, request)
				_, _ = io.WriteString(w, tc.payload)
			}))
			defer server.Close()
			clientProfile := profile.NewClientProfile()
			clientProfile.HttpProfile.Scheme = "http"
			clientProfile.HttpProfile.Endpoint = strings.TrimPrefix(server.URL, "http://")
			sdk, err := tke2022.NewClient(common.NewCredential("fixture-id", "fixture-key"), "ap-guangzhou", clientProfile)
			if err != nil {
				t.Fatal(err)
			}
			client := newFakeTencentSDKClient(api)
			client.nativeCvmClient = &fakeNativeCvmAPI{instanceName: "compute-alpha", tags: computeOwnershipTags()}
			client.nativeTkeClient = sdk
			response := handleWithClient(nodePoolReadbackSyncRequest(), protectedResourceEnv(), client)
			assertNodePoolReadbackFailure(t, response, tc.code, tc.requestID)
			if len(captured) != 1 || captured[0].ClusterId == nil || *captured[0].ClusterId != "cls-123" || nodePoolIdFilterValue(captured[0]) != "np-historical" {
				t.Fatalf("readback changed historical identity or retried: %#v", captured)
			}
		})
	}
}

type nodePoolReadbackFixture struct {
	*fakeNativeTkeAPI
	response *tke2022.DescribeNodePoolsResponse
	err      error
}

func (api *nodePoolReadbackFixture) DescribeNodePools(request *tke2022.DescribeNodePoolsRequest) (*tke2022.DescribeNodePoolsResponse, error) {
	api.record("DescribeNodePools")
	api.describeNodePoolsRequest = append(api.describeNodePoolsRequest, request)
	return api.response, api.err
}

func TestNodePoolReadbackFixtures(t *testing.T) {
	sdkErr := tcerrors.NewTencentCloudSDKError("AuthFailure.UnauthorizedOperation", "provider-secret np-historical", "req-sensitive")
	for _, tc := range []struct {
		name          string
		response      *tke2022.DescribeNodePoolsResponseParams
		err           error
		code          string
		providerCode  string
		requestID     string
		missingResult bool
	}{
		{name: "nil response", missingResult: true, code: "tencent_node_pool_readback_response_invalid"},
		{name: "nil envelope", code: "tencent_node_pool_readback_response_invalid"},
		{name: "nil count", response: &tke2022.DescribeNodePoolsResponseParams{NodePools: []*tke2022.NodePool{{NodePoolId: common.StringPtr("np-historical")}}}, code: "tencent_node_pool_readback_response_invalid"},
		{name: "nil pool", response: &tke2022.DescribeNodePoolsResponseParams{TotalCount: common.Int64Ptr(1), NodePools: []*tke2022.NodePool{nil}}, code: "tencent_node_pool_readback_response_invalid"},
		{name: "nil pool ID", response: &tke2022.DescribeNodePoolsResponseParams{TotalCount: common.Int64Ptr(1), NodePools: []*tke2022.NodePool{{}}}, code: "tencent_node_pool_readback_id_mismatch"},
		{name: "multiple pools", response: &tke2022.DescribeNodePoolsResponseParams{TotalCount: common.Int64Ptr(1), NodePools: []*tke2022.NodePool{{}, {}}}, code: "tencent_node_pool_readback_empty_or_ambiguous"},
		{name: "missing pools", response: &tke2022.DescribeNodePoolsResponseParams{TotalCount: common.Int64Ptr(1)}, code: "tencent_node_pool_readback_empty_or_ambiguous"},
		{name: "generic error with misleading text", err: errors.New("tencent_node_pool_readback_id_mismatch provider-secret np-historical req-sensitive"), code: "tencent_describe_node_pool_failed"},
		{name: "SDK error", err: sdkErr, code: "tencent_describe_node_pool_failed", requestID: "req-sensitive", providerCode: "AuthFailure.UnauthorizedOperation"},
		{name: "wrapped SDK error", err: fmt.Errorf("provider-secret: %w", sdkErr), code: "tencent_describe_node_pool_failed", requestID: "req-sensitive", providerCode: "AuthFailure.UnauthorizedOperation"},
		{name: "unsafe SDK code", err: tcerrors.NewTencentCloudSDKError("AuthFailure provider-secret", "provider-secret", "req-sensitive"), code: "tencent_describe_node_pool_failed", requestID: "req-sensitive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &nodePoolReadbackFixture{fakeNativeTkeAPI: &fakeNativeTkeAPI{nodePoolId: "np-historical", replicas: 1}, err: tc.err}
			if !tc.missingResult {
				api.response = &tke2022.DescribeNodePoolsResponse{Response: tc.response}
			}
			if tc.response != nil {
				tc.response.RequestId = common.StringPtr("req-readback")
				tc.requestID = "req-readback"
			}
			client := newFakeTencentSDKClient(api.fakeNativeTkeAPI)
			client.nativeTkeClient = api
			client.nativeCvmClient = &fakeNativeCvmAPI{instanceName: "compute-alpha", tags: computeOwnershipTags()}
			request := nodePoolReadbackSyncRequest()
			before, _ := json.Marshal(request)
			response := client.SyncComputeAllocation(request, nil)
			assertNodePoolReadbackFailure(t, response, tc.code, tc.requestID)
			if tc.providerCode != "" && response.ProviderData["providerErrorCode"] != tc.providerCode {
				t.Errorf("bounded SDK code lost: %#v", response.ProviderData)
			}
			if tc.providerCode == "" && response.ProviderData["providerErrorCode"] != tc.code {
				t.Errorf("unexpected provider code: %#v", response.ProviderData)
			}
			after, _ := json.Marshal(request)
			if string(before) != string(after) || !reflect.DeepEqual(api.calls, []string{"DescribeClusterMachines", "DescribeClusterMachines", "DescribeNodePools"}) {
				t.Fatalf("failed readback changed input or continued: calls=%v before=%s after=%s", api.calls, before, after)
			}
		})
	}
}

func TestNodePoolReadbackMutationAdmission(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response *tke2022.DescribeNodePoolsResponseParams
		err      error
		code     string
	}{
		{name: "invalid", code: "tencent_node_pool_readback_response_invalid"},
		{name: "empty", response: &tke2022.DescribeNodePoolsResponseParams{TotalCount: common.Int64Ptr(0)}, code: "tencent_node_pool_readback_empty_or_ambiguous"},
		{name: "ambiguous", response: &tke2022.DescribeNodePoolsResponseParams{TotalCount: common.Int64Ptr(2), NodePools: []*tke2022.NodePool{{}, {}}}, code: "tencent_node_pool_readback_empty_or_ambiguous"},
		{name: "wrong ID", response: &tke2022.DescribeNodePoolsResponseParams{TotalCount: common.Int64Ptr(1), NodePools: []*tke2022.NodePool{{NodePoolId: common.StringPtr("np-foreign")}}}, code: "tencent_node_pool_readback_id_mismatch"},
		{name: "wrapped SDK", err: fmt.Errorf("provider-secret: %w", tcerrors.NewTencentCloudSDKError("AuthFailure.UnauthorizedOperation", "provider-secret", "req-sensitive")), code: "tencent_describe_node_pool_failed"},
		{name: "existing generic IAM rejection", err: errors.New("permission denied provider-secret"), code: "tencent_describe_node_pool_failed"},
		{name: "existing SDK IAM rejection", err: tcerrors.NewTencentCloudSDKError("InternalError", "permission denied provider-secret", "req-sensitive"), code: "tencent_describe_node_pool_failed"},
	} {
		for _, caller := range []string{"prepare", "create", "read", "destroy", "claim truth"} {
			t.Run(tc.name+"/"+caller, func(t *testing.T) {
				api := &nodePoolReadbackFixture{fakeNativeTkeAPI: &fakeNativeTkeAPI{},
					response: &tke2022.DescribeNodePoolsResponse{Response: tc.response}, err: tc.err}
				client := newFakeTencentSDKClient(api.fakeNativeTkeAPI)
				client.nativeTkeClient = api
				request := nodePoolReadbackSyncRequest()
				before, _ := json.Marshal(request)
				var response Response
				wantCode := tc.code
				switch caller {
				case "prepare":
					response = client.PrepareComputeAllocation(request, nil)
				case "create":
					response = client.CreateComputeAllocation(request, nil)
				case "read":
					response = client.ReadComputeAllocation(request, nil)
				case "destroy":
					response = client.DestroyComputeAllocation(request, nil)
				case "claim truth":
					response = client.ComputeClaimTruth(request, nil)
					wantCode = "provider_describe"
					if tc.err != nil {
						wantCode = "iam_rbac"
					}
				}
				if response.Ok || response.ErrorCode != wantCode || response.MutationCount != 0 ||
					response.Status == "absent" || response.Status == "external_deleted" || !reflect.DeepEqual(api.calls, []string{"DescribeNodePools"}) {
					t.Fatalf("unverified NodePool passed admission: response=%#v calls=%v", response, api.calls)
				}
				after, _ := json.Marshal(request)
				if string(before) != string(after) || nodePoolIdFilterValue(api.describeNodePoolsRequest[0]) != "np-historical" {
					t.Fatalf("historical identity changed: before=%s after=%s", before, after)
				}
				assertProviderTruthReadOnly(t, api.fakeNativeTkeAPI, client.nativeCvmClient.(*fakeNativeCvmAPI))
			})
		}
	}
}

type invalidMachineReadbackFixture struct {
	*fakeNativeTkeAPI
	machines []*tke2022.Machine
}

func (api *invalidMachineReadbackFixture) DescribeClusterMachines(request *tke2022.DescribeClusterMachinesRequest) (*tke2022.DescribeClusterMachinesResponse, error) {
	api.record("DescribeClusterMachines")
	return &tke2022.DescribeClusterMachinesResponse{Response: &tke2022.DescribeClusterMachinesResponseParams{
		TotalCount: common.Int64Ptr(int64(len(api.machines))), Machines: api.machines, RequestId: common.StringPtr("req-machine-invalid"),
	}}, nil
}

func TestRetiredPoolObservationRejectsUnprovenMachineAbsence(t *testing.T) {
	for _, name := range []string{"association gone but Machine remains", "nil Machine", "missing Machine identity", "missing retained Machine name"} {
		t.Run(name, func(t *testing.T) {
			request := nodePoolReadbackSyncRequest()
			api := &fakeNativeTkeAPI{nodePoolId: request.Pool.NodePoolId, describeNodePoolErr: errors.New("retired pool")}
			client := newFakeTencentSDKClient(api)
			cvmAPI := &fakeNativeCvmAPI{empty: true}
			client.nativeCvmClient = cvmAPI
			switch name {
			case "association gone but Machine remains":
				api.rejectMachinePoolFilter, api.replicas, api.omitClusterInstances = true, 1, true
			case "nil Machine":
				client.nativeTkeClient = &invalidMachineReadbackFixture{fakeNativeTkeAPI: api, machines: []*tke2022.Machine{nil}}
			case "missing Machine identity":
				client.nativeTkeClient = &invalidMachineReadbackFixture{fakeNativeTkeAPI: api, machines: []*tke2022.Machine{{}}}
			case "missing retained Machine name":
				request.Allocation.MachineName = ""
			}
			result := handleWithClient(request, protectedResourceEnv(), client)
			if result.Ok || result.ErrorCode == "" || (result.Observation != nil && result.Observation.Available) {
				t.Fatalf("unproven Machine absence became an observed fact: %#v", result)
			}
			assertProviderTruthDescribeOnly(t, api.calls)
			assertProviderTruthReadOnly(t, api, cvmAPI)
		})
	}
}

func TestRetiredPoolObservationPreservesAssociationPermissionFailure(t *testing.T) {
	request := nodePoolReadbackSyncRequest()
	api := &fakeNativeTkeAPI{nodePoolId: request.Pool.NodePoolId, describeNodePoolErr: errors.New("retired pool"),
		describeClusterInstancesErr: tcerrors.NewTencentCloudSDKError("AuthFailure.UnauthorizedOperation", "provider-secret", "req-association-denied")}
	client := newFakeTencentSDKClient(api)
	cvmAPI := &fakeNativeCvmAPI{instanceName: request.Allocation.Id, tags: computeOwnershipTags(), instanceState: "STOPPED"}
	client.nativeCvmClient = cvmAPI
	result := handleWithClient(request, protectedResourceEnv(), client)
	const wantCode contracts.ErrorCode = "tencent_describe_tke_instance_failed"
	if result.Ok || contracts.ErrorCode(result.ErrorCode) != wantCode || result.ProviderRequestId != "req-association-denied" ||
		result.ProviderData["providerErrorCode"] != "AuthFailure.UnauthorizedOperation" ||
		result.Observation == nil || !result.Observation.Available || result.Observation.State != contracts.ResourceObservedStopped ||
		contracts.ErrorCode(result.Observation.ReasonCode) != wantCode {
		t.Fatalf("association failure or owned stopped state was lost: %#v", result)
	}
	if strings.Contains(result.Message, "provider-secret") || strings.Contains(result.ProviderData["providerErrorMessage"], "provider-secret") {
		t.Fatal("SDK message leaked")
	}
	assertProviderTruthDescribeOnly(t, api.calls)
	assertProviderTruthReadOnly(t, api, cvmAPI)
}
