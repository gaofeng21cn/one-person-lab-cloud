package coordination

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The caller holds Fabric's workspace lock. A deletion is a durable barrier to
// any later provision attempt, including replays of the original command.
func refuseDeletedProvision(ctx context.Context, tx *sql.Tx, workspace, tenant string) error {
	var closing bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fabric.operations WHERE kind='resource_delete' AND tenant_id=$2 AND (resource_id=$1 OR accepted_input->>'workspaceId'=$1))`, workspace, tenant).Scan(&closing); err != nil {
		return persistenceError(err)
	}
	if closing {
		return status.Error(codes.FailedPrecondition, "the original Workspace resources are closing")
	}
	return nil
}

// Resolve by the owner's original API command, never by an inferred resource ID.
func (s *Service) originalResourceSet(ctx context.Context, tenant, workspace, original string) (string, error) {
	var set string
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT resource_id,COUNT(*) OVER() FROM fabric.idempotency_records WHERE tenant_scope=$1 AND operation_name='EnsureResources' AND idempotency_key=$2`, tenant, original).Scan(&set, &count)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", persistenceError(err)
	}
	if count != 1 || set == "" {
		return "", status.Error(codes.DataLoss, "original resource command is ambiguous or invalid")
	}
	var storedTenant, storedWorkspace string
	if err = s.DB.QueryRowContext(ctx, `SELECT tenant_id,workspace_id FROM fabric.resource_sets WHERE id=$1`, set).Scan(&storedTenant, &storedWorkspace); err != nil {
		return "", persistenceError(err)
	}
	if storedTenant != tenant || storedWorkspace != workspace {
		return "", status.Error(codes.PermissionDenied, "original resource command belongs to another Workspace")
	}
	return set, nil
}

func (s *Service) deleteOriginalResources(ctx context.Context, r *api.MutateResourcesCommand) (*api.Operation, error) {
	tenant, workspace := r.GetContext().GetScope().GetTenant().GetTenantId(), r.GetWorkspaceId()
	if tenant == "" || workspace == "" || r.GetContext().GetIdempotencyKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant, Workspace and closing command identity required")
	}
	if err := s.authorizeWorkspace(ctx, r.GetContext(), workspace, tenant, deleteResourcesAuthorizationAction); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, persistenceError(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "fabric:workspace:"+workspace); err != nil {
		return nil, persistenceError(err)
	}
	if err = s.authorizeWorkspace(ctx, r.GetContext(), workspace, tenant, deleteResourcesAuthorizationAction); err != nil {
		return nil, err
	}
	set, err := s.originalResourceSet(ctx, tenant, workspace, r.GetOriginalProvisionIdempotencyKey())
	if err != nil {
		return nil, err
	}
	if set != "" {
		tx.Rollback()
		resolved := proto.Clone(r).(*api.MutateResourcesCommand)
		resolved.ResourceSetId = set
		return s.DeleteResources(ctx, resolved)
	}
	var hasResources bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fabric.resource_sets WHERE workspace_id=$1)`, workspace).Scan(&hasResources); err != nil {
		return nil, persistenceError(err)
	}
	if hasResources {
		return nil, status.Error(codes.FailedPrecondition, "Workspace resources exist without this original command identity")
	}
	var operation, original string
	err = tx.QueryRowContext(ctx, `SELECT id,COALESCE(accepted_input->>'originalProvisionIdempotencyKey','') FROM fabric.operations WHERE tenant_id=$1 AND resource_id=$2 AND kind='resource_delete'`, tenant, workspace).Scan(&operation, &original)
	if err == nil {
		if original != r.GetOriginalProvisionIdempotencyKey() {
			return nil, status.Error(codes.FailedPrecondition, "resource closeout identifies another original command")
		}
		tx.Rollback()
		return s.operation(ctx, operation)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, persistenceError(err)
	}
	command := proto.Clone(r).(*api.MutateResourcesCommand)
	command.Context = nil
	body, err := protojson.Marshal(command)
	if err != nil {
		return nil, err
	}
	operation = "op_" + uuid.NewString()
	if _, err = s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: operation, TenantID: tenant, ActorID: r.GetContext().GetActorId(), Kind: "resource_delete", ResourceID: workspace, Stage: "compute_deletion", RequestID: r.GetContext().GetRequestId(), AcceptedInput: body}); err != nil {
		return nil, persistenceError(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE fabric.operations SET status='succeeded',observation_result='confirmed',completed_at=now(),updated_at=now() WHERE id=$1`, operation); err != nil {
		return nil, persistenceError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, persistenceError(err)
	}
	return s.operation(ctx, operation)
}

func (s *Service) readOriginalResources(ctx context.Context, r *api.ResourceReadbackRequest) (*api.ResourceReadback, error) {
	tenant, workspace := r.GetContext().GetScope().GetTenant().GetTenantId(), r.GetWorkspaceId()
	if tenant == "" || workspace == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant and original Workspace required")
	}
	if err := s.authorizeWorkspace(ctx, r.GetContext(), workspace, tenant, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES); err != nil {
		return nil, err
	}
	set, err := s.originalResourceSet(ctx, tenant, workspace, r.GetOriginalProvisionIdempotencyKey())
	if err != nil {
		return nil, err
	}
	if set != "" {
		if r.GetResourceSetId() != "" && r.GetResourceSetId() != set {
			return nil, status.Error(codes.FailedPrecondition, "original resource identity differs")
		}
		return s.ReadResources(ctx, &api.ResourceReadbackRequest{Context: r.GetContext(), ResourceSetId: set})
	}
	var closed bool
	if err = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fabric.operations WHERE tenant_id=$1 AND resource_id=$2 AND kind='resource_delete' AND accepted_input->>'originalProvisionIdempotencyKey'=$3 AND status='succeeded' AND observation_result='confirmed') AND NOT EXISTS(SELECT 1 FROM fabric.resource_sets WHERE workspace_id=$2)`, tenant, workspace, r.GetOriginalProvisionIdempotencyKey()).Scan(&closed); err != nil {
		return nil, persistenceError(err)
	}
	if !closed {
		return nil, status.Error(codes.NotFound, "original resource command has no confirmed closeout")
	}
	return &api.ResourceReadback{WorkspaceId: workspace, Outcome: api.Observation_OBSERVATION_CONFIRMED, AbsenceConfirmed: true, ObservedAt: timestamppb.Now()}, nil
}
