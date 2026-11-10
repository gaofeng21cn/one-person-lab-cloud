package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	cvm2017 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm/v20170312"
	tke2022 "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tke/v20220501"
)

type prepaidRetirementSDKCall struct {
	action string
	body   map[string]any
}

// The actual Tencent SDK signs, serializes and decodes every request. Its only
// transport is this in-memory fake; none of these tests can reach a provider.
type prepaidRetirementSDK struct {
	t              *testing.T
	pool           *tke2022.NodePool
	machine        *tke2022.Machine
	instance       *cvm2017.Instance
	machinePresent bool
	outsidePool    bool
	calls          []prepaidRetirementSDKCall
	before         func(string, int)
	editReply      func(string, map[string]any)
	mutationReply  string
}

func (sdk *prepaidRetirementSDK) count(action string) int {
	count := 0
	for _, call := range sdk.calls {
		if call.action == action {
			count++
		}
	}
	return count
}

func (sdk *prepaidRetirementSDK) RoundTrip(r *http.Request) (*http.Response, error) {
	body := map[string]any{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		sdk.t.Fatal(err)
	}
	action := r.Header.Get("X-TC-Action")
	sdk.calls = append(sdk.calls, prepaidRetirementSDKCall{action, body})
	if sdk.before != nil {
		sdk.before(action, sdk.count(action))
	}
	params := map[string]any{"RequestId": "req-" + action}
	switch action {
	case "DescribeNodePools":
		params["NodePools"], params["TotalCount"] = []*tke2022.NodePool{sdk.pool}, 1
	case "DescribeClusterMachines":
		machines := []*tke2022.Machine{}
		if sdk.machinePresent && !(sdk.outsidePool && body["Filters"] != nil) {
			machines = append(machines, sdk.machine)
		}
		params["Machines"], params["TotalCount"] = machines, len(machines)
	case "DescribeInstances":
		instances := []*cvm2017.Instance{}
		if sdk.instance != nil {
			instances = append(instances, sdk.instance)
		}
		params["InstanceSet"], params["TotalCount"] = instances, len(instances)
	case "DeleteClusterMachines", "TerminateInstances":
		if action == "DeleteClusterMachines" {
			sdk.machinePresent = false
		} else {
			sdk.instance.InstanceState = common.StringPtr("SHUTDOWN")
			sdk.instance.IsolatedSource = common.StringPtr("MANMADE")
		}
		switch sdk.mutationReply {
		case "lost":
			return nil, errors.New("synthetic lost reply; private provider diagnostic")
		case "missing id":
			delete(params, "RequestId")
		case "blank id":
			params["RequestId"] = "  "
		case "unsafe id":
			params["RequestId"] = "req-invalid\nprivate provider diagnostic"
		case "sdk error":
			params["Error"] = map[string]string{"Code": "InternalError", "Message": "private provider diagnostic"}
		}
	default:
		sdk.t.Fatalf("unexpected SDK action %q", action)
	}
	if sdk.editReply != nil {
		sdk.editReply(action, params)
	}
	envelope := map[string]any{"Response": params}
	if sdk.mutationReply == "missing envelope" && (action == "DeleteClusterMachines" || action == "TerminateInstances") {
		envelope["Response"] = nil
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		sdk.t.Fatal(err)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(encoded))), Request: r}, nil
}

func newPrepaidRetirementSDK(t *testing.T, action string) (Request, *tencentSDKClient, *prepaidRetirementSDK) {
	t.Helper()
	r := computeDestroyStatusRequest("NativeCVM")
	r.Action, r.Zone, r.Pool.InstanceType = action, "ap-guangzhou-3", "SA5.MEDIUM4"
	r.Tags = map[string]string{"opl_account_id": "acct-alpha", "opl_workspace_id": "ws-alpha", "opl_resource_id": "compute-alpha", "opl_operation_id": "original-create"}
	poolReply, err := (&fakeNativeTkeAPI{discoverNodePoolId: "np-basic"}).DescribeNodePools(tke2022.NewDescribeNodePoolsRequest())
	if err != nil {
		t.Fatal(err)
	}
	sdk := &prepaidRetirementSDK{
		t: t, pool: poolReply.Response.NodePools[0], machinePresent: action == detachPrepaidComputeAction,
		machine: &tke2022.Machine{MachineName: common.StringPtr(r.Allocation.MachineName), LanIP: common.StringPtr(r.Allocation.PrivateIp),
			MachineState: common.StringPtr("Running"), InstanceType: common.StringPtr(r.Pool.InstanceType)},
		instance: &cvm2017.Instance{InstanceId: common.StringPtr(r.Allocation.InstanceId), InstanceName: common.StringPtr(r.Allocation.Id),
			InstanceType: common.StringPtr(r.Pool.InstanceType), Placement: &cvm2017.Placement{Zone: common.StringPtr(r.Zone)},
			PrivateIpAddresses: []*string{common.StringPtr(r.Allocation.PrivateIp)}, InstanceChargeType: common.StringPtr("PREPAID"),
			RenewFlag: common.StringPtr("NOTIFY_AND_MANUAL_RENEW"), InstanceState: common.StringPtr("RUNNING"), IsolatedSource: common.StringPtr("NOTISOLATED")},
	}
	for _, key := range cbsOwnershipTagKeys {
		sdk.instance.Tags = append(sdk.instance.Tags, &cvm2017.Tag{Key: common.StringPtr(key), Value: common.StringPtr(r.Tags[key])})
	}
	credential := common.NewCredential("synthetic-id", "synthetic-key")
	tke, err := tke2022.NewClient(credential, r.Region, profile.NewClientProfile())
	if err != nil {
		t.Fatal(err)
	}
	cvm, err := cvm2017.NewClient(credential, r.Region, profile.NewClientProfile())
	if err != nil {
		t.Fatal(err)
	}
	tke.WithHttpTransport(sdk)
	cvm.WithHttpTransport(sdk)
	return r, &tencentSDKClient{region: r.Region, clusterId: r.Pool.ClusterId, nativeTkeClient: tke, nativeCvmClient: cvm}, sdk
}

func invokePrepaidRetirement(client *tencentSDKClient, r Request) Response {
	if r.Action == detachPrepaidComputeAction {
		return client.DetachPrepaidComputeAllocation(r, nil)
	}
	return client.RetirePrepaidComputeAllocation(r, nil)
}

func TestPrepaidRetirementSDKSingleSend(t *testing.T) {
	for _, action := range []string{detachPrepaidComputeAction, retirePrepaidComputeAction} {
		for _, state := range []string{"RUNNING", "STOPPED"} {
			t.Run(action+"/"+state, func(t *testing.T) {
				r, client, sdk := newPrepaidRetirementSDK(t, action)
				sdk.instance.InstanceState = common.StringPtr(state)
				out := invokePrepaidRetirement(client, r)
				mutation, expected := "TerminateInstances", map[string]any{"InstanceIds": []any{r.Allocation.InstanceId}}
				if action == detachPrepaidComputeAction {
					mutation, expected = "DeleteClusterMachines", map[string]any{"ClusterId": r.Pool.ClusterId, "MachineNames": []any{r.Allocation.MachineName}, "InstanceDeleteMode": "retain", "EnableScaleDown": true}
				}
				if !out.Ok || out.Status != "accepted" || out.InstanceId != r.Allocation.InstanceId || out.ProviderRequestId != "req-"+mutation || out.MutationCount != 1 || out.Retryable {
					t.Fatalf("response=%+v", out)
				}
				last := sdk.calls[len(sdk.calls)-1]
				if last.action != mutation || !reflect.DeepEqual(last.body, expected) || sdk.count(mutation) != 1 || sdk.count("DeleteClusterMachines")+sdk.count("TerminateInstances") != 1 {
					t.Fatalf("wrong SDK sends: %+v", sdk.calls)
				}
				if sdk.count("DescribeInstances") != 2 || sdk.count("DescribeClusterMachines") != 2 {
					t.Fatalf("fresh Machine and independent CVM identity/owner reads required: %+v", sdk.calls)
				}
			})
		}
	}
}

func TestPrepaidRetirementSDKLostOrMalformedReplyNeverResends(t *testing.T) {
	for _, action := range []string{detachPrepaidComputeAction, retirePrepaidComputeAction} {
		for _, reply := range []string{"lost", "missing id", "blank id", "unsafe id", "missing envelope", "sdk error"} {
			t.Run(action+"/"+reply, func(t *testing.T) {
				r, client, sdk := newPrepaidRetirementSDK(t, action)
				sdk.mutationReply = reply
				out := invokePrepaidRetirement(client, r)
				if out.Ok || out.Retryable || out.MutationCount != 1 || out.InstanceId != r.Allocation.InstanceId || out.ProviderRequestId != "" || out.Message != "" || !strings.HasSuffix(out.ErrorCode, "_dispatch_unconfirmed") || sdk.count("DeleteClusterMachines")+sdk.count("TerminateInstances") != 1 {
					t.Fatalf("response=%+v calls=%+v", out, sdk.calls)
				}
			})
		}
	}
}

func TestPrepaidRetirementSDKRejectsIdentityBillingAndState(t *testing.T) {
	checks := []struct {
		name   string
		change func(*Request, *prepaidRetirementSDK)
	}{
		{"request region", func(r *Request, _ *prepaidRetirementSDK) { r.Region = "ap-shanghai" }},
		{"request cluster", func(r *Request, _ *prepaidRetirementSDK) { r.Pool.ClusterId = "cls-other" }},
		{"request machine type", func(r *Request, _ *prepaidRetirementSDK) { r.Allocation.MachineType = "Native" }},
		{"request zone absent", func(r *Request, _ *prepaidRetirementSDK) { r.Zone = "" }},
		{"request private IP absent", func(r *Request, _ *prepaidRetirementSDK) { r.Allocation.PrivateIp = "" }},
		{"request node name absent", func(r *Request, _ *prepaidRetirementSDK) { r.Allocation.NodeName = "" }},
		{"request resource tag", func(r *Request, _ *prepaidRetirementSDK) { r.Tags["opl_resource_id"] = "other" }},
		{"request account tag", func(r *Request, _ *prepaidRetirementSDK) { r.Tags["opl_account_id"] = "other" }},
		{"request workspace absent", func(r *Request, _ *prepaidRetirementSDK) { delete(r.Tags, "opl_workspace_id") }},
		{"request original operation absent", func(r *Request, _ *prepaidRetirementSDK) { delete(r.Tags, "opl_operation_id") }},
		{"request tag whitespace", func(r *Request, _ *prepaidRetirementSDK) { r.Tags["opl_operation_id"] = " original-create" }},
		{"CVM id", func(_ *Request, s *prepaidRetirementSDK) { s.instance.InstanceId = common.StringPtr("ins-other") }},
		{"CVM resource name", func(_ *Request, s *prepaidRetirementSDK) { s.instance.InstanceName = common.StringPtr("other") }},
		{"CVM zone", func(_ *Request, s *prepaidRetirementSDK) {
			s.instance.Placement.Zone = common.StringPtr("ap-guangzhou-4")
		}},
		{"CVM placement missing", func(_ *Request, s *prepaidRetirementSDK) { s.instance.Placement = nil }},
		{"CVM instance type", func(_ *Request, s *prepaidRetirementSDK) { s.instance.InstanceType = common.StringPtr("SA5.LARGE8") }},
		{"CVM private IP", func(_ *Request, s *prepaidRetirementSDK) {
			s.instance.PrivateIpAddresses = []*string{common.StringPtr("10.0.0.12")}
		}},
		{"CVM charge", func(_ *Request, s *prepaidRetirementSDK) {
			s.instance.InstanceChargeType = common.StringPtr("POSTPAID_BY_HOUR")
		}},
		{"CVM charge missing", func(_ *Request, s *prepaidRetirementSDK) { s.instance.InstanceChargeType = nil }},
		{"CVM automatic renewal", func(_ *Request, s *prepaidRetirementSDK) {
			s.instance.RenewFlag = common.StringPtr("NOTIFY_AND_AUTO_RENEW")
		}},
		{"CVM renewal missing", func(_ *Request, s *prepaidRetirementSDK) { s.instance.RenewFlag = nil }},
		{"CVM isolation missing", func(_ *Request, s *prepaidRetirementSDK) { s.instance.IsolatedSource = nil }},
		{"CVM expired isolation", func(_ *Request, s *prepaidRetirementSDK) { s.instance.IsolatedSource = common.StringPtr("EXPIRE") }},
		{"CVM pending state", func(_ *Request, s *prepaidRetirementSDK) { s.instance.InstanceState = common.StringPtr("TERMINATING") }},
		{"CVM automatic recycle", func(_ *Request, s *prepaidRetirementSDK) {
			s.instance.InstanceState, s.instance.IsolatedSource = common.StringPtr("SHUTDOWN"), common.StringPtr("ARREAR")
		}},
		{"CVM duplicate tag", func(_ *Request, s *prepaidRetirementSDK) {
			s.instance.Tags = append(s.instance.Tags, s.instance.Tags[0])
		}},
		{"Machine outside original pool", func(_ *Request, s *prepaidRetirementSDK) { s.machinePresent, s.outsidePool = true, true }},
	}
	for _, key := range cbsOwnershipTagKeys {
		checks = append(checks, struct {
			name   string
			change func(*Request, *prepaidRetirementSDK)
		}{"CVM ownership " + key, func(_ *Request, s *prepaidRetirementSDK) {
			for _, tag := range s.instance.Tags {
				if stringValue(tag.Key) == key {
					tag.Value = common.StringPtr("foreign")
				}
			}
		}})
	}
	for _, action := range []string{detachPrepaidComputeAction, retirePrepaidComputeAction} {
		for _, check := range checks {
			t.Run(action+"/"+check.name, func(t *testing.T) {
				r, client, sdk := newPrepaidRetirementSDK(t, action)
				check.change(&r, sdk)
				assertPrepaidRetirementRefused(t, invokePrepaidRetirement(client, r), sdk)
			})
		}
	}
}

func assertPrepaidRetirementRefused(t *testing.T, out Response, sdk *prepaidRetirementSDK) {
	t.Helper()
	if out.Ok || out.Retryable || out.MutationCount != 0 || out.ErrorCode == "" || sdk.count("DeleteClusterMachines")+sdk.count("TerminateInstances") != 0 {
		t.Fatalf("response=%+v SDK sends=%+v", out, sdk.calls)
	}
}

func TestPrepaidDetachSDKRejectsPoolAndMachineDrift(t *testing.T) {
	for _, check := range []struct {
		name   string
		change func(*prepaidRetirementSDK)
	}{
		{"absent Machine", func(s *prepaidRetirementSDK) { s.machinePresent = false }},
		{"absent CVM", func(s *prepaidRetirementSDK) { s.instance = nil }},
		{"wrong pool", func(s *prepaidRetirementSDK) { s.pool.NodePoolId = common.StringPtr("np-other") }},
		{"non Native pool", func(s *prepaidRetirementSDK) { s.pool.Type = common.StringPtr("Regular") }},
		{"non CVM pool", func(s *prepaidRetirementSDK) { s.pool.Native.MachineType = common.StringPtr("CXM") }},
		{"autoscaling enabled", func(s *prepaidRetirementSDK) { s.pool.Native.EnableAutoscaling = common.BoolPtr(true) }},
		{"auto repair enabled", func(s *prepaidRetirementSDK) { s.pool.Native.AutoRepair = common.BoolPtr(true) }},
		{"Machine private IP", func(s *prepaidRetirementSDK) { s.machine.LanIP = common.StringPtr("10.0.0.12") }},
		{"recycled CVM", func(s *prepaidRetirementSDK) {
			s.instance.InstanceState, s.instance.IsolatedSource = common.StringPtr("SHUTDOWN"), common.StringPtr("MANMADE")
		}},
	} {
		t.Run(check.name, func(t *testing.T) {
			r, client, sdk := newPrepaidRetirementSDK(t, detachPrepaidComputeAction)
			check.change(sdk)
			assertPrepaidRetirementRefused(t, invokePrepaidRetirement(client, r), sdk)
		})
	}
}

func TestPrepaidRetireSDKAlreadyRecycledOrAbsentIsOnlyStageReadback(t *testing.T) {
	for _, state := range []string{"SHUTDOWN", "NOT_FOUND", "MACHINE_PRESENT"} {
		t.Run(state, func(t *testing.T) {
			r, client, sdk := newPrepaidRetirementSDK(t, retirePrepaidComputeAction)
			switch state {
			case "SHUTDOWN":
				sdk.instance.InstanceState, sdk.instance.IsolatedSource = common.StringPtr("SHUTDOWN"), common.StringPtr("MANMADE")
			case "NOT_FOUND":
				sdk.instance = nil
			case "MACHINE_PRESENT":
				sdk.machinePresent = true
			}
			out := invokePrepaidRetirement(client, r)
			if state == "MACHINE_PRESENT" {
				assertPrepaidRetirementRefused(t, out, sdk)
				return
			}
			if !out.Ok || out.MutationCount != 0 || out.CVMStatus != state || out.TKEStatus != "NOT_FOUND" || out.MachinePresent == nil || *out.MachinePresent || out.Status == "external_deleted" || out.Status == "accepted" || sdk.count("TerminateInstances") != 0 || sdk.count("DescribeInstances") != map[string]int{"SHUTDOWN": 2, "NOT_FOUND": 1}[state] {
				t.Fatalf("stage readback became deletion completion: %+v calls=%+v", out, sdk.calls)
			}
		})
	}
}

func TestPrepaidRetirementSDKFreshReadBeforeEachAction(t *testing.T) {
	for _, action := range []string{detachPrepaidComputeAction, retirePrepaidComputeAction} {
		for _, drift := range []string{"CVM ownership", "Machine presence", "pool self provisioning"} {
			if action == retirePrepaidComputeAction && drift == "pool self provisioning" {
				continue
			}
			t.Run(action+"/"+drift, func(t *testing.T) {
				r, client, sdk := newPrepaidRetirementSDK(t, action)
				if before := client.ReadComputeDestroyStatus(r, nil); !before.Ok {
					t.Fatalf("initial read: %+v", before)
				}
				switch drift {
				case "CVM ownership":
					sdk.instance.Tags[0].Value = common.StringPtr("foreign")
				case "Machine presence":
					sdk.machinePresent = !sdk.machinePresent
				case "pool self provisioning":
					sdk.pool.Native.AutoRepair = common.BoolPtr(true)
				}
				assertPrepaidRetirementRefused(t, invokePrepaidRetirement(client, r), sdk)
			})
		}
	}
	// The same SDK client cannot reuse detach's CVM ownership snapshot for retire.
	r, client, sdk := newPrepaidRetirementSDK(t, detachPrepaidComputeAction)
	if out := client.DetachPrepaidComputeAllocation(r, nil); !out.Ok {
		t.Fatalf("detach: %+v", out)
	}
	r.Action = retirePrepaidComputeAction
	sdk.instance.RenewFlag = common.StringPtr("NOTIFY_AND_AUTO_RENEW")
	out := client.RetirePrepaidComputeAllocation(r, nil)
	if out.Ok || out.MutationCount != 0 || sdk.count("TerminateInstances") != 0 || sdk.count("DescribeInstances") != 4 {
		t.Fatalf("retire reused detach evidence: %+v calls=%+v", out, sdk.calls)
	}
}

func TestPrepaidDetachSDKUsesExistingSelfProvisioningPredicate(t *testing.T) {
	for _, field := range []string{"autoscaling", "repair", "both"} {
		t.Run(field, func(t *testing.T) {
			r, client, sdk := newPrepaidRetirementSDK(t, detachPrepaidComputeAction)
			if field == "autoscaling" || field == "both" {
				sdk.pool.Native.EnableAutoscaling = nil
			}
			if field == "repair" || field == "both" {
				sdk.pool.Native.AutoRepair = nil
			}
			// Pool creation fields and TKE instance expansion are not new gates.
			sdk.pool.Native.InstanceChargePrepaid = nil
			sdk.pool.Native.InstanceTypes = nil
			sdk.pool.Labels = nil
			if out := invokePrepaidRetirement(client, r); !out.Ok || out.MutationCount != 1 || sdk.count("DescribeClusterInstances") != 0 {
				t.Fatalf("unrelated admission gate: %+v calls=%+v", out, sdk.calls)
			}
		})
	}
}

func TestPrepaidRetirementSDKRechecksOwnerAfterIdentityRead(t *testing.T) {
	for _, action := range []string{detachPrepaidComputeAction, retirePrepaidComputeAction} {
		t.Run(action, func(t *testing.T) {
			r, client, sdk := newPrepaidRetirementSDK(t, action)
			sdk.before = func(method string, count int) {
				if method == "DescribeInstances" && count == 2 {
					sdk.instance.Tags[0].Value = common.StringPtr("foreign")
				}
			}
			assertPrepaidRetirementRefused(t, invokePrepaidRetirement(client, r), sdk)
		})
	}
}

func TestPrepaidRetirementSDKRejectsUnknownLiveReads(t *testing.T) {
	for _, action := range []string{detachPrepaidComputeAction, retirePrepaidComputeAction} {
		methods := []string{"DescribeClusterMachines", "DescribeInstances"}
		if action == detachPrepaidComputeAction {
			methods = append(methods, "DescribeNodePools")
		}
		for _, method := range methods {
			problems := []string{"missing total", "error", "nil item"}
			if method != "DescribeClusterMachines" {
				problems = append(problems, "missing id", "unsafe id")
			}
			for _, problem := range problems {
				t.Run(fmt.Sprintf("%s/%s/%s", action, method, problem), func(t *testing.T) {
					r, client, sdk := newPrepaidRetirementSDK(t, action)
					sdk.editReply = func(read string, params map[string]any) {
						if read != method {
							return
						}
						switch problem {
						case "missing id":
							delete(params, "RequestId")
						case "unsafe id":
							params["RequestId"] = "req\ninvalid"
						case "missing total":
							delete(params, "TotalCount")
						case "error":
							params["Error"] = map[string]string{"Code": "InternalError", "Message": "private provider diagnostic"}
						case "nil item":
							field := "InstanceSet"
							if method == "DescribeNodePools" {
								field = "NodePools"
							} else if method == "DescribeClusterMachines" {
								field = "Machines"
							}
							params[field], params["TotalCount"] = []any{nil}, 1
						}
					}
					assertPrepaidRetirementRefused(t, invokePrepaidRetirement(client, r), sdk)
				})
			}
		}
	}
}

func TestPrepaidRetirementDispatchLiveProtectionOptionalInterfaceAndDryRun(t *testing.T) {
	for _, action := range []string{detachPrepaidComputeAction, retirePrepaidComputeAction} {
		t.Run(action, func(t *testing.T) {
			r, client, sdk := newPrepaidRetirementSDK(t, action)
			env := protectedResourceEnv()
			delete(env, "RUN_TENCENT_CREATE_RELEASE_EXECUTION")
			if out := handleWithClient(r, env, client); out.ErrorCode != "live_mutation_flag_required" || len(sdk.calls) != 0 {
				t.Fatalf("missing live gate: %+v", out)
			}
			env["RUN_TENCENT_CREATE_RELEASE_EXECUTION"] = "1"
			original := r.Allocation.InstanceId
			r.Allocation.InstanceId = env["OPL_SYSTEM_COMPUTE_CVM_ID"]
			if out := handleWithClient(r, env, client); out.ErrorCode != "protected_system_resource" || len(sdk.calls) != 0 {
				t.Fatalf("missing protection gate: %+v", out)
			}
			r.Allocation.InstanceId = original
			if out := handleWithClient(r, env, unimplementedTencentClient{}); out.Ok || !strings.HasSuffix(out.ErrorCode, "_unavailable") || out.MutationCount != 0 {
				t.Fatalf("missing optional interface: %+v", out)
			}
			r.DryRun = true
			if out := handleWithClient(r, env, client); out.Ok || out.MutationCount != 0 || len(sdk.calls) != 0 {
				t.Fatalf("dry run sent an SDK request: %+v", out)
			}
			r.DryRun = false
			if out := handleWithClient(r, env, client); !out.Ok || out.MutationCount != 1 {
				t.Fatalf("optional interface dispatch: %+v", out)
			}
		})
	}
}

func TestPrepaidGenericDestroySDKRequiresSeparateRetirementStages(t *testing.T) {
	for _, tc := range []struct {
		name   string
		charge *string
		code   string
	}{
		{"prepaid", common.StringPtr("PREPAID"), "compute_prepaid_retirement_required"},
		{"missing", nil, "compute_destroy_charge_type_unverified"},
		{"empty", common.StringPtr(""), "compute_destroy_charge_type_unverified"},
		{"unknown", common.StringPtr("UNKNOWN"), "compute_destroy_charge_type_unverified"},
		{"noncanonical", common.StringPtr(" POSTPAID_BY_HOUR "), "compute_destroy_charge_type_unverified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, client, sdk := newPrepaidRetirementSDK(t, detachPrepaidComputeAction)
			r.Action = "destroy_compute_allocation"
			sdk.pool.Native.InstanceChargeType = tc.charge
			sdk.pool.Native.EnableAutoscaling, sdk.pool.Native.AutoRepair = common.BoolPtr(true), common.BoolPtr(true)
			out := handleWithClient(r, protectedResourceEnv(), client)
			if out.Ok || out.ErrorCode != tc.code || out.MutationCount != 0 || out.Retryable || out.InstanceId != r.Allocation.InstanceId || out.ProviderRequestId != "req-DescribeNodePools" {
				t.Fatalf("generic destroy must require explicit retirement: %+v", out)
			}
			if len(sdk.calls) != 1 || sdk.calls[0].action != "DescribeNodePools" || !sdk.machinePresent || stringValue(sdk.instance.InstanceState) != "RUNNING" ||
				!boolValue(sdk.pool.Native.EnableAutoscaling) || !boolValue(sdk.pool.Native.AutoRepair) {
				t.Fatalf("refusal must precede every mutation or follow-up read: calls=%+v pool=%+v", sdk.calls, sdk.pool)
			}
		})
	}
}

func TestPrepaidGenericDestroySDKRefusalLeavesIndependentStagesAvailable(t *testing.T) {
	r, client, sdk := newPrepaidRetirementSDK(t, detachPrepaidComputeAction)
	r.Action = "destroy_compute_allocation"
	out := handleWithClient(r, protectedResourceEnv(), client)
	if out.Ok || out.ErrorCode != "compute_prepaid_retirement_required" || out.MutationCount != 0 || len(sdk.calls) != 1 {
		t.Fatalf("generic destroy must not send a prepaid termination: %+v calls=%+v", out, sdk.calls)
	}
	r.Action = detachPrepaidComputeAction
	if out = handleWithClient(r, protectedResourceEnv(), client); !out.Ok || out.MutationCount != 1 || sdk.count("DeleteClusterMachines") != 1 || sdk.count("TerminateInstances") != 0 {
		t.Fatalf("separate detach: %+v calls=%+v", out, sdk.calls)
	}
	r.Action = retirePrepaidComputeAction
	if out = handleWithClient(r, protectedResourceEnv(), client); !out.Ok || out.MutationCount != 1 || sdk.count("DeleteClusterMachines") != 1 || sdk.count("TerminateInstances") != 1 {
		t.Fatalf("separate retirement: %+v calls=%+v", out, sdk.calls)
	}
	if sdk.count("ModifyNodePool") != 0 {
		t.Fatal("prepaid retirement changed a shared pool")
	}
}

func TestPrepaidGenericDestroyGuardPreservesExplicitPostpaidAndLegacyMachineTypes(t *testing.T) {
	for _, tc := range []struct {
		machineType string
		charge      string
	}{
		{"NativeCVM", "POSTPAID_BY_HOUR"},
		{"Native", "PREPAID"},
		{"Native", "UNKNOWN"},
		{"CXM", "PREPAID"},
		{"CXM", "UNKNOWN"},
	} {
		t.Run(tc.machineType+"/"+tc.charge, func(t *testing.T) {
			r := computeDestroyStatusRequest(tc.machineType)
			api := &fakeNativeTkeAPI{nodePoolId: r.Pool.NodePoolId, replicas: 1, machineType: tc.machineType, instanceChargeType: tc.charge, clusterInstanceID: r.Allocation.InstanceId}
			client := newFakeTencentSDKClient(api)
			client.nativeCvmClient.(*fakeNativeCvmAPI).instanceChargeType = tc.charge
			out := client.DestroyComputeAllocation(r, map[string]string{"TENCENT_TKE_NODE_DELETE_ATTEMPTS": "1", "TENCENT_TKE_NODE_DELETE_DELAY_MS": "0"})
			if !out.Ok || out.MutationCount != 1 || countStrings(api.calls, "DeleteClusterMachines") != 1 || api.deleteMachinesRequest == nil || stringValue(api.deleteMachinesRequest.InstanceDeleteMode) != "terminate" {
				t.Fatalf("existing explicit postpaid/legacy behavior changed: %+v calls=%+v", out, api.calls)
			}
			if tc.machineType != "NativeCVM" && len(client.nativeCvmClient.(*fakeNativeCvmAPI).describeInstancesRequest) != 0 {
				t.Fatal("legacy Machine destroy must remain TKE-only")
			}
		})
	}
}
