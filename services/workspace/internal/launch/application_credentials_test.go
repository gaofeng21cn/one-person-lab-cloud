package launch

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerservice"
)

type credentialServe struct {
	api.ServeProductServiceClient
	api.ServeRuntimeAdapterClient
	page          *api.DeploymentPage
	access        *api.WorkspaceAccess
	response      *api.WorkspaceApplicationCredentials
	last          *api.ReadApplicationCredentialsRequest
	accessErr     error
	listErr       error
	credentialErr error
	reads         int
	lists         []*api.ListDeploymentsRpcRequest
}

func (s *credentialServe) GetWorkspaceAccess(_ context.Context, r *api.GetWorkspaceAccessRpcRequest, _ ...grpc.CallOption) (*api.WorkspaceAccess, error) {
	s.reads++
	if r.GetContext().GetAuthorizationContextId() != "" {
		panic("cross-owner authorization context")
	}
	return s.access, s.accessErr
}
func (s *credentialServe) ListDeployments(_ context.Context, r *api.ListDeploymentsRpcRequest, _ ...grpc.CallOption) (*api.DeploymentPage, error) {
	s.lists = append(s.lists, proto.Clone(r).(*api.ListDeploymentsRpcRequest))
	return s.page, s.listErr
}
func (s *credentialServe) ReadApplicationCredentials(_ context.Context, r *api.ReadApplicationCredentialsRequest, _ ...grpc.CallOption) (*api.WorkspaceApplicationCredentials, error) {
	s.last = proto.Clone(r).(*api.ReadApplicationCredentialsRequest)
	return s.response, s.credentialErr
}
func credentialServeFixture() *credentialServe {
	failed := &api.Deployment{Id: "failed-newer", WorkspaceId: "workspace-original", Status: api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_FAILED}
	return &credentialServe{access: &api.WorkspaceAccess{WorkspaceId: "workspace-original", DeploymentId: "deployment-current", ApplicationCredentialsAvailable: proto.Bool(true)},
		page: &api.DeploymentPage{
			Items: []*api.Deployment{failed}, NextCursor: proto.String("next"), LatestAttempt: failed,
			CurrentDeployment: &api.Deployment{Id: "deployment-current", WorkspaceId: "workspace-original", RuntimeInstanceId: proto.String("runtime-current"), Status: api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE},
		},
		response: &api.WorkspaceApplicationCredentials{WorkspaceId: "workspace-original", RuntimeInstanceId: "runtime-current", Username: "declared-admin", Password: "fixture-password"}}
}
func TestRevealApplicationCredentialsRequiresLiveSessionBeforeStore(t *testing.T) {
	c, ctx := modelCallContext("tenant-original")
	for _, tc := range []struct {
		name string
		call *api.CallContext
		code codes.Code
	}{{"missing context", nil, codes.Unauthenticated}, {"missing session", &api.CallContext{ActorId: c.ActorId, RequestId: c.RequestId, Scope: c.Scope}, codes.Unauthenticated}, {"grant is not customer session", &api.CallContext{ActorId: c.ActorId, RequestId: c.RequestId, Scope: c.Scope, AcceptedOperationGrantId: proto.String("grant")}, codes.Unauthenticated}} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (&Service{}).RevealWorkspaceApplicationCredentials(ctx, &api.RevealWorkspaceApplicationCredentialsRpcRequest{Context: tc.call, WorkspaceId: "workspace-original"})
			if status.Code(err) != tc.code {
				t.Fatalf("code=%s want=%s", status.Code(err), tc.code)
			}
		})
	}
}
func TestRevealApplicationCredentialsUsesCurrentServeAndNoPersistence(t *testing.T) {
	db := runtimeDatabase(t)
	s, _, _, _ := seedRuntimeOrder(t, db)
	s.Auth = ownerservice.NewAuthorizer(ownerservice.OwnerWorkspace, &modelReadIdentity{})
	if _, err := db.Exec(`UPDATE workspace.workspaces SET status='active' WHERE id='workspace-original'`); err != nil {
		t.Fatal(err)
	}
	c, ctx := modelCallContext("tenant-original")
	c.AuthorizationContextId = "workspace-proof"
	upstream := credentialServeFixture()
	s.ServeProduct = upstream
	s.ServeCredentials = upstream
	req := &api.RevealWorkspaceApplicationCredentialsRpcRequest{Context: c, WorkspaceId: "workspace-original"}
	var before string
	if err := db.QueryRow(`SELECT result::text FROM workspace.operations WHERE id='operation-original'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		value, err := s.RevealWorkspaceApplicationCredentials(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if value.GetUsername() != "declared-admin" || value.GetRuntimeInstanceId() != "runtime-current" {
			t.Fatal("incorrect current credential projection")
		}
		// History deliberately contains only a newer failed attempt and has a next
		// cursor. Each reveal consumes the owner selection with exactly one read.
		if len(upstream.lists) != i+1 {
			t.Fatalf("reveal read deployment history %d times, want %d", len(upstream.lists), i+1)
		}
		list := upstream.lists[i]
		if list.GetWorkspaceId() != "workspace-original" || list.GetQueryLimit() != 1 || list.GetQueryCursor() != "" || list.GetContext().GetAuthorizationContextId() != "" || list.GetContext().GetSessionId() != c.GetSessionId() {
			t.Fatalf("incorrect owner selection request: %v", list)
		}
		upstream.response.Password = "changed-in-runtime"
	}
	if upstream.reads != 2 || upstream.last.GetDeploymentId() != "deployment-current" || upstream.last.GetRuntimeInstanceId() != "runtime-current" || upstream.last.GetContext().GetSessionId() != c.GetSessionId() {
		t.Fatal("current runtime or live session not forwarded")
	}
	if upstream.last.GetContext().GetAuthorizationContextId() != "workspace-proof" || c.GetAuthorizationContextId() != "workspace-proof" {
		t.Fatal("explicit reveal decision was lost or mutated before its resource-read continuation")
	}
	var after string
	if err := db.QueryRow(`SELECT result::text FROM workspace.operations WHERE id='operation-original'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after || strings.Contains(after, "fixture-password") || strings.Contains(after, "changed-in-runtime") {
		t.Fatal("credential read persisted data")
	}
}
func TestRevealApplicationCredentialsRejectsOwnerStateAndReadbackBoundaries(t *testing.T) {
	db := runtimeDatabase(t)
	s, _, _, _ := seedRuntimeOrder(t, db)
	c, ctx := modelCallContext("tenant-original")
	for _, name := range []string{"owner denied", "foreign tenant", "inactive", "serve unavailable", "credentials unavailable", "foreign access", "missing access deployment", "list unavailable", "missing page", "missing selection", "failed selection", "missing deployment identity", "foreign deployment", "missing runtime", "access deployment mismatch", "credential read failed", "foreign credentials", "wrong runtime", "empty username", "empty password"} {
		t.Run(name, func(t *testing.T) {
			if _, err := db.Exec(`UPDATE workspace.workspaces SET status='active' WHERE id='workspace-original'`); err != nil {
				t.Fatal(err)
			}
			identity := &modelReadIdentity{}
			s.Auth = ownerservice.NewAuthorizer(ownerservice.OwnerWorkspace, identity)
			upstream := credentialServeFixture()
			s.ServeProduct = upstream
			s.ServeCredentials = upstream
			call := proto.Clone(c).(*api.CallContext)
			want := codes.FailedPrecondition
			revealExpected := false
			switch name {
			case "owner denied":
				identity.denial = status.Error(codes.PermissionDenied, "owner required")
				want = codes.PermissionDenied
			case "foreign tenant":
				call.Scope.GetTenant().TenantId = "foreign"
				want = codes.PermissionDenied
			case "inactive":
				if _, err := db.Exec(`UPDATE workspace.workspaces SET status='suspended' WHERE id='workspace-original'`); err != nil {
					t.Fatal(err)
				}
			case "serve unavailable":
				upstream.accessErr = status.Error(codes.Unavailable, "unavailable")
				want = codes.Unavailable
			case "credentials unavailable":
				upstream.access.ApplicationCredentialsAvailable = proto.Bool(false)
			case "foreign access":
				upstream.access.WorkspaceId = "foreign"
			case "missing access deployment":
				upstream.access.DeploymentId = ""
			case "list unavailable":
				upstream.listErr = status.Error(codes.Unavailable, "deployment read unavailable")
				want = codes.Unavailable
			case "missing page":
				upstream.page = nil
				want = codes.Unavailable
			case "missing selection":
				// Even an active history item cannot replace an absent owner selection.
				upstream.page.Items = []*api.Deployment{upstream.page.CurrentDeployment}
				upstream.page.CurrentDeployment = nil
			case "failed selection":
				upstream.page.CurrentDeployment.Status = api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_FAILED
			case "missing deployment identity":
				upstream.page.CurrentDeployment.Id = ""
			case "foreign deployment":
				upstream.page.CurrentDeployment.WorkspaceId = "foreign"
			case "missing runtime":
				upstream.page.CurrentDeployment.RuntimeInstanceId = nil
			case "access deployment mismatch":
				upstream.access.DeploymentId = "deployment-other"
			case "credential read failed":
				upstream.credentialErr = status.Error(codes.Unavailable, "credential read unavailable")
				want = codes.Unavailable
				revealExpected = true
			case "foreign credentials":
				upstream.response.WorkspaceId = "foreign"
				revealExpected = true
			case "wrong runtime":
				upstream.response.RuntimeInstanceId = "old-runtime"
				revealExpected = true
			case "empty username":
				upstream.response.Username = " \t"
				revealExpected = true
			case "empty password":
				upstream.response.Password = ""
				revealExpected = true
			}
			value, err := s.RevealWorkspaceApplicationCredentials(ctx, &api.RevealWorkspaceApplicationCredentialsRpcRequest{Context: call, WorkspaceId: "workspace-original"})
			if value != nil || status.Code(err) != want {
				t.Fatalf("code=%s want=%s", status.Code(err), want)
			}
			if (name == "owner denied" || name == "foreign tenant" || name == "inactive") && upstream.reads != 0 {
				t.Fatal("rejected request reached Serve")
			}
			if (upstream.last != nil) != revealExpected {
				t.Fatalf("credential reveal reached=%t, want %t", upstream.last != nil, revealExpected)
			}
		})
	}
}
