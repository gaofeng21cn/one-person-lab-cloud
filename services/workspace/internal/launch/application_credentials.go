package launch

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerservice"
)

// RevealWorkspaceApplicationCredentials authorizes every explicit reveal against
// the live identity owner. Serve selects the current application and reads its
// installed credential; neither the launch snapshot nor a secret cache is used.
func (s *Service) RevealWorkspaceApplicationCredentials(ctx context.Context, r *api.RevealWorkspaceApplicationCredentialsRpcRequest) (*api.WorkspaceApplicationCredentials, error) {
	c := r.GetContext()
	if err := ownerservice.ValidateCallContext(ctx, c); err != nil {
		return nil, err
	}
	if c.GetSessionId() == "" || c.GetAcceptedOperationGrantId() != "" {
		return nil, status.Error(codes.Unauthenticated, "a live customer session is required")
	}
	wid := strings.TrimSpace(r.GetWorkspaceId())
	if wid == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace id is required")
	}
	if s.Store == nil {
		return nil, status.Error(codes.Unavailable, "Workspace store unavailable")
	}
	// Serialize with Workspace lifecycle admissions for the entire transient read.
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, dbError(err)
	}
	defer tx.Rollback()
	var tenant, state string
	if err = tx.QueryRowContext(ctx, `SELECT tenant_id,status FROM workspace.workspaces WHERE id=$1 FOR UPDATE`, wid).Scan(&tenant, &state); err != nil {
		return nil, dbError(err)
	}
	if err = s.authorize(ctx, c, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REVEALWORKSPACEAPPLICATIONCREDENTIALS, workspaceResource(wid), tenant); err != nil {
		return nil, err
	}
	if state != "active" {
		return nil, status.Error(codes.FailedPrecondition, "Workspace is not active")
	}
	if s.ServeProduct == nil || s.ServeCredentials == nil {
		return nil, status.Error(codes.Unavailable, "Serve credential readback is unavailable")
	}
	call := proto.Clone(c).(*api.CallContext)
	call.AuthorizationContextId = ""
	access, err := s.ServeProduct.GetWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: call, WorkspaceId: wid})
	if err != nil {
		return nil, err
	}
	if access.GetWorkspaceId() != wid || !access.GetApplicationCredentialsAvailable() {
		return nil, status.Error(codes.FailedPrecondition, "current application login credentials are unavailable")
	}
	// Deployment history is keyset-paginated by Serve. Only its active selection
	// is eligible, including when more recent attempts have failed.
	cursor, seen := "", map[string]bool{}
	var current *api.Deployment
	for {
		page, err := s.ServeProduct.ListDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: call, WorkspaceId: wid, QueryCursor: proto.String(cursor), QueryLimit: proto.Int32(100)})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return nil, status.Error(codes.Unavailable, "Serve deployment readback is unavailable")
		}
		for _, d := range page.GetItems() {
			if d.GetWorkspaceId() != wid {
				return nil, status.Error(codes.FailedPrecondition, "Serve deployment does not match the Workspace")
			}
			if d.GetStatus() == api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE {
				if current != nil {
					return nil, status.Error(codes.FailedPrecondition, "Serve current deployment is ambiguous")
				}
				current = d
			}
		}
		cursor = page.GetNextCursor()
		if cursor == "" {
			break
		}
		if seen[cursor] {
			return nil, status.Error(codes.DataLoss, "Serve deployment cursor repeated")
		}
		seen[cursor] = true
	}
	if current.GetId() == "" || current.GetRuntimeInstanceId() == "" {
		return nil, status.Error(codes.FailedPrecondition, "Workspace has no current application runtime")
	}
	credentials, err := s.ServeCredentials.ReadApplicationCredentials(ctx, &api.ReadApplicationCredentialsRequest{Context: call, WorkspaceId: wid, DeploymentId: current.GetId(), RuntimeInstanceId: current.GetRuntimeInstanceId()})
	if err != nil {
		return nil, err
	}
	if credentials.GetWorkspaceId() != wid || credentials.GetRuntimeInstanceId() != current.GetRuntimeInstanceId() || strings.TrimSpace(credentials.GetUsername()) == "" || credentials.GetPassword() == "" {
		return nil, status.Error(codes.FailedPrecondition, "application credentials do not match the current runtime")
	}
	// Construct only the declared public shape, including no protobuf unknowns.
	return &api.WorkspaceApplicationCredentials{WorkspaceId: wid, RuntimeInstanceId: current.GetRuntimeInstanceId(), Username: credentials.GetUsername(), Password: credentials.GetPassword()}, nil
}
