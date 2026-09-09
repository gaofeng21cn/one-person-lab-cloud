package server

import (
	"context"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"

	controlplaneent "opl-cloud/services/control-plane/ent"
	"opl-cloud/services/control-plane/ent/account"
	"opl-cloud/services/control-plane/ent/runtimeoperation"
)

// SaveWalletAdjustment reserves refund capacity in the same transaction as the
// first operation write. Subsequent writes compare the exact persisted snapshot
// so that two processes cannot dispatch from the same unclaimed state.
func (s *postgresEntStateStore) SaveWalletAdjustment(ctx context.Context, operationID string, operation walletAdjustmentOperation) (walletAdjustmentOperation, error) {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return operation, err
	}
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	if operation.PersistedResult == "" {
		if _, err := client.Account.Query().Where(account.IDEQ(operation.AccountID), lockRowForUpdate).Only(ctx); err != nil {
			return operation, err
		}
	}
	existing, err := client.RuntimeOperation.Query().Where(runtimeoperation.IDEQ(operationID), lockRowForUpdate).Only(ctx)
	if err != nil && !controlplaneent.IsNotFound(err) {
		return operation, err
	}
	row := walletAdjustmentRow(operationID, operation)
	reserve := existing == nil
	if existing != nil {
		current, decodeErr := decodeWalletAdjustment(recordFromEnt(existing, runtimeOpEntFields))
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
	if reserve {
		if operation.Kind == "business_refund" {
			original, err := client.RuntimeOperation.Query().Where(runtimeoperation.IDEQ(operation.RelatedOperationID), lockRowForUpdate).Only(ctx)
			if controlplaneent.IsNotFound(err) {
				return operation, errWalletAdjustmentConflict
			}
			if err != nil {
				return operation, err
			}
			rows, err := walletRefundRows(ctx, client, operation.RelatedOperationID, operationID)
			if err != nil {
				return operation, err
			}
			if err := validateWalletRefundReservation(recordFromEnt(original, runtimeOpEntFields), rows, operation); err != nil {
				return operation, err
			}
		}
	}
	if existing != nil {
		builder := client.RuntimeOperation.UpdateOneID(operationID)
		setRecordFieldsWithEmptyText(builder, row, runtimeOpEntFields, true)
		if err := execCreate(ctx, builder); err != nil {
			return operation, err
		}
	} else {
		if err := saveRecord(ctx, operationID, row, client.RuntimeOperation.Create(), runtimeOpEntFields); err != nil {
			return operation, err
		}
	}
	if err := tx.Commit(); err != nil {
		return operation, err
	}
	return decodeWalletAdjustment(row)
}

func walletRefundRows(ctx context.Context, client *controlplaneent.Client, originalID, excludedID string) ([]map[string]any, error) {
	query := client.RuntimeOperation.Query().Where(runtimeoperation.ActionEQ("gateway.wallet_adjustment.v1"), func(selector *sql.Selector) {
		selector.Where(sql.P(func(b *sql.Builder) {
			if selector.Dialect() == dialect.Postgres {
				b.WriteString("(").Ident(selector.C(runtimeoperation.FieldResult)).WriteString("::jsonb ->> 'relatedOperationId')")
			} else {
				b.WriteString("json_extract(").Ident(selector.C(runtimeoperation.FieldResult)).WriteString(", '$.relatedOperationId')")
			}
			b.WriteString(" = ").Arg(originalID)
		}))
	})
	if excludedID != "" {
		query.Where(runtimeoperation.IDNEQ(excludedID))
	}
	refunds, err := query.All(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(refunds))
	for _, refund := range refunds {
		rows = append(rows, recordFromEnt(refund, runtimeOpEntFields))
	}
	return rows, nil
}

func (s *postgresEntStateStore) ReserveWorkspaceLaunchCloseoutRefund(ctx context.Context, operation workspaceLaunchReconcileOperation) ([]walletRefundOperation, error) {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	if _, err := client.Account.Query().Where(account.IDEQ(operation.stringFact("accountId")), lockRowForUpdate).Only(ctx); err != nil {
		return nil, err
	}
	// Match the normal wallet writer's wallet-row -> original-row lock order.
	if _, err := client.RuntimeOperation.Query().Where(runtimeoperation.IDEQ(workspaceLaunchRefundOperationID(operation.ID)), lockRowForUpdate).Only(ctx); err != nil && !controlplaneent.IsNotFound(err) {
		return nil, err
	}
	original, err := client.RuntimeOperation.Query().Where(runtimeoperation.IDEQ(operation.ID), lockRowForUpdate).Only(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := walletRefundRows(ctx, client, operation.ID, "")
	if err != nil {
		return nil, err
	}
	refunds, created, err := prepareWorkspaceLaunchCloseoutRefund(recordFromEnt(original, runtimeOpEntFields), rows, operation)
	if err != nil {
		return nil, err
	}
	if created != nil {
		row := walletAdjustmentRow(created.ID, created.Operation)
		if err := saveRecord(ctx, created.ID, row, client.RuntimeOperation.Create(), runtimeOpEntFields); err != nil {
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
