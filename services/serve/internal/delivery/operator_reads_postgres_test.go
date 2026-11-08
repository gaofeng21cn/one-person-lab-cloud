package delivery_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/serve/internal/delivery"
)

func TestServeOperatorReadsOwnerTruth(t *testing.T) {
	db, tenant, _ := fixture(t)
	identity := &fakeIdentity{}
	service, err := delivery.New(db, ownerAuthorizer(identity))
	if err != nil {
		t.Fatal(err)
	}
	ctx := serveContext()
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedSelectionDeployment(t, db, "ws-operator", tenant, "dep-old", "superseded", created)
	seedSelectionDeployment(t, db, "ws-operator", tenant, "dep-current", "active", created.Add(time.Second))
	seedSelectionDeployment(t, db, "ws-operator", tenant, "dep-latest", "failed", created.Add(2*time.Second))
	seedSelectionDeployment(t, db, "ws-other", "tenant-other", "dep-other", "active", created)
	operator := call("", true)
	operator.AuthorizationContextId = "operator-context"
	page, err := service.ListOperatorDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: operator, WorkspaceId: "ws-operator", QueryLimit: proto.Int32(1)})
	if err != nil {
		t.Fatalf("operator list: %v", err)
	}
	if len(page.GetItems()) != 1 || page.GetItems()[0].GetId() != "dep-latest" || page.GetNextCursor() != "dep-latest" || page.GetCurrentDeployment().GetId() != "dep-current" || page.GetLatestAttempt().GetId() != "dep-latest" {
		t.Fatalf("operator selections = %v", page)
	}
	assertOperatorAuthorization(t, identity.last, operator, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORDEPLOYMENTS, "ws-operator")
	next, err := service.ListOperatorDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: operator, WorkspaceId: "ws-operator", QueryCursor: proto.String(page.GetNextCursor()), QueryLimit: proto.Int32(1)})
	if err != nil || len(next.GetItems()) != 1 || next.GetItems()[0].GetId() != "dep-current" || next.GetCurrentDeployment().GetId() != "dep-current" || next.GetLatestAttempt().GetId() != "dep-latest" {
		t.Fatalf("operator next = %v / %v", next, err)
	}
	customer, err := service.ListDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: call(tenant, false), WorkspaceId: "ws-operator", QueryLimit: proto.Int32(1)})
	if err != nil || !proto.Equal(page, customer) {
		t.Fatalf("customer/operator truth differs: %v / %v", customer, err)
	}
	access, err := service.GetOperatorWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: operator, WorkspaceId: "ws-operator"})
	if err != nil || access.GetDeploymentId() != "dep-current" || access.GetUrl() != "https://ws-operator.example/dep-current" || !access.GetApplicationCredentialsAvailable() {
		t.Fatalf("operator access = %v / %v", access, err)
	}
	assertOperatorAuthorization(t, identity.last, operator, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATORWORKSPACEACCESS, "ws-operator")
	customerAccess, err := service.GetWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: call(tenant, false), WorkspaceId: "ws-operator"})
	if err != nil || !proto.Equal(access, customerAccess) {
		t.Fatalf("customer/operator access differs: %v / %v", customerAccess, err)
	}
	empty, err := service.ListOperatorDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: operator, WorkspaceId: "ws-undelivered"})
	if err != nil || len(empty.GetItems()) != 0 || empty.GetCurrentDeployment() != nil || empty.GetLatestAttempt() != nil || empty.GetNextCursor() != "" {
		t.Fatalf("undelivered operator list = %v / %v", empty, err)
	}
	missing, err := service.GetOperatorWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: operator, WorkspaceId: "ws-undelivered"})
	assertAccessUnavailable(t, missing, err)
	seedDeployment(t, db, "ws-starting", tenant, "dep-starting", "active", "starting", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	pending, err := service.GetOperatorWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: operator, WorkspaceId: "ws-starting"})
	assertAccessUnavailable(t, pending, err)
}

func assertOperatorAuthorization(t *testing.T, r *api.AuthorizationRequest, c *api.CallContext, action api.AuthorizationActionEnum, workspace string) {
	t.Helper()
	if r.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_SERVE || r.GetAction() != action || r.GetResource().GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE || r.GetResource().GetId() != workspace || !proto.Equal(r.GetScope(), c.GetScope()) || r.GetActorId() != c.GetActorId() || r.GetSessionId() != c.GetSessionId() || r.GetRequestId() != c.GetRequestId() || r.GetAuthorizationContextId() != c.GetAuthorizationContextId() {
		t.Fatalf("operator authorization lost exact binding: %v", r)
	}
}

func TestServeOperatorReadsRejectInvalidContextBeforeDatabase(t *testing.T) {
	db, err := sql.Open("postgres", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	identity := &fakeIdentity{}
	service, err := delivery.New(db, ownerAuthorizer(identity))
	if err != nil {
		t.Fatal(err)
	}
	valid := call("", true)
	for _, test := range []struct {
		name      string
		ctx       context.Context
		call      *api.CallContext
		workspace string
		code      codes.Code
	}{
		{"nil-context", serveContext(), nil, "ws-operator", codes.InvalidArgument},
		{"blank-workspace", serveContext(), valid, " \t", codes.InvalidArgument},
		{"padded-workspace", serveContext(), valid, " ws-operator ", codes.InvalidArgument},
		{"unverified-peer", context.Background(), valid, "ws-operator", codes.Unauthenticated},
		{"workspace-peer", ownerservice.WithPeerOwner(context.Background(), owneridentity.Workspace.Service()), valid, "ws-operator", codes.PermissionDenied},
		{"capability-peer", ownerservice.WithPeerOwner(context.Background(), owneridentity.Capability.Service()), valid, "ws-operator", codes.PermissionDenied},
		{"tenant-peer", ownerservice.WithPeerOwner(context.Background(), owneridentity.Tenant.Service()), valid, "ws-operator", codes.PermissionDenied},
		{"tenant-scope", serveContext(), call("tenant-alpha", false), "ws-operator", codes.PermissionDenied},
		{"foreign-tenant-scope", serveContext(), call("tenant-other", false), "ws-operator", codes.PermissionDenied},
	} {
		t.Run(test.name, func(t *testing.T) { assertOperatorReadCode(t, service, test.ctx, test.call, test.workspace, test.code) })
	}
	for _, test := range []struct {
		name   string
		mutate func(*api.CallContext)
		code   codes.Code
	}{
		{"missing-actor", func(c *api.CallContext) { c.ActorId = "" }, codes.Unauthenticated},
		{"missing-session", func(c *api.CallContext) { c.SessionId = nil }, codes.Unauthenticated},
		{"missing-request", func(c *api.CallContext) { c.RequestId = "" }, codes.Unauthenticated},
		{"missing-scope", func(c *api.CallContext) { c.Scope = nil }, codes.Unauthenticated},
		{"empty-scope", func(c *api.CallContext) { c.Scope = &api.AuthorizationScope{} }, codes.PermissionDenied},
		{"expired", func(c *api.CallContext) { c.DeadlineAt = timestamppb.New(time.Now().Add(-time.Minute)) }, codes.DeadlineExceeded},
		{"invalid-deadline", func(c *api.CallContext) { c.DeadlineAt = &timestamppb.Timestamp{Seconds: 1 << 62} }, codes.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := proto.Clone(valid).(*api.CallContext)
			test.mutate(c)
			assertOperatorReadCode(t, service, serveContext(), c, "ws-operator", test.code)
		})
	}
	if identity.calls != 0 {
		t.Fatalf("invalid operator contexts reached authority %d times", identity.calls)
	}
}

func assertOperatorReadCode(t *testing.T, s *delivery.Service, ctx context.Context, c *api.CallContext, workspace string, code codes.Code) {
	t.Helper()
	page, err := s.ListOperatorDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: c, WorkspaceId: workspace})
	if page != nil || status.Code(err) != code {
		t.Fatalf("operator list = %v / %v, want %s", page, err, code)
	}
	access, err := s.GetOperatorWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: c, WorkspaceId: workspace})
	if access != nil || status.Code(err) != code {
		t.Fatalf("operator access = %v / %v, want %s", access, err, code)
	}
}

type operatorDecisionIdentity struct {
	*fakeIdentity
	mutate func(*api.AuthorizationDecision)
}

func (f operatorDecisionIdentity) AuthorizeAction(ctx context.Context, r *api.AuthorizationRequest, opts ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	d, err := f.fakeIdentity.AuthorizeAction(ctx, r, opts...)
	d = proto.Clone(d).(*api.AuthorizationDecision)
	f.mutate(d)
	return d, err
}

func TestServeOperatorReadsRecheckDecisionBindingsAndDenial(t *testing.T) {
	db, tenant, _ := fixture(t)
	seedSelectionDeployment(t, db, "ws-operator", tenant, "dep-current", "active", time.Now())
	for _, test := range []struct {
		name   string
		mutate func(*api.AuthorizationDecision)
	}{
		{"denied", func(d *api.AuthorizationDecision) { d.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED }},
		{"customer-action", func(d *api.AuthorizationDecision) {
			d.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS
		}},
		{"other-operator-action", func(d *api.AuthorizationDecision) {
			if d.Action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORDEPLOYMENTS {
				d.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATORWORKSPACEACCESS
			} else {
				d.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORDEPLOYMENTS
			}
		}},
		{"resource", func(d *api.AuthorizationDecision) { d.Resource.Id = proto.String("ws-other") }},
		{"resource-kind", func(d *api.AuthorizationDecision) {
			d.Resource.Kind = api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_OPERATION
		}},
		{"actor", func(d *api.AuthorizationDecision) { d.ActorId = "actor-other" }},
		{"session", func(d *api.AuthorizationDecision) { d.SessionId = proto.String("session-other") }},
		{"scope", func(d *api.AuthorizationDecision) { d.Scope = call(tenant, false).Scope }},
		{"audience", func(d *api.AuthorizationDecision) { d.AudienceOwner = api.OwnerEnum_OWNER_ENUM_WORKSPACE }},
	} {
		t.Run(test.name, func(t *testing.T) {
			identity := operatorDecisionIdentity{fakeIdentity: &fakeIdentity{}, mutate: test.mutate}
			service, err := delivery.New(db, ownerservice.NewAuthorizer(ownerservice.OwnerServe, identity).Authorize)
			if err != nil {
				t.Fatal(err)
			}
			assertOperatorReadCode(t, service, serveContext(), call("", true), "ws-operator", codes.PermissionDenied)
		})
	}
}

func TestServeCustomerReadsKeepCustomerActionsForPlatformScope(t *testing.T) {
	db, tenant, _ := fixture(t)
	seedSelectionDeployment(t, db, "ws-customer-action", tenant, "dep-current", "active", time.Now())
	identity := &fakeIdentity{deny: true}
	service, err := delivery.New(db, ownerAuthorizer(identity))
	if err != nil {
		t.Fatal(err)
	}
	platform := call("", true)
	if page, err := service.ListDeployments(serveContext(), &api.ListDeploymentsRpcRequest{Context: platform, WorkspaceId: "ws-customer-action"}); page != nil || status.Code(err) != codes.PermissionDenied {
		t.Fatalf("customer denial became success: %v / %v", page, err)
	}
	if identity.last.GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS {
		t.Fatalf("customer list action switched: %v", identity.last)
	}
	if access, err := service.GetWorkspaceAccess(serveContext(), &api.GetWorkspaceAccessRpcRequest{Context: platform, WorkspaceId: "ws-customer-action"}); access != nil || status.Code(err) != codes.PermissionDenied {
		t.Fatalf("customer access denial became success: %v / %v", access, err)
	}
	if identity.last.GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEACCESS {
		t.Fatalf("customer access action switched: %v", identity.last)
	}
}

func TestServeOperatorAccessWithoutDeployment(t *testing.T) {
	db, _, _ := fixture(t)
	service, err := delivery.New(db, ownerAuthorizer(&fakeIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	access, err := service.GetOperatorWorkspaceAccess(serveContext(), &api.GetWorkspaceAccessRpcRequest{Context: call("", true), WorkspaceId: "ws-undelivered"})
	assertAccessUnavailable(t, access, err)
}
