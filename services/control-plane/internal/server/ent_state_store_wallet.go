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
			refunds, err := client.RuntimeOperation.Query().Where(runtimeoperation.ActionEQ("gateway.wallet_adjustment.v1"), runtimeoperation.IDNEQ(operationID), func(selector *sql.Selector) {
				selector.Where(sql.P(func(b *sql.Builder) {
					if selector.Dialect() == dialect.Postgres {
						b.WriteString("(").Ident(selector.C(runtimeoperation.FieldResult)).WriteString("::jsonb ->> 'relatedOperationId')")
					} else {
						b.WriteString("json_extract(").Ident(selector.C(runtimeoperation.FieldResult)).WriteString(", '$.relatedOperationId')")
					}
					b.WriteString(" = ").Arg(operation.RelatedOperationID)
				}))
			}).All(ctx)
			if err != nil {
				return operation, err
			}
			rows := make([]map[string]any, 0, len(refunds))
			for _, refund := range refunds {
				rows = append(rows, recordFromEnt(refund, runtimeOpEntFields))
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
