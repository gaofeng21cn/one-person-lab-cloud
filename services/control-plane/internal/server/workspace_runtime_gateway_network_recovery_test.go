package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"opl-cloud/services/control-plane/internal/clients"
)

type runtimeGatewayNetworkRecoveryRouteFabric struct {
	fakeFabricClient
	calls atomic.Int32
	input clients.WorkspaceRuntimeGatewayNetworkRecoveryInput
	err   error
}

func (f *runtimeGatewayNetworkRecoveryRouteFabric) RecoverWorkspaceRuntimeGatewayNetwork(_ context.Context, input clients.WorkspaceRuntimeGatewayNetworkRecoveryInput, key string) (clients.WorkspaceRuntimeGatewayNetworkRecoveryResult, error) {
	f.calls.Add(1)
	f.input = input
	if f.err != nil {
		return clients.WorkspaceRuntimeGatewayNetworkRecoveryResult{}, f.err
	}
	return clients.WorkspaceRuntimeGatewayNetworkRecoveryResult{
		SchemaVersion: 1, OperationID: key, AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, ComputeID: input.ComputeID,
		RuntimeID: input.RuntimeID, RuntimeServiceName: input.RuntimeServiceName, GatewayContainerID: "gateway-id", NetworkID: "network-id", NetworkName: "network-name", Status: "succeeded",
		Runtime: clients.WorkspaceRuntime{ID: input.RuntimeID, OperationID: input.RuntimeOperationID, WorkspaceID: input.WorkspaceID, ServiceName: input.RuntimeServiceName, Status: "running", Ready: true},
	}, nil
}

func TestWorkspaceRuntimeGatewayNetworkRecoveryRouteReplaysPersistedFailureAsConflict(t *testing.T) {
	store := newMemoryTableStore()
	fabric := &runtimeGatewayNetworkRecoveryRouteFabric{
		fakeFabricClient: fakeFabricClient{runtimeStatusErr: &clients.FabricHTTPError{StatusCode: http.StatusInternalServerError, Body: `{"error":"local_docker_runtime_gateway_network_readback_mismatch"}`}},
		err:              &clients.FabricHTTPError{StatusCode: http.StatusConflict, Body: `{"error":"workspace_runtime_gateway_network_recovery_conflict"}`},
	}
	server, err := NewPersistentServer(newTestService(fakeLedgerClient{}, fabric), store)
	if err != nil {
		t.Fatal(err)
	}
	operation := seedCanonicalRuntimeAccessWorkspaceForTest(t, store, "usr-alpha")
	provider, _ := json.Marshal("local-docker")
	operation.raw["providerProfileRef"] = provider
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	operator := reservedOperatorSessionForTest(t, server)
	body := `{"confirmationWorkspaceId":"ws-alpha","reason":"restore gateway access after control plane replacement"}`
	path := "/api/operator/workspaces/ws-alpha/runtime-gateway-network/recover"
	first := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, path, body, "case-20260903-failed-network")
	if first.Code != http.StatusConflict || fabric.calls.Load() != 1 || !strings.Contains(first.Body.String(), `"status":"failed"`) {
		t.Fatalf("first status=%d calls=%d body=%s", first.Code, fabric.calls.Load(), first.Body.String())
	}
	replay := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, path, body, "case-20260903-failed-network")
	if replay.Code != http.StatusConflict || fabric.calls.Load() != 1 || !strings.Contains(replay.Body.String(), `"status":"failed"`) {
		t.Fatalf("replay status=%d calls=%d body=%s", replay.Code, fabric.calls.Load(), replay.Body.String())
	}
	events, err := store.ListAuditEvents(context.Background(), "acct-alpha")
	if err != nil || len(events) != 1 || events[0]["result"] != "failed" || events[0]["errorCode"] != "workspace_runtime_gateway_network_recovery_conflict" {
		t.Fatalf("audit=%#v err=%v", events, err)
	}
}

func TestWorkspaceRuntimeGatewayNetworkRecoveryRoutePersistsAuthorizationAndReplays(t *testing.T) {
	store := newMemoryTableStore()
	fabric := &runtimeGatewayNetworkRecoveryRouteFabric{fakeFabricClient: fakeFabricClient{runtimeStatusErr: &clients.FabricHTTPError{StatusCode: http.StatusInternalServerError, Body: `{"error":"local_docker_runtime_gateway_network_readback_mismatch"}`}}}
	server, err := NewPersistentServer(newTestService(fakeLedgerClient{}, fabric), store)
	if err != nil {
		t.Fatal(err)
	}
	operation := seedCanonicalRuntimeAccessWorkspaceForTest(t, store, "usr-alpha")
	provider, _ := json.Marshal("local-docker")
	operation.raw["providerProfileRef"] = provider
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	operator := reservedOperatorSessionForTest(t, server)
	body := `{"confirmationWorkspaceId":"ws-alpha","reason":"restore gateway access after control plane replacement"}`
	path := "/api/operator/workspaces/ws-alpha/runtime-gateway-network/recover"
	first := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, path, body, "case-20260903-network")
	if first.Code != http.StatusOK || fabric.calls.Load() != 1 {
		t.Fatalf("first status=%d calls=%d body=%s", first.Code, fabric.calls.Load(), first.Body.String())
	}
	if fabric.input.AccountID != "acct-alpha" || fabric.input.WorkspaceID != "ws-alpha" || fabric.input.ComputeID != operation.stringFact("computeAllocationId") ||
		fabric.input.RuntimeID != operation.stringFact("runtimeId") || fabric.input.RuntimeOperationID != operation.stringFact("runtimeBindingRef") {
		t.Fatalf("owner chain input=%#v", fabric.input)
	}
	replay := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, path, body, "case-20260903-network")
	if replay.Code != http.StatusOK || fabric.calls.Load() != 1 || !strings.Contains(replay.Body.String(), `"status":"succeeded"`) {
		t.Fatalf("replay status=%d calls=%d body=%s", replay.Code, fabric.calls.Load(), replay.Body.String())
	}
	changed := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, path, `{"confirmationWorkspaceId":"ws-alpha","reason":"different intent"}`, "case-20260903-network")
	if changed.Code != http.StatusConflict || fabric.calls.Load() != 1 {
		t.Fatalf("changed status=%d calls=%d body=%s", changed.Code, fabric.calls.Load(), changed.Body.String())
	}
	events, err := store.ListAuditEvents(context.Background(), "acct-alpha")
	if err != nil || len(events) != 1 || events[0]["action"] != workspaceRuntimeGatewayNetworkRecoveryAction || events[0]["result"] != "succeeded" {
		t.Fatalf("audit=%#v err=%v", events, err)
	}
}
