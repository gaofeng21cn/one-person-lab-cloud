package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

type d3InitialMutationGate struct {
	*workspaceLaunchUnitAdapter
	started chan struct{}
	release chan struct{}
}

func (adapter *d3InitialMutationGate) MutateStage(ctx context.Context, operation workspaceLaunchReconcileOperation, key string) error {
	close(adapter.started)
	select {
	case <-adapter.release:
		return adapter.workspaceLaunchUnitAdapter.MutateStage(ctx, operation, key)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestPostgresD3LaunchConcurrentFirstMutationDoesNotParkWinner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	account, owner := provisionedAccountRowsFor("acct-d3-reserve", "usr-d3-reserve", "d3-reserve@example.com", 921)
	mustStore(t, store.CreateProvisionedAccount(ctx, account, owner))
	command := workspaceLaunchUnitCommand()
	command.OperationID, command.AccountID, command.OwnerUserID = "launch-d3-reserve", "acct-d3-reserve", "usr-d3-reserve"
	command.WorkspaceID, command.Sub2APIUserID = "ws-d3-reserve", 921
	operation, err := newWorkspaceLaunchFixtureOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	operation.Stage = contracts.StageRuntime
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(ctx, row))
	adapter := &d3InitialMutationGate{workspaceLaunchUnitAdapter: &workspaceLaunchUnitAdapter{}, started: make(chan struct{}), release: make(chan struct{})}
	first := make(chan error, 1)
	go func() {
		_, err := NewWorkspaceLaunchReconciler(store, adapter).Reconcile(ctx, operation.ID)
		first <- err
	}()
	select {
	case <-adapter.started:
	case <-ctx.Done():
		t.Fatal("first mutation never dispatched")
	}
	second, secondErr := NewWorkspaceLaunchReconciler(store, adapter).Reconcile(ctx, operation.ID)
	close(adapter.release)
	firstErr := <-first
	if secondErr != nil || second.Status == contracts.StatusManualReview || firstErr != nil {
		t.Fatalf("in-flight mutation was interrupted: second=%s secondErr=%v firstErr=%v", workspaceLaunchReconcileResultSummary(second), secondErr, firstErr)
	}
	row, found, err := store.GetRuntimeOperation(ctx, operation.ID)
	if err != nil || !found {
		t.Fatalf("read final operation: found=%v err=%v", found, err)
	}
	final, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil || final.Stage != contracts.StageActivation || adapter.mutations != 1 {
		t.Fatalf("original mutation did not advance exactly once: operation=%s mutations=%d err=%v", workspaceLaunchReconcileResultSummary(final), adapter.mutations, err)
	}
}

type d3SchedulerStore struct {
	*memoryTableStore
	queries []runtimeOperationQuery
}

func (store *d3SchedulerStore) PageRuntimeOperations(ctx context.Context, query runtimeOperationQuery) (tablePage, error) {
	store.queries = append(store.queries, query)
	if query.Action != workspaceLaunchAction || query.Limit <= 0 || query.Limit > workspaceLaunchWorkerPageSize || query.Offset != 0 {
		return tablePage{}, errors.New("scheduler requested an unbounded or offset scan")
	}
	return store.memoryTableStore.PageRuntimeOperations(ctx, query)
}

type d3SchedulerFabric struct {
	fakeFabricClient
	mu                  sync.Mutex
	active, peak, reads int
	profiles            map[string]int
	slowOperation       string
	slowStarted         chan struct{}
	releaseSlow         chan struct{}
}

func (*d3SchedulerFabric) PreflightWorkspaceLaunch(context.Context, clients.WorkspaceLaunchPreflightInput) (clients.WorkspaceLaunchPreflight, error) {
	return clients.WorkspaceLaunchPreflight{}, errors.New("unexpected preflight")
}

func (fabric *d3SchedulerFabric) ReadWorkspaceLaunchStage(ctx context.Context, input clients.WorkspaceLaunchStageInput) (clients.WorkspaceLaunchStageResult, error) {
	fabric.mu.Lock()
	fabric.active++
	fabric.reads++
	fabric.profiles[input.ProviderProfileRef]++
	if fabric.active > fabric.peak {
		fabric.peak = fabric.active
	}
	fabric.mu.Unlock()
	defer func() { fabric.mu.Lock(); fabric.active--; fabric.mu.Unlock() }()
	if input.Binding.LaunchOperationID == fabric.slowOperation {
		select {
		case fabric.slowStarted <- struct{}{}:
		default:
		}
		select {
		case <-fabric.releaseSlow:
		case <-ctx.Done():
			return clients.WorkspaceLaunchStageResult{}, ctx.Err()
		}
	} else {
		timer := time.NewTimer(3 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return clients.WorkspaceLaunchStageResult{}, ctx.Err()
		}
	}
	resources := input.Resources
	switch input.Binding.Stage {
	case string(contracts.StageCompute):
		resources.ComputeAllocationID, resources.ComputeBindingRef = "compute-"+input.Binding.WorkspaceID, input.Binding.FabricOperationID
	case string(contracts.StageStorage):
		resources.StorageID, resources.StorageBindingRef = "storage-"+input.Binding.WorkspaceID, input.Binding.FabricOperationID
	case string(contracts.StageAttachment):
		resources.AttachmentID, resources.AttachmentBindingRef = "attachment-"+input.Binding.WorkspaceID, input.Binding.FabricOperationID
	default:
		return clients.WorkspaceLaunchStageResult{}, errors.New("remaining stages are outside the scheduler test")
	}
	return clients.WorkspaceLaunchStageResult{SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, State: "ready", Reason: "none", Binding: input.Binding, Resources: resources}, nil
}

func (*d3SchedulerFabric) EnsureWorkspaceLaunchStage(context.Context, clients.WorkspaceLaunchStageInput) (clients.WorkspaceLaunchStageResult, error) {
	return clients.WorkspaceLaunchStageResult{}, errors.New("recovery must read existing resources without another mutation")
}

func TestD3LaunchSchedulerFiftyAccountsAdvancePastSlowAccount(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store := &d3SchedulerStore{memoryTableStore: newMemoryTableStore()}
	for index := 0; index < 50; index++ {
		command := workspaceLaunchUnitCommand()
		command.OperationID, command.AccountID = fmt.Sprintf("launch-d3-%03d", index), fmt.Sprintf("account-d3-%03d", index)
		command.OwnerUserID, command.WorkspaceID = fmt.Sprintf("owner-d3-%03d", index), fmt.Sprintf("workspace-d3-%03d", index)
		command.ProviderProfileRef = "tencent-tke"
		if index%2 == 0 {
			command.ProviderProfileRef = "local-docker"
		}
		operation, err := newWorkspaceLaunchFixtureOperation(command)
		if err != nil {
			t.Fatal(err)
		}
		operation.Stage = contracts.StageCompute
		row, err := workspaceLaunchReconcileOperationRow(operation)
		if err != nil {
			t.Fatal(err)
		}
		mustStore(t, store.SaveRuntimeOperation(ctx, row))
	}
	fabric := &d3SchedulerFabric{profiles: map[string]int{}, slowOperation: "launch-d3-000", slowStarted: make(chan struct{}, 1), releaseSlow: make(chan struct{})}
	app, err := newControlPlaneAppWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := newWorkspaceLaunchScheduler(app, newTestService(fakeLedgerClient{}, fabric))
	started := time.Now()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	if err := scheduler.beginRound(ctx); err != nil {
		t.Fatal(err)
	}
	completed := 0
	for completed != 49 {
		select {
		case result := <-scheduler.completed:
			delete(scheduler.inFlight, result.accountID)
			if result.err != nil {
				t.Fatal(result.err)
			}
			if err := scheduler.dispatch(ctx); err != nil {
				t.Fatal(err)
			}
		case <-ticker.C:
			completed = 0
			for index := 1; index < 50; index++ {
				row, found, err := store.GetRuntimeOperation(ctx, fmt.Sprintf("launch-d3-%03d", index))
				if err != nil || !found {
					t.Fatalf("missing operation %d: %v", index, err)
				}
				operation, err := decodeWorkspaceLaunchReconcileOperation(row)
				if err != nil {
					t.Fatal(err)
				}
				if operation.Stage == contracts.StageSecret {
					completed++
				}
			}
			if completed != 49 {
				if err := scheduler.beginRound(ctx); err != nil {
					t.Fatal(err)
				}
			}
		case <-ctx.Done():
			t.Fatalf("other accounts blocked behind slow operation: advanced=%d/49", completed)
		}
	}
	elapsed := time.Since(started)
	select {
	case <-fabric.slowStarted:
	default:
		t.Fatal("slow operation was never scheduled")
	}
	row, _, _ := store.GetRuntimeOperation(ctx, fabric.slowOperation)
	slow, _ := decodeWorkspaceLaunchReconcileOperation(row)
	if slow.Stage != contracts.StageCompute {
		t.Fatalf("slow operation unexpectedly completed: %s", slow.Stage)
	}
	close(fabric.releaseSlow)
	if err := scheduler.collect(true); err != nil {
		t.Fatal(err)
	}
	fabric.mu.Lock()
	defer fabric.mu.Unlock()
	if fabric.peak > workspaceLaunchWorkerConcurrency || fabric.peak < 2 || fabric.profiles["tencent-tke"] == 0 || fabric.profiles["local-docker"] == 0 {
		t.Fatalf("execution bound/provider neutrality: peak=%d profiles=%v", fabric.peak, fabric.profiles)
	}
	t.Logf("accounts=50 workspaces=50 advanced_while_one_slow=49 stages_per_fast_account=3 execution_limit=%d observed_peak=%d query_pages=%d owner_reads=%d accelerated_tick=20ms elapsed=%s", workspaceLaunchWorkerConcurrency, fabric.peak, len(store.queries), fabric.reads, elapsed)
}

func TestD3LaunchSchedulerRetriesUnavailableProviderOnlyNextRound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store := &d3SchedulerStore{memoryTableStore: newMemoryTableStore()}
	operation, err := newWorkspaceLaunchFixtureOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	operation.Stage = contracts.StageSecret
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(ctx, row))
	app, err := newControlPlaneAppWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	fabric := &d3SchedulerFabric{profiles: map[string]int{}}
	scheduler := newWorkspaceLaunchScheduler(app, newTestService(fakeLedgerClient{}, fabric))
	if err := scheduler.beginRound(ctx); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.collect(true); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		if err := scheduler.dispatch(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if fabric.reads != 1 {
		t.Fatalf("completion events repeatedly polled unavailable provider: reads=%d", fabric.reads)
	}
	if err := scheduler.beginRound(ctx); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.collect(true); err != nil {
		t.Fatal(err)
	}
	if fabric.reads != 2 {
		t.Fatalf("next scheduled round did not retry original operation once: reads=%d", fabric.reads)
	}
}
