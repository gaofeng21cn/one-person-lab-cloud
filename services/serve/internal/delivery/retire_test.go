package delivery

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

// TestRetireIsImplementedNotUnimplemented is the ordering proof for the deletion
// closure. The contract defines Retire on ServeAgentCoordination and the Workspace
// deletion path calls it. Because Serve embeds UnimplementedServeAgentCoordination
// server and overrides only StopRuntime, a Retire that is not implemented here falls
// back to the generated stub, which answers Unimplemented without touching Serve.
// The Workspace deletion aborts on that error and never reaches the deletion
// receipt step, so the Ledger receipt dispatch is never exercised at all.
//
// The generated stub's Unimplemented error is therefore the discriminator: an
// unimplemented Retire reports Unimplemented, and an implemented one fails later on
// this Service's own admission and configuration instead. This test drives a
// zero-value Service, so it needs neither PostgreSQL nor a live peer.
func TestRetireIsImplementedNotUnimplemented(t *testing.T) {
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.Workspace.Service())
	_, err := (&Service{}).Retire(ctx, &api.RuntimeStopCommand{RuntimeInstanceId: "runtime-alpha", DeploymentId: "deployment-alpha"})
	if status.Code(err) == codes.Unimplemented {
		t.Fatalf("Retire fell through to the generated stub: %v", err)
	}
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("code=%v err=%v, want Serve's own Unavailable once Retire is implemented", status.Code(err), err)
	}
}

// TestRetireRejectsAnUnadmittedCaller proves the retirement is a Workspace-owner
// coordination action. It is not the execution adapter's own stop, so Serve's own
// peer is refused before any runtime is touched.
func TestRetireRejectsAnUnadmittedCaller(t *testing.T) {
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.Serve.Service())
	if _, err := (&Service{}).Retire(ctx, &api.RuntimeStopCommand{RuntimeInstanceId: "runtime-alpha"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code=%v err=%v, want PermissionDenied for an unadmitted caller", status.Code(err), err)
	}
}

// TestRetireIsDistinctFromStopRuntime records why the two are not aliases. A stop
// suspends a runtime that may be resumed; a retirement releases it for good. The
// contract carries both under the same command message, so this test pins the part
// of the distinction that is observable without a database: the coordination entry
// point admits the Workspace owner, while the execution-adapter entry point does
// not admit it at all.
func TestRetireIsDistinctFromStopRuntime(t *testing.T) {
	workspace := ownerservice.WithPeerOwner(context.Background(), owneridentity.Workspace.Service())
	if _, err := (&Service{}).Retire(workspace, &api.RuntimeStopCommand{RuntimeInstanceId: "runtime-alpha"}); status.Code(err) != codes.Unavailable {
		t.Fatalf("Retire admitted the Workspace owner: code=%v err=%v", status.Code(err), err)
	}
	if _, err := (&Service{}).StopRuntime(workspace, &api.RuntimeStopCommand{RuntimeInstanceId: "runtime-alpha"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("StopRuntime admitted the Workspace owner: code=%v err=%v", status.Code(err), err)
	}
}
