package delivery_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/serve/internal/delivery"
)

type credentialRuntime struct {
	runtimeForServe
	reads    int
	response *api.WorkspaceApplicationCredentials
	readErr  error
}

func (r *credentialRuntime) Credentials(_ context.Context, c *api.RuntimeDeployCommand, _ delivery.ExecutionTarget) (*api.WorkspaceApplicationCredentials, error) {
	r.reads++
	if r.readErr != nil {
		return nil, r.readErr
	}
	if r.response != nil {
		return r.response, nil
	}
	return &api.WorkspaceApplicationCredentials{WorkspaceId: c.GetWorkspaceId(), RuntimeInstanceId: contracts.WorkspaceApplicationRuntimeID(c.GetRuntimeInstanceId()), Username: "declared-admin", Password: "provider-password"}, nil
}
func credentialsFixture(t *testing.T, declared bool) (*delivery.Service, *api.ReadApplicationCredentialsRequest, *credentialRuntime) {
	t.Helper()
	s, r, cap := reservationFixture(t)
	if declared {
		r.DeploymentDescriptor.ApplicationRevision.Credentials = []*api.WorkspaceApplicationCredential{{Name: "login", Kind: api.WorkspaceApplicationCredentialKindEnum_WORKSPACE_APPLICATION_CREDENTIAL_KIND_ENUM_WORKSPACE_ADMIN_PASSWORD, Target: "/run/secrets/admin", Username: proto.String("declared-admin")}}
	}
	r.DeploymentDescriptorDigest = descriptorDigestOf(t, r.DeploymentDescriptor)
	cap.version.DeploymentDescriptorDigest = r.DeploymentDescriptorDigest
	s.Resources = &resourcesForServe{confirmed: true, workspace: "ws-first", dataAttachment: "attachment-original"}
	runtime := &credentialRuntime{}
	s.Runtime = runtime
	reservation, err := s.Reserve(workspaceContext(), r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Deploy(workspaceContext(), deployReserved(r, reservation)); err != nil {
		t.Fatal(err)
	}
	return s, &api.ReadApplicationCredentialsRequest{Context: call("tenant-alpha", false), WorkspaceId: "ws-first", RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId}, runtime
}
func TestApplicationCredentialsAdmitOnlyWorkspaceAndServeSelf(t *testing.T) {
	for _, peer := range []ownerservice.Service{owneridentity.ConsoleBFF, owneridentity.Fabric.Service(), owneridentity.Ledger.Service(), owneridentity.Gateway.Service()} {
		ctx := ownerservice.WithPeerOwner(t.Context(), peer)
		if _, err := (&delivery.Service{}).ReadApplicationCredentials(ctx, &api.ReadApplicationCredentialsRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("peer=%s code=%s", peer, status.Code(err))
		}
	}
	// Granting Workspace credential reads must not grant execution or route writes.
	ctx := workspaceContext()
	s := &delivery.Service{}
	if _, err := s.StartRuntime(ctx, &api.RuntimeDeployCommand{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("start code=%s", status.Code(err))
	}
	if _, err := s.StopRuntime(ctx, &api.RuntimeStopCommand{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("stop code=%s", status.Code(err))
	}
	if _, err := s.ObserveRuntime(ctx, &api.RuntimeReadbackRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("observe code=%s", status.Code(err))
	}
}
func TestApplicationCredentialsReadsExactCurrentReadyRuntime(t *testing.T) {
	s, r, runtime := credentialsFixture(t, true)
	for i := 0; i < 2; i++ {
		value, err := s.ReadApplicationCredentials(workspaceContext(), r)
		if err != nil {
			t.Fatal(err)
		}
		if value.GetWorkspaceId() != r.WorkspaceId || value.GetRuntimeInstanceId() != r.RuntimeInstanceId || value.GetUsername() != "declared-admin" || value.GetPassword() != "provider-password" {
			t.Fatal("credential identity translation or declared projection failed")
		}
	}
	if runtime.reads != 2 {
		t.Fatal("credential response was cached")
	}
	var stored bool
	if err := s.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM serve.agent_runtime_actions WHERE input_snapshot::text LIKE '%provider-password%')`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored {
		t.Fatal("credential was persisted")
	}
}
func TestApplicationCredentialsRejectsCurrentIdentityReadinessAndDeclaration(t *testing.T) {
	for _, name := range []string{"no context", "missing workspace", "foreign workspace", "foreign tenant", "stale deployment", "stale runtime", "inactive deployment", "stopped runtime", "runtime not actually ready", "undeclared credential", "wrong username", "wrong provider runtime", "wrong provider workspace", "empty password", "provider unavailable"} {
		t.Run(name, func(t *testing.T) {
			s, r, runtime := credentialsFixture(t, name != "undeclared credential")
			want := codes.FailedPrecondition
			wantReads := 0
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := s.DB.Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			switch name {
			case "no context":
				r.Context = nil
				want = codes.Unauthenticated
			case "missing workspace":
				r.WorkspaceId = ""
				want = codes.InvalidArgument
			case "foreign workspace":
				r.WorkspaceId = "another-workspace"
			case "foreign tenant":
				r.Context.Scope.GetTenant().TenantId = "foreign"
			case "stale deployment":
				r.DeploymentId = "old-deployment"
			case "stale runtime":
				r.RuntimeInstanceId = "old-runtime"
			case "inactive deployment":
				exec(`UPDATE serve.agent_deployments SET status='superseded' WHERE id=$1`, r.DeploymentId)
			case "stopped runtime":
				exec(`UPDATE serve.agent_runtime_instances SET status='stopped' WHERE id=$1`, r.RuntimeInstanceId)
			case "runtime not actually ready":
				runtime.state = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_PENDING
			case "wrong username", "wrong provider runtime", "wrong provider workspace", "empty password":
				runtime.response = &api.WorkspaceApplicationCredentials{WorkspaceId: r.WorkspaceId, RuntimeInstanceId: contracts.WorkspaceApplicationRuntimeID(r.RuntimeInstanceId), Username: "declared-admin", Password: "provider-password"}
				wantReads = 1
				switch name {
				case "wrong username":
					runtime.response.Username = "another-admin"
				case "wrong provider runtime":
					runtime.response.RuntimeInstanceId = "rt_app_" + strings.Repeat("0", 32)
				case "wrong provider workspace":
					runtime.response.WorkspaceId = "foreign"
				case "empty password":
					runtime.response.Password = ""
				}
			case "provider unavailable":
				runtime.readErr = status.Error(codes.Unavailable, "provider unavailable")
				want = codes.Unavailable
				wantReads = 1
			}
			value, err := s.ReadApplicationCredentials(workspaceContext(), r)
			if value != nil || status.Code(err) != want || runtime.reads != wantReads {
				t.Fatalf("code=%s want=%s credential reads=%d want=%d", status.Code(err), want, runtime.reads, wantReads)
			}
		})
	}
}
