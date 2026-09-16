package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

// digestOf is the fixture digest for one syntactically valid image reference.
func digestOf(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// The workspace gateway has one upstream. A workspace whose current application
// publishes an entry is served by that application's resolved destination;
// otherwise the launched workspace runtime serves it. The Control Plane resolves
// neither a provider resource name nor a declared port of its own.
func TestWorkspaceEntryUpstreamFollowsTheCurrentApplication(t *testing.T) {
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "selected-app", true)
	workspace, found, err := app.tables.GetWorkspace(context.Background(), "ws-alpha")
	if err != nil || !found {
		t.Fatal("missing workspace", err)
	}

	serviceName, port, err := app.workspaceEntryUpstream(context.Background(), workspace, workspaceLaunchReconcileOperation{})
	if err != nil || serviceName != "app-entry-main" || port != 8080 {
		t.Fatalf("application upstream=%q:%d err=%v", serviceName, port, err)
	}
	if url := workspaceGatewayEntryURL("ws-alpha"); url != "https://"+workspaceDomain()+"/w/ws-alpha/" {
		t.Fatalf("gateway entry url=%q", url)
	}
}

// An application that publishes no entry leaves the gateway without a
// destination for that workspace: the caller must not fall back to another
// application's or the retired runtime's service.
func TestWorkspaceEntryUpstreamRejectsAnApplicationWithoutAnEntry(t *testing.T) {
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	seedApplicationRevisionForLifecycle(t, app, "ws-alpha", contracts.WorkspaceApplicationRevision{
		SchemaVersion: 1, ApplicationID: "worker-app", Version: "1.0.0", Platform: "linux/amd64",
		Image:          "registry.example/worker@sha256:" + digestOf("worker-app"),
		ExposurePolicy: "cloud_private",
	}, contracts.WorkspaceApplicationRuntimeConfiguration{}, nil, 0, true)
	workspace, found, err := app.tables.GetWorkspace(context.Background(), "ws-alpha")
	if err != nil || !found {
		t.Fatal("missing workspace", err)
	}
	if _, _, err := app.workspaceEntryUpstream(context.Background(), workspace, workspaceLaunchReconcileOperation{}); err == nil {
		t.Fatal("an application without a published entry must not resolve an upstream")
	}
}

// A workspace with no current application is served by its launched runtime on
// the runtime's own port.
func TestWorkspaceEntryUpstreamFallsBackToTheLaunchedRuntime(t *testing.T) {
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	workspace, found, err := app.tables.GetWorkspace(context.Background(), "ws-alpha")
	if err != nil || !found {
		t.Fatal("missing workspace", err)
	}
	workspace = cloneMap(workspace)
	delete(workspace, "currentApplicationDeploymentId")
	raw, err := json.Marshal("opl-compute-alpha")
	if err != nil {
		t.Fatal(err)
	}
	operation := workspaceLaunchReconcileOperation{raw: map[string]json.RawMessage{"runtimeServiceName": raw}}
	serviceName, port, err := app.workspaceEntryUpstream(context.Background(), workspace, operation)
	if err != nil || serviceName != "opl-compute-alpha" || port != 3000 {
		t.Fatalf("runtime upstream=%q:%d err=%v", serviceName, port, err)
	}
	if _, _, err := app.workspaceEntryUpstream(context.Background(), workspace, workspaceLaunchReconcileOperation{}); err == nil {
		t.Fatal("a runtime without a recorded service must not resolve an upstream")
	}
}
