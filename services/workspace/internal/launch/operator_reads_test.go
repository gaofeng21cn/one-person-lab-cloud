package launch

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"opl-cloud/services/internal/ownerstore"
)

type operatorReadIdentity struct {
	api.CloudIdentityAuthorizationClient
	deny bool
	last *api.AuthorizationRequest
}

func (i *operatorReadIdentity) AuthorizeAction(_ context.Context, request *api.AuthorizationRequest, _ ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	i.last = request
	if i.deny {
		return nil, status.Error(codes.PermissionDenied, "operator action denied")
	}
	now := time.Now()
	return &api.AuthorizationDecision{
		Issuer:  api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY,
		Result:  api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED,
		ActorId: request.GetActorId(), SessionId: request.SessionId,
		Scope: request.GetScope(), AudienceOwner: request.GetAudienceOwner(),
		Action: request.GetAction(), Resource: request.GetResource(),
		PermissionVersion: 1, IssuedAt: timestamppb.New(now.Add(-time.Second)),
		ExpiresAt: timestamppb.New(now.Add(time.Minute)),
	}, nil
}

func operatorReadService(t *testing.T, db *sql.DB) (*Service, *operatorReadIdentity) {
	t.Helper()
	identity := &operatorReadIdentity{}
	store, err := ownerstore.New(db, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{Store: store, Auth: ownerservice.NewAuthorizer(owneridentity.Workspace, identity)}
	return service, identity
}

func TestListOperatorWorkspacesUsesOriginalCreateAndPlatformAuthorization(t *testing.T) {
	db := runtimeDatabase(t)
	service, identity := operatorReadService(t, db)
	_, op, _, _ := seedRuntimeOrder(t, db)
	if _, err := db.ExecContext(t.Context(), `UPDATE workspace.workspaces SET status='active' WHERE id=$1`, op.ResourceID); err != nil {
		t.Fatal(err)
	}
	modelResult, _ := json.Marshal(map[string]any{"resourceSetId": "wrong-model-operation-set"})
	if _, err := db.ExecContext(t.Context(), `INSERT INTO workspace.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,request_id,accepted_input,result,observation_result)
		VALUES('models-operation','tenant-original','actor-original','update_models','workspace-original','running','reload','request-models','{}'::jsonb,$1,'confirmed')`, modelResult); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE workspace.workspaces SET active_operation_id='models-operation' WHERE id='workspace-original'`); err != nil {
		t.Fatal(err)
	}
	call, ctx := operatorReadCall()
	page, err := service.ListOperatorWorkspaces(ctx, &api.ListOperatorWorkspacesRpcRequest{Context: call, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.GetTotal() != 1 || len(page.GetItems()) != 1 || page.GetPage() != 1 || page.GetPageSize() != 10 {
		t.Fatalf("page=%v", page)
	}
	item := page.GetItems()[0]
	if item.GetWorkspace().GetId() != op.ResourceID || item.GetTenantId() != op.TenantID || item.GetCreatedBy() != op.ActorID || item.GetOperationId() != op.ID || item.GetResourceSetId() != "resource-set-original" {
		t.Fatalf("operator item=%v", item)
	}
	if identity.last.GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORWORKSPACES || identity.last.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || identity.last.GetScope().GetPlatform() == nil || identity.last.GetResource().GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE || identity.last.GetResource().Id != nil {
		t.Fatalf("authorization=%v", identity.last)
	}
	detail, err := service.GetOperatorWorkspace(ctx, &api.GetOperatorWorkspaceRpcRequest{Context: call, WorkspaceId: op.ResourceID})
	if err != nil || !proto.Equal(detail, item) {
		t.Fatalf("detail=%v list=%v err=%v", detail, item, err)
	}
	// Business fields retain the shared Customer decoder; only the management
	// reference is sourced from the create operation, not the models result.
	w, tenant, err := scanWorkspace(db.QueryRowContext(ctx, `SELECT `+workspaceColumns+workspaceJoin+` WHERE w.id=$1`, op.ResourceID))
	if err != nil || tenant != item.TenantId || !proto.Equal(w, item.Workspace) {
		t.Fatalf("shared Workspace DTO=%v operator=%v err=%v", w, item, err)
	}
}

func TestOperatorWorkspaceReadsRequirePlatformScopeAndIndependentAction(t *testing.T) {
	db := runtimeDatabase(t)
	service, identity := operatorReadService(t, db)
	_, op, _, _ := seedRuntimeOrder(t, db)
	call, ctx := operatorReadCall()
	item, err := service.GetOperatorWorkspace(ctx, &api.GetOperatorWorkspaceRpcRequest{Context: call, WorkspaceId: op.ResourceID})
	if err != nil || item.GetOperationId() != op.ID {
		t.Fatalf("item=%v err=%v", item, err)
	}
	if identity.last.GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATORWORKSPACE || identity.last.GetResource().GetId() != op.ResourceID || identity.last.GetScope().GetPlatform() == nil {
		t.Fatalf("authorization=%v", identity.last)
	}
	identity.deny = true
	if _, err = service.GetOperatorWorkspace(ctx, &api.GetOperatorWorkspaceRpcRequest{Context: call, WorkspaceId: op.ResourceID}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("denied detail error=%v", err)
	}
	if _, err = service.ListOperatorWorkspaces(ctx, &api.ListOperatorWorkspacesRpcRequest{Context: call, Page: 1, PageSize: 10}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("denied list error=%v", err)
	}
	tenantCall := *call
	tenantCall.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: op.TenantID}}}
	if _, err = service.GetOperatorWorkspace(ctx, &api.GetOperatorWorkspaceRpcRequest{Context: &tenantCall, WorkspaceId: op.ResourceID}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("tenant-scoped operator read error=%v", err)
	}
	identity.last = nil
	if _, err = service.ListOperatorWorkspaces(ctx, &api.ListOperatorWorkspacesRpcRequest{Context: &tenantCall, Page: 1, PageSize: 10}); status.Code(err) != codes.PermissionDenied || identity.last != nil {
		t.Fatalf("tenant-scoped list error=%v authorization=%v", err, identity.last)
	}
	if _, err = service.GetOperatorWorkspace(context.Background(), &api.GetOperatorWorkspaceRpcRequest{Context: call, WorkspaceId: op.ResourceID}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unverified caller error=%v", err)
	}
}

func TestOperatorWorkspaceReadRejectsInvalidParametersBeforeAuthorization(t *testing.T) {
	db := runtimeDatabase(t)
	service, identity := operatorReadService(t, db)
	call, ctx := operatorReadCall()
	for _, request := range []*api.ListOperatorWorkspacesRpcRequest{
		{Context: call, Page: 0, PageSize: 10},
		{Context: call, Page: -1, PageSize: 10},
		{Context: call, Page: 1, PageSize: 0},
		{Context: call, Page: 1, PageSize: -1},
		{Context: call, Page: 1, PageSize: 101},
	} {
		if _, err := service.ListOperatorWorkspaces(ctx, request); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("request=%v error=%v", request, err)
		}
	}
	if _, err := service.GetOperatorWorkspace(ctx, &api.GetOperatorWorkspaceRpcRequest{Context: call, WorkspaceId: " workspace-original"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("whitespace id error=%v", err)
	}
	if identity.last != nil {
		t.Fatalf("invalid requests reached authorization: %v", identity.last)
	}
}

func TestOperatorWorkspacePagesAndMissingCreateReferences(t *testing.T) {
	db := runtimeDatabase(t)
	service, _ := operatorReadService(t, db)
	_, op, _, _ := seedRuntimeOrder(t, db)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO workspace.workspaces(id,tenant_id,name,status,compute_plan_id,storage_plan_id,created_by,version,created_at)
		SELECT 'workspace-newer','tenant-other','Another tenant',status,compute_plan_id,storage_plan_id,'actor-other',1,created_at+interval '1 second'
		FROM workspace.workspaces WHERE id=$1`, op.ResourceID); err != nil {
		t.Fatal(err)
	}
	call, ctx := operatorReadCall()
	for _, tc := range []struct {
		page int32
		id   string
	}{{1, "workspace-newer"}, {2, op.ResourceID}, {3, ""}, {2147483647, ""}} {
		page, err := service.ListOperatorWorkspaces(ctx, &api.ListOperatorWorkspacesRpcRequest{Context: call, Page: tc.page, PageSize: 1})
		if err != nil || page.GetTotal() != 2 || page.GetPage() != tc.page || page.GetPageSize() != 1 {
			t.Fatalf("page=%v err=%v", page, err)
		}
		if tc.id == "" {
			if len(page.Items) != 0 {
				t.Fatalf("out of range page=%v", page)
			}
		} else if len(page.Items) != 1 || page.Items[0].GetWorkspace().GetId() != tc.id {
			t.Fatalf("page=%v want=%s", page, tc.id)
		}
	}
	item, err := service.GetOperatorWorkspace(ctx, &api.GetOperatorWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-newer"})
	if err != nil || item.GetTenantId() != "tenant-other" || item.GetCreatedBy() != "actor-other" || item.GetOperationId() != "" || item.GetResourceSetId() != "" {
		t.Fatalf("unprovisioned item=%v err=%v", item, err)
	}
	if _, err = service.GetOperatorWorkspace(ctx, &api.GetOperatorWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-missing"}); status.Code(err) != codes.NotFound {
		t.Fatalf("missing Workspace error=%v", err)
	}
}

func TestOperatorWorkspaceRejectsCorruptCreateReferences(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
	}{
		{"wrong result type", `UPDATE workspace.operations SET result='{"resourceSetId":42}' WHERE id='operation-original'`},
		{"wrong tenant", `UPDATE workspace.operations SET tenant_id='tenant-wrong' WHERE id='operation-original'`},
		{"ambiguous create", `INSERT INTO workspace.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,request_id,accepted_input,result)
			SELECT 'duplicate-create',tenant_id,actor_id,kind,resource_id,status,stage,'duplicate-request',accepted_input,result FROM workspace.operations WHERE id='operation-original'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := runtimeDatabase(t)
			service, _ := operatorReadService(t, db)
			_, op, _, _ := seedRuntimeOrder(t, db)
			if _, err := db.ExecContext(t.Context(), tc.sql); err != nil {
				t.Fatal(err)
			}
			call, ctx := operatorReadCall()
			if _, err := service.GetOperatorWorkspace(ctx, &api.GetOperatorWorkspaceRpcRequest{Context: call, WorkspaceId: op.ResourceID}); status.Code(err) != codes.DataLoss {
				t.Fatalf("corrupt detail error=%v", err)
			}
			if _, err := service.ListOperatorWorkspaces(ctx, &api.ListOperatorWorkspacesRpcRequest{Context: call, Page: 1, PageSize: 10}); status.Code(err) != codes.DataLoss {
				t.Fatalf("corrupt list error=%v", err)
			}
		})
	}
}

func operatorReadCall() (*api.CallContext, context.Context) {
	session := "session-operator-read"
	call := &api.CallContext{
		ActorId: "operator-actor", SessionId: &session, RequestId: "operator-read-request",
		Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
	}
	return call, ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
}

func TestOperatorWorkspacePageUsesOneSnapshot(t *testing.T) {
	db := runtimeDatabase(t)
	// Lock holder, owner read and pg_stat_activity probe need separate connections.
	db.SetMaxOpenConns(4)
	service, _ := operatorReadService(t, db)
	_, op, _, _ := seedRuntimeOrder(t, db)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ctx = ownerservice.WithPeerOwner(ctx, owneridentity.ConsoleBFF)
	lock, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	if _, err = lock.ExecContext(ctx, `LOCK TABLE workspace.operations IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	call, _ := operatorReadCall()
	type answer struct {
		page *api.OperatorWorkspacePage
		err  error
	}
	result := make(chan answer, 1)
	go func() {
		page, err := service.ListOperatorWorkspaces(ctx, &api.ListOperatorWorkspacesRpcRequest{Context: call, Page: 1, PageSize: 10})
		result <- answer{page, err}
	}()
	// Count can finish, while the item query blocks on operations. Once this
	// barrier is observed, insert a Workspace in the lock holder's transaction.
	for {
		var blocked bool
		err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
			AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '%ORDER BY w.created_at DESC%')`).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case got := <-result:
			t.Fatalf("read ended before snapshot barrier: %v %v", got.page, got.err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, err = lock.ExecContext(ctx, `INSERT INTO workspace.workspaces(id,tenant_id,name,status,compute_plan_id,storage_plan_id,created_by,version)
		VALUES('workspace-concurrent','tenant-other','Concurrent','provisioning','compute-original','storage-original','actor-other',1)`); err != nil {
		t.Fatal(err)
	}
	if err = lock.Commit(); err != nil {
		t.Fatal(err)
	}
	got := <-result
	if got.err != nil || got.page.GetTotal() != 1 || len(got.page.GetItems()) != 1 || got.page.Items[0].Workspace.Id != op.ResourceID {
		t.Fatalf("inconsistent snapshot page=%v err=%v", got.page, got.err)
	}
	page, err := service.ListOperatorWorkspaces(ctx, &api.ListOperatorWorkspacesRpcRequest{Context: call, Page: 1, PageSize: 10})
	if err != nil || page.GetTotal() != 2 || len(page.GetItems()) != 2 {
		t.Fatalf("next snapshot page=%v err=%v", page, err)
	}
}
