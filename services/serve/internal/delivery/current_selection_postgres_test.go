package delivery_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/serve/internal/delivery"
)

func TestServeDeploymentListRejectsInvalidContextBeforeDatabase(t *testing.T) {
	// A closed pool makes accidental database access return Internal. Validation
	// must retain its original error even while persistence is unavailable.
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
	valid := call("tenant-alpha", false)
	missingActor := proto.Clone(valid).(*api.CallContext)
	missingActor.ActorId = ""
	expired := proto.Clone(valid).(*api.CallContext)
	expired.DeadlineAt = timestamppb.New(time.Now().Add(-time.Minute))
	invalidDeadline := proto.Clone(valid).(*api.CallContext)
	invalidDeadline.DeadlineAt = &timestamppb.Timestamp{Seconds: 1 << 62}
	acceptedGrant := proto.Clone(valid).(*api.CallContext)
	acceptedGrant.SessionId = nil
	acceptedGrant.AcceptedOperationGrantId = proto.String("original-grant")
	for _, test := range []struct {
		name    string
		ctx     context.Context
		request *api.ListDeploymentsRpcRequest
		code    codes.Code
		message string
	}{
		{name: "nil-request", ctx: serveContext(), code: codes.InvalidArgument, message: "authenticated call context and workspace are required"},
		{name: "missing-context", ctx: serveContext(), request: &api.ListDeploymentsRpcRequest{WorkspaceId: "ws-owned"}, code: codes.InvalidArgument, message: "authenticated call context and workspace are required"},
		{name: "blank-workspace", ctx: serveContext(), request: &api.ListDeploymentsRpcRequest{Context: valid, WorkspaceId: " \t"}, code: codes.InvalidArgument, message: "authenticated call context and workspace are required"},
		{name: "unverified-peer", ctx: context.Background(), request: &api.ListDeploymentsRpcRequest{Context: valid, WorkspaceId: "ws-owned"}, code: codes.Unauthenticated, message: "verified calling service is required"},
		{name: "missing-actor", ctx: serveContext(), request: &api.ListDeploymentsRpcRequest{Context: missingActor, WorkspaceId: "ws-owned"}, code: codes.Unauthenticated, message: "actor, request, scope and session or accepted grant are required"},
		{name: "expired-context", ctx: serveContext(), request: &api.ListDeploymentsRpcRequest{Context: expired, WorkspaceId: "ws-owned"}, code: codes.DeadlineExceeded, message: "call context expired"},
		{name: "invalid-deadline", ctx: serveContext(), request: &api.ListDeploymentsRpcRequest{Context: invalidDeadline, WorkspaceId: "ws-owned"}, code: codes.DeadlineExceeded, message: "call context expired"},
		{name: "valid-session-reaches-database", ctx: serveContext(), request: &api.ListDeploymentsRpcRequest{Context: valid, WorkspaceId: "ws-owned"}, code: codes.Internal, message: "serve persistence failed"},
		{name: "valid-grant-reaches-database", ctx: serveContext(), request: &api.ListDeploymentsRpcRequest{Context: acceptedGrant, WorkspaceId: "ws-owned"}, code: codes.Internal, message: "serve persistence failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			page, err := service.ListDeployments(test.ctx, test.request)
			if page != nil || status.Code(err) != test.code || status.Convert(err).Message() != test.message {
				t.Fatalf("list validation = %v / %v, want %s: %s", page, err, test.code, test.message)
			}
		})
	}
	if identity.calls != 0 {
		t.Fatalf("invalid or database-unavailable requests reached CloudIdentity %d times", identity.calls)
	}
}

func seedSelectionDeployment(t *testing.T, db *sql.DB, workspace, tenant, id, state string, created time.Time) {
	t.Helper()
	runtimeState, url := "", ""
	if state == "active" {
		runtimeState, url = "ready", "https://"+workspace+".example/"+id
	}
	seedDeployment(t, db, workspace, tenant, id, state, runtimeState, url,
		api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	if _, err := db.ExecContext(context.Background(), `UPDATE serve.agent_deployments SET created_at=$2 WHERE id=$1`, id, created); err != nil {
		t.Fatal(err)
	}
}

func TestServeDeploymentSelectionsIgnoreHistoryPagination(t *testing.T) {
	db, tenant, _ := fixture(t)
	identity := &fakeIdentity{}
	service, err := delivery.New(db, ownerAuthorizer(identity))
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	const workspace = "ws-selection"
	seedSelectionDeployment(t, db, workspace, tenant, "dep-active", "active", created)
	for i := 1; i <= 55; i++ {
		seedSelectionDeployment(t, db, workspace, tenant, fmt.Sprintf("dep-failed-%02d", i), "failed", created.Add(time.Duration(i)*time.Second))
	}
	// Equal creation times must use the same id tie-break as the history index.
	for _, id := range []string{"dep-attempt-a", "dep-attempt-z"} {
		seedSelectionDeployment(t, db, workspace, tenant, id, "queued", created.Add(time.Minute))
	}
	for _, test := range []struct {
		name, cursor, first string
		limit               int32
		count               int
	}{
		{name: "default-page-excludes-active", first: "dep-attempt-z", count: 50},
		{name: "limit-one", first: "dep-attempt-z", limit: 1, count: 1},
		{name: "limit-includes-all-history", first: "dep-attempt-z", limit: 100, count: 58},
		{name: "cursor-excludes-latest", cursor: "dep-attempt-z", first: "dep-attempt-a", limit: 2, count: 2},
		{name: "page-containing-only-active", cursor: "dep-failed-01", first: "dep-active", limit: 1, count: 1},
		{name: "empty-history-page", cursor: "dep-active", limit: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			page, err := service.ListDeployments(serveContext(), &api.ListDeploymentsRpcRequest{
				Context: call(tenant, false), WorkspaceId: workspace, QueryCursor: proto.String(test.cursor), QueryLimit: proto.Int32(test.limit),
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.GetItems()) != test.count || test.count > 0 && page.GetItems()[0].GetId() != test.first {
				t.Fatalf("history page = %v, want count=%d first=%q", page, test.count, test.first)
			}
			if page.GetCurrentDeployment().GetId() != "dep-active" || page.GetCurrentDeployment().GetStatus() != api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE {
				t.Fatalf("current selection followed history pagination: %v", page)
			}
			if page.GetLatestAttempt().GetId() != "dep-attempt-z" || page.GetLatestAttempt().GetStatus() != api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_QUEUED {
				t.Fatalf("latest attempt followed history pagination or lost its tie-break: %v", page)
			}
			if identity.last.GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS {
				t.Fatalf("list selection changed its authorization action: %v", identity.last)
			}
		})
	}
	access, err := service.GetWorkspaceAccess(serveContext(), &api.GetWorkspaceAccessRpcRequest{Context: call(tenant, false), WorkspaceId: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if access.GetDeploymentId() != "dep-active" || access.GetUrl() != "https://ws-selection.example/dep-active" {
		t.Fatalf("access does not identify the active deployment supplying its URL: %v", access)
	}
	if identity.last.GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEACCESS {
		t.Fatalf("access lost its separate authorization action: %v", identity.last)
	}
}

func TestServeDeploymentSelectionsReadOwnApplicationSource(t *testing.T) {
	db, tenant, _ := fixture(t)
	service, err := delivery.New(db, ownerAuthorizer(&fakeIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"agent", "opl_app"} {
		workspace, id := "ws-source-"+kind, "dep-source-"+kind
		seedSelectionDeployment(t, db, workspace, tenant, id, "active", time.Now().UTC())
		if kind == "opl_app" {
			if _, err := db.ExecContext(context.Background(), `UPDATE serve.agent_deployments SET application_kind='opl_app',capability_version_id='',runtime_version_id='runtime-release-current' WHERE id=$1`, id); err != nil {
				t.Fatal(err)
			}
		}
		page, err := service.ListDeployments(serveContext(), &api.ListDeploymentsRpcRequest{Context: call(tenant, false), WorkspaceId: workspace, QueryLimit: proto.Int32(1)})
		if err != nil {
			t.Fatal(err)
		}
		exact, err := service.GetDeployment(serveContext(), &api.GetDeploymentRpcRequest{Context: call(tenant, false), WorkspaceId: workspace, DeploymentId: id})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range []*api.Deployment{page.GetCurrentDeployment(), page.GetLatestAttempt(), page.GetItems()[0], exact} {
			if kind == "opl_app" {
				if item.GetCapabilityVersionId() != "" || item.GetRuntimeVersionId() != "runtime-release-current" {
					t.Fatalf("default App source came from another deployment: %v", item)
				}
			} else if item.GetCapabilityVersionId() == "" || item.RuntimeVersionId != nil {
				t.Fatalf("Agent source was confused with a default App: %v", item)
			}
		}
	}
}

func TestServeDeploymentSelectionsWithoutActive(t *testing.T) {
	db, tenant, _ := fixture(t)
	service, err := delivery.New(db, ownerAuthorizer(&fakeIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		state  string
		status api.DeploymentStatusEnum
	}{
		{"queued", api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_QUEUED},
		{"failed", api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_FAILED},
	} {
		t.Run(test.state, func(t *testing.T) {
			workspace, id := "ws-initial-"+test.state, "dep-initial-"+test.state
			created := time.Now().UTC()
			historyCount := 0
			if test.state == "failed" {
				seedSelectionDeployment(t, db, workspace, tenant, id+"-older", "queued", created.Add(-time.Second))
				historyCount = 1
			}
			seedSelectionDeployment(t, db, workspace, tenant, id, test.state, created)
			// A continuation page, even when empty, retains the global latest attempt.
			page, err := service.ListDeployments(serveContext(), &api.ListDeploymentsRpcRequest{
				Context: call(tenant, false), WorkspaceId: workspace, QueryCursor: proto.String(id), QueryLimit: proto.Int32(1),
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.GetItems()) != historyCount || page.GetCurrentDeployment() != nil || page.GetLatestAttempt().GetId() != id || page.GetLatestAttempt().GetStatus() != test.status {
				t.Fatalf("initial attempt was confused with an active deployment: %v", page)
			}
			access, err := service.GetWorkspaceAccess(serveContext(), &api.GetWorkspaceAccessRpcRequest{Context: call(tenant, false), WorkspaceId: workspace})
			assertAccessUnavailable(t, access, err)
		})
	}
	// Empty selections are an owner-confirmed absence, not an authorization bypass.
	page, err := service.ListDeployments(serveContext(), &api.ListDeploymentsRpcRequest{Context: call(tenant, true), WorkspaceId: "ws-no-delivery"})
	if err != nil || page == nil || len(page.GetItems()) != 0 || page.GetCurrentDeployment() != nil || page.GetLatestAttempt() != nil {
		t.Fatalf("empty platform read = %v / %v", page, err)
	}
	for _, workspace := range []string{"ws-no-delivery", "ws-initial-queued"} {
		page, err := service.ListDeployments(serveContext(), &api.ListDeploymentsRpcRequest{Context: call("tenant-other", false), WorkspaceId: workspace})
		assertReadRefused(t, err)
		if page != nil {
			t.Fatalf("refused read leaked selection: %v", page)
		}
	}
}

func TestServeDeploymentSelectionsShareReadSnapshot(t *testing.T) {
	db, tenant, _ := fixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	const workspace = "ws-snapshot"
	seedSelectionDeployment(t, db, workspace, tenant, "dep-before", "active", created)
	authorize := ownerAuthorizer(&fakeIdentity{})
	switched := false
	service, err := delivery.New(db, func(ctx context.Context, c *api.CallContext, action api.AuthorizationActionEnum, resource *api.AuthorizationResource, scope ownerservice.ResourceScope) error {
		if err := authorize(ctx, c, action, resource, scope); err != nil {
			return err
		}
		if !switched {
			// The tenant guard has established the list transaction's snapshot. Commit
			// a switch on a different connection before history and selection are read.
			switched = true
			if _, err := db.ExecContext(ctx, `UPDATE serve.agent_deployments SET status='superseded' WHERE id='dep-before'`); err != nil {
				t.Fatal(err)
			}
			seedSelectionDeployment(t, db, workspace, tenant, "dep-after", "active", created.Add(time.Second))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	request := &api.ListDeploymentsRpcRequest{Context: call(tenant, false), WorkspaceId: workspace}
	page, err := service.ListDeployments(serveContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !switched || len(page.GetItems()) != 1 || page.GetItems()[0].GetId() != "dep-before" || page.GetItems()[0].GetStatus() != api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE || page.GetCurrentDeployment().GetId() != "dep-before" || page.GetLatestAttempt().GetId() != "dep-before" {
		t.Fatalf("list mixed pre-switch and post-switch snapshots: %v", page)
	}
	next, err := service.ListDeployments(serveContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	if next.GetCurrentDeployment().GetId() != "dep-after" || next.GetLatestAttempt().GetId() != "dep-after" || len(next.GetItems()) != 2 {
		t.Fatalf("next list failed to observe the committed switch: %v", next)
	}
	access, err := service.GetWorkspaceAccess(serveContext(), &api.GetWorkspaceAccessRpcRequest{Context: call(tenant, false), WorkspaceId: workspace})
	if err != nil || access.GetDeploymentId() != "dep-after" || access.GetUrl() != "https://ws-snapshot.example/dep-after" {
		t.Fatalf("post-switch access = %v / %v", access, err)
	}
}

func TestServeDeploymentSelectionsPreserveReadFailures(t *testing.T) {
	db, tenant, _ := fixture(t)
	service, err := delivery.New(db, ownerAuthorizer(&fakeIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name, corruptID, cursor string
	}{
		{"current-outside-page", "dep-current-current-outside-page", ""},
		{"latest-outside-page", "dep-latest-latest-outside-page", "dep-latest-latest-outside-page"},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := "ws-" + test.name
			seedSelectionDeployment(t, db, workspace, tenant, "dep-current-"+test.name, "active", created)
			seedSelectionDeployment(t, db, workspace, tenant, "dep-latest-"+test.name, "failed", created.Add(time.Second))
			if _, err := db.ExecContext(context.Background(), `UPDATE serve.agent_deployments SET data_compatibility='{"unexpected":true}'::jsonb WHERE id=$1`, test.corruptID); err != nil {
				t.Fatal(err)
			}
			page, err := service.ListDeployments(serveContext(), &api.ListDeploymentsRpcRequest{
				Context: call(tenant, false), WorkspaceId: workspace, QueryLimit: proto.Int32(1), QueryCursor: proto.String(test.cursor),
			})
			if page != nil || status.Code(err) != codes.DataLoss {
				t.Fatalf("selection decode failure became an empty selection: %v / %v", page, err)
			}
		})
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	page, err := service.ListDeployments(serveContext(), &api.ListDeploymentsRpcRequest{Context: call(tenant, true), WorkspaceId: "ws-unavailable"})
	if page != nil || status.Code(err) != codes.Internal {
		t.Fatalf("database failure became an empty list: %v / %v", page, err)
	}
	access, err := service.GetWorkspaceAccess(serveContext(), &api.GetWorkspaceAccessRpcRequest{Context: call(tenant, true), WorkspaceId: "ws-unavailable"})
	if access != nil || status.Code(err) != codes.Internal {
		t.Fatalf("database failure became unavailable access: %v / %v", access, err)
	}
}
