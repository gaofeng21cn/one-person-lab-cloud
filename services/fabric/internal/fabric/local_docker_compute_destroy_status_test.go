package fabric

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

// convergingComputeRemovalRunner holds the local compute network removal until
// the test releases it, so one readback can observe the exact window in which a
// dispatched destroy is still converging.
type convergingComputeRemovalRunner struct {
	network        dockerNetworkInspect
	removalEntered chan struct{}
	removalRelease chan struct{}
	enteredOnce    bool
}

func (r *convergingComputeRemovalRunner) Run(_ context.Context, _ []byte, args ...string) ([]byte, error) {
	switch {
	case len(args) >= 1 && args[0] == "info":
		return []byte("test-docker-version"), nil
	case len(args) == 7 && args[0] == "network" && args[1] == "ls":
		if r.network.ID == "" {
			return nil, nil
		}
		return json.Marshal(dockerObjectInventoryRow{ID: r.network.ID, Name: r.network.Name})
	case len(args) >= 4 && args[0] == "network" && args[1] == "create":
		r.network = dockerNetworkInspect{ID: "network-converging-removal", Name: args[len(args)-1], Labels: map[string]string{}}
		for index := 0; index+1 < len(args); index++ {
			if args[index] != "--label" {
				continue
			}
			key, value, found := strings.Cut(args[index+1], "=")
			if found {
				r.network.Labels[key] = value
			}
		}
		return []byte(r.network.ID), nil
	case len(args) == 3 && args[0] == "network" && args[1] == "inspect" && r.network.ID != "":
		return json.Marshal([]dockerNetworkInspect{r.network})
	case len(args) == 3 && args[0] == "network" && args[1] == "rm":
		if !r.enteredOnce {
			r.enteredOnce = true
			close(r.removalEntered)
		}
		<-r.removalRelease
		r.network = dockerNetworkInspect{}
		return []byte("removed"), nil
	default:
		return nil, fmt.Errorf("unexpected docker call: %q", args)
	}
}

// A local compute destroy is executed by an asynchronous worker. While the
// network removal is still converging, the read-only destroy-status readback must
// classify the deletion as unfinished and retryable instead of reporting only the
// provider's raw presence status, which the owning Control Plane can only treat as
// an unclassified conflict.
func TestLocalDockerComputeDestroyStatusClassifiesAConvergingRemoval(t *testing.T) {
	ctx := context.Background()
	runner := &convergingComputeRemovalRunner{removalEntered: make(chan struct{}), removalRelease: make(chan struct{})}
	store := NewMemoryOperationStore()
	storageRoot := localDockerStorageTestRoot(t)
	provider := newLocalDockerProvider(LocalDockerProviderConfig{
		GatewaySecretRoot: localDockerSecretTestRoot(t), HostStorageRoot: storageRoot, StorageQuotaBackend: localDockerStorageTestQuota(storageRoot),
	}, runner)
	service := NewServiceWithOperationStore(provider, store)
	image := defaultLocalDockerWorkspaceImageRepository + "@sha256:" + strings.Repeat("a", 64)
	launchID, accountID, workspaceID := "launch-compute-destroy-status", "acct-compute-destroy-status", "ws-compute-destroy-status"
	launchHash := strings.Repeat("b", 64)
	preflight, err := service.PreflightWorkspaceLaunch(ctx, WorkspaceLaunchPreflightInput{
		SchemaVersion: 1, LaunchOperationID: launchID, AccountID: accountID, WorkspaceID: workspaceID,
		PackageID: "basic", SizeGB: 10, WorkspaceImageDigest: image, RequestHash: launchHash,
	})
	if err != nil || !preflight.Available {
		t.Fatalf("preflight=%#v err=%v", preflight, err)
	}
	input := WorkspaceLaunchStageInput{
		Binding:            localLaunchBinding(launchID, accountID, workspaceID, "ensure_compute_allocation", "ensure_compute_allocation", launchID+":ensure-compute-allocation"),
		ProviderProfileRef: "local-docker", ProviderBindingRef: preflight.ProviderBindingRef, SpecDigest: preflight.SpecDigest,
		PackageID: "basic", SizeGB: 10, WorkspaceImageDigest: image,
	}
	input.Binding.RequestHash = workspaceLaunchStageRequestHash(input, launchHash)
	result, err := service.EnsureWorkspaceLaunchStage(ctx, input)
	if err != nil || result.State != "ready" {
		t.Fatalf("ensure compute allocation result=%#v err=%v", result, err)
	}
	computeID := result.Resources.ComputeAllocationID

	// No destroy was recorded yet: the present network is still retryable, and the
	// owner may perform the removal itself.
	idle, err := service.ReadComputeDestroyStatus(ctx, computeID)
	if err != nil || idle.ID != computeID || idle.WorkspaceID != workspaceID {
		t.Fatalf("idle compute destroy readback=%#v err=%v", idle, err)
	}
	if idle.Status == "external_deleted" || idle.DestroyState != contracts.WorkspaceDeleteOutcomePendingRetry {
		t.Fatalf("present compute without a dispatch must stay retryable: %#v", idle)
	}

	destroying, err := service.DestroyComputeAllocation(ctx, computeID)
	if err != nil || destroying.Status != "destroying" {
		t.Fatalf("destroy compute=%#v err=%v", destroying, err)
	}
	<-runner.removalEntered
	converging, err := service.ReadComputeDestroyStatus(ctx, computeID)
	if err != nil || converging.ID != computeID || converging.WorkspaceID != workspaceID {
		t.Fatalf("converging compute destroy readback=%#v err=%v", converging, err)
	}
	if converging.Status == "external_deleted" || !contracts.WorkspaceDeleteOutcomeRetryable(converging.DestroyState) ||
		converging.DestroyState != contracts.WorkspaceDeleteOutcomeUnconfirmedSend {
		t.Fatalf("converging removal must be classified as an unfinished deletion: %#v", converging)
	}

	close(runner.removalRelease)
	if _, err := waitForLocalDockerOperation(ctx, service, "destroy_compute_allocation", "compute_allocation", computeID, "succeeded"); err != nil {
		t.Fatalf("wait for compute destroy operation: %v", err)
	}
	absent, err := service.ReadComputeDestroyStatus(ctx, computeID)
	if err != nil || absent.Status != "external_deleted" || absent.DestroyState != "" {
		t.Fatalf("completed removal must report absence facts only: %#v err=%v", absent, err)
	}
}
