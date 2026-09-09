package fabric

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestD3TencentLaunchWaitsForOriginalPoolHeadBeforePreparingAnotherWorkspace(t *testing.T) {
	testD3TencentLaunchPool(t, nil, nil)
}

func TestD3PostgresTencentLaunchPoolResumesAcrossServiceInstances(t *testing.T) {
	databaseURL := fabricTestDatabaseURL(t)
	store, err := newTestPostgresOperationStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.client.Close()
	reopened, err := newTestPostgresOperationStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.client.Close()
	testD3TencentLaunchPool(t, store, reopened)
}

func TestD3TencentLaunchResumesUndispatchedPoolHeadWithoutInventingProviderAbsence(t *testing.T) {
	ctx := context.Background()
	service, store, provider, preflight, image, launchHash := newTencentWorkspaceLaunchService(t)
	input := workspaceLaunchStageFixtureInput(preflight, image, launchHash, "ensure_compute_allocation", "ensure_compute_allocation", WorkspaceLaunchResources{})
	prepareCalls, scaleCalls, providerReads := 0, 0, 0
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		switch request.Action {
		case "prepare_compute_allocation":
			prepareCalls++
			if prepareCalls == 1 {
				return provisionerResponse{}, context.DeadlineExceeded
			}
			return provisionerResponse{OK: true, CurrentReplicas: 1, TargetReplicas: 2, Machines: []provisionerMachine{{MachineID: "machine-before"}}}, nil
		case "create_compute_allocation":
			scaleCalls++
			return provisionerResponse{OK: false, Retryable: true, Status: "provisioning"}, nil
		default:
			providerReads++
			t.Fatalf("undispatched original consulted provider: %s", request.Action)
			return provisionerResponse{}, nil
		}
	}
	if _, err := service.EnsureWorkspaceLaunchStage(ctx, input); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("interrupted prepare err=%v", err)
	}
	restarted := NewServiceWithOperationStore(provider, store)
	before, _ := store.List(ctx)
	readback, err := restarted.ReadWorkspaceLaunchStage(ctx, input)
	after, _ := store.List(ctx)
	if err != nil || readback.Reason != "compute_dispatch_pending" || !reflect.DeepEqual(before, after) || prepareCalls != 1 || scaleCalls != 0 || providerReads != 0 {
		t.Fatalf("undispatched read=%#v err=%v prepare=%d scale=%d reads=%d", readback, err, prepareCalls, scaleCalls, providerReads)
	}
	result, err := restarted.EnsureWorkspaceLaunchStage(ctx, input)
	if err != nil || result.State != "pending" || result.Reason != "provider_provisioning" || prepareCalls != 2 || scaleCalls != 1 {
		t.Fatalf("original first dispatch=%#v err=%v prepare=%d scale=%d", result, err, prepareCalls, scaleCalls)
	}
}

func testD3TencentLaunchPool(t *testing.T, postgresStore, reopenedStore *PostgresOperationStore) {
	t.Helper()
	ctx := context.Background()
	service, memoryStore, provider, preflight, image, launchHash := newTencentWorkspaceLaunchService(t)
	var store OperationStore = memoryStore
	if postgresStore != nil {
		operations, err := memoryStore.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, operation := range operations {
			if err := postgresStore.Append(ctx, operation); err != nil {
				t.Fatal(err)
			}
		}
		store = postgresStore
		service = NewServiceWithOperationStore(provider, store)
	}
	first := workspaceLaunchStageFixtureInput(preflight, image, launchHash, "ensure_compute_allocation", "ensure_compute_allocation", WorkspaceLaunchResources{})

	storedPreflight, err := store.Get(ctx, preflight.ProviderBindingRef)
	if err != nil {
		t.Fatal(err)
	}
	admission, ok := decodeWorkspaceLaunchPreflight(storedPreflight)
	if !ok {
		t.Fatal("invalid original preflight")
	}
	admission.Input.LaunchOperationID = "launch-beta"
	admission.Input.AccountID = "acct-beta"
	admission.Input.WorkspaceID = "ws-beta"
	admission.ProviderBindingRef = workspaceLaunchPreflightBindingRef(admission)
	if err := service.persistWorkspaceLaunchPreflight(ctx, admission); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ProviderBindingRef = admission.ProviderBindingRef
	second.Binding.LaunchOperationID = admission.Input.LaunchOperationID
	second.Binding.AccountID = admission.Input.AccountID
	second.Binding.WorkspaceID = admission.Input.WorkspaceID
	second.Binding.FabricOperationID = "launch-beta:ensure_compute_allocation"
	second.Binding.IdempotencyKey = second.Binding.FabricOperationID
	second.Binding.RequestHash = workspaceLaunchStageRequestHash(second, launchHash)

	allocation := ComputeAllocation{
		ID: workspaceLaunchComputeID(first.Binding), AccountID: first.Binding.AccountID, WorkspaceID: first.Binding.WorkspaceID,
		PackageID: "basic", Provider: "tencent-tke", ProviderResourceID: "ins-original", PoolID: "pool-basic-2c4g", NodePoolID: "np-basic",
		MachineName: "machine-original", InstanceID: "ins-original", CVMInstanceID: "ins-original", NodeName: "node-original",
		PrivateIP: "10.0.0.18", InstanceType: "SA5.MEDIUM4", Zone: "ap-guangzhou-3", ChargeType: "PREPAID",
		RenewFlag: "NOTIFY_AND_MANUAL_RENEW", Deadline: "2026-10-08T00:00:00Z",
	}
	prepared := ComputeAllocationPreparation{
		PoolID: allocation.PoolID, PackageID: allocation.PackageID, NodePoolID: allocation.NodePoolID, InstanceType: allocation.InstanceType,
		Zone: allocation.Zone, MaxReplicas: 20, BaselineReplicas: 1, TargetReplicas: 2, BeforeMachineNames: []string{"machine-before"},
	}
	prepareCalls, scaleCalls, providerReads := 0, 0, 0
	originalReady := false
	readFailure := false
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		switch request.Action {
		case "prepare_compute_allocation":
			prepareCalls++
			return provisionerResponse{OK: true, CurrentReplicas: 1, TargetReplicas: 2, Machines: []provisionerMachine{{MachineID: "machine-before"}}}, nil
		case "create_compute_allocation":
			scaleCalls++
			return provisionerResponse{OK: false, Retryable: true, Status: "provisioning", ProviderRequestID: "request-original-scale"}, nil
		case "read_compute_allocation":
			providerReads++
			if readFailure {
				return provisionerResponse{}, context.DeadlineExceeded
			}
			if originalReady && request.Allocation.ID == allocation.ID {
				return tencentComputeAllocationResponse(allocation, "request-original-read"), nil
			}
			return provisionerResponse{OK: false, Retryable: true, Status: "provisioning", ProviderRequestID: "request-original-read"}, nil
		case "compute_claim_truth":
			return tencentTargetOwnedProofResponse(allocation, prepared), nil
		case "tag_compute_machine":
			return provisionerResponse{OK: true, Status: "tagged", MutationEvidence: &ComputeClaimMutationEvidence{}}, nil
		default:
			t.Fatalf("unexpected provisioner action %q", request.Action)
			return provisionerResponse{}, nil
		}
	}
	provider.kubectl = func(_ context.Context, args []string, _ []byte) ([]byte, error) {
		if !originalReady || len(args) < 2 || args[0] != "get" {
			t.Fatalf("unexpected Kubernetes call before exact original readiness: %v", args)
		}
		ownership, err := workspaceLaunchComputeOwnership(allocation)
		if err != nil {
			t.Fatal(err)
		}
		return tencentOwnershipKubernetesReadback(args, allocation, ownership, true), nil
	}

	for _, input := range []WorkspaceLaunchStageInput{first, second} {
		result, err := service.EnsureWorkspaceLaunchStage(ctx, input)
		if err != nil || result.State != "pending" || result.Binding != input.Binding {
			t.Fatalf("launch=%s result=%#v err=%v", input.Binding.LaunchOperationID, result, err)
		}
	}
	if prepareCalls != 1 || scaleCalls != 1 {
		t.Fatalf("second Workspace bypassed the original pending NodePool head: prepare=%d scale=%d; want one original prepare and scale", prepareCalls, scaleCalls)
	}
	beforeRead, _ := store.List(ctx)
	readsBeforeQueued := providerReads
	for range 3 {
		result, err := service.ReadWorkspaceLaunchStage(ctx, second)
		if err != nil || result.State != "pending" || result.Reason != "compute_pool_queued" {
			t.Fatalf("queued read=%#v err=%v", result, err)
		}
	}
	afterRead, _ := store.List(ctx)
	if !reflect.DeepEqual(beforeRead, afterRead) || providerReads != readsBeforeQueued {
		t.Fatal("queued read changed persisted state or consulted provider before admission")
	}
	readFailure = true
	if _, err := service.ReadWorkspaceLaunchStage(ctx, first); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("uncertain original read error=%v", err)
	}
	if result, err := service.EnsureWorkspaceLaunchStage(ctx, second); err != nil || result.Reason != "compute_pool_queued" || prepareCalls != 1 || scaleCalls != 1 {
		t.Fatalf("uncertain original allowed later growth: result=%#v err=%v", result, err)
	}
	readFailure = false

	var restarted *Service
	if reopenedStore != nil {
		now := time.Now()
		stale, claimed, err := store.TryClaimComputePoolHead(ctx, first.Binding.FabricOperationID, "np-basic", "d3-expired-owner", now, now.Add(time.Minute))
		if err != nil || !claimed {
			t.Fatalf("original lease claimed=%v err=%v", claimed, err)
		}
		readsBeforeLease := providerReads
		concurrent := NewServiceWithOperationStore(provider, reopenedStore)
		if result, err := concurrent.EnsureWorkspaceLaunchStage(ctx, first); err != nil || result.Reason != "compute_pool_queued" || prepareCalls != 1 || scaleCalls != 1 || providerReads != readsBeforeLease {
			t.Fatalf("concurrent owner crossed live lease: result=%#v err=%v", result, err)
		}
		if _, err := postgresStore.db.ExecContext(ctx, `UPDATE fabric_operations SET compute_pool_lease_expires_at = clock_timestamp() - interval '1 second' WHERE id = $1`, stale.ID); err != nil {
			t.Fatal(err)
		}
		current, claimed, err := reopenedStore.TryClaimComputePoolHead(ctx, stale.ID, "np-basic", "d3-current-owner", now, now.Add(time.Minute))
		if err != nil || !claimed {
			t.Fatalf("reopened lease claimed=%v err=%v", claimed, err)
		}
		staleSuccess := stale
		staleSuccess.Status, staleSuccess.FinishedAt = "succeeded", time.Now()
		if err := store.SaveRuntime(ctx, staleSuccess); !errors.Is(err, ErrRuntimeOperationNotCurrent) {
			t.Fatalf("stale stage completion err=%v", err)
		}
		if err := store.ConvergeRuntimeReadback(ctx, stale, staleSuccess); !errors.Is(err, ErrRuntimeOperationNotCurrent) {
			t.Fatalf("stale stage readback err=%v", err)
		}
		if head, found, err := reopenedStore.ComputePoolHead(ctx, "np-basic"); err != nil || !found || head.ID != current.ID || head.ComputePoolLeaseOwner != current.ComputePoolLeaseOwner {
			t.Fatalf("stale owner changed FIFO head=%#v found=%v err=%v", head, found, err)
		}
		if err := reopenedStore.ReleaseComputePoolHead(ctx, current.ID, "np-basic", current.ComputePoolLeaseOwner); err != nil {
			t.Fatal(err)
		}
		restarted = NewServiceWithOperationStore(provider, reopenedStore)
	} else {
		restarted = NewServiceWithOperationStore(provider, store)
	}

	originalReady = true
	readback, err := restarted.ReadWorkspaceLaunchStage(ctx, first)
	if err != nil || readback.State != "pending" || readback.Reason != "ownership_pending" {
		t.Fatalf("original late Machine read=%#v err=%v", readback, err)
	}
	ready, err := restarted.EnsureWorkspaceLaunchStage(ctx, first)
	if err != nil || ready.State != "ready" || ready.Resources.ComputeAllocationID != allocation.ID || scaleCalls != 1 {
		t.Fatalf("original resumed=%#v err=%v scale=%d", ready, err, scaleCalls)
	}
	beforeRead, _ = store.List(ctx)
	dispatch, err := restarted.ReadWorkspaceLaunchStage(ctx, second)
	afterRead, _ = store.List(ctx)
	if err != nil || dispatch.State != "pending" || dispatch.Reason != "compute_dispatch_pending" || !reflect.DeepEqual(beforeRead, afterRead) || prepareCalls != 1 || scaleCalls != 1 {
		t.Fatalf("successor admission read=%#v err=%v prepare=%d scale=%d", dispatch, err, prepareCalls, scaleCalls)
	}
	secondResult, err := restarted.EnsureWorkspaceLaunchStage(ctx, second)
	if err != nil || secondResult.State != "pending" || prepareCalls != 2 || scaleCalls != 2 {
		t.Fatalf("successor dispatch=%#v err=%v prepare=%d scale=%d", secondResult, err, prepareCalls, scaleCalls)
	}
	for _, input := range []WorkspaceLaunchStageInput{first, second} {
		if _, err := restarted.EnsureWorkspaceLaunchStage(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	if prepareCalls != 2 || scaleCalls != 2 {
		t.Fatalf("replay repeated pool growth: prepare=%d scale=%d", prepareCalls, scaleCalls)
	}
}

func TestD3FailedLaunchRecoveryDoesNotDisplaceAnActivePoolHead(t *testing.T) {
	for _, backend := range []string{"memory", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			var store OperationStore = NewMemoryOperationStore()
			if backend == "postgres" {
				postgresStore, err := newTestPostgresOperationStore(fabricTestDatabaseURL(t))
				if err != nil {
					t.Fatal(err)
				}
				defer postgresStore.client.Close()
				store = postgresStore
			}
			ctx := context.Background()
			now := time.Now()
			failed := newOperation("ensure_compute_allocation", "workspace_launch_stage", "old-stage", "acct-old", "ws-old", "old-key", "old-request-hash", now.Add(-time.Hour))
			failed.ID, failed.CreatedAt = failed.OperationID, now.Add(-time.Hour)
			failed.Status, failed.FinishedAt = "failed", now.Add(-time.Minute)
			if err := store.Append(ctx, failed); err != nil {
				t.Fatal(err)
			}
			active := newOperation("ensure_compute_allocation", "workspace_launch_stage", "active-stage", "acct-active", "ws-active", "active-key", "active-request-hash", now)
			active.ID, active.CreatedAt = active.OperationID, now
			active.ComputePoolKey, active.Status = "np-basic", "started"
			if _, claimed, err := store.ClaimComputePoolRuntime(ctx, active); err != nil || !claimed {
				t.Fatalf("active admission claimed=%v err=%v", claimed, err)
			}
			current, claimed, err := store.TryClaimComputePoolHead(ctx, active.ID, "np-basic", "active-owner", now, now.Add(time.Minute))
			if err != nil || !claimed {
				t.Fatalf("active lease claimed=%v err=%v", claimed, err)
			}
			failed.ComputePoolKey, failed.Status, failed.FinishedAt = "np-basic", "started", time.Time{}
			recovery, claimed, err := store.ClaimComputePoolRuntime(ctx, failed)
			if err != nil || claimed || recovery.Status != "failed" {
				t.Fatalf("older recovery displaced current head: recovery=%#v claimed=%v err=%v", recovery, claimed, err)
			}
			current.Status, current.FinishedAt = "succeeded", time.Now()
			if err := store.SaveRuntime(ctx, current); err != nil {
				t.Fatal(err)
			}
			recovery, claimed, err = store.ClaimComputePoolRuntime(ctx, failed)
			if err != nil || !claimed || recovery.ID != failed.ID || recovery.Status != "started" {
				t.Fatalf("original recovery did not resume after current head: recovery=%#v claimed=%v err=%v", recovery, claimed, err)
			}
		})
	}
}
