package fabric

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type closeoutTestProvider struct {
	*workspaceLaunchDeleteProjectionProvider
	runtimeReady   atomic.Bool
	runtimePresent atomic.Bool
	unknown        atomic.Bool
	runtimeDeletes atomic.Int32
}

func (p *closeoutTestProvider) ObserveWorkspaceRuntimeDelete(_ context.Context, id string) (WorkspaceRuntimeDeleteObservation, error) {
	result := WorkspaceRuntimeDeleteObservation{SchemaVersion: 1, WorkspaceID: id, State: "absent"}
	if p.runtimePresent.Load() {
		result.State = "present"
		result.Residuals = []WorkspaceRuntimeDeleteResidual{{Kind: "Deployment", Name: "original-runtime"}}
	}
	return result, nil
}
func (p *closeoutTestProvider) DestroyWorkspaceRuntime(_ context.Context, id string) (WorkspaceRuntime, error) {
	p.runtimeDeletes.Add(1)
	p.runtimePresent.Store(false)
	return WorkspaceRuntime{WorkspaceID: id, Status: "destroyed"}, nil
}
func (p *closeoutTestProvider) ReadWorkspaceLaunchStage(ctx context.Context, request WorkspaceLaunchProviderRequest) (WorkspaceLaunchProviderResult, error) {
	if request.Input.Binding.Stage == "runtime" && !p.runtimeReady.Load() {
		return WorkspaceLaunchProviderResult{}, ErrWorkspaceLaunchPending
	}
	return p.workspaceLaunchDeleteProjectionProvider.ReadWorkspaceLaunchStage(ctx, request)
}
func (p *closeoutTestProvider) DestroyWorkspaceLaunchGatewaySecret(context.Context, WorkspaceLaunchProviderRequest) error {
	p.runtimePresent.Store(false)
	return nil
}

func (p *closeoutTestProvider) ReadWorkspaceLaunchCloseoutResource(_ context.Context, request WorkspaceLaunchProviderRequest) (workspaceLaunchCloseoutResourceReadback, error) {
	if p.unknown.Load() {
		return workspaceLaunchCloseoutResourceReadback{}, context.DeadlineExceeded
	}
	if request.Input.Binding.Stage == "secret" {
		state := "absent"
		if p.runtimePresent.Load() {
			state = "present"
		}
		return workspaceLaunchCloseoutResourceReadback{State: state}, nil
	}
	state, err := decodeLocalDockerWorkspaceLaunchState(request.Current)
	result := workspaceLaunchCloseoutResourceReadback{State: "present", Compute: state.Compute, Storage: state.Storage}
	if request.Input.Binding.Stage == "storage" && p.storageDeleteCalls.Load() > 0 || request.Input.Binding.Stage == "ensure_compute_allocation" && p.computeDeleteCalls.Load() > 0 {
		result.State = "absent"
	}
	return result, err
}
func closeoutFixture(t *testing.T) (*Service, *MemoryOperationStore, *closeoutTestProvider, WorkspaceLaunchCloseoutInput) {
	t.Helper()
	_, store, original, _ := workspaceLaunchDeleteProjectionFixture(t)
	provider := &closeoutTestProvider{workspaceLaunchDeleteProjectionProvider: original}
	service := NewServiceWithOperationStore(provider, store)
	ops, _ := store.List(context.Background())
	for _, op := range ops {
		admission, ok := decodeWorkspaceLaunchPreflight(op)
		if !ok {
			continue
		}
		return service, store, provider, WorkspaceLaunchCloseoutInput{SchemaVersion: 1, LaunchOperationID: admission.Input.LaunchOperationID, AccountID: admission.Input.AccountID, WorkspaceID: admission.Input.WorkspaceID, ProviderProfileRef: admission.ProviderProfileRef, ProviderBindingRef: admission.ProviderBindingRef, SpecDigest: admission.SpecDigest, IdempotencyKey: "closeout-original"}
	}
	t.Fatal("preflight missing")
	return nil, nil, nil, WorkspaceLaunchCloseoutInput{}
}
func closeoutUntilAbsent(t *testing.T, service *Service, input WorkspaceLaunchCloseoutInput) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var result WorkspaceLaunchCloseoutResult
	for time.Now().Before(deadline) {
		var err error
		result, err = service.CloseoutWorkspaceLaunch(context.Background(), input)
		if err != nil || result.State == "blocked" {
			t.Fatalf("result=%#v err=%v", result, err)
		}
		if result.State == "absent" && result.Frozen {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("closeout did not converge: %#v", result)
}
func TestWorkspaceLaunchCloseoutDeletesPartialLaunchAndRejectsLateEnsureAfterRestart(t *testing.T) {
	service, store, provider, input := closeoutFixture(t)
	before, _ := store.List(context.Background())
	preview, err := service.ReadWorkspaceLaunchCloseout(context.Background(), input)
	after, _ := store.List(context.Background())
	if err != nil || preview.State != "eligible" || preview.Frozen || !reflect.DeepEqual(before, after) {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	frozen, err := service.FreezeWorkspaceLaunch(context.Background(), input)
	if err != nil || !frozen.Frozen || provider.storageDeleteCalls.Load()+provider.computeDeleteCalls.Load() != 0 {
		t.Fatalf("freeze=%#v err=%v", frozen, err)
	}
	for range 3 {
		closeoutUntilAbsent(t, NewServiceWithOperationStore(provider, store), input)
	}
	if provider.storageDeleteCalls.Load() != 1 || provider.computeDeleteCalls.Load() != 1 {
		t.Fatalf("delete calls storage=%d compute=%d", provider.storageDeleteCalls.Load(), provider.computeDeleteCalls.Load())
	}
	_, err = service.EnsureWorkspaceLaunchStage(context.Background(), WorkspaceLaunchStageInput{Binding: WorkspaceLaunchStageBinding{LaunchOperationID: input.LaunchOperationID}})
	if !errors.Is(err, ErrWorkspaceLaunchFrozen) {
		t.Fatalf("late ensure err=%v", err)
	}
	changed := input
	changed.IdempotencyKey = "another-closeout"
	if _, err := service.CloseoutWorkspaceLaunch(context.Background(), changed); !errors.Is(err, ErrLaunchStageBindingConflict) {
		t.Fatalf("changed identity err=%v", err)
	}
}
func TestWorkspaceLaunchCloseoutUnknownCannotDeleteOrPretendAbsent(t *testing.T) {
	service, store, provider, input := closeoutFixture(t)
	provider.unknown.Store(true)
	result, err := service.CloseoutWorkspaceLaunch(context.Background(), input)
	if err != nil || !result.Frozen || result.State != "pending" || provider.storageDeleteCalls.Load()+provider.computeDeleteCalls.Load() != 0 {
		t.Fatalf("unknown=%#v err=%v", result, err)
	}
	provider.unknown.Store(false)
	closeoutUntilAbsent(t, NewServiceWithOperationStore(provider, store), input)
}
func closeoutRuntimeStages(t *testing.T, service *Service, store OperationStore, input WorkspaceLaunchCloseoutInput, status string) {
	t.Helper()
	ctx := context.Background()
	x, err := service.readWorkspaceLaunchCloseout(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	admission, _ := service.launchStages.workspaceLaunchPreflight(ctx, input.ProviderBindingRef)
	prior, _ := decodeWorkspaceLaunchStageRecord(x.stages["attachment"])
	for _, stage := range []string{"secret", "runtime"} {
		action, _ := workspaceLaunchStageAction(stage)
		binding := WorkspaceLaunchStageBinding{SchemaVersion: 1, LaunchOperationID: input.LaunchOperationID, AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, Stage: stage, Action: action, FabricOperationID: input.LaunchOperationID + ":" + stage, IdempotencyKey: input.LaunchOperationID + ":" + stage}
		stageInput := WorkspaceLaunchStageInput{Binding: binding, ProviderProfileRef: input.ProviderProfileRef, ProviderBindingRef: input.ProviderBindingRef, SpecDigest: input.SpecDigest, PackageID: admission.Input.PackageID, SizeGB: admission.Input.SizeGB, WorkspaceImageDigest: admission.Input.WorkspaceImageDigest, Resources: prior.Resources}
		stageInput.Binding.RequestHash = workspaceLaunchStageRequestHash(stageInput, admission.Input.RequestHash)
		op, record, err := newWorkspaceLaunchStageOperation(stageInput, input.ProviderProfileRef, service.now)
		if err != nil {
			t.Fatal(err)
		}
		op.Status = status
		if stage == "secret" {
			op.Status = "succeeded"
			record.Resources.SecretBindingRef = binding.FabricOperationID
		}
		setWorkspaceLaunchStageRecord(&op, record)
		if err := store.Append(ctx, op); err != nil {
			t.Fatal(err)
		}
		prior = record
	}
}
func TestWorkspaceLaunchCloseoutReadyBeforeFreezeIsRejectedAndLateReadyIsCleaned(t *testing.T) {
	for _, state := range []string{"live_ready", "persisted_success", "late_ready"} {
		t.Run(state, func(t *testing.T) {
			service, store, provider, input := closeoutFixture(t)
			status := "started"
			if state == "persisted_success" {
				status = "succeeded"
			}
			closeoutRuntimeStages(t, service, store, input, status)
			provider.runtimePresent.Store(true)
			provider.runtimeReady.Store(state == "live_ready")
			result, err := service.FreezeWorkspaceLaunch(context.Background(), input)
			if state != "late_ready" {
				if err != nil || result.Frozen || result.State != "blocked" || result.Reason != "runtime_already_deliverable" {
					t.Fatalf("result=%#v err=%v", result, err)
				}
				if provider.runtimeDeletes.Load() != 0 {
					t.Fatal("deleted delivered runtime")
				}
				return
			}
			if err != nil || !result.Frozen {
				t.Fatalf("freeze=%#v err=%v", result, err)
			}
			provider.runtimeReady.Store(true)
			closeoutUntilAbsent(t, NewServiceWithOperationStore(provider, store), input)
			if provider.runtimeDeletes.Load() != 1 {
				t.Fatal("late runtime was not cleaned exactly once")
			}
		})
	}
}
func TestWorkspaceLaunchCloseoutTencentOriginalUndispatchedAndUnknownPoolHead(t *testing.T) {
	for _, dispatched := range []bool{false, true} {
		t.Run(map[bool]string{false: "undispatched", true: "unknown_provider"}[dispatched], func(t *testing.T) {
			ctx := context.Background()
			service, store, provider, preflight, image, launchHash := newTencentWorkspaceLaunchService(t)
			stage := workspaceLaunchStageFixtureInput(preflight, image, launchHash, "ensure_compute_allocation", "ensure_compute_allocation", WorkspaceLaunchResources{})
			mutations := 0
			provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
				switch request.Action {
				case "prepare_compute_allocation":
					if !dispatched {
						return provisionerResponse{}, context.DeadlineExceeded
					}
					return provisionerResponse{OK: true, CurrentReplicas: 1, TargetReplicas: 2, Machines: []provisionerMachine{{MachineID: "before"}}}, nil
				case "create_compute_allocation":
					mutations++
					return provisionerResponse{OK: false, Retryable: true, Status: "provisioning"}, nil
				case "read_compute_allocation":
					return provisionerResponse{}, context.DeadlineExceeded
				default:
					t.Fatalf("unexpected IO %s", request.Action)
					return provisionerResponse{}, nil
				}
			}
			provider.kubectl = func(_ context.Context, args []string, _ []byte) ([]byte, error) {
				if args[0] != "get" {
					t.Fatalf("unexpected K8s mutation %v", args)
				}
				return []byte(`{"kind":"List","items":[]}`), nil
			}
			_, _ = service.EnsureWorkspaceLaunchStage(ctx, stage)
			input := WorkspaceLaunchCloseoutInput{SchemaVersion: 1, LaunchOperationID: stage.Binding.LaunchOperationID, AccountID: stage.Binding.AccountID, WorkspaceID: stage.Binding.WorkspaceID, ProviderProfileRef: stage.ProviderProfileRef, ProviderBindingRef: preflight.ProviderBindingRef, SpecDigest: preflight.SpecDigest, IdempotencyKey: "close-original"}
			result, err := NewServiceWithOperationStore(provider, store).CloseoutWorkspaceLaunch(ctx, input)
			if err != nil || !result.Frozen {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			head, exists, err := store.ComputePoolHead(ctx, "np-basic")
			if dispatched {
				if result.State != "pending" || !exists || head.ID != stage.Binding.FabricOperationID || err != nil || mutations != 1 {
					t.Fatalf("unknown released pool result=%#v head=%#v exists=%v err=%v", result, head, exists, err)
				}
			} else if result.State != "absent" || exists || err != nil || mutations != 0 {
				t.Fatalf("undispatched=%#v exists=%v err=%v", result, exists, err)
			}
			if _, err := service.EnsureWorkspaceLaunchStage(ctx, stage); !errors.Is(err, ErrWorkspaceLaunchFrozen) {
				t.Fatalf("frozen original replay err=%v", err)
			}
		})
	}
}
func TestWorkspaceLaunchCloseoutPostgresFreezeSurvivesServiceRestart(t *testing.T) {
	_, memory, provider, input := closeoutFixture(t)
	databaseURL := fabricTestDatabaseURL(t)
	store, err := newTestPostgresOperationStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.client.Close()
	ops, _ := memory.List(context.Background())
	for _, op := range ops {
		if err := store.Append(context.Background(), op); err != nil {
			t.Fatal(err)
		}
	}
	result, err := NewServiceWithOperationStore(provider, store).FreezeWorkspaceLaunch(context.Background(), input)
	if err != nil || !result.Frozen {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	reopened, err := newTestPostgresOperationStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.client.Close()
	closeoutUntilAbsent(t, NewServiceWithOperationStore(provider, reopened), input)
}

func TestWorkspaceLaunchCloseoutWaitsForOriginalEnsureBeforeFreezing(t *testing.T) {
	ctx := context.Background()
	service, store, provider, preflight, image, launchHash := newTencentWorkspaceLaunchService(t)
	stage := workspaceLaunchStageFixtureInput(preflight, image, launchHash, "ensure_compute_allocation", "ensure_compute_allocation", WorkspaceLaunchResources{})
	entered, release := make(chan struct{}), make(chan struct{})
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		if request.Action != "prepare_compute_allocation" {
			t.Errorf("unexpected provider action %s", request.Action)
		}
		close(entered)
		<-release
		return provisionerResponse{}, context.DeadlineExceeded
	}
	provider.kubectl = func(_ context.Context, args []string, _ []byte) ([]byte, error) {
		if args[0] != "get" {
			t.Errorf("unexpected K8s mutation %v", args)
		}
		return []byte(`{"kind":"List","items":[]}`), nil
	}
	ensureDone := make(chan error, 1)
	go func() { _, err := service.EnsureWorkspaceLaunchStage(ctx, stage); ensureDone <- err }()
	<-entered
	input := WorkspaceLaunchCloseoutInput{SchemaVersion: 1, LaunchOperationID: stage.Binding.LaunchOperationID, AccountID: stage.Binding.AccountID, WorkspaceID: stage.Binding.WorkspaceID, ProviderProfileRef: stage.ProviderProfileRef, ProviderBindingRef: stage.ProviderBindingRef, SpecDigest: stage.SpecDigest, IdempotencyKey: "freeze-original"}
	freezeDone := make(chan WorkspaceLaunchCloseoutResult, 1)
	freezeErrors := make(chan error, 1)
	go func() {
		result, err := NewServiceWithOperationStore(provider, store).FreezeWorkspaceLaunch(ctx, input)
		freezeDone <- result
		freezeErrors <- err
	}()
	select {
	case result := <-freezeDone:
		close(release)
		t.Fatalf("froze while original Ensure in flight: %#v", result)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-ensureDone; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	result := <-freezeDone
	if err := <-freezeErrors; err != nil || !result.Frozen {
		t.Fatalf("freeze=%#v err=%v", result, err)
	}
	if _, err := service.EnsureWorkspaceLaunchStage(ctx, stage); !errors.Is(err, ErrWorkspaceLaunchFrozen) {
		t.Fatalf("late ensure=%v", err)
	}
}

type closeoutDockerRunner struct {
	mu      sync.Mutex
	base    localDockerComputeRoundTripRunner
	removes int
}

func (r *closeoutDockerRunner) Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(args) > 1 && args[0] == "info" {
		return []byte(`{"NCPU":64,"MemTotal":274877906944}`), nil
	}
	if len(args) > 1 && (args[0] == "container" || args[0] == "volume") && args[1] == "ls" {
		return nil, nil
	}
	if len(args) == 3 && args[0] == "network" && args[1] == "rm" {
		r.removes++
		r.base.network = dockerNetworkInspect{}
		return nil, nil
	}
	return r.base.Run(ctx, stdin, args...)
}
func TestWorkspaceLaunchCloseoutLocalAdapterRemovesOriginalPartialResources(t *testing.T) {
	ctx := context.Background()
	runner := &closeoutDockerRunner{}
	root := localDockerStorageTestRoot(t)
	profile, err := json.Marshal(localDockerProviderProfile{SchemaVersion: 1, Packages: []localDockerPackageProfile{{ID: "basic", Name: "Basic", Available: true, Compute: ComputePlan{ID: "local-basic", Server: "2c4g", CPU: 2, MemoryGB: 4, DiskGB: 10, InstanceType: "local-2c4g"}, Storage: localDockerStoragePlan{SizeGB: 10, QuotaPolicy: "linux-project"}}}})
	if err != nil {
		t.Fatal(err)
	}
	provider := newLocalDockerProvider(LocalDockerProviderConfig{GatewaySecretRoot: localDockerSecretTestRoot(t), HostStorageRoot: root, StorageQuotaBackend: localDockerStorageTestQuota(root), ProviderProfileJSON: profile, TrustedWorkspaceImageSources: []string{"ghcr.io/gaofeng21cn/one-person-lab-app"}}, runner)
	store := NewMemoryOperationStore()
	service := NewServiceWithOperationStore(provider, store)
	image := "ghcr.io/gaofeng21cn/one-person-lab-app@sha256:" + strings.Repeat("a", 64)
	launchHash := strings.Repeat("b", 64)
	preflight, err := service.PreflightWorkspaceLaunch(ctx, WorkspaceLaunchPreflightInput{SchemaVersion: 1, LaunchOperationID: "local-closeout", AccountID: "acct-local", WorkspaceID: "ws-local", PackageID: "basic", SizeGB: 10, WorkspaceImageDigest: image, RequestHash: launchHash})
	if err != nil || !preflight.Available {
		t.Fatalf("preflight=%#v err=%v", preflight, err)
	}
	resources := WorkspaceLaunchResources{}
	for _, stage := range []string{"ensure_compute_allocation", "storage", "attachment"} {
		action, _ := workspaceLaunchStageAction(stage)
		input := WorkspaceLaunchStageInput{Binding: WorkspaceLaunchStageBinding{SchemaVersion: 1, LaunchOperationID: "local-closeout", AccountID: "acct-local", WorkspaceID: "ws-local", Stage: stage, Action: action, FabricOperationID: "local-closeout:" + stage, IdempotencyKey: "local-closeout:" + stage}, ProviderProfileRef: preflight.ProviderProfileRef, ProviderBindingRef: preflight.ProviderBindingRef, SpecDigest: preflight.SpecDigest, PackageID: "basic", SizeGB: 10, WorkspaceImageDigest: image, Resources: resources}
		input.Binding.RequestHash = workspaceLaunchStageRequestHash(input, launchHash)
		result, err := service.EnsureWorkspaceLaunchStage(ctx, input)
		if err != nil || result.State != "ready" {
			t.Fatalf("stage=%s result=%#v err=%v", stage, result, err)
		}
		resources = result.Resources
	}
	input := WorkspaceLaunchCloseoutInput{SchemaVersion: 1, LaunchOperationID: "local-closeout", AccountID: "acct-local", WorkspaceID: "ws-local", ProviderProfileRef: preflight.ProviderProfileRef, ProviderBindingRef: preflight.ProviderBindingRef, SpecDigest: preflight.SpecDigest, IdempotencyKey: "local-close-original"}
	closeoutUntilAbsent(t, NewServiceWithOperationStore(provider, store), input)
	closeoutUntilAbsent(t, NewServiceWithOperationStore(provider, store), input)
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.base.mutationCalls != 1 || runner.removes != 1 {
		t.Fatalf("compute creates=%d deletes=%d", runner.base.mutationCalls, runner.removes)
	}
}

func TestWorkspaceLaunchCloseoutTencentLateMachineIsDeletedWithoutAnotherPurchase(t *testing.T) {
	ctx := context.Background()
	service, store, provider, preflight, image, launchHash := newTencentWorkspaceLaunchService(t)
	stage := workspaceLaunchStageFixtureInput(preflight, image, launchHash, "ensure_compute_allocation", "ensure_compute_allocation", WorkspaceLaunchResources{})
	allocation := computeDestroyPhaseResource()
	allocation.ID, allocation.OperationID, allocation.AccountID, allocation.WorkspaceID = workspaceLaunchComputeID(stage.Binding), stage.Binding.FabricOperationID, stage.Binding.AccountID, stage.Binding.WorkspaceID
	allocation.CostTags = oplCostTags(allocation.AccountID, allocation.WorkspaceID, allocation.ID, allocation.OperationID)
	prepared := ComputeAllocationPreparation{PoolID: allocation.PoolID, PackageID: allocation.PackageID, NodePoolID: allocation.NodePoolID, InstanceType: allocation.InstanceType, Zone: allocation.Zone, MaxReplicas: 20, BaselineReplicas: 1, TargetReplicas: 2, BeforeMachineNames: []string{"before"}}
	var ready, deleted atomic.Bool
	var creates, deletes atomic.Int32
	provider.convergenceWait = func(context.Context, int) error { return nil }
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		switch request.Action {
		case "prepare_compute_allocation":
			return provisionerResponse{OK: true, CurrentReplicas: 1, TargetReplicas: 2, Machines: []provisionerMachine{{MachineID: "before"}}}, nil
		case "create_compute_allocation":
			creates.Add(1)
			return provisionerResponse{OK: false, Retryable: true, Status: "provisioning"}, nil
		case "read_compute_allocation":
			if !ready.Load() {
				return provisionerResponse{OK: false, Retryable: true, Status: "provisioning"}, nil
			}
			response := tencentComputeAllocationResponse(allocation, "read-original")
			for key, value := range allocation.ProviderData {
				response.ProviderData[key] = value
			}
			return response, nil
		case "compute_claim_truth":
			return tencentTargetOwnedProofResponse(allocation, prepared), nil
		case "read_compute_destroy_status":
			present := !deleted.Load()
			response := provisionerResponse{OK: true, Status: "present", InstanceID: allocation.InstanceID, MachinePresent: &present, ProviderRequestID: "read-original-machine", CVMStatus: "RUNNING", TKEStatus: "RUNNING", ProviderData: map[string]string{
				"clusterId": allocation.ProviderData["clusterId"], "region": allocation.ProviderData["region"], "nodePoolId": allocation.NodePoolID, "machineName": allocation.MachineName, "nodeName": allocation.NodeName, "privateIp": allocation.PrivateIP, "machineType": "NativeCVM", "cvmApplicable": "true",
			}}
			if !present {
				response.Status = "external_deleted"
				response.CVMStatus = "NOT_FOUND"
				response.TKEStatus = "NOT_FOUND"
				response.ProviderData["machinePresent"] = "false"
				response.ProviderData["tkeStatus"] = "NOT_FOUND"
				response.ProviderData["cvmStatus"] = "NOT_FOUND"
				response.ProviderData["syncResult"] = "missing"
				response.ProviderData["describeClusterMachinesReq"] = "read-original-absent"
				response.ProviderData["describeCvmRequestId"] = "read-cvm-absent"
			}
			return response, nil
		case "destroy_compute_allocation":
			deletes.Add(1)
			deleted.Store(true)
			present := false
			return provisionerResponse{OK: true, Status: "external_deleted", NodePoolID: allocation.NodePoolID, InstanceID: allocation.InstanceID, NodeName: allocation.NodeName, MachinePresent: &present, CVMStatus: "NOT_FOUND", TKEStatus: "NOT_FOUND", ProviderRequestID: "delete-original", MutationCount: 1, ProviderData: map[string]string{
				"machineType": "NativeCVM", "cvmApplicable": "true", "machinePresent": "false", "tkeStatus": "NOT_FOUND", "cvmStatus": "NOT_FOUND", "deleteMethod": "DeleteClusterMachines", "scaleDown": "true", "deleteMode": "terminate", "describeNodePoolRequestId": "read-pool", "verifyMachineDeletedReqId": "read-machine-absent", "describeCvmRequestId": "read-cvm-absent",
			}}, nil
		default:
			t.Errorf("unexpected provisioner IO %s", request.Action)
			return provisionerResponse{}, errors.New("unexpected provider IO")
		}
	}
	provider.kubectl = func(_ context.Context, args []string, _ []byte) ([]byte, error) {
		if args[0] == "get" && (args[1] == "node/"+allocation.NodeName || args[1] == computeClaimMachineResource(allocation)) {
			ownership, err := workspaceLaunchComputeOwnership(allocation)
			if err != nil {
				return nil, err
			}
			return tencentOwnershipKubernetesReadback(args, allocation, ownership, true), nil
		}
		if args[0] == "get" {
			return []byte(`{"kind":"List","items":[]}`), nil
		}
		if args[0] == "delete" {
			return nil, nil
		}
		t.Errorf("unexpected K8s IO %v", args)
		return nil, errors.New("unexpected K8s IO")
	}
	result, err := service.EnsureWorkspaceLaunchStage(ctx, stage)
	if err != nil || result.State != "pending" {
		t.Fatalf("launch=%#v err=%v", result, err)
	}
	input := WorkspaceLaunchCloseoutInput{SchemaVersion: 1, LaunchOperationID: stage.Binding.LaunchOperationID, AccountID: stage.Binding.AccountID, WorkspaceID: stage.Binding.WorkspaceID, ProviderProfileRef: stage.ProviderProfileRef, ProviderBindingRef: stage.ProviderBindingRef, SpecDigest: stage.SpecDigest, IdempotencyKey: "close-late-machine"}
	frozen, err := service.FreezeWorkspaceLaunch(ctx, input)
	if err != nil || !frozen.Frozen || frozen.State != "pending" {
		t.Fatalf("freeze=%#v err=%v", frozen, err)
	}
	ready.Store(true)
	snapshot, snapshotErr := service.readWorkspaceLaunchCloseout(ctx, input)
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	rb, readErr := provider.ReadWorkspaceLaunchCloseoutResource(service.launchStages.providerOperationContext(ctx, snapshot.stages["ensure_compute_allocation"], true), snapshot.requests["ensure_compute_allocation"])
	if readErr != nil {
		t.Fatalf("late compute read=%#v err=%v", rb, readErr)
	}
	closeoutUntilAbsent(t, NewServiceWithOperationStore(provider, store), input)
	if creates.Load() != 1 || deletes.Load() != 1 {
		t.Fatalf("purchases=%d deletes=%d", creates.Load(), deletes.Load())
	}
	if head, exists, err := store.ComputePoolHead(ctx, allocation.NodePoolID); err != nil || exists {
		t.Fatalf("pool=%#v exists=%v err=%v", head, exists, err)
	}
}

func TestWorkspaceLaunchCloseoutTencentCBSResponseLossUsesOriginalDiscoveryAndDeletesWithoutStaticBinding(t *testing.T) {
	ctx := context.Background()
	fixture := newTencentWorkspaceLaunchStorageResponseLossFixture(t)
	store := reopenTencentWorkspaceLaunchOperations(t, fixture.operations, nil)
	provider := NewTencentProvider()
	service := NewServiceWithOperationStore(provider, store)
	parent, err := store.Get(ctx, fixture.input.Binding.FabricOperationID)
	if err != nil {
		t.Fatal(err)
	}
	record, _ := decodeWorkspaceLaunchStageRecord(parent)
	request, err := service.launchStages.WorkspaceLaunchProviderRequest(ctx, fixture.input, record)
	if err != nil {
		t.Fatal(err)
	}
	var deleted bool
	deletes := 0
	provider.provision = func(_ context.Context, input provisionerRequest) (provisionerResponse, error) {
		switch input.Action {
		case "discover_storage_volume":
			if deleted {
				t.Fatal("forgot original discovered disk after delete/restart")
			}
			return provisionerResponse{OK: true, StorageState: "storage_existing_exact", StorageVolumeID: "disk-response-loss", Status: "ready", CBSStatus: "UNATTACHED", ProviderRequestID: "discover-original", ProviderData: map[string]string{"region": "ap-guangzhou", "zone": "ap-guangzhou-3", "diskType": "CLOUD_BSSD", "sizeGb": "10", "renewFlag": "NOTIFY_AND_MANUAL_RENEW", "deadline": "2026-10-09T00:00:00Z"}}, nil
		case "sync_storage_volume":
			response := provisionerResponse{OK: true, StorageVolumeID: "disk-response-loss", Status: "ready", CBSStatus: "UNATTACHED", ProviderRequestID: "read-original", ProviderData: map[string]string{"region": "ap-guangzhou", "zone": "ap-guangzhou-3", "diskType": "CLOUD_BSSD", "sizeGb": "10", "renewFlag": "NOTIFY_AND_MANUAL_RENEW", "deadline": "2026-10-09T00:00:00Z"}}
			if deleted {
				response.Status = "external_deleted"
				response.CBSStatus = "NOT_FOUND"
				response.ProviderData["storageVolumeId"] = "disk-response-loss"
				response.ProviderData["cbsStatus"] = "NOT_FOUND"
				response.ProviderData["status"] = "external_deleted"
				response.ProviderData["storageDestroyPhase"] = "absence_confirmed"
				response.ProviderData["storageDestroyMutationCount"] = "1"
				response.ProviderData["describeCbsRequestId"] = "read-absent"
			}
			return response, nil
		case "destroy_storage_volume":
			deletes++
			deleted = true
			return provisionerResponse{OK: true, StorageVolumeID: "disk-response-loss", CBSStatus: "NOT_FOUND", Status: "external_deleted", ProviderRequestID: "delete-original", MutationCount: 1, ProviderData: map[string]string{"storageVolumeId": "disk-response-loss", "cbsStatus": "NOT_FOUND", "status": "external_deleted", "storageDestroyPhase": "absence_confirmed", "storageDestroyMutationCount": "1", "terminateCbsRequestId": "delete-original", "describeCbsRequestId": "read-absent", "region": "ap-guangzhou"}}, nil
		default:
			t.Fatalf("unexpected cloud action %s", input.Action)
			return provisionerResponse{}, nil
		}
	}
	provider.kubectl = func(_ context.Context, args []string, _ []byte) ([]byte, error) {
		if args[0] != "get" && args[0] != "delete" {
			t.Fatalf("unexpected static binding creation %v", args)
		}
		return []byte(`{"kind":"List","items":[]}`), nil
	}
	providerCtx := service.launchStages.providerOperationContext(ctx, parent, true)
	result, err := provider.ReadWorkspaceLaunchCloseoutResource(providerCtx, request)
	if err != nil || result.State != "present" || result.Storage == nil {
		t.Fatalf("read=%#v err=%v", result, err)
	}
	// Ordinary succeeded-Workspace DELETE retains its existing complete-binding admission.
	if _, err := provider.DestroyStorageVolume(ctx, *result.Storage); err == nil || deletes != 0 {
		t.Fatalf("ordinary delete admitted incomplete binding: %v", err)
	}
	service.volumes[result.Storage.ID] = *result.Storage
	if _, err := service.DestroyStorageVolume(context.WithValue(ctx, workspaceLaunchStorageCloseoutContextKey{}, request), result.Storage.ID); err != nil {
		t.Fatal(err)
	}
	restarted := NewServiceWithOperationStore(provider, store)
	result, err = provider.ReadWorkspaceLaunchCloseoutResource(restarted.launchStages.providerOperationContext(ctx, parent, true), request)
	if err != nil || result.State != "absent" || deletes != 1 || fixture.createCalls != 1 {
		t.Fatalf("result=%#v err=%v creates=%d deletes=%d", result, err, fixture.createCalls, deletes)
	}
}

func TestWorkspaceLaunchCloseoutTencentDeletesExactStandaloneGatewaySecretAfterLostResponse(t *testing.T) {
	ctx := context.Background()
	service, store, provider, preflight, image, launchHash := newTencentWorkspaceLaunchService(t)
	key := "local-only-closeout-key"
	sum := sha256.Sum256([]byte(key))
	fingerprint := "sha256:" + hex.EncodeToString(sum[:])
	stage := workspaceLaunchStageFixtureInput(preflight, image, launchHash, "secret", "ensure_gateway_secret", WorkspaceLaunchResources{GatewaySecretFingerprint: fingerprint})
	stage.GatewayCredential = &WorkspaceLaunchGatewayCredential{KeyID: 17, Value: key}
	parent, record, err := newWorkspaceLaunchStageOperation(stage, "tencent-tke", service.now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(ctx, parent); err != nil {
		t.Fatal(err)
	}
	admission, _ := service.launchStages.workspaceLaunchPreflight(ctx, preflight.ProviderBindingRef)
	request := WorkspaceLaunchProviderRequest{Input: stage, Current: record, ProviderPlan: admission.CanonicalProviderPlan}
	secretRef := gatewaySecretName(stage.Binding.WorkspaceID)
	present := true
	deletes := 0
	loseRead := false
	provider.kubectl = func(_ context.Context, args []string, _ []byte) ([]byte, error) {
		if args[0] == "delete" {
			if args[1] != "secret/"+secretRef {
				t.Fatalf("wrong secret deleted %v", args)
			}
			deletes++
			present = false
			loseRead = true
			return nil, nil
		}
		if args[0] != "get" || args[1] != "secret/"+secretRef {
			t.Fatalf("unexpected secret IO %v", args)
		}
		if loseRead {
			loseRead = false
			return nil, context.DeadlineExceeded
		}
		if !present {
			return nil, nil
		}
		return mustJSON(map[string]any{"kind": "Secret", "type": "Opaque", "metadata": map[string]any{"name": secretRef, "labels": map[string]string{"app.kubernetes.io/name": "opl-gateway-secret"}, "annotations": map[string]string{"oplcloud.cn/account-id": stage.Binding.AccountID, "oplcloud.cn/workspace-id": stage.Binding.WorkspaceID, "oplcloud.cn/workspace-api-key-id": "17", "oplcloud.cn/secret-version": hex.EncodeToString(sum[:])[:16], "oplcloud.cn/secret-fingerprint": fingerprint}}, "data": map[string]string{"opl_gateway_api_key": base64.StdEncoding.EncodeToString([]byte(key))}}), nil
	}
	providerCtx := service.launchStages.providerOperationContext(ctx, parent, false)
	result, err := provider.ReadWorkspaceLaunchCloseoutResource(providerCtx, request)
	if err != nil || result.State != "present" {
		t.Fatalf("secret=%#v err=%v", result, err)
	}
	wrong := request
	wrong.Current.GatewayKeyID = 18
	if err := provider.DestroyWorkspaceLaunchGatewaySecret(providerCtx, wrong); err == nil || deletes != 0 {
		t.Fatalf("wrong key deleted original: err=%v deletes=%d", err, deletes)
	}
	if err := provider.DestroyWorkspaceLaunchGatewaySecret(providerCtx, request); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost response err=%v", err)
	}
	if err := provider.DestroyWorkspaceLaunchGatewaySecret(providerCtx, request); err != nil {
		t.Fatal(err)
	}
	result, err = provider.ReadWorkspaceLaunchCloseoutResource(providerCtx, request)
	if err != nil || result.State != "absent" || deletes != 1 {
		t.Fatalf("read=%#v err=%v deletes=%d", result, err, deletes)
	}
}

type closeoutScopedStore struct {
	OperationStore
	rejectList bool
}

func (s *closeoutScopedStore) List(ctx context.Context) ([]FabricOperation, error) {
	if s.rejectList {
		return nil, errors.New("global history read forbidden")
	}
	return s.OperationStore.List(ctx)
}
func TestWorkspaceLaunchCloseoutPreviewReadsOnlyOriginalWorkspaceStages(t *testing.T) {
	_, store, provider, input := closeoutFixture(t)
	for i := 0; i < 100; i++ {
		if err := store.Append(context.Background(), FabricOperation{ID: fmt.Sprintf("foreign-%d", i), WorkspaceID: "another-workspace", ResourceKind: "workspace_launch_stage"}); err != nil {
			t.Fatal(err)
		}
	}
	scoped := &closeoutScopedStore{OperationStore: store}
	service := NewServiceWithOperationStore(provider, scoped)
	scoped.rejectList = true
	result, err := service.ReadWorkspaceLaunchCloseout(context.Background(), input)
	if err != nil || result.State != "eligible" || len(result.Resources) != 5 {
		t.Fatalf("scoped preview=%#v err=%v", result, err)
	}
}

func TestWorkspaceLaunchCloseoutQueuedOrderCancelsWithoutDisplacingUnknownHead(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "postgres_restart"}[postgres], func(t *testing.T) {
			ctx := context.Background()
			service, memory, provider, preflight, image, launchHash := newTencentWorkspaceLaunchService(t)
			var store OperationStore = memory
			var reopened OperationStore = memory
			if postgres {
				databaseURL := fabricTestDatabaseURL(t)
				pg, err := newTestPostgresOperationStore(databaseURL)
				if err != nil {
					t.Fatal(err)
				}
				defer pg.client.Close()
				ops, _ := memory.List(ctx)
				for _, op := range ops {
					if err := pg.Append(ctx, op); err != nil {
						t.Fatal(err)
					}
				}
				secondStore, err := newTestPostgresOperationStore(databaseURL)
				if err != nil {
					t.Fatal(err)
				}
				defer secondStore.client.Close()
				store, reopened = pg, secondStore
				service = NewServiceWithOperationStore(provider, store)
			}
			first := workspaceLaunchStageFixtureInput(preflight, image, launchHash, "ensure_compute_allocation", "ensure_compute_allocation", WorkspaceLaunchResources{})
			original, err := store.Get(ctx, preflight.ProviderBindingRef)
			if err != nil {
				t.Fatal(err)
			}
			admission, valid := decodeWorkspaceLaunchPreflight(original)
			if !valid {
				t.Fatal("invalid original preflight")
			}
			admission.Input.LaunchOperationID, admission.Input.AccountID, admission.Input.WorkspaceID = "launch-queued-closeout", "acct-queued", "ws-queued"
			admission.ProviderBindingRef = workspaceLaunchPreflightBindingRef(admission)
			if err := service.persistWorkspaceLaunchPreflight(ctx, admission); err != nil {
				t.Fatal(err)
			}
			second := first
			second.ProviderBindingRef = admission.ProviderBindingRef
			second.Binding.LaunchOperationID, second.Binding.AccountID, second.Binding.WorkspaceID = admission.Input.LaunchOperationID, admission.Input.AccountID, admission.Input.WorkspaceID
			second.Binding.FabricOperationID = "launch-queued-closeout:ensure_compute_allocation"
			second.Binding.IdempotencyKey = second.Binding.FabricOperationID
			second.Binding.RequestHash = workspaceLaunchStageRequestHash(second, launchHash)
			creates := 0
			provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
				switch request.Action {
				case "prepare_compute_allocation":
					return provisionerResponse{OK: true, CurrentReplicas: 1, TargetReplicas: 2, Machines: []provisionerMachine{{MachineID: "machine-before"}}}, nil
				case "create_compute_allocation":
					creates++
					return provisionerResponse{OK: false, Retryable: true, Status: "provisioning"}, nil
				case "read_compute_allocation":
					return provisionerResponse{}, context.DeadlineExceeded
				default:
					t.Fatalf("unexpected provider mutation %s", request.Action)
					return provisionerResponse{}, nil
				}
			}
			provider.kubectl = func(_ context.Context, args []string, _ []byte) ([]byte, error) {
				if args[0] != "get" {
					t.Fatalf("queued order mutated K8s: %v", args)
				}
				return []byte(`{"kind":"List","items":[]}`), nil
			}
			for _, stage := range []WorkspaceLaunchStageInput{first, second} {
				result, err := service.EnsureWorkspaceLaunchStage(ctx, stage)
				if err != nil || result.State != "pending" {
					t.Fatalf("ensure=%#v err=%v", result, err)
				}
			}
			queued, err := store.Get(ctx, second.Binding.FabricOperationID)
			record, valid := decodeWorkspaceLaunchStageRecord(queued)
			if err != nil || !valid || !record.ComputePoolQueued || creates != 1 {
				t.Fatalf("queued=%#v err=%v creates=%d", queued, err, creates)
			}
			input := WorkspaceLaunchCloseoutInput{SchemaVersion: 1, LaunchOperationID: second.Binding.LaunchOperationID, AccountID: second.Binding.AccountID, WorkspaceID: second.Binding.WorkspaceID, ProviderProfileRef: second.ProviderProfileRef, ProviderBindingRef: second.ProviderBindingRef, SpecDigest: second.SpecDigest, IdempotencyKey: "close-queued-original"}
			for range 2 {
				restarted := NewServiceWithOperationStore(provider, reopened)
				result, err := restarted.CloseoutWorkspaceLaunch(ctx, input)
				if err != nil || !result.Frozen || result.State != "absent" || len(result.Resources) != 5 {
					t.Fatalf("queued closeout=%#v err=%v", result, err)
				}
				if _, err := restarted.EnsureWorkspaceLaunchStage(ctx, second); !errors.Is(err, ErrWorkspaceLaunchFrozen) {
					t.Fatalf("closed order resumed: %v", err)
				}
			}
			head, exists, err := reopened.ComputePoolHead(ctx, "np-basic")
			if err != nil || !exists || head.ID != first.Binding.FabricOperationID || creates != 1 {
				t.Fatalf("changed original head=%#v exists=%v err=%v creates=%d", head, exists, err, creates)
			}
			closed, err := reopened.Get(ctx, second.Binding.FabricOperationID)
			if err != nil || closed.Status != "failed" || closed.ErrorCode != "workspace_launch_closed" || closed.FinishedAt.IsZero() {
				t.Fatalf("closed queue=%#v err=%v", closed, err)
			}
			marker, err := reopened.Get(ctx, workspaceLaunchCloseoutID(input.LaunchOperationID))
			var receipt WorkspaceLaunchCloseoutResult
			if err != nil || marker.Status != "succeeded" || marker.FinishedAt.IsZero() || !decodeWorkspaceLaunchCloseoutPayload(marker.RedactedProviderPayload["workspaceLaunchCloseoutReadback"], &receipt) || receipt.Binding != input || receipt.State != "absent" || len(receipt.Resources) != 5 {
				t.Fatalf("missing closeout evidence=%#v err=%v", marker, err)
			}
		})
	}
}

func TestWorkspaceLaunchCloseoutWaitsForLivePoolLeaseBeforeRetiringOriginal(t *testing.T) {
	ctx := context.Background()
	service, store, provider, preflight, image, launchHash := newTencentWorkspaceLaunchService(t)
	stage := workspaceLaunchStageFixtureInput(preflight, image, launchHash, "ensure_compute_allocation", "ensure_compute_allocation", WorkspaceLaunchResources{})
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		if request.Action != "prepare_compute_allocation" {
			t.Fatalf("unexpected cloud action %s", request.Action)
		}
		return provisionerResponse{}, context.DeadlineExceeded
	}
	provider.kubectl = func(_ context.Context, args []string, _ []byte) ([]byte, error) {
		if args[0] != "get" {
			t.Fatalf("unexpected K8s mutation %v", args)
		}
		return []byte(`{"kind":"List","items":[]}`), nil
	}
	if _, err := service.EnsureWorkspaceLaunchStage(ctx, stage); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	now := time.Now()
	if _, claimed, err := store.TryClaimComputePoolHead(ctx, stage.Binding.FabricOperationID, "np-basic", "original-dispatch-owner", now, now.Add(time.Minute)); err != nil || !claimed {
		t.Fatalf("lease=%v err=%v", claimed, err)
	}
	input := WorkspaceLaunchCloseoutInput{SchemaVersion: 1, LaunchOperationID: stage.Binding.LaunchOperationID, AccountID: stage.Binding.AccountID, WorkspaceID: stage.Binding.WorkspaceID, ProviderProfileRef: stage.ProviderProfileRef, ProviderBindingRef: stage.ProviderBindingRef, SpecDigest: stage.SpecDigest, IdempotencyKey: "close-live-lease"}
	result, err := service.CloseoutWorkspaceLaunch(ctx, input)
	if err != nil || !result.Frozen || result.State != "pending" || result.Reason != "launch_dispatch_in_flight" {
		t.Fatalf("live lease=%#v err=%v", result, err)
	}
	if head, exists, err := store.ComputePoolHead(ctx, "np-basic"); err != nil || !exists || head.ComputePoolLeaseOwner != "original-dispatch-owner" {
		t.Fatalf("released live lease=%#v err=%v", head, err)
	}
	restarted := NewServiceWithOperationStore(provider, store)
	restarted.now = func() time.Time { return now.Add(2 * time.Minute) }
	closeoutUntilAbsent(t, restarted, input)
	if _, exists, err := store.ComputePoolHead(ctx, "np-basic"); err != nil || exists {
		t.Fatalf("expired original not retired exists=%v err=%v", exists, err)
	}
}

func TestWorkspaceLaunchCloseoutRejectsMalformedRetainedProviderStateBeforeIO(t *testing.T) {
	ctx := context.Background()
	service, store, provider, preflight, image, launchHash := newTencentWorkspaceLaunchService(t)
	stage := workspaceLaunchStageFixtureInput(preflight, image, launchHash, "ensure_compute_allocation", "ensure_compute_allocation", WorkspaceLaunchResources{})
	parent, record, err := newWorkspaceLaunchStageOperation(stage, "tencent-tke", service.now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(ctx, parent); err != nil {
		t.Fatal(err)
	}
	request, err := service.launchStages.WorkspaceLaunchProviderRequest(ctx, stage, record)
	if err != nil {
		t.Fatal(err)
	}
	request.Current.ProviderState = json.RawMessage(`{"compute":42}`)
	provider.provision = func(context.Context, provisionerRequest) (provisionerResponse, error) {
		t.Fatal("malformed retained state consulted provider")
		return provisionerResponse{}, nil
	}
	result, err := provider.ReadWorkspaceLaunchCloseoutResource(service.launchStages.providerOperationContext(ctx, parent, true), request)
	if !errors.Is(err, ErrLaunchStageBindingConflict) || result.State == "absent" {
		t.Fatalf("malformed state=%#v err=%v", result, err)
	}
}
