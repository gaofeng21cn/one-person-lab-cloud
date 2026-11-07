package coordination_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// TestOperatorReadPeerBoundaryKeepsConsoleBFFReadOnly proves that the new
// operator read peer is admitted only for ReadResources and cannot cross any
// Fabric mutation entry point. The real owner identity interceptor is used by
// startSecretPeerSurface; the unreachable database makes an admitted read stop
// after its peer/request boundary without provisioning a resource.
func TestOperatorReadPeerBoundaryKeepsConsoleBFFReadOnly(t *testing.T) {
	clientFor := startSecretPeerSurface(t, owneridentity.ConsoleBFF, owneridentity.Build.Service())
	operator := clientFor(owneridentity.ConsoleBFF)
	build := clientFor(owneridentity.Build.Service())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	call := &api.CallContext{
		ActorId:        "operator",
		RequestId:      "operator-read-request",
		IdempotencyKey: "operator-read-key",
		SessionId:      proto.String("interactive-session"),
		Scope:          &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
	}
	read := &api.ResourceReadbackRequest{Context: call, WorkspaceId: "workspace-operator", ResourceSetId: "set-operator"}
	if _, err := build.ReadResources(ctx, read); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("untrusted Build peer crossed operator read guard: %v", err)
	}
	if _, err := operator.ReadResources(ctx, read); status.Code(err) != codes.Internal {
		t.Fatalf("valid operator read did not reach persistence: %v", err)
	}
	t.Run("invalid operator requests stop before persistence", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			mutate func(*api.ResourceReadbackRequest)
			code   codes.Code
		}{
			{"missing context", func(r *api.ResourceReadbackRequest) { r.Context = nil }, codes.Unauthenticated},
			{"missing session", func(r *api.ResourceReadbackRequest) { r.Context.SessionId = nil }, codes.Unauthenticated},
			{"blank session", func(r *api.ResourceReadbackRequest) { r.Context.SessionId = proto.String(" ") }, codes.Unauthenticated},
			{"grant instead of session", func(r *api.ResourceReadbackRequest) {
				r.Context.SessionId = nil
				r.Context.AcceptedOperationGrantId = proto.String("grant")
			}, codes.Unauthenticated},
			{"grant with session", func(r *api.ResourceReadbackRequest) { r.Context.AcceptedOperationGrantId = proto.String("grant") }, codes.Unauthenticated},
			{"tenant scope", func(r *api.ResourceReadbackRequest) {
				r.Context.Scope = command("operator-tenant").Context.Scope
			}, codes.PermissionDenied},
			{"empty scope", func(r *api.ResourceReadbackRequest) { r.Context.Scope = &api.AuthorizationScope{} }, codes.PermissionDenied},
			{"missing Workspace", func(r *api.ResourceReadbackRequest) { r.WorkspaceId = "" }, codes.InvalidArgument},
			{"blank Workspace", func(r *api.ResourceReadbackRequest) { r.WorkspaceId = " " }, codes.InvalidArgument},
			{"missing resource set", func(r *api.ResourceReadbackRequest) { r.ResourceSetId = "" }, codes.InvalidArgument},
			{"blank resource set", func(r *api.ResourceReadbackRequest) { r.ResourceSetId = " " }, codes.InvalidArgument},
			{"resource action selector", func(r *api.ResourceReadbackRequest) { r.ResourceActionId = "action" }, codes.InvalidArgument},
			{"original provision selector", func(r *api.ResourceReadbackRequest) { r.OriginalProvisionIdempotencyKey = "original" }, codes.InvalidArgument},
			{"blank resource action selector", func(r *api.ResourceReadbackRequest) { r.ResourceActionId = " " }, codes.InvalidArgument},
			{"blank original provision selector", func(r *api.ResourceReadbackRequest) { r.OriginalProvisionIdempotencyKey = " " }, codes.InvalidArgument},
		} {
			t.Run(tc.name, func(t *testing.T) {
				request := proto.Clone(read).(*api.ResourceReadbackRequest)
				tc.mutate(request)
				if got, err := operator.ReadResources(ctx, request); status.Code(err) != tc.code || got != nil {
					t.Fatalf("invalid operator read returned %v, %v; want %v", got, err, tc.code)
				}
			})
		}
	})
	t.Run("operator cannot mutate resources or secrets", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			call func() error
			code codes.Code
		}{
			{"EnsureResources", func() error {
				_, err := operator.EnsureResources(ctx, &api.EnsureResourcesCommand{Context: call, WorkspaceId: read.WorkspaceId})
				return err
			}, codes.PermissionDenied},
			{"AdmitResources", func() error {
				_, err := operator.AdmitResources(ctx, &api.ResourceAdmissionRequest{Context: call, WorkspaceId: read.WorkspaceId})
				return err
			}, codes.Unimplemented},
			{"ResizeResources", func() error {
				_, err := operator.ResizeResources(ctx, &api.ResizeResourcesCommand{Context: call, WorkspaceId: read.WorkspaceId, ResourceSetId: read.ResourceSetId})
				return err
			}, codes.Unimplemented},
			{"RenewResources", func() error {
				_, err := operator.RenewResources(ctx, &api.RenewResourcesCommand{Context: call, WorkspaceId: read.WorkspaceId, ResourceSetId: read.ResourceSetId})
				return err
			}, codes.Unimplemented},
			{"DeleteResources", func() error {
				_, err := operator.DeleteResources(ctx, &api.MutateResourcesCommand{Context: call, WorkspaceId: read.WorkspaceId, ResourceSetId: read.ResourceSetId})
				return err
			}, codes.PermissionDenied},
			{"SuspendResources", func() error {
				_, err := operator.SuspendResources(ctx, &api.MutateResourcesCommand{Context: call, WorkspaceId: read.WorkspaceId, ResourceSetId: read.ResourceSetId})
				return err
			}, codes.Unimplemented},
			{"ResumeResources", func() error {
				_, err := operator.ResumeResources(ctx, &api.MutateResourcesCommand{Context: call, WorkspaceId: read.WorkspaceId, ResourceSetId: read.ResourceSetId})
				return err
			}, codes.Unimplemented},
			{"BindSecret", func() error {
				_, err := operator.BindSecret(ctx, &api.SecretBindingCommand{Context: call, WorkspaceId: read.WorkspaceId})
				return err
			}, codes.PermissionDenied},
			{"RebindSecret", func() error {
				_, err := operator.RebindSecret(ctx, &api.SecretBindingRebindCommand{Context: call, WorkspaceId: read.WorkspaceId})
				return err
			}, codes.PermissionDenied},
			{"PutManagedSecret", func() error {
				_, err := operator.PutManagedSecret(ctx, &api.ManagedSecretCommand{Context: call, WorkspaceId: read.WorkspaceId})
				return err
			}, codes.PermissionDenied},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if err := tc.call(); status.Code(err) != tc.code {
					t.Fatalf("Console BFF %s returned %v; want %v", tc.name, err, tc.code)
				}
			})
		}
	})
}
