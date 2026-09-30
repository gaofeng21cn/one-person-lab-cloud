package launch

// This file owns the Workspace's model configuration read: the accepted model
// intent it persists, the version its runtime actually applied, and the status the
// two owner facts together prove. The applied version is the Workspace's own
// column, which only advances after the runtime confirmed a real reload, so the
// read never presents a requested version as the applied one and never copies
// Serve's runtime state into a second writer.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerservice"
)

// GetWorkspaceModels returns one Workspace's model configuration. The latest
// persisted configuration intent is the answer's version and selections, while the
// applied version and the status are derived from the Workspace's own confirmed
// fact and the owning operation, so an unconfirmed change reads as pending or
// needs_attention rather than as applied. A Workspace that never changed its
// configuration answers with the accepted order's frozen selections at the launch
// version.
func (s *Service) GetWorkspaceModels(ctx context.Context, r *api.GetWorkspaceModelsRpcRequest) (*api.ModelConfiguration, error) {
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	workspaceID := strings.TrimSpace(r.GetWorkspaceId())
	if workspaceID == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace id is required")
	}
	var tenant string
	var appliedVersion int64
	var workspaceUpdated time.Time
	var acceptedInput []byte
	err := s.Store.DB().QueryRowContext(ctx, `SELECT w.tenant_id,w.model_configuration_version,w.updated_at,COALESCE(o.accepted_input,'{}'::jsonb)
		FROM workspace.workspaces w
		LEFT JOIN LATERAL (
			SELECT accepted_input FROM workspace.operations op
			WHERE op.resource_id=w.id AND op.kind='create_workspace'
			ORDER BY op.created_at DESC, op.id DESC LIMIT 1
		) o ON true
		WHERE w.id=$1`, workspaceID).Scan(&tenant, &appliedVersion, &workspaceUpdated, &acceptedInput)
	if err != nil {
		return nil, dbError(err)
	}
	if err := s.authorize(ctx, r.Context, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEMODELS, workspaceResource(workspaceID), tenant); err != nil {
		return nil, err
	}
	configuration := &api.ModelConfiguration{
		WorkspaceId: workspaceID, Version: appliedVersion, AppliedVersion: proto.Int64(appliedVersion),
		UpdatedAt: timestamppb.New(workspaceUpdated),
	}
	latest, found, err := s.latestModelConfiguration(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if !found {
		// No configuration change was ever recorded, so the accepted order's frozen
		// model selections are the configuration the launch applied.
		selections, err := acceptedModelSelections(acceptedInput)
		if err != nil {
			return nil, err
		}
		configuration.Selections = selections
		configuration.Status = api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_APPLIED
		return configuration, nil
	}
	configuration.Version = latest.version
	configuration.Selections = latest.selections
	configuration.OperationId = proto.String(latest.operationID)
	configuration.UpdatedAt = timestamppb.New(latest.updatedAt)
	configuration.Status = modelConfigurationStatus(latest, appliedVersion)
	return configuration, nil
}

// modelConfigurationRow is one persisted configuration intent together with the
// lifecycle state of the Workspace operation that carries it. The configuration's
// own foreign key makes the operation exist, so the two facts are read as one row
// instead of a second lookup that would have to invent an answer for an impossible
// absence.
type modelConfigurationRow struct {
	version        int64
	operationID    string
	operationState string
	observation    string
	selections     []*api.ModelSelection
	updatedAt      time.Time
}

func (s *Service) latestModelConfiguration(ctx context.Context, workspaceID string) (modelConfigurationRow, bool, error) {
	var row modelConfigurationRow
	var raw []byte
	err := s.Store.DB().QueryRowContext(ctx, `SELECT c.version,c.operation_id,c.runtime_reload_observation,c.selections,c.updated_at,o.status
		FROM workspace.model_configurations c
		JOIN workspace.operations o ON o.id=c.operation_id
		WHERE c.workspace_id=$1 ORDER BY c.version DESC LIMIT 1`, workspaceID).
		Scan(&row.version, &row.operationID, &row.observation, &raw, &row.updatedAt, &row.operationState)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	if err != nil {
		return row, false, dbError(err)
	}
	selections, err := decodeModelSelections(raw)
	if err != nil {
		return row, false, err
	}
	row.selections = selections
	return row, true, nil
}

// modelConfigurationStatus derives the customer-facing status from the facts the
// Workspace owns: a version the runtime confirmed as applied is applied, a failed
// operation is failed, an unconfirmed observation or a needs_attention operation is
// needs_attention, and anything else is still pending. The requested version is
// never reported as applied.
func modelConfigurationStatus(latest modelConfigurationRow, appliedVersion int64) api.ModelConfigurationStatusEnum {
	switch {
	case latest.version == appliedVersion && latest.observation == "confirmed":
		return api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_APPLIED
	case latest.operationState == "failed":
		return api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_FAILED
	case latest.observation == "unknown" || latest.operationState == "needs_attention":
		return api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_NEEDS_ATTENTION
	default:
		return api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_PENDING
	}
}

// acceptedModelSelections reads the model selections the accepted launch order
// froze, so the launch configuration is answered from the original accepted order
// rather than from a second stored copy.
func acceptedModelSelections(acceptedInput []byte) ([]*api.ModelSelection, error) {
	order := acceptedOrder{}
	if len(acceptedInput) == 0 || json.Unmarshal(acceptedInput, &order) != nil || len(order.Quote) == 0 {
		return nil, status.Error(codes.DataLoss, "stored Workspace order names no accepted application")
	}
	accepted := &api.QuoteAcceptance{}
	if protojson.Unmarshal(order.Quote, accepted) != nil || accepted.GetQuote().GetId() == "" {
		return nil, status.Error(codes.DataLoss, "stored Workspace order names no accepted application")
	}
	return accepted.GetQuote().GetModelSelections(), nil
}

// modelSelectionsJSON and decodeModelSelections are the row's one selection shape:
// a JSON array of protojson-encoded ModelSelection values. The read path decodes it,
// and the configuration change path persists it, so the stored form has a single
// definition instead of one per caller.
func modelSelectionsJSON(selections []*api.ModelSelection) ([]byte, error) {
	items := make([]json.RawMessage, 0, len(selections))
	for _, selection := range selections {
		raw, err := protojson.Marshal(selection)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "model selection cannot be encoded")
		}
		items = append(items, raw)
	}
	return json.Marshal(items)
}

func decodeModelSelections(raw []byte) ([]*api.ModelSelection, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil, status.Error(codes.DataLoss, "stored model selections are invalid")
	}
	selections := make([]*api.ModelSelection, 0, len(items))
	for _, item := range items {
		selection := &api.ModelSelection{}
		if protojson.Unmarshal(item, selection) != nil || strings.TrimSpace(selection.GetSlot()) == "" || strings.TrimSpace(selection.GetModelId()) == "" {
			return nil, status.Error(codes.DataLoss, "stored model selection is invalid")
		}
		selections = append(selections, selection)
	}
	return selections, nil
}
