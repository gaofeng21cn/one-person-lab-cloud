package main

import (
	"encoding/json"
	"errors"
	"testing"

	cbs2017 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cbs/v20170312"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tcerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
)

type storageFinalReleaseCBS struct {
	fakeNativeCbsAPI
	reply     string
	readReply string
}

func (api *storageFinalReleaseCBS) DescribeDisks(request *cbs2017.DescribeDisksRequest) (*cbs2017.DescribeDisksResponse, error) {
	result, err := api.fakeNativeCbsAPI.DescribeDisks(request)
	switch api.readReply {
	case "nil":
		return nil, nil
	case "nil-envelope":
		return &cbs2017.DescribeDisksResponse{}, nil
	case "no-request-id":
		result.Response.RequestId = nil
	case "no-count":
		result.Response.TotalCount = nil
	}
	return result, err
}

func (api *storageFinalReleaseCBS) TerminateDisks(request *cbs2017.TerminateDisksRequest) (*cbs2017.TerminateDisksResponse, error) {
	api.terminateDisksRequests = append(api.terminateDisksRequests, request)
	result := &cbs2017.TerminateDisksResponse{Response: &cbs2017.TerminateDisksResponseParams{RequestId: common.StringPtr("req-final-cbs")}}
	switch api.reply {
	case "error":
		return nil, errors.New("response lost after send")
	case "error-with-id":
		return result, errors.New("ambiguous response")
	case "nil":
		return nil, nil
	case "nil-envelope":
		return &cbs2017.TerminateDisksResponse{}, nil
	case "no-request-id":
		result.Response.RequestId = nil
	case "blank-request-id":
		result.Response.RequestId = common.StringPtr(" ")
	}
	return result, nil
}

func storageFinalReleaseRequest() Request {
	request := destroyStorageRequest()
	request.Action = storageFinalReleaseAction
	request.Zone = request.Storage.Zone
	request.Storage.Deadline = "2026-12-07T00:00:00Z"
	return request
}

func TestStorageFinalReleaseSDKExactDiskAndAdmission(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutate    func(*Request, *storageFinalReleaseCBS)
		ok        bool
		describes int
		mutations int
	}{
		{"exact recycled disk", func(*Request, *storageFinalReleaseCBS) {}, true, 1, 1},
		{"manual without notification", func(_ *Request, a *storageFinalReleaseCBS) { a.renewFlag = "DISABLE_NOTIFY_AND_MANUAL_RENEW" }, true, 1, 1},
		{"absence", func(_ *Request, a *storageFinalReleaseCBS) { a.empty = true }, true, 1, 0},
		{"sdk exact absence", func(_ *Request, a *storageFinalReleaseCBS) {
			a.err = tcerrors.NewTencentCloudSDKError(cbs2017.INVALIDDISKID_NOTFOUND, "absent", "req-absent")
		}, true, 1, 0},
		{"dry run", func(r *Request, _ *storageFinalReleaseCBS) { r.DryRun = true }, false, 0, 0},
		{"wrong region", func(r *Request, _ *storageFinalReleaseCBS) { r.Region = "ap-shanghai" }, false, 0, 0},
		{"missing region", func(r *Request, _ *storageFinalReleaseCBS) { r.Region = "" }, false, 0, 0},
		{"missing account", func(r *Request, _ *storageFinalReleaseCBS) { r.AccountId = "" }, false, 0, 0},
		{"wrong account", func(r *Request, _ *storageFinalReleaseCBS) { r.AccountId = "foreign" }, false, 0, 0},
		{"missing tag", func(r *Request, _ *storageFinalReleaseCBS) { delete(r.Tags, "opl_operation_id") }, false, 0, 0},
		{"extra request tag", func(r *Request, _ *storageFinalReleaseCBS) { r.Tags["extra"] = "foreign" }, false, 0, 0},
		{"padded request tag", func(r *Request, _ *storageFinalReleaseCBS) { r.Tags["opl_operation_id"] = " op-storage-alpha" }, false, 0, 0},
		{"wrong account tag", func(r *Request, _ *storageFinalReleaseCBS) { r.Tags["opl_account_id"] = "foreign" }, false, 0, 0},
		{"wrong workspace tag", func(_ *Request, a *storageFinalReleaseCBS) { a.tags = map[string]string{"opl_workspace_id": "foreign"} }, false, 1, 0},
		{"wrong resource tag", func(_ *Request, a *storageFinalReleaseCBS) { a.tags = map[string]string{"opl_resource_id": "foreign"} }, false, 1, 0},
		{"wrong operation tag", func(_ *Request, a *storageFinalReleaseCBS) { a.tags = map[string]string{"opl_operation_id": "foreign"} }, false, 1, 0},
		{"duplicate tag", func(_ *Request, a *storageFinalReleaseCBS) { a.duplicateTag = "opl_operation_id" }, false, 1, 0},
		{"empty disk suffix", func(r *Request, _ *storageFinalReleaseCBS) { r.Storage.Id = "disk-" }, false, 0, 0},
		{"missing disk", func(r *Request, _ *storageFinalReleaseCBS) { r.Storage.Id = "" }, false, 0, 0},
		{"zero size", func(r *Request, _ *storageFinalReleaseCBS) { r.Storage.SizeGB = 0 }, false, 0, 0},
		{"missing zone", func(r *Request, _ *storageFinalReleaseCBS) { r.Storage.Zone = "" }, false, 0, 0},
		{"top level zone drift", func(r *Request, _ *storageFinalReleaseCBS) { r.Zone = "ap-guangzhou-4" }, false, 0, 0},
		{"missing type", func(r *Request, _ *storageFinalReleaseCBS) { r.Storage.DiskType = "" }, false, 0, 0},
		{"padded type", func(r *Request, _ *storageFinalReleaseCBS) { r.Storage.DiskType = " CLOUD_BSSD" }, false, 0, 0},
		{"missing deadline", func(r *Request, _ *storageFinalReleaseCBS) { r.Storage.Deadline = "" }, false, 0, 0},
		{"invalid deadline", func(r *Request, _ *storageFinalReleaseCBS) { r.Storage.Deadline = "invalid" }, false, 0, 0},
		{"deadline beyond original", func(_ *Request, a *storageFinalReleaseCBS) { a.deadline = "2027-01-01T00:00:00Z" }, false, 1, 0},
		{"wrong disk id", func(_ *Request, a *storageFinalReleaseCBS) { a.diskID = "disk-foreign" }, false, 1, 0},
		{"wrong disk name", func(_ *Request, a *storageFinalReleaseCBS) { a.diskName = "foreign" }, false, 1, 0},
		{"wrong usage", func(_ *Request, a *storageFinalReleaseCBS) { a.diskUsage = "SYSTEM_DISK" }, false, 1, 0},
		{"wrong zone", func(_ *Request, a *storageFinalReleaseCBS) { a.zone = "ap-guangzhou-4" }, false, 1, 0},
		{"wrong size", func(_ *Request, a *storageFinalReleaseCBS) { a.diskSize = 20 }, false, 1, 0},
		{"wrong disk type", func(_ *Request, a *storageFinalReleaseCBS) { a.diskType = "CLOUD_SSD" }, false, 1, 0},
		{"not prepaid", func(_ *Request, a *storageFinalReleaseCBS) { a.diskChargeType = "CDHPAID" }, false, 1, 0},
		{"auto renew", func(_ *Request, a *storageFinalReleaseCBS) { a.renewFlag = "NOTIFY_AND_AUTO_RENEW" }, false, 1, 0},
		{"unknown renew", func(_ *Request, a *storageFinalReleaseCBS) { a.renewFlag = "UNKNOWN" }, false, 1, 0},
		{"attached", func(_ *Request, a *storageFinalReleaseCBS) { a.diskState = "ATTACHED" }, false, 1, 0},
		{"unattached", func(_ *Request, a *storageFinalReleaseCBS) { a.diskState = "UNATTACHED" }, false, 1, 0},
		{"unknown state", func(_ *Request, a *storageFinalReleaseCBS) { a.diskState = "UNKNOWN" }, false, 1, 0},
		{"missing state", func(_ *Request, a *storageFinalReleaseCBS) { a.omitDiskState = true }, false, 1, 0},
		{"missing provider deadline", func(_ *Request, a *storageFinalReleaseCBS) { a.omitDeadline = true }, false, 1, 0},
		{"duplicate result", func(_ *Request, a *storageFinalReleaseCBS) { a.duplicate = true }, false, 1, 0},
		{"nil result", func(_ *Request, a *storageFinalReleaseCBS) { a.readReply = "nil" }, false, 1, 0},
		{"nil envelope", func(_ *Request, a *storageFinalReleaseCBS) { a.readReply = "nil-envelope" }, false, 1, 0},
		{"missing read request id", func(_ *Request, a *storageFinalReleaseCBS) { a.readReply = "no-request-id" }, false, 1, 0},
		{"missing absent request id", func(_ *Request, a *storageFinalReleaseCBS) { a.empty = true; a.readReply = "no-request-id" }, false, 1, 0},
		{"missing cardinality", func(_ *Request, a *storageFinalReleaseCBS) { a.readReply = "no-count" }, false, 1, 0},
		{"read error", func(_ *Request, a *storageFinalReleaseCBS) { a.err = errors.New("network error") }, false, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := storageFinalReleaseRequest()
			api := &storageFinalReleaseCBS{fakeNativeCbsAPI: fakeNativeCbsAPI{diskState: "TORECYCLE", deadline: "2026-11-07T00:00:00Z"}}
			tc.mutate(&request, api)
			got := (&tencentSDKClient{region: "ap-guangzhou", nativeCbsClient: api}).ReleaseRecycledStorageVolume(request, nil)
			if got.Ok != tc.ok || got.MutationCount != tc.mutations || len(api.terminateDisksRequests) != tc.mutations || got.Retryable {
				t.Fatalf("out=%+v terminate=%d", got, len(api.terminateDisksRequests))
			}
			assertExactCBSDescribeRequests(t, &api.fakeNativeCbsAPI, tc.describes)
			if tc.mutations == 1 {
				call := api.terminateDisksRequests[0]
				if len(call.DiskIds) != 1 || stringValue(call.DiskIds[0]) != "disk-storage-alpha" || call.DeleteSnapshot != nil || got.Status != "accepted" || got.ProviderRequestId != "req-final-cbs" {
					t.Fatalf("unexpected release: out=%+v call=%+v", got, call)
				}
			}
			if tc.ok && tc.mutations == 0 && (got.CBSStatus != "NOT_FOUND" || got.Status != "external_deleted" || got.ProviderRequestId == "") {
				t.Fatalf("absence not confirmed: %+v", got)
			}
		})
	}
}

func TestStorageFinalReleaseSDKAmbiguousSendNeverRetriesOrPolls(t *testing.T) {
	for _, reply := range []string{"error", "error-with-id", "nil", "nil-envelope", "no-request-id", "blank-request-id"} {
		t.Run(reply, func(t *testing.T) {
			api := &storageFinalReleaseCBS{fakeNativeCbsAPI: fakeNativeCbsAPI{diskState: "TORECYCLE"}, reply: reply}
			got := (&tencentSDKClient{region: "ap-guangzhou", nativeCbsClient: api}).ReleaseRecycledStorageVolume(storageFinalReleaseRequest(), nil)
			if got.Ok || got.Retryable || got.MutationCount != 1 || got.ErrorCode != "storage_final_release_dispatch_unconfirmed" || got.StorageVolumeId != "disk-storage-alpha" || len(api.terminateDisksRequests) != 1 {
				t.Fatalf("ambiguous dispatch: %+v calls=%d", got, len(api.terminateDisksRequests))
			}
			assertExactCBSDescribeRequests(t, &api.fakeNativeCbsAPI, 1)
			if reply == "error-with-id" && got.ProviderRequestId != "req-final-cbs" {
				t.Fatalf("lost request evidence: %+v", got)
			}
		})
	}
}

func TestStorageFinalReleaseSDKMissingClientRefuses(t *testing.T) {
	for _, client := range []*tencentSDKClient{nil, {region: "ap-guangzhou"}} {
		if got := client.ReleaseRecycledStorageVolume(storageFinalReleaseRequest(), nil); got.Ok || got.MutationCount != 0 {
			t.Fatalf("missing client accepted: %+v", got)
		}
	}
}

func TestStorageFinalReleaseRequestDispatcherEnforcesExistingAuthority(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mutate      func(*Request, map[string]string)
		unsupported bool
		code        string
		mutations   int
	}{
		{name: "authorized", mutate: func(*Request, map[string]string) {}, mutations: 1},
		{name: "live flag missing", mutate: func(_ *Request, e map[string]string) { delete(e, "RUN_TENCENT_CREATE_RELEASE_EXECUTION") }, code: "live_mutation_flag_required"},
		{name: "missing environment", mutate: func(_ *Request, e map[string]string) { delete(e, "TENCENTCLOUD_SECRET_ID") }, code: "tencent_env_missing"},
		{name: "dry run", mutate: func(r *Request, _ map[string]string) { r.DryRun = true }, code: "storage_final_release_unavailable"},
		{name: "unsupported client", mutate: func(*Request, map[string]string) {}, unsupported: true, code: "storage_final_release_unavailable"},
		{name: "protected target", mutate: func(r *Request, e map[string]string) { r.Allocation.InstanceId = e["OPL_SYSTEM_COMPUTE_CVM_ID"] }, code: "protected_system_resource"},
		{name: "wrong tags", mutate: func(r *Request, _ map[string]string) { r.Tags["opl_workspace_id"] = "foreign" }, code: "tencent_cbs_readback_opl_workspace_id_tag_mismatch"},
		{name: "wrong region", mutate: func(r *Request, _ map[string]string) { r.Region = "ap-shanghai" }, code: "storage_provider_config_identity_mismatch"},
		{name: "non recycled", mutate: func(*Request, map[string]string) {}, code: "storage_final_release_identity_or_state_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := storageFinalReleaseRequest()
			env := protectedResourceEnv()
			env["RUN_TENCENT_CREATE_RELEASE_EXECUTION"] = "1"
			tc.mutate(&request, env)
			// Exercise the same JSON Request boundary and dispatcher used by main.
			body, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Request
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatal(err)
			}
			api := &storageFinalReleaseCBS{fakeNativeCbsAPI: fakeNativeCbsAPI{diskState: "TORECYCLE"}}
			if tc.name == "non recycled" {
				api.diskState = "UNATTACHED"
			}
			var client TencentClient = &tencentSDKClient{region: "ap-guangzhou", nativeCbsClient: api}
			if tc.unsupported {
				client = unimplementedTencentClient{}
			}
			got := handleWithClient(decoded, env, client)
			if got.ErrorCode != tc.code || got.MutationCount != tc.mutations || len(api.terminateDisksRequests) != tc.mutations || got.Ok != (tc.code == "") {
				t.Fatalf("dispatch=%+v calls=%d", got, len(api.terminateDisksRequests))
			}
		})
	}
}
