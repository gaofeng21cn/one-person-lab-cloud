package launch

import (
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

func TestWorkspaceAccessRequiresServePeerBeforeStore(t *testing.T) {
	for _, peer := range []ownerservice.Service{owneridentity.ConsoleBFF, owneridentity.Ledger.Service(), owneridentity.Workspace.Service()} {
		ctx := ownerservice.WithPeerOwner(t.Context(), peer)
		if _, err := (&Service{}).ReadWorkspaceAccess(ctx, &api.ReadWorkspaceAccessRequest{WorkspaceId: "workspace-original"}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("peer %s: %v", peer, err)
		}
	}
}

func TestWorkspaceAccessReadsConfirmedTermWithoutExtendingExpiry(t *testing.T) {
	db := runtimeDatabase(t)
	s, _, _, periodStart, _ := deletionWorkspace(t, db)
	ctx := ownerservice.WithPeerOwner(t.Context(), owneridentity.Serve.Service())
	request := &api.ReadWorkspaceAccessRequest{WorkspaceId: "workspace-original"}
	check := func(want api.WorkspaceAccessVerdict) {
		t.Helper()
		answer, err := s.ReadWorkspaceAccess(ctx, request)
		if err != nil || answer.GetWorkspaceId() != request.WorkspaceId || answer.GetVerdict() != want {
			t.Fatalf("answer=%v err=%v want=%v", answer, err, want)
		}
	}
	exec := func(query string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	check(api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_PERMITTED)
	s.accessNow = func() time.Time { return periodStart.Add(30 * 24 * time.Hour) }
	check(api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_CLOSED)
	s.accessNow = func() time.Time { return periodStart.Add(-time.Second) }
	check(api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_CLOSED)
	s.accessNow = nil
	check(api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_PERMITTED)
	exec(`UPDATE workspace.subscriptions SET current_period_end=current_period_end+interval '1 day'`)
	check(api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_UNVERIFIED)
	exec(`UPDATE workspace.workspaces SET status='deleting'`)
	check(api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_CLOSED)
	exec(`INSERT INTO workspace.workspaces(id,tenant_id,name,status,compute_plan_id,storage_plan_id,created_by,version)
		VALUES('workspace-unverified','tenant-original','No confirmed term','active','compute-original','storage-original','actor-original',1)`)
	request.WorkspaceId = "workspace-unverified"
	check(api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_UNVERIFIED)
	request.WorkspaceId = "missing-workspace"
	check(api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_CLOSED)
}
