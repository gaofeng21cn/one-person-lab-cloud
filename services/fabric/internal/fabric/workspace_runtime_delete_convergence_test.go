package fabric

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// labelledRuntimeObject builds one owned Runtime object as the cluster reports it.
func labelledRuntimeObject(kind, name, workspaceID string) map[string]any {
	return map[string]any{"kind": kind, "metadata": map[string]any{"name": name, "labels": map[string]any{"oplcloud.cn/workspace-id": workspaceID}}}
}

// runtimeDeleteKubectlStub answers the labelled reads from a mutable object set
// and applies ordinary kubectl deletion semantics to it.
type runtimeDeleteKubectlStub struct {
	workspaceID string
	objects     []map[string]any
	calls       [][]string
	persist     bool
}

func (s *runtimeDeleteKubectlStub) run(_ context.Context, args []string, _ []byte) ([]byte, error) {
	s.calls = append(s.calls, append([]string(nil), args...))
	if args[0] == "delete" {
		if !s.persist {
			return nil, nil
		}
		remaining := make([]map[string]any, 0, len(s.objects))
		for _, object := range s.objects {
			ref := strings.ToLower(stringValue(object["kind"])) + "/" + stringValue(nested(object, "metadata", "name"))
			if !slices.Contains(args[1:], ref) {
				remaining = append(remaining, object)
			}
		}
		s.objects = remaining
		return nil, nil
	}
	if strings.HasPrefix(args[1], "secret/") {
		return nil, nil
	}
	// kubectl answers only the requested kinds, so the stub does too.
	wanted := strings.Split(args[1], ",")
	items := make([]any, 0, len(s.objects))
	for _, object := range s.objects {
		if !slices.Contains(wanted, strings.ToLower(stringValue(object["kind"]))) {
			continue
		}
		items = append(items, object)
	}
	return mustJSON(map[string]any{"kind": "List", "items": items}), nil
}

func (s *runtimeDeleteKubectlStub) deleteTarget() []string {
	for _, call := range s.calls {
		if len(call) > 1 && call[0] == "delete" {
			return call
		}
	}
	return nil
}

func TestTencentDestroyWorkspaceRuntimeRemovesControllerAndChildren(t *testing.T) {
	stub := &runtimeDeleteKubectlStub{workspaceID: "ws-alpha", persist: true, objects: []map[string]any{
		labelledRuntimeObject("Deployment", "opl-compute-alpha", "ws-alpha"),
		labelledRuntimeObject("ReplicaSet", "opl-compute-alpha-7d6c", "ws-alpha"),
		// A normal rolling update retains its old ReplicaSet and may overlap Pods.
		labelledRuntimeObject("ReplicaSet", "opl-compute-alpha-previous", "ws-alpha"),
		labelledRuntimeObject("Pod", "opl-compute-alpha-7d6c-4xk9", "ws-alpha"),
		labelledRuntimeObject("Pod", "opl-compute-alpha-previous-6qh2", "ws-alpha"),
		labelledRuntimeObject("Service", "opl-compute-alpha", "ws-alpha"),
		labelledRuntimeObject("NetworkPolicy", "opl-compute-alpha", "ws-alpha"),
		labelledRuntimeObject("Secret", "opl-compute-alpha-env", "ws-alpha"),
	}}
	provider := NewTencentProvider()
	provider.kubectl = stub.run

	runtime, err := provider.DestroyWorkspaceRuntime(context.Background(), "ws-alpha")
	if err != nil || runtime.Status != "destroyed" || runtime.WorkspaceID != "ws-alpha" {
		t.Fatalf("runtime=%#v err=%v", runtime, err)
	}
	target := stub.deleteTarget()
	for _, want := range []string{"deployment/opl-compute-alpha", "replicaset/opl-compute-alpha-7d6c", "replicaset/opl-compute-alpha-previous", "pod/opl-compute-alpha-7d6c-4xk9", "pod/opl-compute-alpha-previous-6qh2", "service/opl-compute-alpha", "networkpolicy/opl-compute-alpha", "secret/opl-compute-alpha-env", "--wait=true"} {
		if !slices.Contains(target, want) {
			t.Fatalf("delete %#v missing %q", target, want)
		}
	}
	observation, err := provider.ObserveWorkspaceRuntimeDelete(context.Background(), "ws-alpha")
	if err != nil || observation.State != WorkspaceOwnerObservationAbsent {
		t.Fatalf("post-destroy observation=%#v err=%v", observation, err)
	}
}

func TestTencentDestroyWorkspaceRuntimeConvergesOrphanChildrenWithoutController(t *testing.T) {
	// The Deployment is already gone but its children remain labelled. The old
	// name-based path deleted nothing here and could never converge.
	stub := &runtimeDeleteKubectlStub{workspaceID: "ws-alpha", persist: true, objects: []map[string]any{
		labelledRuntimeObject("ReplicaSet", "opl-compute-alpha-7d6c", "ws-alpha"),
		labelledRuntimeObject("Pod", "opl-compute-alpha-7d6c-4xk9", "ws-alpha"),
	}}
	provider := NewTencentProvider()
	provider.kubectl = stub.run

	if _, err := provider.DestroyWorkspaceRuntime(context.Background(), "ws-alpha"); err != nil {
		t.Fatalf("orphan children did not converge: %v", err)
	}
	target := stub.deleteTarget()
	if !slices.Contains(target, "replicaset/opl-compute-alpha-7d6c") || !slices.Contains(target, "pod/opl-compute-alpha-7d6c-4xk9") {
		t.Fatalf("delete %#v did not retire the orphan children", target)
	}
	if observation, err := provider.ObserveWorkspaceRuntimeDelete(context.Background(), "ws-alpha"); err != nil || observation.State != WorkspaceOwnerObservationAbsent {
		t.Fatalf("observation=%#v err=%v", observation, err)
	}
}

func TestTencentDestroyWorkspaceRuntimeStaysPendingWhileControllerSurvives(t *testing.T) {
	// The delete reports success but the controller is still there: the Runtime
	// must not claim destroyed.
	stub := &runtimeDeleteKubectlStub{workspaceID: "ws-alpha", persist: false, objects: []map[string]any{
		labelledRuntimeObject("Deployment", "opl-compute-alpha", "ws-alpha"),
		labelledRuntimeObject("Pod", "opl-compute-alpha-7d6c-4xk9", "ws-alpha"),
	}}
	provider := NewTencentProvider()
	provider.kubectl = stub.run

	runtime, err := provider.DestroyWorkspaceRuntime(context.Background(), "ws-alpha")
	if !errors.Is(err, ErrWorkspaceLaunchPending) || runtime.Status == "destroyed" {
		t.Fatalf("surviving controller reported as %#v err=%v", runtime, err)
	}
}

func TestTencentObserveWorkspaceRuntimeDeleteRequiresControllerAbsence(t *testing.T) {
	// The Pod is gone but its Deployment survives. A Pod-level read must never be
	// reported as an absent Runtime.
	stub := &runtimeDeleteKubectlStub{workspaceID: "ws-alpha", persist: true, objects: []map[string]any{
		labelledRuntimeObject("Deployment", "opl-compute-alpha", "ws-alpha"),
	}}
	provider := NewTencentProvider()
	provider.kubectl = stub.run
	observation, err := provider.ObserveWorkspaceRuntimeDelete(context.Background(), "ws-alpha")
	if err != nil || observation.State != WorkspaceRuntimeDeleteObservationPresent {
		t.Fatalf("observation=%#v err=%v", observation, err)
	}
	if len(observation.Residuals) != 1 || observation.Residuals[0].Kind != "Deployment" {
		t.Fatalf("residuals=%#v", observation.Residuals)
	}
	if _, err := provider.DestroyWorkspaceRuntime(context.Background(), "ws-alpha"); err != nil {
		t.Fatalf("destroy with a surviving controller: %v", err)
	}
	if observation, err := provider.ObserveWorkspaceRuntimeDelete(context.Background(), "ws-alpha"); err != nil || observation.State != WorkspaceOwnerObservationAbsent {
		t.Fatalf("post-destroy observation=%#v err=%v", observation, err)
	}
}

func TestTencentDestroyWorkspaceRuntimeRejectsForeignOrAmbiguousOwnershipBeforeMutation(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		objects []map[string]any
	}{
		{"two distinct runtime names", []map[string]any{
			labelledRuntimeObject("Deployment", "opl-compute-alpha", "ws-alpha"),
			labelledRuntimeObject("Deployment", "opl-compute-other", "ws-alpha"),
		}},
		{"duplicate child identity", []map[string]any{
			labelledRuntimeObject("ReplicaSet", "opl-compute-alpha-7d6c", "ws-alpha"),
			labelledRuntimeObject("ReplicaSet", "opl-compute-alpha-7d6c", "ws-alpha"),
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			stub := &runtimeDeleteKubectlStub{workspaceID: "ws-alpha", persist: true, objects: testCase.objects}
			provider := NewTencentProvider()
			provider.kubectl = stub.run
			if _, err := provider.DestroyWorkspaceRuntime(context.Background(), "ws-alpha"); err == nil {
				t.Fatal("ambiguous ownership was accepted")
			}
			if stub.deleteTarget() != nil {
				t.Fatalf("ambiguity caused a mutation: %#v", stub.calls)
			}
		})
	}
}
