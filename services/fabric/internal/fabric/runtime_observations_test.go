package fabric

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

func observationWorkload(workspaceID string, replicas int, ready bool) []any {
	runtimeID, operationID, name := "rt_"+workspaceID, "op-"+workspaceID, "runtime-"+workspaceID
	tags := oplCostTags("acct", workspaceID, runtimeID, operationID)
	labels := stringAnyMap(k8sCostLabels(tags))
	labels["oplcloud.cn/runtime-id"] = runtimeID
	labels["oplcloud.cn/runtime-operation-id"] = k8sCostLabelValue(operationID)
	template := map[string]any{"metadata": map[string]any{"labels": labels}, "spec": map[string]any{"containers": []any{map[string]any{"name": "workspace", "image": "example/image@sha256:" + strings.Repeat("a", 64)}}}}
	count := 0
	if ready {
		count = 1
	}
	deployment := map[string]any{"kind": "Deployment", "metadata": map[string]any{"uid": "deploy-" + workspaceID, "name": name, "generation": float64(2), "labels": labels, "annotations": stringAnyMap(tags)}, "spec": map[string]any{"replicas": float64(replicas), "template": template}, "status": map[string]any{"observedGeneration": float64(2), "updatedReplicas": float64(count), "readyReplicas": float64(count), "availableReplicas": float64(count)}}
	result := []any{deployment}
	if replicas == 0 {
		return result
	}
	rs := map[string]any{"kind": "ReplicaSet", "metadata": map[string]any{"uid": "rs-" + workspaceID, "name": name + "-rs", "labels": labels, "ownerReferences": []any{map[string]any{"kind": "Deployment", "uid": "deploy-" + workspaceID, "controller": true}}}, "spec": map[string]any{"replicas": float64(1), "template": template}}
	phase, condition := "Pending", "False"
	if ready {
		phase, condition = "Running", "True"
	}
	pod := map[string]any{"kind": "Pod", "metadata": map[string]any{"uid": "pod-" + workspaceID, "name": name + "-pod", "labels": labels, "ownerReferences": []any{map[string]any{"kind": "ReplicaSet", "uid": "rs-" + workspaceID, "controller": true}}}, "status": map[string]any{"phase": phase, "conditions": []any{map[string]any{"type": "Ready", "status": condition}}}}
	return append(result, rs, pod)
}

func inventoryProvider(t *testing.T, items []any) *TencentProvider {
	t.Helper()
	p := NewTencentProvider()
	p.kubectl = func(_ context.Context, args []string, _ []byte) ([]byte, error) {
		if !reflect.DeepEqual(args, []string{"get", "deployment,replicaset,pod", "-l", "oplcloud.cn/workspace-id", "-o", "json"}) {
			t.Fatalf("unexpected mutation or read: %#v", args)
		}
		return mustJSON(map[string]any{"kind": "List", "items": items}), nil
	}
	return p
}

func TestRuntimeObservationsRetainUnregisteredPhysicalObjects(t *testing.T) {
	items := []any{}
	for i := 0; i < 24; i++ {
		items = append(items, observationWorkload(fmt.Sprintf("ws-%d", i), 1, i == 0)...)
	}
	store := NewMemoryOperationStore()
	for i := 0; i < 2; i++ {
		ws := fmt.Sprintf("ws-%d", i)
		runtime := WorkspaceRuntime{ID: "rt_" + ws, OperationID: "op-" + ws, WorkspaceID: ws, ServiceName: "runtime-" + ws, Status: "running"}
		op := newOperation("create_workspace_runtime", "workspace_runtime", ws, "acct", ws, "op-"+ws, "hash", time.Now())
		op.ID = "fop-" + ws
		op.Status = "succeeded"
		op.RedactedProviderPayload = map[string]any{"resource": runtime}
		if err := store.Append(context.Background(), op); err != nil {
			t.Fatal(err)
		}
	}
	service := NewServiceWithOperationStore(inventoryProvider(t, items), store)
	before, _ := store.List(context.Background())
	result, err := service.RuntimeObservations(context.Background())
	if err != nil || len(result.Items) != 24 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	verified, unregistered := 0, 0
	for _, item := range result.Items {
		switch item.Ownership {
		case contracts.RuntimeOwnershipVerified:
			verified++
		case contracts.RuntimeOwnershipUnregistered:
			unregistered++
		}
	}
	if verified != 2 || unregistered != 22 {
		t.Fatalf("verified=%d unregistered=%d result=%#v", verified, unregistered, result)
	}
	after, _ := store.List(context.Background())
	if !reflect.DeepEqual(before, after) {
		t.Fatal("observation changed owner records")
	}
}

func TestRuntimeObservationUsesCurrentGenerationAndPodController(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func([]any)
		state  contracts.ResourceObservedState
		reason string
		fail   bool
	}{
		{name: "running", state: contracts.ResourceObservedRunning},
		{name: "old generation", change: func(items []any) {
			items[0].(map[string]any)["status"].(map[string]any)["observedGeneration"] = float64(1)
		}, state: contracts.ResourceObservedPending, reason: "runtime_generation_pending"},
		{name: "stale ready pod template", change: func(items []any) {
			items[1].(map[string]any)["spec"].(map[string]any)["template"] = map[string]any{"spec": map[string]any{"containers": []any{map[string]any{"image": "old"}}}}
		}, state: contracts.ResourceObservedPending, reason: "runtime_workload_pending"},
		{name: "pod missing controller", change: func(items []any) { delete(items[2].(map[string]any)["metadata"].(map[string]any), "ownerReferences") }, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items := observationWorkload("ws", 1, true)
			if tc.change != nil {
				tc.change(items)
			}
			result, err := inventoryProvider(t, items).readRuntimeObservations(context.Background())
			if tc.fail {
				if err == nil {
					t.Fatal("orphan pod accepted")
				}
				return
			}
			if err != nil || len(result) != 1 || result[0].ObservedState != tc.state || result[0].ReasonCode != tc.reason {
				t.Fatalf("result=%#v err=%v", result, err)
			}
		})
	}
}

func TestRuntimeObservationSuspensionAndLegacySummaryAreDistinct(t *testing.T) {
	p := inventoryProvider(t, observationWorkload("ws", 0, false))
	items, err := p.readRuntimeObservations(context.Background())
	if err != nil || items[0].ObservedState != contracts.ResourceObservedSuspended || items[0].DesiredState != contracts.ResourceObservedSuspended {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	summary, err := p.RuntimeHealthSummary(context.Background())
	if err != nil || summary.Total != 1 || summary.Ready != 0 || summary.Unready != 1 {
		t.Fatalf("summary=%#v err=%v", summary, err)
	}
}

func TestRuntimeObservationsCannotHideOrphanPods(t *testing.T) {
	items := observationWorkload("ws", 1, true)[1:]
	if _, err := NewService(inventoryProvider(t, items)).RuntimeObservations(context.Background()); err == nil {
		t.Fatal("live pod without deployment became empty inventory")
	}
}

type observationDockerRunner struct {
	container dockerContainerInspect
	calls     [][]string
}

func (r *observationDockerRunner) Run(_ context.Context, _ []byte, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	switch {
	case reflect.DeepEqual(args, []string{"container", "ls", "-a", "--filter", "label=opl.fabric.kind=runtime", "--format", "{{.Names}}"}):
		return []byte(r.container.Name + "\n"), nil
	case len(args) == 8 && args[0] == "container" && args[1] == "ls":
		return mustJSON(dockerObjectInventoryRow{ID: r.container.ID, Names: r.container.Name}), nil
	case len(args) == 3 && args[0] == "container" && args[1] == "inspect" && args[2] == r.container.ID:
		return mustJSON([]dockerContainerInspect{r.container}), nil
	default:
		return nil, fmt.Errorf("unexpected docker mutation or read: %v", args)
	}
}

func TestLocalRuntimeObservationsRetainUnregisteredStoppedContainer(t *testing.T) {
	ws := "ws-local"
	runner := &observationDockerRunner{}
	runner.container.ID, runner.container.Name = "container", localRuntimeName(ws)
	runner.container.Config.Labels = localDockerLabels("acct", ws, localRuntimeID(ws), "op-local", "runtime")
	runner.container.State.Status = "exited"
	provider := newLocalDockerProvider(localDockerStorageTestConfig(localDockerStorageTestRoot(t)), runner)
	service := NewService(provider)
	result, err := service.RuntimeObservations(context.Background())
	if err != nil || len(result.Items) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	item := result.Items[0]
	if item.WorkspaceID != ws || item.Ownership != contracts.RuntimeOwnershipUnregistered || item.ObservedState != contracts.ResourceObservedSuspended || item.DesiredState != contracts.ResourceObservedUnknown {
		t.Fatalf("unregistered container=%#v", item)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("unexpected reads=%#v", runner.calls)
	}
}

func TestLocalRuntimeObservationUsesPersistedPowerTarget(t *testing.T) {
	ws := "ws-local"
	runner := &observationDockerRunner{}
	runner.container.ID, runner.container.Name = "container", localRuntimeName(ws)
	runtimeID := localRuntimeID(ws)
	runner.container.Config.Labels = localDockerLabels("acct", ws, runtimeID, "op-local", "runtime")
	runner.container.State.Status = "exited"
	provider := newLocalDockerProvider(localDockerStorageTestConfig(localDockerStorageTestRoot(t)), runner)
	store := NewMemoryOperationStore()
	owner := newOperation("create_workspace_runtime", "workspace_runtime", ws, "acct", ws, "op-local", "create", time.Now())
	owner.ID, owner.Status = "owner", "succeeded"
	fillOperationResource(&owner, WorkspaceRuntime{ID: runtimeID, OperationID: "op-local", WorkspaceID: ws, ServiceName: runner.container.Name})
	if err := store.Append(context.Background(), owner); err != nil {
		t.Fatal(err)
	}
	service := NewServiceWithOperationStore(provider, store)
	before, err := service.RuntimeObservations(context.Background())
	if err != nil || before.Items[0].DesiredState != contracts.ResourceObservedRunning {
		t.Fatalf("stopped container must not imply authorized suspension: %#v %v", before, err)
	}
	input := WorkspaceRuntimePowerInput{SchemaVersion: 1, AccountID: "acct", WorkspaceID: ws, RuntimeID: runtimeID, RuntimeOperationID: "op-local", PaidThrough: time.Now().Add(-time.Hour).Format(time.RFC3339), DesiredState: "suspended", IdempotencyKey: "power"}
	power := newOperation(workspaceRuntimePowerAction, "workspace_runtime_power", ws, "acct", ws, input.IdempotencyKey, hashInput(input), time.Now())
	power.ID, power.Status = "power", "succeeded"
	power.RedactedProviderPayload = map[string]any{"power": input}
	if err := store.Append(context.Background(), power); err != nil {
		t.Fatal(err)
	}
	result, err := service.RuntimeObservations(context.Background())
	if err != nil || result.Items[0].Ownership != contracts.RuntimeOwnershipVerified || result.Items[0].DesiredState != contracts.ResourceObservedSuspended || result.Items[0].ObservedState != contracts.ResourceObservedSuspended {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
