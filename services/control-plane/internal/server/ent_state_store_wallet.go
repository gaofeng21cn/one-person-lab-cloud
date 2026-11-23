package server

import (
	"context"
)

// SaveWalletAdjustment reserves refund capacity in the same transaction as the
// first operation write. Subsequent writes compare the exact persisted snapshot
// so that two processes cannot dispatch from the same unclaimed state.
func (s *postgresEntStateStore) SaveWalletAdjustment(ctx context.Context, operationID string, operation walletAdjustmentOperation) (walletAdjustmentOperation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return operation, err
	}
	defer func() { _ = tx.Rollback() }()
	if operation.PersistedResult == "" {
		if _, found, err := s.getSQLRecord(ctx, tx, "control_plane_accounts", operation.AccountID, accountEntFields, true); err != nil {
			return operation, err
		} else if !found {
			return operation, errIdempotencyConflict
		}
	}
	existing, existingFound, err := s.getSQLRecord(ctx, tx, "control_plane_runtime_operations", operationID, runtimeOpEntFields, true)
	if err != nil {
		return operation, err
	}
	row := walletAdjustmentRow(operationID, operation)
	reserve := !existingFound
	if existingFound {
		current, decodeErr := decodeWalletAdjustment(existing)
		if decodeErr != nil || !runtimeOperationIdentityMatches(existing, row) || current.RequestHash != operation.RequestHash {
			return operation, errIdempotencyConflict
		}
		if operation.PersistedResult == "" {
			return current, tx.Commit()
		}
		if current.PersistedResult != operation.PersistedResult || current.PersistedStatus != operation.PersistedStatus {
			return operation, errWalletAdjustmentConflict
		}
		reserve = !current.AdjustmentAttempted && operation.AdjustmentAttempted
	} else if operation.PersistedResult != "" {
		return operation, errWalletAdjustmentConflict
	}
	if reserve && operation.Kind == "business_refund" {
		original, originalFound, err := s.getSQLRecord(ctx, tx, "control_plane_runtime_operations", operation.RelatedOperationID, runtimeOpEntFields, true)
		if err != nil {
			return operation, err
		}
		if !originalFound {
			return operation, errWalletAdjustmentConflict
		}
		rows, err := s.walletRefundRows(ctx, tx, operation.RelatedOperationID, operationID)
		if err != nil {
			return operation, err
		}
		if err := validateWalletRefundReservation(original, rows, operation); err != nil {
			return operation, err
		}
	}
	if existingFound {
		if err := s.updateSQLRecord(ctx, tx, "control_plane_runtime_operations", operationID, row, runtimeOpEntFields, true); err != nil {
			return operation, err
		}
	} else {
		if err := s.insertSQLRecord(ctx, tx, "control_plane_runtime_operations", operationID, row, runtimeOpEntFields); err != nil {
			return operation, err
		}
	}
	if err := tx.Commit(); err != nil {
		return operation, err
	}
	return decodeWalletAdjustment(row)
}

func (s *postgresEntStateStore) walletRefundRows(ctx context.Context, runner sqlQueryRunner, originalID, excludedID string) ([]map[string]any, error) {
	relatedExpr := "json_extract(result, '$.relatedOperationId')"
	if s.dialect == "postgres" {
		relatedExpr = "(result::jsonb ->> 'relatedOperationId')"
	}
	suffix := "WHERE action = 'gateway.wallet_adjustment.v1' AND " + relatedExpr + " = ?"
	args := []any{originalID}
	if excludedID != "" {
		suffix += " AND id <> ?"
		args = append(args, excludedID)
	}
	return s.listSQLRecords(ctx, runner, "control_plane_runtime_operations", runtimeOpEntFields, suffix, args...)
}

func (s *postgresEntStateStore) ReserveWorkspaceLaunchCloseoutRefund(ctx context.Context, operation workspaceLaunchReconcileOperation) ([]walletRefundOperation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, found, err := s.getSQLRecord(ctx, tx, "control_plane_accounts", operation.stringFact("accountId"), accountEntFields, true); err != nil {
		return nil, err
	} else if !found {
		return nil, errIdempotencyConflict
	}
	// Match the normal wallet writer's wallet-row -> original-row lock order.
	if _, _, err := s.getSQLRecord(ctx, tx, "control_plane_runtime_operations", workspaceLaunchRefundOperationID(operation.ID), runtimeOpEntFields, true); err != nil {
		return nil, err
	}
	original, originalFound, err := s.getSQLRecord(ctx, tx, "control_plane_runtime_operations", operation.ID, runtimeOpEntFields, true)
	if err != nil {
		return nil, err
	}
	if !originalFound {
		return nil, errWalletAdjustmentConflict
	}
	rows, err := s.walletRefundRows(ctx, tx, operation.ID, "")
	if err != nil {
		return nil, err
	}
	refunds, created, err := prepareWorkspaceLaunchCloseoutRefund(original, rows, operation)
	if err != nil {
		return nil, err
	}
	if created != nil {
		row := walletAdjustmentRow(created.ID, created.Operation)
		if err := s.insertSQLRecord(ctx, tx, "control_plane_runtime_operations", created.ID, row, runtimeOpEntFields); err != nil {
			return nil, err
		}
		created.Operation, err = decodeWalletAdjustment(row)
		if err != nil {
			return nil, err
		}
		refunds[len(refunds)-1] = *created
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return refunds, nil
}
