package server

import (
	"context"
	"time"

	controlplaneent "opl-cloud/services/control-plane/ent"
	"opl-cloud/services/control-plane/ent/adminauditevent"
	"opl-cloud/services/control-plane/ent/runtimeoperation"
)

func (s *postgresEntStateStore) ApplyWorkspaceImageReleaseMutation(ctx context.Context, mutation workspaceImageReleaseMutation) (workspaceImageReleasePolicy, error) {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return workspaceImageReleasePolicy{}, err
	}
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	auditID := stringValue(mutation.AuditEvent["id"])
	if existing, auditErr := client.AdminAuditEvent.Query().Where(adminauditevent.IDEQ(auditID), lockRowForUpdate).Only(ctx); auditErr == nil {
		return workspaceImageReleaseReplay(recordFromEnt(existing, auditEntFields), mutation)
	} else if !controlplaneent.IsNotFound(auditErr) {
		return workspaceImageReleasePolicy{}, auditErr
	}

	var currentRow map[string]any
	entity, queryErr := client.RuntimeOperation.Query().Where(runtimeoperation.IDEQ(workspaceImageReleasePolicyID), lockRowForUpdate).Only(ctx)
	if queryErr == nil {
		currentRow = recordFromEnt(entity, runtimeOpEntFields)
	} else if !controlplaneent.IsNotFound(queryErr) {
		return workspaceImageReleasePolicy{}, queryErr
	}
	if existing, auditErr := client.AdminAuditEvent.Query().Where(adminauditevent.IDEQ(auditID), lockRowForUpdate).Only(ctx); auditErr == nil {
		return workspaceImageReleaseReplay(recordFromEnt(existing, auditEntFields), mutation)
	} else if !controlplaneent.IsNotFound(auditErr) {
		return workspaceImageReleasePolicy{}, auditErr
	}

	current := mutation.Base
	createdAt := ""
	if currentRow != nil {
		current, err = decodeWorkspaceImageReleasePolicyRow(currentRow)
		createdAt = stringValue(currentRow["createdAt"])
		if err != nil {
			return workspaceImageReleasePolicy{}, err
		}
	}
	desired, err := prepareWorkspaceImageReleaseMutation(current, mutation, time.Now().UTC())
	if err != nil {
		return workspaceImageReleasePolicy{}, err
	}
	row := workspaceImageReleasePolicyRow(desired, createdAt)
	if entity == nil {
		err = saveRecord(ctx, workspaceImageReleasePolicyID, row, client.RuntimeOperation.Create(), runtimeOpEntFields)
	} else {
		builder := client.RuntimeOperation.UpdateOneID(workspaceImageReleasePolicyID)
		setRecordFieldsWithEmptyText(builder, row, runtimeOpEntFields, true)
		err = execCreate(ctx, builder)
	}
	if err != nil {
		if controlplaneent.IsConstraintError(err) {
			_ = tx.Rollback()
			return s.replayWorkspaceImageReleaseMutation(ctx, mutation)
		}
		return workspaceImageReleasePolicy{}, err
	}
	audit := workspaceImageReleaseAudit(mutation, current, desired)
	if err := saveRecord(ctx, auditID, audit, client.AdminAuditEvent.Create(), auditEntFields); err != nil {
		if controlplaneent.IsConstraintError(err) {
			_ = tx.Rollback()
			return s.replayWorkspaceImageReleaseMutation(ctx, mutation)
		}
		return workspaceImageReleasePolicy{}, err
	}
	if err := tx.Commit(); err != nil {
		return workspaceImageReleasePolicy{}, err
	}
	return desired, nil
}

func (s *postgresEntStateStore) replayWorkspaceImageReleaseMutation(ctx context.Context, mutation workspaceImageReleaseMutation) (workspaceImageReleasePolicy, error) {
	existing, err := s.client.AdminAuditEvent.Get(ctx, stringValue(mutation.AuditEvent["id"]))
	if err != nil {
		if controlplaneent.IsNotFound(err) {
			return workspaceImageReleasePolicy{}, errIdempotencyConflict
		}
		return workspaceImageReleasePolicy{}, err
	}
	return workspaceImageReleaseReplay(recordFromEnt(existing, auditEntFields), mutation)
}
