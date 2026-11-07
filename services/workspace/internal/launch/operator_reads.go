package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerservice"
)

const maxOperatorWorkspacePageSize int32 = 100

const operatorWorkspaceColumns = workspaceColumns + `,w.created_by,launch.id,launch.tenant_id,launch.result,COALESCE(launch.operation_count,0)`

// Keep the shared business DTO reader, but resolve the resource reference from
// the original create. active_operation_id can point to a models operation with
// another result schema. Multiple creates are corruption, never chosen by date.
const operatorWorkspaceJoins = workspaceJoin + ` LEFT JOIN LATERAL (
	SELECT id,tenant_id,accepted_input,result,COUNT(*) OVER() AS operation_count
	FROM workspace.operations
	WHERE resource_id=w.id AND kind='create_workspace'
	ORDER BY created_at,id
	LIMIT 1
) launch ON true`

type operatorWorkspaceRow struct {
	createdBy       string
	operationID     sql.NullString
	operationTenant sql.NullString
	operationResult []byte
	operationCount  int64
}

func scanOperatorWorkspace(row rowScanner) (*api.OperatorWorkspace, error) {
	var value operatorWorkspaceRow
	w, tenant, err := scanWorkspace(operatorWorkspaceScanner{row: row, destinations: []any{
		&value.createdBy, &value.operationID, &value.operationTenant, &value.operationResult, &value.operationCount,
	}})
	if err != nil {
		return nil, err
	}
	if value.operationCount > 1 {
		return nil, status.Error(codes.DataLoss, "Workspace has ambiguous create operations")
	}
	if value.operationCount == 1 && (!value.operationID.Valid || !value.operationTenant.Valid || value.operationTenant.String != tenant) {
		return nil, status.Error(codes.DataLoss, "Workspace create operation identity is invalid")
	}
	resourceSetID := ""
	if value.operationCount == 1 && len(value.operationResult) > 0 {
		var result orderResult
		if json.Unmarshal(value.operationResult, &result) != nil {
			return nil, status.Error(codes.DataLoss, "stored Workspace create result is invalid")
		}
		resourceSetID = result.ResourceSetID
	}
	return &api.OperatorWorkspace{
		Workspace: w, TenantId: tenant, ResourceSetId: resourceSetID,
		CreatedBy: value.createdBy, OperationId: value.operationID.String,
	}, nil
}

type operatorWorkspaceScanner struct {
	row          rowScanner
	destinations []any
}

func (s operatorWorkspaceScanner) Scan(dest ...any) error {
	return s.row.Scan(append(dest, s.destinations...)...)
}

func (s *Service) ListOperatorWorkspaces(ctx context.Context, r *api.ListOperatorWorkspacesRpcRequest) (*api.OperatorWorkspacePage, error) {
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	if r.GetPage() < 1 || r.GetPageSize() < 1 || r.GetPageSize() > maxOperatorWorkspacePageSize {
		return nil, status.Error(codes.InvalidArgument, "page must be positive and page_size must be between 1 and 100")
	}
	if err := s.authorize(ctx, r.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORWORKSPACES, workspaceResource(""), ""); err != nil {
		return nil, err
	}
	offset := (int64(r.GetPage()) - 1) * int64(r.GetPageSize())
	tx, err := s.Store.DB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, dbError(err)
	}
	defer tx.Rollback()
	var total int64
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace.workspaces`).Scan(&total); err != nil {
		return nil, dbError(err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+operatorWorkspaceColumns+operatorWorkspaceJoins+`
		ORDER BY w.created_at DESC,w.id DESC LIMIT $1 OFFSET $2`, r.GetPageSize(), offset)
	if err != nil {
		return nil, dbError(err)
	}
	page := &api.OperatorWorkspacePage{Total: total, Page: r.GetPage(), PageSize: r.GetPageSize()}
	for rows.Next() {
		item, scanErr := scanOperatorWorkspace(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		page.Items = append(page.Items, item)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, dbError(err)
	}
	if err = rows.Close(); err != nil {
		return nil, dbError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, dbError(err)
	}
	return page, nil
}

func (s *Service) GetOperatorWorkspace(ctx context.Context, r *api.GetOperatorWorkspaceRpcRequest) (*api.OperatorWorkspace, error) {
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	workspaceID := strings.TrimSpace(r.GetWorkspaceId())
	if workspaceID == "" || workspaceID != r.GetWorkspaceId() {
		return nil, status.Error(codes.InvalidArgument, "workspace id is required and must not contain surrounding whitespace")
	}
	if err := s.authorize(ctx, r.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATORWORKSPACE, workspaceResource(workspaceID), ""); err != nil {
		return nil, err
	}
	return scanOperatorWorkspace(s.Store.DB().QueryRowContext(ctx, `SELECT `+operatorWorkspaceColumns+operatorWorkspaceJoins+` WHERE w.id=$1`, workspaceID))
}
