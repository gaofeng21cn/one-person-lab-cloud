package delivery_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	"opl-cloud/services/serve/internal/delivery"
)

// Fail the second descendant retirement, independent of traversal order. With
// two real version switches, recovery must retain at least one confirmed child
// effect as well as the original Workspace order's unfinished operation.
type workspaceRetirementRecoveryRuntime struct {
	runtimeForServe
	commands       []*api.RuntimeDeployCommand
	rootRuntime    string
	failAfterChild int
	failedRuntime  string
	absentAttempts map[string]int
	absentKeys     map[string]string
}

func (r *workspaceRetirementRecoveryRuntime) Start(ctx context.Context, command *api.RuntimeDeployCommand, target delivery.ExecutionTarget) (delivery.RuntimeObservation, error) {
	r.commands = append(r.commands, proto.Clone(command).(*api.RuntimeDeployCommand))
	return r.runtimeForServe.Start(ctx, command, target)
}

func (r *workspaceRetirementRecoveryRuntime) Lifecycle(ctx context.Context, command *api.RuntimeDeployCommand, target delivery.ExecutionTarget, desired string) error {
	if desired == "absent" {
		id := command.GetRuntimeInstanceId()
		r.absentAttempts[id]++
		if r.absentKeys == nil {
			r.absentKeys = make(map[string]string)
		}
		if previous := r.absentKeys[id]; previous != "" && previous != command.GetContext().GetIdempotencyKey() {
			return status.Error(codes.AlreadyExists, "retirement changed its downstream action identity")
		}
		r.absentKeys[id] = command.GetContext().GetIdempotencyKey()
		if id != r.rootRuntime && r.failAfterChild > 0 {
			r.failAfterChild--
			if r.failAfterChild == 0 {
				r.failedRuntime = id
				return status.Error(codes.Unavailable, "successor absence readback unavailable once")
			}
		}
	}
	return r.runtimeForServe.Lifecycle(ctx, command, target, desired)
}

func workspaceRetirementRecoveryFixture(t *testing.T, upgrades int) (*delivery.Service, *api.RuntimeReservationCommand, *api.RuntimeReservation, *workspaceRetirementRecoveryRuntime) {
	t.Helper()
	s, request, catalog := reservationFixture(t)
	runtime := &workspaceRetirementRecoveryRuntime{absentAttempts: make(map[string]int)}
	s.Resources, s.Runtime = &resourcesForServe{confirmed: true}, runtime
	root, err := s.Reserve(workspaceContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	runtime.rootRuntime = root.GetRuntimeInstanceId()
	if _, err = s.Deploy(workspaceContext(), deployReserved(request, root)); err != nil {
		t.Fatalf("deploy original application: %v", err)
	}
	catalog.versions = make(map[string]*api.CapabilityVersion)
	current := root.GetDeploymentId()
	// Admit one more version than setup uses, so the fence test submits a valid
	// new upgrade rather than failing on catalog lookup or a stale precondition.
	for version := 2; version <= upgrades+2; version++ {
		next := replacementVersion(t, fmt.Sprintf("cv-retirement-%d", version), strings.Repeat(fmt.Sprint(version), 64))
		catalog.versions[next.GetId()] = next
		if version > upgrades+1 {
			break
		}
		update := proto.Clone(request.GetContext()).(*api.CallContext)
		update.IdempotencyKey = fmt.Sprintf("retirement-setup-upgrade-%d", version)
		operation, err := s.UpdateWorkspaceVersion(serveContext(), &api.UpdateWorkspaceVersionRpcRequest{
			Context: update, WorkspaceId: request.GetWorkspaceId(),
			Body: &api.UpdateWorkspaceVersionRequest{CapabilityVersionId: proto.String(next.GetId()), ExpectedCurrentAgentDeploymentId: current},
		})
		if err != nil || operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
			t.Fatalf("setup version switch %d: %v %v", version, operation, err)
		}
		successor, _, _, previous := currentDeploymentFacts(t, s, request.GetWorkspaceId())
		if successor == current || previous != current {
			t.Fatalf("version switch did not extend original lineage: current=%s successor=%s previous=%s", current, successor, previous)
		}
		current = successor
	}
	return s, request, root, runtime
}

func TestWorkspaceRetirementRecoversFailedSuccessorAcrossRestart(t *testing.T) {
	s, request, root, runtime := workspaceRetirementRecoveryFixture(t, 2)
	ctx := workspaceContext()
	current, _, _, _ := currentDeploymentFacts(t, s, request.GetWorkspaceId())
	command := &api.RuntimeStopCommand{
		Context: request.Context, RuntimeInstanceId: root.GetRuntimeInstanceId(), DeploymentId: root.GetDeploymentId(),
		RetainedDataAttachmentId: request.GetDataAttachmentId(),
	}
	runtime.failAfterChild = 2
	retired, err := s.Retire(ctx, command)
	if runtime.failedRuntime == "" {
		t.Fatalf("retirement did not reach the injected successor failure: operation=%v error=%v", retired, err)
	}
	if err == nil || retired.GetStatus() == api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || retired.GetObservationResult() == api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED {
		t.Fatalf("failed successor was reported as retired: %v %v", retired, err)
	}
	var originalOperation, operationStatus, observation string
	if err = s.DB.QueryRow(`SELECT id,status,COALESCE(observation_result,'') FROM serve.operations WHERE kind='runtime_retire' AND resource_id=$1`, root.GetRuntimeInstanceId()).Scan(&originalOperation, &operationStatus, &observation); err != nil {
		t.Fatalf("read original retirement operation: %v", err)
	}
	if operationStatus == "succeeded" || observation == "confirmed" {
		t.Fatalf("original order completed before successor absence: status=%s observation=%s", operationStatus, observation)
	}

	confirmedCalls := make(map[string]int)
	for _, start := range runtime.commands {
		var actionObservation string
		if err = s.DB.QueryRow(`SELECT observation_result FROM serve.agent_runtime_actions WHERE runtime_instance_id=$1 AND action='terminate' AND expected_deployment_id=$2`, start.GetRuntimeInstanceId(), start.GetDeploymentId()).Scan(&actionObservation); err != nil {
			t.Fatalf("runtime %s was not durably frozen before provider cleanup: %v", start.GetRuntimeInstanceId(), err)
		}
		if actionObservation == "confirmed" {
			confirmedCalls[start.GetRuntimeInstanceId()] = runtime.absentAttempts[start.GetRuntimeInstanceId()]
		}
		if start.GetRuntimeInstanceId() == runtime.failedRuntime && actionObservation == "confirmed" {
			t.Fatal("failed successor effect was persisted as confirmed")
		}
	}
	if len(confirmedCalls) == 0 {
		t.Fatal("partial retirement did not retain any confirmed effect to recover")
	}
	generation, target := routeBinding(t, s, request.GetWorkspaceId())
	if target != "" {
		t.Fatalf("provider cleanup began before the route was cleared: %s", target)
	}

	// A fresh service object must recover both the fence and prior effects from
	// PostgreSQL. Only the provider fake retains its external call history.
	restarted, err := delivery.New(s.DB, s.Authorize)
	if err != nil {
		t.Fatal(err)
	}
	restarted.Identity, restarted.Capability, restarted.References = s.Identity, s.Capability, s.References
	restarted.Resources, restarted.Runtime = s.Resources, runtime
	startsBefore := runtime.starts
	for _, start := range runtime.commands {
		if _, err = restarted.StartRuntime(servePeerContext(), start); status.Code(err) != codes.FailedPrecondition {
			t.Errorf("pending retirement admitted adapter start for %s: %v", start.GetRuntimeInstanceId(), err)
		}
		// Superseded Deploy commands already fail their execution epoch check.
		// The current successor exercises the retirement fence itself.
		if start.GetDeploymentId() == current {
			if _, err = restarted.Deploy(ctx, start); status.Code(err) != codes.FailedPrecondition {
				t.Errorf("pending retirement admitted current deployment %s: %v", start.GetRuntimeInstanceId(), err)
			}
		}
	}
	for _, key := range []string{request.GetContext().GetIdempotencyKey(), "reserve-during-retirement"} {
		reserve := proto.Clone(request).(*api.RuntimeReservationCommand)
		reserve.Context.IdempotencyKey = key
		if _, err = restarted.Reserve(ctx, reserve); status.Code(err) != codes.FailedPrecondition {
			t.Errorf("pending retirement admitted reservation %q: %v", key, err)
		}
	}
	update := proto.Clone(request.GetContext()).(*api.CallContext)
	update.IdempotencyKey = "upgrade-during-retirement"
	if _, err = restarted.UpdateWorkspaceVersion(serveContext(), &api.UpdateWorkspaceVersionRpcRequest{
		Context: update, WorkspaceId: request.GetWorkspaceId(),
		Body: &api.UpdateWorkspaceVersionRequest{CapabilityVersionId: proto.String("cv-retirement-4"), ExpectedCurrentAgentDeploymentId: current},
	}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("pending retirement admitted another version switch: %v", err)
	}
	if runtime.starts != startsBefore {
		t.Fatalf("pending retirement reached provider Start: before=%d after=%d", startsBefore, runtime.starts)
	}

	// The injected dependency has recovered: the identical original command
	// resumes the existing operation and does not issue confirmed effects again.
	retry := proto.Clone(command).(*api.RuntimeStopCommand)
	retry.Context.IdempotencyKey = "new-request-for-original-retirement"
	recovered, err := restarted.Retire(ctx, retry)
	if err != nil || recovered.GetOperationId() != originalOperation || recovered.GetResourceId() != root.GetRuntimeInstanceId() || recovered.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || recovered.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED {
		t.Fatalf("original retirement did not recover across restart: %v %v", recovered, err)
	}
	for id, calls := range confirmedCalls {
		if runtime.absentAttempts[id] != calls {
			t.Errorf("confirmed absence was repeated for %s: before=%d after=%d", id, calls, runtime.absentAttempts[id])
		}
	}
	if runtime.absentAttempts[runtime.failedRuntime] != 2 {
		t.Errorf("failed successor was not retried exactly once: %v", runtime.absentAttempts)
	}
	var live, unknown int
	if err = s.DB.QueryRow(`SELECT count(*) FROM serve.agent_runtime_instances WHERE workspace_id=$1 AND status <> 'terminated'`, request.GetWorkspaceId()).Scan(&live); err != nil || live != 0 {
		t.Fatalf("recovery left live lineage runtimes: %d %v", live, err)
	}
	if err = s.DB.QueryRow(`SELECT count(*) FROM serve.agent_runtime_actions a JOIN serve.agent_runtime_instances r ON r.id=a.runtime_instance_id WHERE r.workspace_id=$1 AND a.action='terminate' AND a.observation_result <> 'confirmed'`, request.GetWorkspaceId()).Scan(&unknown); err != nil || unknown != 0 {
		t.Fatalf("recovery left unconfirmed termination intents: %d %v", unknown, err)
	}
	if after, target := routeBinding(t, s, request.GetWorkspaceId()); after != generation || target != "" {
		t.Fatalf("recovery repeated the confirmed route effect: generation=%d want=%d target=%s", after, generation, target)
	}
}

func TestWorkspaceRetirementRejectsRoutesOutsideOriginalLineage(t *testing.T) {
	for _, test := range []struct {
		name, workspace, tenant string
	}{
		{"foreign_tenant", "ws-foreign", "tenant-foreign"},
		{"another_workspace", "ws-other", "tenant-alpha"},
		{"same_workspace_unrelated_lineage", "ws-first", "tenant-alpha"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, request, root, runtime := workspaceRetirementRecoveryFixture(t, 1)
			// This is a real unrelated persisted delivery, with no predecessor
			// link to the original order. Simulate a corrupt route readback.
			const unrelatedDeployment = "dep-unrelated-retirement-route"
			const unrelatedRuntime = "rt_" + unrelatedDeployment
			seedDeployment(t, s.DB, test.workspace, test.tenant, unrelatedDeployment, "superseded", "ready", "https://unrelated.example/app", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
			if _, err := s.DB.Exec(`UPDATE serve.agent_deployments SET runtime_instance_id=$1 WHERE id=$2`, unrelatedRuntime, unrelatedDeployment); err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.Exec(`UPDATE serve.access_bindings SET target_execution_resource_id=$1,target_runtime_instance_id=$1,target_deployment_id=$2 WHERE workspace_id=$3`, unrelatedRuntime, unrelatedDeployment, request.GetWorkspaceId()); err != nil {
				t.Fatal(err)
			}
			var bindingBefore, runtimeBefore string
			if err := s.DB.QueryRow(`SELECT to_jsonb(b)::text FROM serve.access_bindings b WHERE workspace_id=$1`, request.GetWorkspaceId()).Scan(&bindingBefore); err != nil {
				t.Fatal(err)
			}
			if err := s.DB.QueryRow(`SELECT to_jsonb(r)::text FROM serve.agent_runtime_instances r WHERE id=$1`, unrelatedRuntime).Scan(&runtimeBefore); err != nil {
				t.Fatal(err)
			}
			var switchesBefore int
			if err := s.DB.QueryRow(`SELECT count(*) FROM serve.access_switches WHERE workspace_id=$1`, request.GetWorkspaceId()).Scan(&switchesBefore); err != nil {
				t.Fatal(err)
			}
			startsBefore, lifecycleBefore := runtime.starts, len(runtime.lifecycle)
			retired, err := s.Retire(workspaceContext(), &api.RuntimeStopCommand{
				Context: request.Context, RuntimeInstanceId: root.GetRuntimeInstanceId(), DeploymentId: root.GetDeploymentId(),
				RetainedDataAttachmentId: request.GetDataAttachmentId(),
			})
			if err == nil || retired.GetStatus() == api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
				t.Errorf("retirement accepted a route outside the original lineage: %v %v", retired, err)
			}
			if runtime.starts != startsBefore || len(runtime.lifecycle) != lifecycleBefore || len(runtime.absentAttempts) != 0 {
				t.Errorf("unowned route reached provider: starts=%d lifecycle=%v absence=%v", runtime.starts, runtime.lifecycle[lifecycleBefore:], runtime.absentAttempts)
			}
			var bindingAfter, runtimeAfter string
			if err = s.DB.QueryRow(`SELECT to_jsonb(b)::text FROM serve.access_bindings b WHERE workspace_id=$1`, request.GetWorkspaceId()).Scan(&bindingAfter); err != nil {
				t.Fatal(err)
			}
			if bindingAfter != bindingBefore {
				t.Errorf("rejected retirement mutated the unowned route: before=%s after=%s", bindingBefore, bindingAfter)
			}
			if err = s.DB.QueryRow(`SELECT to_jsonb(r)::text FROM serve.agent_runtime_instances r WHERE id=$1`, unrelatedRuntime).Scan(&runtimeAfter); err != nil {
				t.Fatal(err)
			}
			if runtimeAfter != runtimeBefore {
				t.Error("rejected retirement mutated the unrelated runtime")
			}
			var switchesAfter, confirmed int
			if err = s.DB.QueryRow(`SELECT count(*) FROM serve.access_switches WHERE workspace_id=$1`, request.GetWorkspaceId()).Scan(&switchesAfter); err != nil || switchesAfter != switchesBefore {
				t.Errorf("rejected retirement wrote a route switch: before=%d after=%d error=%v", switchesBefore, switchesAfter, err)
			}
			if err = s.DB.QueryRow(`SELECT count(*) FROM serve.operations WHERE kind='runtime_retire' AND resource_id=$1 AND (status='succeeded' OR observation_result='confirmed')`, root.GetRuntimeInstanceId()).Scan(&confirmed); err != nil || confirmed != 0 {
				t.Errorf("rejected route confirmed the original retirement: count=%d error=%v", confirmed, err)
			}
		})
	}
}

// Keep the provider call in flight without holding the fake's mutex. PostgreSQL
// readback distinguishes legal effects before closeout from forbidden late ones.
type workspaceRetirementSuspendingRuntime struct {
	runtimeForServe
	db             *sql.DB
	workspace      string
	suspendEntered chan struct{}
	releaseSuspend <-chan struct{}
	suspendOnce    sync.Once
	mu             sync.Mutex
	mutableEffects []string
	lateEffects    []string
}

func (r *workspaceRetirementSuspendingRuntime) recordMutableEffect(ctx context.Context, command *api.RuntimeDeployCommand, effect string) error {
	var closing bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM serve.agent_runtime_actions a JOIN serve.agent_runtime_instances i ON i.id=a.runtime_instance_id WHERE i.workspace_id=$1 AND a.action='terminate')`, r.workspace).Scan(&closing); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	event := effect + ":" + command.GetRuntimeInstanceId()
	r.mutableEffects = append(r.mutableEffects, event)
	if closing {
		r.lateEffects = append(r.lateEffects, event)
	}
	return nil
}

func (r *workspaceRetirementSuspendingRuntime) Start(ctx context.Context, command *api.RuntimeDeployCommand, target delivery.ExecutionTarget) (delivery.RuntimeObservation, error) {
	if err := r.recordMutableEffect(ctx, command, "start"); err != nil {
		return delivery.RuntimeObservation{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runtimeForServe.Start(ctx, command, target)
}

func (r *workspaceRetirementSuspendingRuntime) Observe(ctx context.Context, command *api.RuntimeDeployCommand, target delivery.ExecutionTarget) (delivery.RuntimeObservation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runtimeForServe.Observe(ctx, command, target)
}

func (r *workspaceRetirementSuspendingRuntime) Lifecycle(ctx context.Context, command *api.RuntimeDeployCommand, target delivery.ExecutionTarget, desired string) error {
	if desired != "absent" {
		if err := r.recordMutableEffect(ctx, command, desired); err != nil {
			return err
		}
	}
	var waitErr error
	if desired == "suspended" {
		r.suspendOnce.Do(func() {
			close(r.suspendEntered)
			select {
			case <-r.releaseSuspend:
			case <-ctx.Done():
				waitErr = ctx.Err()
			}
		})
	}
	if waitErr != nil {
		return waitErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runtimeForServe.Lifecycle(ctx, command, target, desired)
}

func TestWorkspaceRetirementWaitsForInFlightExclusiveSuspension(t *testing.T) {
	s, request, root, next, _ := exclusiveSwitchFixture(t)
	if limit := s.DB.Stats().MaxOpenConnections; limit != 2 {
		t.Fatalf("product connection pool must retain the harness limit of two: %d", limit)
	}
	// Observe the same isolated database without lending the product pool an
	// extra connection. Resolve its identity before either owner holds a lock.
	var database string
	if err := s.DB.QueryRowContext(t.Context(), `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatalf("resolve isolated database for the lock observer: %v", err)
	}
	observerDSN, err := url.Parse(os.Getenv(ownerstoretest.AdminDSNEnv))
	if err != nil {
		t.Fatalf("parse administrator DSN for the lock observer: %v", err)
	}
	observerDSN.Path = "/" + database
	observer, err := sql.Open("postgres", observerDSN.String())
	if err != nil {
		t.Fatalf("open isolated database observer: %v", err)
	}
	t.Cleanup(func() { _ = observer.Close() })
	if err = observer.PingContext(t.Context()); err != nil {
		t.Fatalf("connect isolated database observer: %v", err)
	}
	releaseSuspend := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseSuspend) }) }
	provider := &workspaceRetirementSuspendingRuntime{
		db: observer, workspace: request.GetWorkspaceId(),
		suspendEntered: make(chan struct{}), releaseSuspend: releaseSuspend,
	}
	s.Runtime = provider

	// Give the concurrent owner entry its own authorization fake: only the
	// database and provider are shared, just as across separate Serve processes.
	identity := &fakeIdentity{}
	retiring, err := delivery.New(s.DB, ownerAuthorizer(identity))
	if err != nil {
		t.Fatal(err)
	}
	retiring.Identity, retiring.Resources, retiring.Runtime = identity, s.Resources, provider
	update := &api.UpdateWorkspaceVersionRpcRequest{
		Context: call("tenant-alpha", false), WorkspaceId: request.GetWorkspaceId(),
		Body: &api.UpdateWorkspaceVersionRequest{CapabilityVersionId: proto.String(next.GetId()), ExpectedCurrentAgentDeploymentId: root.GetDeploymentId()},
	}
	update.Context.IdempotencyKey = "exclusive-upgrade-racing-retirement"
	upgradeCtx, cancelUpgrade := context.WithCancel(serveContext())
	retireCtx, cancelRetire := context.WithCancel(workspaceContext())
	type result struct {
		operation *api.Operation
		err       error
	}
	upgradeResult, retirementResult := make(chan result, 1), make(chan result, 1)
	upgradeExited, retirementExited := make(chan struct{}), make(chan struct{})
	retirementStarted := false
	t.Cleanup(func() {
		release()
		cancelUpgrade()
		cancelRetire()
		<-upgradeExited
		if retirementStarted {
			<-retirementExited
		}
	})
	go func() {
		defer close(upgradeExited)
		operation, err := s.UpdateWorkspaceVersion(upgradeCtx, update)
		upgradeResult <- result{operation, err}
	}()
	select {
	case <-provider.suspendEntered:
	case early := <-upgradeResult:
		t.Fatalf("exclusive upgrade did not suspend its predecessor: %v %v", early.operation, early.err)
	}

	// Identify the real holder while the external suspended call is in flight.
	// Both key halves and the database identity must match lockWorkspace.
	const workspaceLock = `locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database())
 AND classid=((hashtextextended($1,0) >> 32) & 4294967295)::oid
 AND objid=(hashtextextended($1,0) & 4294967295)::oid AND objsubid=1`
	lockKey := "serve-workspace:" + request.GetWorkspaceId()
	var holderPID int
	if err = observer.QueryRowContext(t.Context(), `SELECT pid FROM pg_locks WHERE granted AND `+workspaceLock, lockKey).Scan(&holderPID); err != nil {
		t.Fatalf("in-flight suspension does not hold the Workspace owner lock: %v", err)
	}
	var runtimes int
	if err = observer.QueryRowContext(t.Context(), `SELECT count(*) FROM serve.agent_runtime_instances WHERE workspace_id=$1`, request.GetWorkspaceId()).Scan(&runtimes); err != nil || runtimes != 2 {
		t.Fatalf("exclusive upgrade did not persist root and successor before suspension: count=%d error=%v", runtimes, err)
	}
	retirementStarted = true
	go func() {
		defer close(retirementExited)
		operation, err := retiring.Retire(retireCtx, &api.RuntimeStopCommand{
			Context: request.Context, RuntimeInstanceId: root.GetRuntimeInstanceId(), DeploymentId: root.GetDeploymentId(),
			RetainedDataAttachmentId: request.GetDataAttachmentId(),
		})
		retirementResult <- result{operation, err}
	}()
	for {
		select {
		case early := <-retirementResult:
			t.Fatalf("retirement did not wait for the in-flight suspension: %v %v", early.operation, early.err)
		default:
		}
		var waiting bool
		if err = observer.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE NOT granted AND `+workspaceLock+` AND $2::integer=ANY(pg_blocking_pids(pid)))`, lockKey, holderPID).Scan(&waiting); err != nil {
			t.Fatalf("read retirement's Workspace lock wait: %v", err)
		}
		if waiting {
			break
		}
	}
	var rootState string
	var intents int
	if err = observer.QueryRowContext(t.Context(), `SELECT status FROM serve.agent_runtime_instances WHERE id=$1`, root.GetRuntimeInstanceId()).Scan(&rootState); err != nil || rootState != "ready" {
		t.Fatalf("in-flight suspension committed state before its provider result: state=%s error=%v", rootState, err)
	}
	if err = observer.QueryRowContext(t.Context(), `SELECT count(*) FROM serve.agent_runtime_actions a JOIN serve.agent_runtime_instances i ON i.id=a.runtime_instance_id WHERE i.workspace_id=$1 AND a.action='terminate'`, request.GetWorkspaceId()).Scan(&intents); err != nil || intents != 0 {
		t.Fatalf("waiting retirement crossed the Workspace owner lock: terminate intents=%d error=%v", intents, err)
	}

	// Let PostgreSQL serialize the two continuations; either scheduling outcome
	// must end with complete closeout, never a later start or writer restore.
	release()
	closed := <-retirementResult
	upgraded := <-upgradeResult
	if closed.err != nil || closed.operation.GetResourceId() != root.GetRuntimeInstanceId() || closed.operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || closed.operation.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED {
		t.Fatalf("concurrent retirement did not complete: %v %v; upgrade=%v %v", closed.operation, closed.err, upgraded.operation, upgraded.err)
	}
	assertClosed := func() {
		t.Helper()
		var total, terminated int
		if err := observer.QueryRowContext(t.Context(), `SELECT count(*),count(*) FILTER (WHERE status='terminated') FROM serve.agent_runtime_instances WHERE workspace_id=$1`, request.GetWorkspaceId()).Scan(&total, &terminated); err != nil || total != 2 || terminated != total {
			t.Fatalf("late upgrade overwrote runtime absence: total=%d terminated=%d error=%v", total, terminated, err)
		}
		var routed bool
		if err := observer.QueryRowContext(t.Context(), `SELECT target_execution_resource_id IS NOT NULL OR target_runtime_instance_id IS NOT NULL OR target_deployment_id IS NOT NULL FROM serve.access_bindings WHERE workspace_id=$1`, request.GetWorkspaceId()).Scan(&routed); err != nil || routed {
			t.Fatalf("late upgrade restored the deleted route: routed=%v error=%v", routed, err)
		}
		provider.mu.Lock()
		defer provider.mu.Unlock()
		if len(provider.lateEffects) != 0 {
			t.Errorf("upgrade reached provider after persisted retirement: %v", provider.lateEffects)
		}
	}
	assertClosed()
	provider.mu.Lock()
	effectsBefore := len(provider.mutableEffects)
	provider.mu.Unlock()
	// Resume the exact accepted upgrade once more after both goroutines have
	// finished. A completed reply may replay, but no provider effect may recur.
	_, _ = s.UpdateWorkspaceVersion(serveContext(), update)
	fresh := proto.Clone(update).(*api.UpdateWorkspaceVersionRpcRequest)
	fresh.Context.IdempotencyKey = "late-exclusive-upgrade-after-retirement"
	if operation, err := s.UpdateWorkspaceVersion(serveContext(), fresh); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("closed Workspace admitted a new exclusive upgrade: %v %v", operation, err)
	}
	provider.mu.Lock()
	effectsAfter := len(provider.mutableEffects)
	provider.mu.Unlock()
	if effectsAfter != effectsBefore {
		t.Errorf("late upgrade repeated provider start/suspend/resume: before=%d after=%d", effectsBefore, effectsAfter)
	}
	assertClosed()
}
