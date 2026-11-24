package fabric

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"
)

type runtimeGatewayNetworkRecoveryTestProvider struct {
	testProvider
	calls  atomic.Int32
	result WorkspaceRuntimeGatewayNetworkRecoveryResult
}

func (p *runtimeGatewayNetworkRecoveryTestProvider) Descriptor() ProviderDescriptor {
	descriptor := p.testProvider.Descriptor()
	descriptor.Name = "local-docker"
	return descriptor
}

func (p *runtimeGatewayNetworkRecoveryTestProvider) RecoverWorkspaceRuntimeGatewayNetwork(_ context.Context, input WorkspaceRuntimeGatewayNetworkRecoveryInput, _ ComputeAllocation) (WorkspaceRuntimeGatewayNetworkRecoveryResult, error) {
	p.calls.Add(1)
	result := p.result
	result.SchemaVersion, result.OperationID, result.AccountID = 1, input.IdempotencyKey, input.AccountID
	result.WorkspaceID, result.ComputeID, result.RuntimeID = input.WorkspaceID, input.ComputeID, input.RuntimeID
	result.RuntimeServiceName, result.GatewayContainerID = input.RuntimeServiceName, "gateway-container-id"
	result.NetworkID, result.NetworkName, result.Status = "network-id", "opl-compute-alpha", "succeeded"
	result.Runtime = WorkspaceRuntime{ID: input.RuntimeID, OperationID: input.RuntimeOperationID, WorkspaceID: input.WorkspaceID, ServiceName: input.RuntimeServiceName, Status: "running", Ready: true}
	return result, nil
}

func TestRecoverWorkspaceRuntimeGatewayNetworkReplaysAndRejectsIdentityDrift(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryOperationStore()
	provider := &runtimeGatewayNetworkRecoveryTestProvider{}
	service := NewServiceWithOperationStore(provider, store)
	service.computes["compute-alpha"] = ComputeAllocation{ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", Provider: "local-docker", Status: "running"}
	predecessor := WorkspaceRuntime{ID: "rt-alpha", OperationID: "launch-alpha:runtime", WorkspaceID: "workspace-alpha", ServiceName: "opl-runtime-alpha", Status: "running", Ready: true}
	operation := newOperation("create_workspace_runtime", "workspace_runtime", predecessor.WorkspaceID, "acct-alpha", predecessor.WorkspaceID, predecessor.OperationID, hashInput(predecessor), time.Now().UTC())
	operation.ID, operation.Status = "fop-runtime-alpha", "succeeded"
	fillOperationResource(&operation, predecessor)
	if err := store.Append(ctx, operation); err != nil {
		t.Fatal(err)
	}
	input := WorkspaceRuntimeGatewayNetworkRecoveryInput{
		AccountID: "acct-alpha", WorkspaceID: predecessor.WorkspaceID, ComputeID: "compute-alpha", RuntimeID: predecessor.ID,
		RuntimeOperationID: predecessor.OperationID, RuntimeServiceName: predecessor.ServiceName, IdempotencyKey: "recover-once",
	}
	first, err := service.RecoverWorkspaceRuntimeGatewayNetwork(ctx, input)
	if err != nil || first.Status != "succeeded" || !first.Runtime.Ready || provider.calls.Load() != 1 {
		t.Fatalf("first=%#v calls=%d err=%v", first, provider.calls.Load(), err)
	}
	replayed, err := service.RecoverWorkspaceRuntimeGatewayNetwork(ctx, input)
	if err != nil || replayed.NetworkID != first.NetworkID || provider.calls.Load() != 1 {
		t.Fatalf("replay=%#v calls=%d err=%v", replayed, provider.calls.Load(), err)
	}
	drifted := input
	drifted.RuntimeID = "rt-other"
	if _, err := service.RecoverWorkspaceRuntimeGatewayNetwork(ctx, drifted); !errors.Is(err, ErrWorkspaceRuntimeGatewayNetworkRecoveryConflict) || provider.calls.Load() != 1 {
		t.Fatalf("drift error=%v calls=%d", err, provider.calls.Load())
	}
	changedIntent := input
	changedIntent.RuntimeServiceName = "opl-runtime-other"
	if _, err := service.RecoverWorkspaceRuntimeGatewayNetwork(ctx, changedIntent); !errors.Is(err, ErrWorkspaceRuntimeGatewayNetworkRecoveryConflict) || provider.calls.Load() != 1 {
		t.Fatalf("changed intent error=%v calls=%d", err, provider.calls.Load())
	}
}

func TestRecoverWorkspaceRuntimeGatewayNetworkIsUnavailableForTencent(t *testing.T) {
	service := runtimeTestService(testProvider{}, NewMemoryOperationStore())
	_, err := service.RecoverWorkspaceRuntimeGatewayNetwork(context.Background(), WorkspaceRuntimeGatewayNetworkRecoveryInput{
		AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", ComputeID: "compute-alpha", RuntimeID: "rt-alpha",
		RuntimeOperationID: "launch-alpha:runtime", RuntimeServiceName: "runtime-alpha", IdempotencyKey: "recover-once",
	})
	if !errors.Is(err, ErrWorkspaceRuntimeGatewayNetworkRecoveryUnavailable) {
		t.Fatalf("Tencent recovery error=%v", err)
	}
}

type runtimeGatewayNetworkRecoveryStateRunner struct {
	stubDockerClient
	network    dockerNetworkInspect
	containers map[string]dockerContainerInspect
	connects   int
}

func (r *runtimeGatewayNetworkRecoveryStateRunner) NetworkList(context.Context, mobyclient.NetworkListOptions) ([]network.Summary, error) {
	return []network.Summary{sdkNetworkSummary(r.network.ID, r.network.Name)}, nil
}

func (r *runtimeGatewayNetworkRecoveryStateRunner) NetworkInspect(context.Context, string, mobyclient.NetworkInspectOptions) (network.Inspect, error) {
	return sdkNetworkInspect(r.network), nil
}

func (r *runtimeGatewayNetworkRecoveryStateRunner) ContainerList(_ context.Context, options mobyclient.ContainerListOptions) ([]dockercontainer.Summary, error) {
	names := dockerFilterValues(options.Filters, "name")
	if len(names) == 0 {
		return nil, nil
	}
	name := strings.TrimSuffix(strings.TrimPrefix(names[0], "^/"), "$")
	item, ok := r.containers[name]
	if !ok {
		return nil, nil
	}
	return []dockercontainer.Summary{{ID: item.ID, Names: []string{"/" + name}}}, nil
}

func (r *runtimeGatewayNetworkRecoveryStateRunner) ContainerInspect(_ context.Context, containerID string) (dockercontainer.InspectResponse, error) {
	for _, item := range r.containers {
		if item.ID == containerID {
			return sdkContainerInspect(item), nil
		}
	}
	return dockercontainer.InspectResponse{}, fmt.Errorf("container not found")
}

func (r *runtimeGatewayNetworkRecoveryStateRunner) NetworkConnect(context.Context, string, string, *network.EndpointSettings) error {
	r.connects++
	return nil
}

func runtimeGatewayNetworkRecoveryStateFixture(t *testing.T) (*LocalDockerProvider, *runtimeGatewayNetworkRecoveryStateRunner, WorkspaceRuntimeGatewayNetworkRecoveryInput, ComputeAllocation) {
	t.Helper()
	input := WorkspaceRuntimeGatewayNetworkRecoveryInput{
		AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", ComputeID: "compute-alpha", RuntimeID: localRuntimeID("workspace-alpha"),
		RuntimeOperationID: "launch-alpha:runtime", RuntimeServiceName: localRuntimeName("workspace-alpha"), IdempotencyKey: "recover-once",
	}
	networkName, networkID := localDockerName("opl-compute", input.ComputeID), "network-alpha"
	imageID := "sha256:" + strings.Repeat("a", 64)
	labels := localDockerLabels(input.AccountID, input.WorkspaceID, input.RuntimeID, input.RuntimeOperationID, "runtime")
	labels["opl.compute.id"] = input.ComputeID
	labels["opl.image.ref"] = imageID
	runtime := localDockerReadyRuntimeContainer(t, input.RuntimeServiceName, "runtime-container", imageID, labels,
		localDockerStoragePaths{Data: "/tmp/data", Projects: "/tmp/projects"}, "/tmp/secrets")
	runtime.NetworkSettings.Networks = map[string]dockerEndpointSettings{networkName: {NetworkID: networkID}}
	gateway := dockerContainerInspect{ID: "gateway-container", Name: "/control-plane"}
	gateway.State.Running = true
	gateway.Config.Labels = map[string]string{"opl.fabric.local-docker.gateway": "control-plane", "opl.cloud.fabric-provider": "local-docker"}
	runner := &runtimeGatewayNetworkRecoveryStateRunner{
		network:    dockerNetworkInspect{ID: networkID, Name: networkName, Labels: localDockerLabels(input.AccountID, input.WorkspaceID, input.ComputeID, "", "compute")},
		containers: map[string]dockerContainerInspect{input.RuntimeServiceName: runtime, "control-plane": gateway},
	}
	provider := newLocalDockerProvider(LocalDockerProviderConfig{RuntimeHost: "127.0.0.1", RuntimeGatewayContainer: "control-plane"}, runner)
	compute := ComputeAllocation{ID: input.ComputeID, AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, Provider: "local-docker", ProviderResourceID: "network/" + networkID, Status: "running"}
	return provider, runner, input, compute
}

func TestRecoverWorkspaceRuntimeGatewayNetworkRejectsOwnershipLabelDriftBeforeMutation(t *testing.T) {
	tests := []struct {
		name      string
		wantError string
		mutate    func(*runtimeGatewayNetworkRecoveryStateRunner)
	}{
		{name: "network ownership", wantError: "local_docker_compute_ownership_mismatch", mutate: func(r *runtimeGatewayNetworkRecoveryStateRunner) { r.network.Labels["opl.account.id"] = "acct-foreign" }},
		{name: "missing network ownership", wantError: "local_docker_compute_ownership_mismatch", mutate: func(r *runtimeGatewayNetworkRecoveryStateRunner) { delete(r.network.Labels, "opl.workspace.id") }},
		{name: "gateway provider", wantError: ErrWorkspaceRuntimeGatewayNetworkRecoveryConflict.Error(), mutate: func(r *runtimeGatewayNetworkRecoveryStateRunner) {
			gateway := r.containers["control-plane"]
			gateway.Config.Labels["opl.cloud.fabric-provider"] = "tencent-tke"
			r.containers["control-plane"] = gateway
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider, runner, input, compute := runtimeGatewayNetworkRecoveryStateFixture(t)
			test.mutate(runner)
			if _, err := provider.RecoverWorkspaceRuntimeGatewayNetwork(context.Background(), input, compute); err == nil || err.Error() != test.wantError {
				t.Fatalf("drift error=%v", err)
			}
			if runner.connects != 0 {
				t.Fatalf("drift triggered %d network mutations", runner.connects)
			}
		})
	}
}
