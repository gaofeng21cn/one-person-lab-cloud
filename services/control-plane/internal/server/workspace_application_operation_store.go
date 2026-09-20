package server

import (
	"context"
	"errors"
	"reflect"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	controlplaneent "opl-cloud/services/control-plane/ent"
	"opl-cloud/services/control-plane/ent/runtimeoperation"
	"opl-cloud/services/control-plane/ent/workspace"
)

var errWorkspaceApplicationOperationCASConflict = errors.New("workspace_application_operation_cas_conflict")

func workspaceDefaultApplicationPreparationBlocks(row map[string]any) bool {
	if stringValue(row["action"]) != workspaceDefaultApplicationAction {
		return false
	}
	request, err := decodeWorkspaceDefaultApplication(row)
	return err != nil || request.Phase == "preparing_credentials"
}

func validateWorkspaceDefaultApplicationPreparation(workspaceRow, desiredRow map[string]any, operations []map[string]any) error {
	request, err := decodeWorkspaceDefaultApplication(desiredRow)
	if err != nil {
		return errWorkspaceApplicationOperationCASConflict
	}
	if request.Phase != "preparing_credentials" {
		return nil
	}
	for _, row := range operations {
		if workspaceDeleteBlocksRotation(row) || workspaceKeyRotationBlocksDelete(row) {
			return errWorkspaceApplicationOperationCASConflict
		}
	}
	if workspaceRow != nil {
		if stringValue(workspaceRow["id"]) != request.WorkspaceID || firstNonEmpty(stringValue(workspaceRow["accountId"]), stringValue(workspaceRow["ownerAccountId"])) != request.AccountID || !workspaceApplicationEntitlementOpen(workspaceRow, time.Now().UTC()) {
			return errWorkspaceApplicationOperationCASConflict
		}
		return nil
	}
	launchRow := findRecord(operations, request.LaunchOperationID)
	launch, err := decodeWorkspaceLaunchReconcileOperation(launchRow)
	if err != nil || launch.stringFact("accountId") != request.AccountID || launch.stringFact("workspaceId") != request.WorkspaceID || launch.stringFact("ownerUserId") != request.OwnerUserID || launch.int64Fact("sub2apiUserId") != request.Sub2APIUserID || launch.Status != "pending" && launch.Status != "running" {
		return errWorkspaceApplicationOperationCASConflict
	}
	return nil
}

// Progress writes preserve the accepted command and compare the complete last
// read result. A late provider response cannot replace a newer activation or
// the outcome of another recovery worker.
func validateWorkspaceApplicationOperationPersistence(current map[string]any, expectedResult string, desired map[string]any) error {
	if expectedResult == "" || stringValue(current["result"]) != expectedResult {
		return errWorkspaceApplicationOperationCASConflict
	}
	for _, field := range []string{"id", "operationId", "accountId", "workspaceId", "resourceId", "resourceKind", "action", "createdAt"} {
		if stringValue(current[field]) != stringValue(desired[field]) {
			return errWorkspaceApplicationOperationCASConflict
		}
	}
	switch stringValue(current["action"]) {
	case workspaceApplicationDeploymentAction:
		before, beforeErr := decodeWorkspaceApplicationDeploymentIntent(current)
		after, afterErr := decodeWorkspaceApplicationDeploymentIntent(desired)
		if beforeErr != nil || afterErr != nil || before.Version != after.Version || before.RequestHash != after.RequestHash {
			return errWorkspaceApplicationOperationCASConflict
		}
		switch stringValue(desired["status"]) {
		case "pending", "running", "manual_review", "succeeded":
		default:
			return errWorkspaceApplicationOperationCASConflict
		}
	case workspaceDefaultApplicationAction:
		before, beforeErr := decodeWorkspaceDefaultApplication(current)
		after, afterErr := decodeWorkspaceDefaultApplication(desired)
		if beforeErr != nil || afterErr != nil {
			return errWorkspaceApplicationOperationCASConflict
		}
		next, err := workspaceDefaultApplicationRow(after)
		if err != nil || stringValue(next["status"]) != stringValue(desired["status"]) {
			return errWorkspaceApplicationOperationCASConflict
		}
		before.Phase, before.GatewaySecret, before.WorkspaceAPIKeyID, before.DeploymentID, before.LastError = "", nil, 0, "", ""
		after.Phase, after.GatewaySecret, after.WorkspaceAPIKeyID, after.DeploymentID, after.LastError = "", nil, 0, "", ""
		if !reflect.DeepEqual(before, after) {
			return errWorkspaceApplicationOperationCASConflict
		}
	default:
		return errWorkspaceApplicationOperationCASConflict
	}
	return nil
}

// A resource-only Launch deliberately carries no installation request: the
// Console Launch delivers capacity, and the application is installed afterwards
// as its own authorized operation. This is that operation's admission rule. It
// binds the request to the Workspace that is already running, to the single
// succeeded resource-only Launch that created it, and to that Workspace's open
// entitlement window, so a start can neither invent a Workspace nor inherit a
// full Launch's application facts.
func validateWorkspaceApplicationInstallationStart(workspaceRow, desiredRow map[string]any, operations []map[string]any) error {
	request, err := decodeWorkspaceDefaultApplication(desiredRow)
	if err != nil || workspaceRow == nil || request.Phase != "credentials_required" {
		return errWorkspaceApplicationOperationCASConflict
	}
	if stringValue(workspaceRow["id"]) != request.WorkspaceID ||
		firstNonEmpty(stringValue(workspaceRow["accountId"]), stringValue(workspaceRow["ownerAccountId"])) != request.AccountID ||
		!workspaceApplicationEntitlementOpen(workspaceRow, time.Now().UTC()) {
		return errWorkspaceApplicationOperationCASConflict
	}
	for _, row := range operations {
		if stringValue(row["action"]) == workspaceDefaultApplicationAction || workspaceDeleteBlocksRotation(row) || workspaceKeyRotationBlocksDelete(row) {
			return errWorkspaceApplicationOperationCASConflict
		}
	}
	launch, err := decodeWorkspaceLaunchReconcileOperation(findRecord(operations, request.LaunchOperationID))
	if err != nil || launch.ID != request.LaunchOperationID ||
		launch.provisioningMode() != contracts.WorkspaceProvisioningResourceOnly || launch.Status != contracts.StatusSucceeded ||
		launch.stringFact("accountId") != request.AccountID || launch.stringFact("workspaceId") != request.WorkspaceID ||
		launch.stringFact("ownerUserId") != request.OwnerUserID || launch.int64Fact("sub2apiUserId") != request.Sub2APIUserID ||
		launch.stringFact("workspaceKeyGroupId") != "" || launch.stringFact("workspaceImageDigest") != "" {
		return errWorkspaceApplicationOperationCASConflict
	}
	return nil
}

// CreateWorkspaceApplicationOperation inserts the one installation request a
// resource-only Workspace may carry. The workspace and its operations are
// locked first, so a start cannot race an in-flight deletion or key rotation,
// and the request ID is the constraint that makes a second concurrent start
// fail instead of overwriting the accepted one.
func (s *postgresEntStateStore) CreateWorkspaceApplicationOperation(ctx context.Context, desiredRow map[string]any) error {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	workspaceID := stringValue(desiredRow["workspaceId"])
	workspaceEntity, workspaceErr := tx.Workspace.Query().Where(workspace.IDEQ(workspaceID), lockRowForUpdate).Only(ctx)
	if workspaceErr != nil && !controlplaneent.IsNotFound(workspaceErr) {
		return workspaceErr
	}
	var workspaceRow map[string]any
	if workspaceErr == nil {
		workspaceRow = recordFromEnt(workspaceEntity, workspaceEntFields)
	}
	entities, err := tx.RuntimeOperation.Query().Where(runtimeoperation.WorkspaceIDEQ(workspaceID), lockRowForUpdate).All(ctx)
	if err != nil {
		return err
	}
	operations := make([]map[string]any, 0, len(entities))
	for _, entity := range entities {
		operations = append(operations, recordFromEnt(entity, runtimeOpEntFields))
	}
	if err := validateWorkspaceApplicationInstallationStart(workspaceRow, desiredRow, operations); err != nil {
		return err
	}
	if err := saveRecord(ctx, stringValue(desiredRow["id"]), desiredRow, tx.RuntimeOperation.Create(), runtimeOpEntFields); err != nil {
		if controlplaneent.IsConstraintError(err) {
			return errWorkspaceApplicationOperationCASConflict
		}
		return err
	}
	return tx.Commit()
}

func (s *postgresEntStateStore) PersistWorkspaceApplicationOperation(ctx context.Context, expectedResult string, desiredRow map[string]any) error {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if stringValue(desiredRow["action"]) == workspaceDefaultApplicationAction {
		workspaceID := stringValue(desiredRow["workspaceId"])
		workspaceEntity, workspaceErr := tx.Workspace.Query().Where(workspace.IDEQ(workspaceID), lockRowForUpdate).Only(ctx)
		if workspaceErr != nil && !controlplaneent.IsNotFound(workspaceErr) {
			return workspaceErr
		}
		var workspaceRow map[string]any
		if workspaceErr == nil {
			workspaceRow = recordFromEnt(workspaceEntity, workspaceEntFields)
		}
		entities, err := tx.RuntimeOperation.Query().Where(runtimeoperation.WorkspaceIDEQ(workspaceID), lockRowForUpdate).All(ctx)
		if err != nil {
			return err
		}
		operations := make([]map[string]any, 0, len(entities))
		for _, entity := range entities {
			operations = append(operations, recordFromEnt(entity, runtimeOpEntFields))
		}
		if err := validateWorkspaceDefaultApplicationPreparation(workspaceRow, desiredRow, operations); err != nil {
			return err
		}
	}
	entity, err := tx.RuntimeOperation.Query().Where(runtimeoperation.IDEQ(stringValue(desiredRow["id"])), lockRowForUpdate).Only(ctx)
	if controlplaneent.IsNotFound(err) {
		return errWorkspaceApplicationOperationCASConflict
	}
	if err != nil {
		return err
	}
	if err := validateWorkspaceApplicationOperationPersistence(recordFromEnt(entity, runtimeOpEntFields), expectedResult, desiredRow); err != nil {
		return err
	}
	if err := tx.RuntimeOperation.UpdateOneID(entity.ID).SetResult(stringValue(desiredRow["result"])).SetStatus(stringValue(desiredRow["status"])).Exec(ctx); err != nil {
		return err
	}
	return tx.Commit()
}
