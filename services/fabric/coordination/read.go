package coordination

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

// ReadResources returns only Fabric's persisted facts. Missing provider
// references remain not_dispatched and cannot become execution bindings.
func (s *Service) ReadResources(ctx context.Context, r *api.ResourceReadbackRequest) (*api.ResourceReadback, error) {
	if err := peer(ctx, owneridentity.Workspace.Service(), owneridentity.Serve.Service()); err != nil {
		return nil, err
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	if strings.TrimSpace(r.GetResourceSetId()) == "" && strings.TrimSpace(r.GetResourceActionId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "resource set or resource action is required")
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, persistenceError(err)
	}
	defer tx.Rollback()
	setID := r.GetResourceSetId()
	if r.GetResourceActionId() != "" {
		var actionSet string
		if err = tx.QueryRowContext(ctx, `SELECT resource_set_id FROM fabric.resource_actions WHERE id=$1`, r.ResourceActionId).Scan(&actionSet); err != nil {
			return nil, persistenceError(err)
		}
		if setID != "" && actionSet != setID {
			return nil, status.Error(codes.InvalidArgument, "resource action belongs to another resource set")
		}
		setID = actionSet
	}
	var tenant, workspace, observation string
	var observed sql.NullTime
	var updated time.Time
	if err = tx.QueryRowContext(ctx, `SELECT tenant_id,workspace_id,observation_result,observed_at,updated_at FROM fabric.resource_sets WHERE id=$1`, setID).Scan(&tenant, &workspace, &observation, &observed, &updated); err != nil {
		return nil, persistenceError(err)
	}
	if err = s.authorizeWorkspace(ctx, r.Context, workspace, tenant, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES); err != nil {
		return nil, err
	}
	value, ok := api.Observation_value["OBSERVATION_"+strings.ToUpper(observation)]
	if !ok {
		return nil, status.Error(codes.DataLoss, "invalid Fabric resource observation")
	}
	out := &api.ResourceReadback{ResourceSetId: setID, WorkspaceId: workspace, ResourceVersion: strconv.FormatInt(updated.UnixNano(), 10), Outcome: api.Observation(value)}
	if observed.Valid {
		out.ObservedAt = timestamppb.New(observed.Time)
	}
	if observation == "unknown" {
		out.ErrorCode = "DEPENDENCY_UNAVAILABLE"
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,kind,COALESCE(provider_resource_ref,''),observation_result,observed_at FROM fabric.resources WHERE resource_set_id=$1 ORDER BY kind,id`, setID)
	if err != nil {
		return nil, persistenceError(err)
	}
	defer rows.Close()
	for rows.Next() {
		fact := &api.ResourceFact{}
		var state string
		var resourceObserved sql.NullTime
		if err = rows.Scan(&fact.Id, &fact.Kind, &fact.OpaqueProviderReference, &state, &resourceObserved); err != nil {
			return nil, persistenceError(err)
		}
		fact.State = state
		if fact.OpaqueProviderReference == "" && !resourceObserved.Valid && state == "unknown" {
			fact.State = "not_dispatched"
		}
		out.Resources = append(out.Resources, fact)
	}
	if err = rows.Err(); err != nil {
		return nil, persistenceError(err)
	}
	if err = rows.Close(); err != nil {
		return nil, persistenceError(err)
	}
	if observation == "confirmed" {
		result, readErr := readLocalResult(ctx, tx, setID)
		if readErr != nil || result.Binding == nil || result.Binding.AccountId == "" || result.Binding.ComputeAllocationId == "" || result.Binding.StorageVolumeId == "" || result.Binding.DataAttachmentId == "" || result.Binding.DataAttachmentOperationId == "" {
			return nil, status.Error(codes.DataLoss, "confirmed resources lack provider execution evidence")
		}
		out.ExecutionResources = result.Binding
	}
	if err = tx.Commit(); err != nil {
		return nil, persistenceError(err)
	}
	return out, nil
}
