package server

import (
	"context"
	"errors"

	controlplaneent "opl-cloud/services/control-plane/ent"
	"opl-cloud/services/control-plane/ent/runtimeoperation"
)

var errWorkspaceApplicationOperationCASConflict = errors.New("workspace_application_operation_cas_conflict")

func workspaceDefaultApplicationPreparationBlocks(row map[string]any) bool {
	if stringValue(row["action"]) != workspaceDefaultApplicationAction {
		return false
	}
	request, err := decodeWorkspaceDefaultApplication(row)
	return err != nil || request.Phase == "preparing_credentials"
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
	default:
		return errWorkspaceApplicationOperationCASConflict
	}
	return nil
}

func (s *postgresEntStateStore) PersistWorkspaceApplicationOperation(ctx context.Context, expectedResult string, desiredRow map[string]any) error {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
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
