package fabric

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func appendD4RuntimeOwner(t *testing.T, store OperationStore, input WorkspaceRuntimePowerInput, provider string) {
	t.Helper()
	now := time.Now().Add(-time.Hour)
	op := newOperation("create_workspace_runtime", "workspace_runtime", input.WorkspaceID, input.AccountID, input.WorkspaceID, input.RuntimeOperationID, "runtime-create", now)
	op.ID = "d4-original-runtime"
	op.Status = "succeeded"
	op.CreatedAt = now
	op.FinishedAt = now
	fillOperationResource(&op, WorkspaceRuntime{ID: input.RuntimeID, OperationID: input.RuntimeOperationID, WorkspaceID: input.WorkspaceID, ServiceName: "runtime-original", Status: "running", Ready: true, CostTags: oplCostTags(input.AccountID, input.WorkspaceID, input.RuntimeID, input.RuntimeOperationID)})
	op.Provider = provider
	if err := store.Append(context.Background(), op); err != nil {
		t.Fatal(err)
	}
}

type d4PowerKubernetes struct {
	*d4KubernetesFixture
	scales       int
	lostResponse bool
	keepPod      bool
}

func newD4PowerKubernetes(input WorkspaceRuntimePowerInput) *d4PowerKubernetes {
	labels := k8sCostLabels(oplCostTags(input.AccountID, input.WorkspaceID, input.RuntimeID, input.RuntimeOperationID))
	labels["oplcloud.cn/runtime-id"] = input.RuntimeID
	deployment := map[string]any{"kind": "Deployment", "metadata": map[string]any{"name": "runtime-original", "generation": 1, "resourceVersion": "1", "labels": labels, "annotations": map[string]string{"opl_operation_id": input.RuntimeOperationID}}, "spec": map[string]any{"replicas": 1}, "status": map[string]any{"observedGeneration": 1, "readyReplicas": 1, "availableReplicas": 1}}
	pod := map[string]any{"kind": "Pod", "metadata": map[string]any{"name": "original-pod", "labels": map[string]string{"oplcloud.cn/workspace-id": input.WorkspaceID}}}
	return &d4PowerKubernetes{d4KubernetesFixture: newD4KubernetesFixture(deployment, pod)}
}

func (f *d4PowerKubernetes) run(ctx context.Context, args []string, stdin []byte) ([]byte, error) {
	if args[0] != "scale" {
		return f.d4KubernetesFixture.run(ctx, args, stdin)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	deployment := f.items[args[1]]
	if deployment == nil {
		return nil, fmt.Errorf("missing deployment")
	}
	if args[4] != "--resource-version="+stringValue(nested(deployment, "metadata", "resourceVersion")) {
		return nil, fmt.Errorf("resource version conflict")
	}
	target, err := strconv.Atoi(strings.TrimPrefix(args[2], "--replicas="))
	if err != nil {
		return nil, err
	}
	if target != 0 && target != 1 {
		return nil, fmt.Errorf("unexpected replica count")
	}
	f.scales++
	deployment["spec"].(map[string]any)["replicas"] = target
	deployment["status"].(map[string]any)["readyReplicas"] = target
	deployment["status"].(map[string]any)["availableReplicas"] = target
	deployment["metadata"].(map[string]any)["resourceVersion"] = strconv.Itoa(f.scales + 1)
	deployment["metadata"].(map[string]any)["generation"] = f.scales + 1
	deployment["status"].(map[string]any)["observedGeneration"] = f.scales + 1
	if target == 0 && !f.keepPod {
		delete(f.items, "pod/original-pod")
	}
	if f.lostResponse {
		f.lostResponse = false
		return nil, context.DeadlineExceeded
	}
	return nil, nil
}

func TestD4RuntimePowerTencentExpiryRenewalAndStaleRequestAcrossRestart(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(fmt.Sprint(postgres), func(t *testing.T) {
			ctx := context.Background()
			var store OperationStore = NewMemoryOperationStore()
			var databaseURL string
			if postgres {
				databaseURL = fabricTestDatabaseURL(t)
				pg, err := newTestPostgresOperationStore(databaseURL)
				if err != nil {
					t.Fatal(err)
				}
				defer pg.client.Close()
				store = pg
			}
			now := time.Now().UTC()
			input := WorkspaceRuntimePowerInput{SchemaVersion: 1, AccountID: "acct-power", WorkspaceID: "ws-power", RuntimeID: "rt_power", RuntimeOperationID: "launch-power:runtime", PaidThrough: now.Add(-time.Minute).Format(time.RFC3339Nano), DesiredState: "suspended", IdempotencyKey: "expire-original-period"}
			appendD4RuntimeOwner(t, store, input, "tencent-tke")
			fixture := newD4PowerKubernetes(input)
			fixture.lostResponse = true
			provider := NewTencentProvider()
			provider.kubectl = fixture.run
			service := NewServiceWithOperationStore(provider, store)
			if _, err := service.SetWorkspaceRuntimePower(ctx, input); !errors.Is(err, context.DeadlineExceeded) || fixture.scales != 1 {
				t.Fatalf("lost response err=%v scales=%d", err, fixture.scales)
			}
			if postgres {
				pg, err := newTestPostgresOperationStore(databaseURL)
				if err != nil {
					t.Fatal(err)
				}
				defer pg.client.Close()
				store = pg
			}
			service = NewServiceWithOperationStore(provider, store)
			result, err := service.SetWorkspaceRuntimePower(ctx, input)
			if err != nil || result.State != "suspended" || fixture.scales != 1 {
				t.Fatalf("expiry readback=%#v err=%v scales=%d", result, err, fixture.scales)
			}
			renewed := input
			renewed.PaidThrough = now.Add(30 * 24 * time.Hour).Format(time.RFC3339Nano)
			renewed.DesiredState = "running"
			renewed.IdempotencyKey = "renew-original-runtime"
			result, err = service.SetWorkspaceRuntimePower(ctx, renewed)
			if err != nil || result.State != "running" || fixture.scales != 2 {
				t.Fatalf("renewal=%#v err=%v scales=%d", result, err, fixture.scales)
			}
			for range 2 {
				if _, err := NewServiceWithOperationStore(provider, store).SetWorkspaceRuntimePower(ctx, input); !errors.Is(err, ErrWorkspaceRuntimePowerConflict) || fixture.scales != 2 {
					t.Fatalf("old expiry overwrote renewal err=%v scales=%d", err, fixture.scales)
				}
			}
			unpaid := input
			unpaid.DesiredState = "running"
			unpaid.IdempotencyKey = "unpaid-start"
			if _, err := service.SetWorkspaceRuntimePower(ctx, unpaid); !errors.Is(err, ErrWorkspaceRuntimePowerConflict) {
				t.Fatalf("unpaid resume=%v", err)
			}
			premature := renewed
			premature.DesiredState = "suspended"
			premature.IdempotencyKey = "early-stop"
			if _, err := service.SetWorkspaceRuntimePower(ctx, premature); !errors.Is(err, ErrWorkspaceRuntimePowerConflict) {
				t.Fatalf("early suspend=%v", err)
			}
			latest, found, err := store.LatestResourceOperation(ctx, "workspace_runtime_power", input.WorkspaceID)
			var evidence WorkspaceRuntimePowerResult
			if err != nil || !found || latest.Status != "succeeded" || latest.FinishedAt.IsZero() || !decodeWorkspaceLaunchCloseoutPayload(latest.RedactedProviderPayload["result"], &evidence) || evidence.Binding != renewed {
				t.Fatalf("missing persisted power evidence=%#v err=%v", latest, err)
			}
			deletion := newOperation("destroy_workspace_runtime", "workspace_runtime", input.WorkspaceID, input.AccountID, input.WorkspaceID, "delete-original", "delete", time.Now())
			deletion.ID = "d4-delete-started"
			deletion.Status = "started"
			deletion.CreatedAt = time.Now()
			if err := store.Append(ctx, deletion); err != nil {
				t.Fatal(err)
			}
			if _, err := service.SetWorkspaceRuntimePower(ctx, renewed); !errors.Is(err, ErrWorkspaceRuntimePowerConflict) || fixture.scales != 2 {
				t.Fatalf("deleting runtime revived err=%v scales=%d", err, fixture.scales)
			}
		})
	}
}

func TestD4RuntimePowerTencentDoesNotConfirmSuspensionWhileOldPodStillRuns(t *testing.T) {
	input := WorkspaceRuntimePowerInput{SchemaVersion: 1, AccountID: "acct-power", WorkspaceID: "ws-power", RuntimeID: "rt_power", RuntimeOperationID: "launch-power:runtime", PaidThrough: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), DesiredState: "suspended", IdempotencyKey: "expire-original"}
	fixture := newD4PowerKubernetes(input)
	fixture.keepPod = true
	provider := NewTencentProvider()
	provider.kubectl = fixture.run
	store := NewMemoryOperationStore()
	appendD4RuntimeOwner(t, store, input, "tencent-tke")
	service := NewServiceWithOperationStore(provider, store)
	result, err := service.SetWorkspaceRuntimePower(context.Background(), input)
	if err != nil || result.State != "pending" || fixture.scales != 1 {
		t.Fatalf("running old Pod=%#v err=%v", result, err)
	}
	delete(fixture.items, "pod/original-pod")
	result, err = service.SetWorkspaceRuntimePower(context.Background(), input)
	if err != nil || result.State != "suspended" || fixture.scales != 1 {
		t.Fatalf("pod absence=%#v err=%v scales=%d", result, err, fixture.scales)
	}
}

type d4PowerDocker struct {
	mu            sync.Mutex
	container     dockerContainerInspect
	present       bool
	starts, stops int
	lostResponse  bool
}

func TestD4RuntimePowerTencentWaitsForCurrentDeploymentReadiness(t *testing.T) {
	input := WorkspaceRuntimePowerInput{SchemaVersion: 1, AccountID: "acct-power", WorkspaceID: "ws-power", RuntimeID: "rt_power", RuntimeOperationID: "launch-power:runtime", PaidThrough: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), DesiredState: "running", IdempotencyKey: "renewal-readiness"}
	fixture := newD4PowerKubernetes(input)
	deployment := fixture.items["deployment/runtime-original"]
	deployment["metadata"].(map[string]any)["generation"] = 2
	provider := NewTencentProvider()
	provider.kubectl = fixture.run
	result, err := provider.SetWorkspaceRuntimePower(context.Background(), input)
	if err != nil || result.State != "pending" || fixture.scales != 0 {
		t.Fatalf("old generation=%#v err=%v scales=%d", result, err, fixture.scales)
	}
	deployment["status"].(map[string]any)["observedGeneration"] = 2
	result, err = provider.SetWorkspaceRuntimePower(context.Background(), input)
	if err != nil || result.State != "running" || fixture.scales != 0 {
		t.Fatalf("current ready generation=%#v err=%v scales=%d", result, err, fixture.scales)
	}
}

func (r *d4PowerDocker) Run(_ context.Context, _ []byte, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(args) < 2 || args[0] != "container" {
		return nil, fmt.Errorf("unexpected docker %v", args)
	}
	switch args[1] {
	case "ls":
		if !r.present {
			return nil, nil
		}
		return json.Marshal(map[string]string{"ID": r.container.ID, "Names": strings.TrimPrefix(r.container.Name, "/")})
	case "inspect":
		return json.Marshal([]dockerContainerInspect{r.container})
	case "stop", "start":
		if args[2] != r.container.ID {
			return nil, fmt.Errorf("wrong original container")
		}
		r.container.State.Running = args[1] == "start"
		r.container.State.Status = "exited"
		if r.container.State.Running {
			r.container.State.Status = "running"
			r.starts++
		} else {
			r.stops++
		}
		if r.lostResponse {
			r.lostResponse = false
			return nil, context.DeadlineExceeded
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected docker mutation %v", args)
}

func TestD4RuntimePowerLocalPreservesContainerAndNeverRecreatesAbsentRuntime(t *testing.T) {
	ctx := context.Background()
	input := WorkspaceRuntimePowerInput{SchemaVersion: 1, AccountID: "acct-local", WorkspaceID: "ws-local", RuntimeID: localRuntimeID("ws-local"), RuntimeOperationID: "launch-local:runtime", PaidThrough: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), DesiredState: "suspended", IdempotencyKey: "local-stop"}
	runner := &d4PowerDocker{present: true, lostResponse: true}
	labels := localDockerLabels(input.AccountID, input.WorkspaceID, input.RuntimeID, input.RuntimeOperationID, "runtime")
	labels["opl.image.ref"] = "sha256:" + strings.Repeat("a", 64)
	runner.container = localDockerReadyRuntimeContainer(t, localRuntimeName(input.WorkspaceID), "original-container-id", labels["opl.image.ref"], labels, localDockerStoragePaths{}, "")
	provider := newLocalDockerProvider(localDockerStorageTestConfig(localDockerStorageTestRoot(t)), runner)
	store := NewMemoryOperationStore()
	appendD4RuntimeOwner(t, store, input, "local-docker")
	service := NewServiceWithOperationStore(provider, store)
	if _, err := service.SetWorkspaceRuntimePower(ctx, input); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop response=%v", err)
	}
	result, err := NewServiceWithOperationStore(provider, store).SetWorkspaceRuntimePower(ctx, input)
	if err != nil || result.State != "suspended" || runner.stops != 1 {
		t.Fatalf("stopped=%#v err=%v stops=%d", result, err, runner.stops)
	}
	renewed := input
	renewed.PaidThrough = time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	renewed.DesiredState = "running"
	renewed.IdempotencyKey = "local-paid-resume"
	runner.container.State.Health.Status = "starting"
	result, err = service.SetWorkspaceRuntimePower(ctx, renewed)
	if err != nil || result.State != "pending" || runner.starts != 1 {
		t.Fatalf("starting=%#v err=%v starts=%d", result, err, runner.starts)
	}
	result, err = service.SetWorkspaceRuntimePower(ctx, renewed)
	if err != nil || result.State != "pending" || runner.starts != 1 {
		t.Fatalf("starting retry=%#v err=%v starts=%d", result, err, runner.starts)
	}
	runner.container.State.Health.Status = "healthy"
	result, err = service.SetWorkspaceRuntimePower(ctx, renewed)
	if err != nil || result.State != "running" || runner.starts != 1 || runner.container.ID != "original-container-id" {
		t.Fatalf("resume=%#v err=%v", result, err)
	}
	runner.present = false
	renewed.PaidThrough = time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339Nano)
	renewed.IdempotencyKey = "missing-runtime-resume"
	result, err = service.SetWorkspaceRuntimePower(ctx, renewed)
	if err != nil || result.State != "absent" || runner.starts != 1 {
		t.Fatalf("absent revived=%#v err=%v", result, err)
	}
	runner.present = true
	runner.container.Config.Labels["opl.account.id"] = "foreign"
	if _, err := provider.SetWorkspaceRuntimePower(ctx, input); !errors.Is(err, ErrWorkspaceRuntimePowerConflict) || runner.stops != 1 {
		t.Fatalf("foreign stopped err=%v", err)
	}
}
