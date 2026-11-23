package server

import (
	"context"
	"errors"
	"time"
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	current, found, err := s.getSQLRecord(ctx, tx, "control_plane_runtime_operations", stringValue(desiredRow["id"]), runtimeOpEntFields, true)
	if err != nil {
		return err
	}
	if !found {
		return errWorkspaceApplicationOperationCASConflict
	}
	if err := validateWorkspaceApplicationOperationPersistence(current, expectedResult, desiredRow); err != nil {
		return err
	}
	if _, err := s.execSQL(ctx, tx, "UPDATE control_plane_runtime_operations SET result = ?, status = ?, updated_at = ? WHERE id = ?", stringValue(desiredRow["result"]), stringValue(desiredRow["status"]), time.Now().UTC(), stringValue(current["id"])); err != nil {
		return err
	}
	return tx.Commit()
}
