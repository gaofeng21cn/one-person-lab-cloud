package main

import (
	"errors"
	"testing"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	cvm2017 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm/v20170312"
)

type finalReleaseCVM struct {
	fakeNativeCvmAPI
	calls int
	lost  bool
}

func (c *finalReleaseCVM) TerminateInstances(r *cvm2017.TerminateInstancesRequest) (*cvm2017.TerminateInstancesResponse, error) {
	c.calls++
	if len(r.InstanceIds) != 1 || stringValue(r.InstanceIds[0]) != "ins-basic-1" {
		panic("wrong release target")
	}
	if c.lost {
		return nil, errors.New("response lost")
	}
	return &cvm2017.TerminateInstancesResponse{Response: &cvm2017.TerminateInstancesResponseParams{RequestId: common.StringPtr("req-release")}}, nil
}

func TestFinalReleaseRequiresExactRecycledPrepaidCVM(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Request, *fakeNativeTkeAPI, *finalReleaseCVM)
		calls  int
		ok     bool
	}{
		{"exact", func(*Request, *fakeNativeTkeAPI, *finalReleaseCVM) {}, 1, true},
		{"lost response", func(_ *Request, _ *fakeNativeTkeAPI, c *finalReleaseCVM) { c.lost = true }, 1, false},
		{"already absent", func(_ *Request, _ *fakeNativeTkeAPI, c *finalReleaseCVM) { c.empty = true }, 0, true},
		{"running", func(_ *Request, _ *fakeNativeTkeAPI, c *finalReleaseCVM) { c.instanceState = "RUNNING" }, 0, false},
		{"unisolated shutdown", func(_ *Request, _ *fakeNativeTkeAPI, c *finalReleaseCVM) { c.isolatedSource = "" }, 0, false},
		{"postpaid", func(_ *Request, _ *fakeNativeTkeAPI, c *finalReleaseCVM) { c.instanceChargeType = "POSTPAID_BY_HOUR" }, 0, false},
		{"foreign tag", func(_ *Request, _ *fakeNativeTkeAPI, c *finalReleaseCVM) { c.tags["opl_workspace_id"] = "foreign" }, 0, false},
		{"region drift", func(r *Request, _ *fakeNativeTkeAPI, _ *finalReleaseCVM) { r.Region = "ap-shanghai" }, 0, false},
		{"machine present", func(_ *Request, k *fakeNativeTkeAPI, _ *finalReleaseCVM) { k.deletedMachineNames = nil }, 0, false},
		{"zone drift", func(_ *Request, _ *fakeNativeTkeAPI, c *finalReleaseCVM) { c.zone = "ap-guangzhou-4" }, 0, false},
		{"malformed read", func(_ *Request, _ *fakeNativeTkeAPI, c *finalReleaseCVM) { c.nilEnvelope = true }, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := computeDestroyStatusRequest("NativeCVM")
			r.Action = "release_recycled_compute_allocation"
			r.Zone = "ap-guangzhou-3"
			r.Pool.InstanceType = "SA5.MEDIUM4"
			r.Tags = map[string]string{"opl_account_id": "acct-alpha", "opl_workspace_id": "ws-alpha", "opl_resource_id": "compute-alpha", "opl_operation_id": "original-create"}
			k := &fakeNativeTkeAPI{nodePoolId: "np-basic", replicas: 1, deletedMachineNames: map[string]bool{"node-basic-1": true}}
			c := &finalReleaseCVM{fakeNativeCvmAPI: fakeNativeCvmAPI{instanceState: "SHUTDOWN", isolatedSource: "MANMADE", tags: map[string]string{}}}
			for key, value := range r.Tags {
				c.tags[key] = value
			}
			tc.mutate(&r, k, c)
			client := &tencentSDKClient{region: "ap-guangzhou", clusterId: "cls-123", nativeTkeClient: k, nativeCvmClient: c}
			out := client.ReleaseRecycledComputeAllocation(r, nil)
			if out.Ok != tc.ok || c.calls != tc.calls || out.MutationCount != tc.calls {
				t.Fatalf("out=%+v calls=%d", out, c.calls)
			}
			if countStrings(k.calls, "DeleteClusterMachines") != 0 || countStrings(k.calls, "ModifyNodePool") != 0 {
				t.Fatal("final release mutated TKE")
			}
		})
	}
}

func TestFinalReleaseDispatchPreservesLiveAndProtectedResourceGates(t *testing.T) {
	r := computeDestroyStatusRequest("NativeCVM")
	r.Action = "release_recycled_compute_allocation"
	env := protectedResourceEnv()
	delete(env, "RUN_TENCENT_CREATE_RELEASE_EXECUTION")
	got := handleWithClient(r, env, unimplementedTencentClient{})
	if got.Ok || got.ErrorCode != "live_mutation_flag_required" {
		t.Fatalf("live gate: %+v", got)
	}
	env["RUN_TENCENT_CREATE_RELEASE_EXECUTION"] = "1"
	r.Allocation.InstanceId = env["OPL_SYSTEM_COMPUTE_CVM_ID"]
	got = handleWithClient(r, env, unimplementedTencentClient{})
	if got.Ok || got.ErrorCode != "protected_system_resource" {
		t.Fatalf("system guard: %+v", got)
	}
}
