package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	controlplaneent "opl-cloud/services/control-plane/ent"
	"opl-cloud/services/control-plane/ent/runtimeoperation"
	"opl-cloud/services/control-plane/ent/workspace"
)

var errWorkspaceApplicationRecoveryConflict = errors.New("workspace_application_recovery_conflict")

func workspaceApplicationRotationParentMatches(intent workspaceApplicationDeploymentIntent, row map[string]any) bool {
	if intent.OriginOperationID == "" || stringValue(row["id"]) != intent.OriginOperationID || stringValue(row["action"]) != "workspace.gateway_key.rotate" ||
		stringValue(row["accountId"]) != intent.AccountID || stringValue(row["workspaceId"]) != intent.WorkspaceID || stringValue(row["status"]) != "started" {
		return false
	}
	parent, err := decodeWorkspaceKeyRotation(row)
	if err != nil || parent.Phase != "runtime_bind" && parent.Phase != "runtime_readback" || parent.NewKeyID != intent.WorkspaceAPIKeyID ||
		parent.ApplicationDeploymentID != "" && parent.ApplicationDeploymentID != intent.OperationID {
		return false
	}
	for _, binding := range intent.SecretBindings {
		if binding.Name == "gateway" && binding.Key == "opl_gateway_api_key" && binding.SecretRef == parent.SecretRef && binding.Version == parent.SecretVersion {
			return true
		}
	}
	return false
}

// Recovery resumes only the frozen command still reserved by this Workspace.
// Its phase decides whether the old or newly activated selection must match.
func workspaceApplicationRecoveryRow(current map[string]any, row map[string]any, operations []map[string]any) (map[string]any, error) {
	intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil || intent.Version != 2 || !workspaceApplicationOwnedResourcesMatch(current, intent) || !workspaceApplicationEntitlementOpen(current, time.Now()) ||
		stringValue(current["reservedApplicationDeploymentId"]) != intent.OperationID {
		return nil, errWorkspaceApplicationRecoveryConflict
	}
	phase := intent.Phase
	if phase == workspaceApplicationDeploymentManualReviewPhase {
		phase = intent.FailurePhase
	}
	switch phase {
	case workspaceApplicationDeploymentIntentPhase, workspaceApplicationDeploymentPredecessorPhase, workspaceApplicationDeploymentRuntimePhase, workspaceApplicationDeploymentActivatingPhase:
		if stringValue(current["currentApplicationDeploymentId"]) != intent.PreviousDeploymentID || stringValue(current["applicationBinding"]) != intent.CurrentBinding ||
			int64(numberField(current, "applicationBindingVersion", 0)) != intent.ExpectedWorkspaceVersion || intent.ActivationAt != "" {
			return nil, errWorkspaceApplicationRecoveryConflict
		}
	case workspaceApplicationDeploymentRetiringPhase, workspaceApplicationDeploymentReceiptPhase, workspaceApplicationDeploymentActivePhase:
		if stringValue(current["currentApplicationDeploymentId"]) != intent.OperationID || stringValue(current["applicationBinding"]) != intent.ApplicationID+"@"+intent.TargetRevision ||
			int64(numberField(current, "applicationBindingVersion", 0)) != intent.ExpectedWorkspaceVersion+1 || intent.ActivationAt == "" {
			return nil, errWorkspaceApplicationRecoveryConflict
		}
	default:
		return nil, errWorkspaceApplicationRecoveryConflict
	}
	parentFound := intent.OriginOperationID == ""
	for _, operation := range operations {
		if stringValue(operation["workspaceId"]) != intent.WorkspaceID {
			continue
		}
		if workspaceDeleteBlocksRotation(operation) {
			return nil, errWorkspaceApplicationRecoveryConflict
		}
		if stringValue(operation["id"]) == intent.OriginOperationID {
			parentFound = workspaceApplicationRotationParentMatches(intent, operation)
		}
		if workspaceKeyRotationBlocksDelete(operation) && !workspaceApplicationRotationParentMatches(intent, operation) {
			return nil, errWorkspaceApplicationRecoveryConflict
		}
	}
	if !parentFound {
		return nil, errWorkspaceApplicationRecoveryConflict
	}
	if intent.Phase != workspaceApplicationDeploymentManualReviewPhase {
		return cloneMap(row), nil
	}
	intent.Phase, intent.FailurePhase, intent.LastError = phase, "", ""
	encoded, err := json.Marshal(intent)
	if err != nil {
		return nil, err
	}
	next := cloneMap(row)
	next["result"], next["status"] = string(encoded), "running"
	if phase == workspaceApplicationDeploymentIntentPhase {
		next["status"] = "pending"
	}
	if err := validateWorkspaceApplicationOperationPersistence(row, stringValue(row["result"]), next); err != nil {
		return nil, err
	}
	return next, nil
}

func (s *postgresEntStateStore) ResumeWorkspaceApplicationDeployment(ctx context.Context, operationID string) (map[string]any, error) {
	initial, err := s.client.RuntimeOperation.Get(ctx, operationID)
	if controlplaneent.IsNotFound(err) {
		return nil, errWorkspaceApplicationRecoveryConflict
	}
	if err != nil {
		return nil, err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	current, err := tx.Workspace.Query().Where(workspace.IDEQ(initial.WorkspaceID), lockRowForUpdate).Only(ctx)
	if controlplaneent.IsNotFound(err) {
		return nil, errWorkspaceApplicationRecoveryConflict
	}
	if err != nil {
		return nil, err
	}
	entities, err := tx.RuntimeOperation.Query().Where(runtimeoperation.WorkspaceIDEQ(current.ID), lockRowForUpdate).All(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(entities))
	var selected map[string]any
	for _, entity := range entities {
		row := recordFromEnt(entity, runtimeOpEntFields)
		rows = append(rows, row)
		if entity.ID == operationID {
			selected = row
		}
	}
	next, err := workspaceApplicationRecoveryRow(recordFromEnt(current, workspaceEntFields), selected, rows)
	if err != nil {
		return nil, err
	}
	if err := tx.RuntimeOperation.UpdateOneID(operationID).SetResult(stringValue(next["result"])).SetStatus(stringValue(next["status"])).Exec(ctx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return next, nil
}

func registerWorkspaceApplicationRecoveryRoutes(mux *http.ServeMux, app *controlPlaneServer) {
	// Retained credential/Key rotations and historical deployments still need
	// explicit recovery of their frozen operation under the original store fences.
	mux.HandleFunc("POST /api/operator/application-deployments/{operationID}/retry", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requiredMutationKey(w, r); !ok {
			return
		}
		row, err := app.tables.ResumeWorkspaceApplicationDeployment(r.Context(), r.PathValue("operationID"))
		if err != nil {
			writeError(w, http.StatusConflict, "workspace_application_recovery_conflict")
			return
		}
		intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"intent": intent})
	}))
}
