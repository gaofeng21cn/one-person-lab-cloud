package fabric

import (
	"context"
	"fmt"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
)

func (p *LocalDockerProvider) readRuntimeObservations(ctx context.Context) ([]runtimeProviderObservation, error) {
	raw, err := p.runner.Run(ctx, nil, "container", "ls", "-a", "--filter", "label=opl.fabric.kind=runtime", "--format", "{{.Names}}")
	if err != nil {
		return nil, err
	}
	result := make([]runtimeProviderObservation, 0)
	seen := map[string]bool{}
	for _, name := range strings.Fields(string(raw)) {
		container, exists, err := p.inspectContainer(ctx, name)
		if err != nil || !exists || container.ID == "" || seen[container.ID] {
			return nil, firstNonNil(err, fmt.Errorf("local_docker_runtime_readback_invalid"))
		}
		seen[container.ID] = true
		labels := container.Config.Labels
		item := runtimeProviderObservation{RuntimeObservation: contracts.RuntimeObservation{
			ObjectRef: "runtime:" + stableSuffix("local-docker", container.ID), AccountID: labels["opl.account.id"], WorkspaceID: labels["opl.workspace.id"], RuntimeID: labels["opl.resource.id"],
			DesiredState: contracts.ResourceObservedUnknown, ObservedState: contracts.ResourceObservedPending,
		}, operationID: labels["opl.operation.id"], serviceName: strings.TrimPrefix(container.Name, "/"), desiredFromOperation: true}
		item.bindingValid = item.AccountID != "" && item.WorkspaceID != "" && item.RuntimeID == localRuntimeID(item.WorkspaceID) && item.operationID != "" && item.serviceName == localRuntimeName(item.WorkspaceID) && labels["opl.fabric.provider"] == "local-docker" && labels["opl.fabric.kind"] == "runtime"
		item.legacyReady = container.State.Running
		switch {
		case container.State.Running && container.State.Health != nil && container.State.Health.Status == "healthy":
			item.ObservedState = contracts.ResourceObservedRunning
		case !container.State.Running && (container.State.Status == "exited" || container.State.Status == "created"):
			item.ObservedState = contracts.ResourceObservedSuspended
		default:
			item.ReasonCode = "runtime_workload_pending"
		}
		result = append(result, item)
	}
	return result, nil
}
