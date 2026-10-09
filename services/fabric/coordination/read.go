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
	"opl-cloud/services/fabric/internal/fabric"
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
	recordedDeletions, err := readRecordedDeletions(ctx, tx, setID)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,kind,COALESCE(provider_resource_ref,''),observation_result,observed_at,COALESCE(deletion_evidence_ref,''),deleted_at FROM fabric.resources WHERE resource_set_id=$1 ORDER BY kind,id`, setID)
	if err != nil {
		return nil, persistenceError(err)
	}
	defer rows.Close()
	released := 0
	resourceFacts := 0
	for rows.Next() {
		fact := &api.ResourceFact{}
		var state string
		var resourceObserved, deletedAt sql.NullTime
		if err = rows.Scan(&fact.Id, &fact.Kind, &fact.OpaqueProviderReference, &state, &resourceObserved, &fact.ReceiptId, &deletedAt); err != nil {
			return nil, persistenceError(err)
		}
		resourceFacts++
		fact.State = state
		switch {
		case deletedAt.Valid:
			// Absence exists only as the owning surface's confirmed readback; the
			// deletion evidence reference names it.
			fact.State = DeletionStateAbsent
			released++
		case recordedDeletions[fact.Kind].confirmed:
			fact.State = DeletionStateAbsent
			fact.ReceiptId = recordedDeletions[fact.Kind].evidenceRef
			released++
		case recordedDeletions[fact.Kind].state != "":
			fact.State = recordedDeletions[fact.Kind].state
		case fact.OpaqueProviderReference == "" && !resourceObserved.Valid && state == "unknown":
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
	// A set whose release has started reports every released kind as its own
	// fact: the runtime, its Secret binding and the mount binding are released
	// through their own owning surfaces, so their recorded readbacks are
	// projected here instead of being inferred from the persisted rows. A pending
	// or unknown handle is never reported absent, and a kind that is still absent
	// from the record has not been dispatched yet. A set without a recorded
	// release keeps the plain persisted-resource shape.
	allKindsAbsent := true
	for _, kind := range deleteResourceKinds() {
		recorded, exists := recordedDeletions[kind]
		if !exists || !recorded.confirmed {
			allKindsAbsent = false
		}
		if len(recordedDeletions) == 0 || kind == DeletionKindCompute || kind == DeletionKindStorage {
			continue
		}
		fact := &api.ResourceFact{Id: recorded.resourceID, Kind: kind, ReceiptId: recorded.evidenceRef}
		switch {
		case !exists:
			fact.State = "not_dispatched"
		case recorded.confirmed:
			fact.State = DeletionStateAbsent
		default:
			fact.State = recorded.state
		}
		out.Resources = append(out.Resources, fact)
	}
	out.AbsenceConfirmed = allKindsAbsent && resourceFacts > 0 && released == resourceFacts
	if observation == "confirmed" && !out.AbsenceConfirmed {
		result, readErr := readResourceResult(ctx, tx, setID)
		if readErr != nil || result.Binding == nil || result.Binding.AccountId == "" || result.Binding.ComputeAllocationId == "" || result.Binding.StorageVolumeId == "" || result.Binding.DataAttachmentId == "" || result.Binding.DataAttachmentOperationId == "" {
			return nil, status.Error(codes.DataLoss, "confirmed resources lack provider execution evidence")
		}
		out.ExecutionResources = result.Binding
		// The workload executor schedules the Workspace application onto the exact
		// node and prepaid storage this confirmed resource set landed on, so the
		// same provider mutation's placement facts are projected here.
		out.ApplicationPlacement = fabric.ApplicationExecutionPlacement(result.Compute, result.Storage)
	}
	if err = tx.Commit(); err != nil {
		return nil, persistenceError(err)
	}
	return out, nil
}
