package fabric

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// The operator health surface only has the readiness response to act on. This
// test drives the same provider entry point with two realistic
// `kubectl get deployment,replicaset,pod -o json` List payloads:
//
//   - a stuck rollout: the Deployment template has moved ahead of the only
//     Ready Running Pod, whose ReplicaSet still carries the previous template;
//   - an unprovable owner chain: the Pod's ReplicaSet exists but does not name
//     its owning Deployment, so the chain cannot be resolved at all.
//
// Before the split both payloads produced identity_unverified and
// failedChecks:["workspace_image_id"], so no caller could tell a rollout that
// may still converge from an integrity fault. The JSON key assertion is
// intentionally a literal: this test must compile and fail on origin/main,
// where the richer contract does not exist yet.
func TestWorkspaceImageReadinessDistinguishesStuckRolloutFromUnprovableOwnerChain(t *testing.T) {
	cloudImage := "registry.example.com/opl/cloud@sha256:" + strings.Repeat("d", 64)
	installedImage := "registry.example.com/opl/workspace@sha256:" + strings.Repeat("a", 64)
	previousImage := "registry.example.com/opl/workspace@sha256:" + strings.Repeat("b", 64)
	rolledImage := "registry.example.com/opl/workspace@sha256:" + strings.Repeat("c", 64)

	t.Setenv("OPL_WORKSPACE_DOMAIN", "workspace.example.com")
	t.Setenv("OPL_CLOUD_IMAGE", cloudImage)
	t.Setenv("OPL_WORKSPACE_IMAGE", installedImage)
	t.Setenv("OPL_K8S_NAMESPACE", "opl-cloud")
	t.Setenv("OPL_IMAGE_PULL_SECRET_NAME", "pull-secret")
	t.Setenv("OPL_WORKSPACE_STORAGE_CLASS", "cbs")
	t.Setenv("OPL_TENCENT_PROVISIONER_BIN", "/bin/true")
	t.Setenv("TENCENT_DEPLOY_KUBECONFIG_REF", "/tmp/kubeconfig")
	t.Setenv("RUN_TENCENT_CREATE_RELEASE_EXECUTION", "1")

	cloudPods := []string{
		readinessCloudPodJSON("control-plane", cloudImage),
		readinessCloudPodJSON("ledger", cloudImage),
		readinessCloudPodJSON("fabric", cloudImage),
	}
	stuckRollout := readinessKubernetesList(append(append([]string{}, cloudPods...),
		readinessWorkspaceDeploymentJSON("workspace-stuck", "uid-deploy-stuck", rolledImage, 4, 4),
		readinessWorkspaceReplicaSetJSON("workspace-stuck", "uid-rs-stuck-old", "uid-deploy-stuck", "6f7c9d8b5c", previousImage, 1, 1, true),
		readinessWorkspaceReplicaSetJSON("workspace-stuck", "uid-rs-stuck-new", "uid-deploy-stuck", "7a8b9c6d5f", rolledImage, 1, 0, true),
		readinessWorkspacePodJSON("workspace-stuck", "uid-pod-stuck-old", "uid-rs-stuck-old", "6f7c9d8b5c", previousImage, "Running", true),
		readinessWorkspacePodJSON("workspace-stuck", "uid-pod-stuck-new", "uid-rs-stuck-new", "7a8b9c6d5f", rolledImage, "Pending", false),
	)...)
	unprovableChain := readinessKubernetesList(append(append([]string{}, cloudPods...),
		readinessWorkspaceDeploymentJSON("workspace-broken", "uid-deploy-broken", rolledImage, 4, 4),
		readinessWorkspaceReplicaSetJSON("workspace-broken", "uid-rs-broken", "uid-deploy-broken", "5c6d7e8f9a", previousImage, 1, 1, false),
		readinessWorkspacePodJSON("workspace-broken", "uid-pod-broken", "uid-rs-broken", "5c6d7e8f9a", previousImage, "Running", true),
	)...)

	fixtures := []struct {
		name            string
		inventory       string
		wantStatus      string
		wantWorkspaceID string
	}{
		{name: "stuck rollout", inventory: stuckRollout, wantStatus: "rollout_incomplete", wantWorkspaceID: "workspace-stuck"},
		{name: "unprovable owner chain", inventory: unprovableChain, wantStatus: "identity_unverified", wantWorkspaceID: "workspace-broken"},
	}
	type observedReadiness struct {
		status             string
		failedChecks       []string
		workspaceIDs       []string
		workspaceImagesOK  bool
		cloudImagesOK      bool
		encodedForEvidence string
	}
	observed := make([]observedReadiness, 0, len(fixtures))
	for _, fixture := range fixtures {
		inventory := fixture.inventory
		provider := NewTencentProvider()
		provider.provision = func(context.Context, provisionerRequest) (provisionerResponse, error) {
			return provisionerResponse{OK: true}, nil
		}
		provider.kubectl = func(_ context.Context, args []string, _ []byte) ([]byte, error) {
			if !slices.Equal(args, []string{"get", "deployment,replicaset,pod", "-o", "json"}) {
				return nil, fmt.Errorf("unexpected kubectl read: %#v", args)
			}
			return []byte(inventory), nil
		}
		readiness, err := provider.Readiness(context.Background())
		if err != nil {
			t.Fatalf("%s: readiness error: %v", fixture.name, err)
		}
		encoded, err := json.Marshal(readiness)
		if err != nil {
			t.Fatalf("%s: encode readiness: %v", fixture.name, err)
		}
		var payload map[string]any
		if err := json.Unmarshal(encoded, &payload); err != nil {
			t.Fatalf("%s: decode readiness: %v", fixture.name, err)
		}
		status, _ := payload["workspaceImageStatus"].(string)
		readinessObserved := observedReadiness{
			status:             status,
			failedChecks:       readinessJSONStrings(payload["failedChecks"]),
			workspaceIDs:       readinessJSONStrings(payload["workspaceImageUnverifiedWorkspaceIds"]),
			workspaceImagesOK:  readiness.WorkspaceImagesReady,
			cloudImagesOK:      readiness.CloudImagesReady,
			encodedForEvidence: string(encoded),
		}
		observed = append(observed, readinessObserved)
		t.Logf("%s readiness: %s", fixture.name, readinessObserved.encodedForEvidence)
	}

	for index, fixture := range fixtures {
		got := observed[index]
		if got.status != fixture.wantStatus {
			t.Errorf("%s: workspaceImageStatus = %q, want %q", fixture.name, got.status, fixture.wantStatus)
		}
		if want := []string{fixture.wantWorkspaceID}; !slices.Equal(got.workspaceIDs, want) {
			t.Errorf("%s: workspaceImageUnverifiedWorkspaceIds = %#v, want %#v", fixture.name, got.workspaceIDs, want)
		}
		if want := []string{"workspace_image_id"}; !slices.Equal(got.failedChecks, want) {
			t.Errorf("%s: failedChecks = %#v, want %#v", fixture.name, got.failedChecks, want)
		}
		if got.workspaceImagesOK {
			t.Errorf("%s: workspaceImagesReady = true, want false while the identity is unproven", fixture.name)
		}
		if !got.cloudImagesOK {
			t.Errorf("%s: cloudImagesReady = false; the fixture cloud Pods should match OPL_CLOUD_IMAGE", fixture.name)
		}
	}
	if observed[0].status == observed[1].status {
		t.Errorf("stuck rollout and unprovable owner chain both report %q; an operator cannot choose between waiting for convergence and investigating an integrity fault", observed[0].status)
	}
}

func readinessJSONStrings(value any) []string {
	items, _ := value.([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, fmt.Sprint(item))
	}
	return result
}

func readinessKubernetesList(items ...string) string {
	return fmt.Sprintf(`{
  "apiVersion": "v1",
  "kind": "List",
  "metadata": {"resourceVersion": "104729"},
  "items": [%s]
}`, strings.Join(items, ","))
}

func readinessCloudPodJSON(component, cloudImage string) string {
	return fmt.Sprintf(`{
  "apiVersion": "v1",
  "kind": "Pod",
  "metadata": {
    "name": "opl-cloud-%[1]s-7d9f8c6b5-x2m4n",
    "namespace": "opl-cloud",
    "uid": "uid-cloud-%[1]s",
    "resourceVersion": "104729",
    "creationTimestamp": "2026-10-20T08:00:00Z",
    "labels": {
      "app.kubernetes.io/component": %[1]q,
      "app.kubernetes.io/part-of": "opl-cloud"
    }
  },
  "spec": {
    "containers": [{"name": %[1]q, "image": %[2]q, "imagePullPolicy": "IfNotPresent"}]
  },
  "status": {
    "phase": "Running",
    "conditions": [
      {"type": "Initialized", "status": "True"},
      {"type": "Ready", "status": "True", "lastTransitionTime": "2026-10-20T08:00:30Z"},
      {"type": "ContainersReady", "status": "True", "lastTransitionTime": "2026-10-20T08:00:30Z"},
      {"type": "PodScheduled", "status": "True"}
    ],
    "containerStatuses": [{
      "name": %[1]q,
      "ready": true,
      "restartCount": 0,
      "image": %[2]q,
      "imageID": "docker-pullable://%[2]s",
      "state": {"running": {"startedAt": "2026-10-20T08:00:10Z"}}
    }]
  }
}`, component, cloudImage)
}

func readinessWorkspaceDeploymentJSON(workspaceID, uid, image string, generation, observedGeneration int) string {
	return fmt.Sprintf(`{
  "apiVersion": "apps/v1",
  "kind": "Deployment",
  "metadata": {
    "name": "runtime-%[1]s",
    "namespace": "opl-cloud",
    "uid": %[2]q,
    "generation": %[4]d,
    "resourceVersion": "104700",
    "creationTimestamp": "2026-10-20T07:00:00Z",
    "labels": {
      "app.kubernetes.io/managed-by": "opl-fabric",
      "app.kubernetes.io/name": "workspace",
      "oplcloud.cn/runtime-id": "rt_%[1]s",
      "oplcloud.cn/workspace-id": %[1]q
    },
    "annotations": {"oplcloud.cn/runtime-operation-id": "op-%[1]s"}
  },
  "spec": {
    "replicas": 1,
    "selector": {"matchLabels": {"oplcloud.cn/workspace-id": %[1]q}},
    "template": {
      "metadata": {"labels": {
        "app.kubernetes.io/managed-by": "opl-fabric",
        "app.kubernetes.io/name": "workspace",
        "oplcloud.cn/runtime-id": "rt_%[1]s",
        "oplcloud.cn/workspace-id": %[1]q
      }},
      "spec": {
        "containers": [{
          "name": "workspace",
          "image": %[3]q,
          "imagePullPolicy": "IfNotPresent",
          "securityContext": {"allowPrivilegeEscalation": false, "capabilities": {"drop": ["ALL"]}}
        }]
      }
    }
  },
  "status": {
    "observedGeneration": %[5]d,
    "replicas": 1,
    "updatedReplicas": 0,
    "readyReplicas": 1,
    "availableReplicas": 1,
    "conditions": [
      {"type": "Available", "status": "True", "reason": "MinimumReplicasAvailable"},
      {"type": "Progressing", "status": "True", "reason": "ReplicaSetUpdated"}
    ]
  }
}`, workspaceID, uid, image, generation, observedGeneration)
}

func readinessWorkspaceReplicaSetJSON(workspaceID, uid, deploymentUID, hash, image string, desired, ready int, ownedByDeployment bool) string {
	ownerReferences := "[]"
	if ownedByDeployment {
		ownerReferences = fmt.Sprintf(`[{"apiVersion":"apps/v1","kind":"Deployment","name":"runtime-%s","uid":%q,"controller":true,"blockOwnerDeletion":true}]`, workspaceID, deploymentUID)
	}
	return fmt.Sprintf(`{
  "apiVersion": "apps/v1",
  "kind": "ReplicaSet",
  "metadata": {
    "name": "runtime-%[1]s-%[3]s",
    "namespace": "opl-cloud",
    "uid": %[2]q,
    "generation": 1,
    "resourceVersion": "104705",
    "creationTimestamp": "2026-10-20T07:01:00Z",
    "labels": {
      "app.kubernetes.io/managed-by": "opl-fabric",
      "app.kubernetes.io/name": "workspace",
      "oplcloud.cn/runtime-id": "rt_%[1]s",
      "oplcloud.cn/workspace-id": %[1]q,
      "pod-template-hash": %[3]q
    },
    "ownerReferences": %[7]s
  },
  "spec": {
    "replicas": %[5]d,
    "selector": {"matchLabels": {"oplcloud.cn/workspace-id": %[1]q, "pod-template-hash": %[3]q}},
    "template": {
      "metadata": {"labels": {
        "app.kubernetes.io/managed-by": "opl-fabric",
        "app.kubernetes.io/name": "workspace",
        "oplcloud.cn/runtime-id": "rt_%[1]s",
        "oplcloud.cn/workspace-id": %[1]q,
        "pod-template-hash": %[3]q
      }},
      "spec": {
        "containers": [{
          "name": "workspace",
          "image": %[4]q,
          "imagePullPolicy": "IfNotPresent",
          "securityContext": {"allowPrivilegeEscalation": false, "capabilities": {"drop": ["ALL"]}}
        }]
      }
    }
  },
  "status": {
    "observedGeneration": 1,
    "replicas": %[5]d,
    "fullyLabeledReplicas": %[5]d,
    "readyReplicas": %[6]d,
    "availableReplicas": %[6]d
  }
}`, workspaceID, uid, hash, image, desired, ready, ownerReferences)
}

func readinessWorkspacePodJSON(workspaceID, uid, replicaSetUID, hash, image, phase string, ready bool) string {
	readyStatus, readyValue, imageID, state := "False", "false", "", `{"waiting":{"reason":"ContainerCreating"}}`
	if ready {
		readyStatus, readyValue = "True", "true"
		imageID = "docker-pullable://" + image
		state = `{"running":{"startedAt":"2026-10-20T07:02:10Z"}}`
	}
	return fmt.Sprintf(`{
  "apiVersion": "v1",
  "kind": "Pod",
  "metadata": {
    "name": "runtime-%[1]s-%[4]s-%[2]s",
    "namespace": "opl-cloud",
    "uid": %[2]q,
    "resourceVersion": "104710",
    "creationTimestamp": "2026-10-20T07:02:00Z",
    "labels": {
      "app.kubernetes.io/managed-by": "opl-fabric",
      "app.kubernetes.io/name": "workspace",
      "oplcloud.cn/runtime-id": "rt_%[1]s",
      "oplcloud.cn/workspace-id": %[1]q,
      "pod-template-hash": %[4]q
    },
    "ownerReferences": [{
      "apiVersion": "apps/v1",
      "kind": "ReplicaSet",
      "name": "runtime-%[1]s-%[4]s",
      "uid": %[3]q,
      "controller": true,
      "blockOwnerDeletion": true
    }]
  },
  "spec": {
    "containers": [{"name": "workspace", "image": %[5]q, "imagePullPolicy": "IfNotPresent"}]
  },
  "status": {
    "phase": %[6]q,
    "conditions": [
      {"type": "Initialized", "status": "True"},
      {"type": "Ready", "status": %[7]q, "lastTransitionTime": "2026-10-20T07:02:30Z"},
      {"type": "ContainersReady", "status": %[7]q, "lastTransitionTime": "2026-10-20T07:02:30Z"},
      {"type": "PodScheduled", "status": "True"}
    ],
    "containerStatuses": [{
      "name": "workspace",
      "ready": %[8]s,
      "restartCount": 0,
      "image": %[5]q,
      "imageID": %[9]q,
      "state": %[10]s
    }]
  }
}`, workspaceID, uid, replicaSetUID, hash, image, phase, readyStatus, readyValue, imageID, state)
}
