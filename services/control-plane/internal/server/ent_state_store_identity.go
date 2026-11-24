package server

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	accountEntFields = []sqlRecordField{
		sqlTextField("owner_user_id", "ownerUserId"),
		sqlIntField("sub2api_user_id", "sub2apiUserId"),
		sqlTextField("name", "name"),
		sqlTextField("status", "status"),
		sqlBoolField("workspace_purchase_enabled", "workspacePurchaseEnabled"),
	}
	userEntFields = []sqlRecordField{
		sqlTextField("account_id", "accountId"),
		sqlTextField("email", "email"),
		sqlTextField("role", "role"),
		sqlTextField("status", "status"),
		sqlTextField("disabled_at", "disabledAt"),
		sqlTextField("disabled_by", "disabledBy"),
		sqlTextField("disabled_reason", "disabledReason"),
		sqlTextField("deleted_at", "deletedAt"),
		sqlTextField("deleted_by", "deletedBy"),
		sqlTextField("delete_reason", "deleteReason"),
	}
	sessionEntFields = []sqlRecordField{
		sqlTextField("user_id", "userId"),
		sqlTextField("csrf", "csrf"),
		sqlTextField("expires_at", "expiresAt"),
	}
)

func (s *postgresEntStateStore) ListUsers(ctx context.Context, includeDeleted bool) ([]map[string]any, error) {
	suffix := ""
	if !includeDeleted {
		suffix = "WHERE status <> 'deleted'"
	}
	return s.listSQLRecords(ctx, s.db, "control_plane_users", userEntFields, suffix)
}

func (s *postgresEntStateStore) GetUser(ctx context.Context, id string) (map[string]any, bool, error) {
	return s.getSQLRecord(ctx, s.db, "control_plane_users", id, userEntFields, false)
}

func (s *postgresEntStateStore) GetUserByEmail(ctx context.Context, email string, includeDeleted bool) (map[string]any, bool, error) {
	email, err := canonicalEmail(email)
	if err != nil {
		return nil, false, err
	}
	suffix := "WHERE email = ? LIMIT 2"
	if !includeDeleted {
		suffix = "WHERE email = ? AND status <> 'deleted' LIMIT 2"
	}
	rows, err := s.listSQLRecords(ctx, s.db, "control_plane_users", userEntFields, suffix, email)
	if err != nil {
		return nil, false, err
	}
	if len(rows) == 0 {
		return nil, false, nil
	}
	if len(rows) > 1 {
		return nil, false, errUserExists
	}
	return rows[0], true, nil
}

func (s *postgresEntStateStore) SaveUser(ctx context.Context, row map[string]any) error {
	operator := isReservedOperatorIdentity(row)
	if stringValue(row["role"]) != "owner" && !operator {
		return errInvalidRole
	}
	return s.replaceRecord(ctx, "control_plane_users", row, userEntFields)
}

func (s *postgresEntStateStore) DeleteUser(ctx context.Context, id string) error {
	return s.deleteSQLByID(ctx, s.db, "control_plane_users", id)
}

func (s *postgresEntStateStore) ListSessions(ctx context.Context) (controlPlaneRecordSet, error) {
	return s.loadSQLRecordSet(ctx, s.db, "control_plane_sessions", sessionEntFields, "")
}

func (s *postgresEntStateStore) GetSession(ctx context.Context, id string) (map[string]any, bool, error) {
	return s.getSQLRecord(ctx, s.db, "control_plane_sessions", id, sessionEntFields, false)
}

func (s *postgresEntStateStore) ListSessionsByUser(ctx context.Context, userID string) (controlPlaneRecordSet, error) {
	return s.loadSQLRecordSet(ctx, s.db, "control_plane_sessions", sessionEntFields, "WHERE user_id = ?", userID)
}

func (s *postgresEntStateStore) SaveSession(ctx context.Context, row map[string]any) error {
	if !validSessionLookupKey(stringValue(row["id"])) {
		return errors.New("invalid_session_id")
	}
	return s.replaceRecord(ctx, "control_plane_sessions", row, sessionEntFields)
}

func (s *postgresEntStateStore) DeleteSession(ctx context.Context, id string) error {
	return s.deleteSQLByID(ctx, s.db, "control_plane_sessions", id)
}

func (s *postgresEntStateStore) ListAccounts(ctx context.Context, accountID string) ([]map[string]any, error) {
	if accountID != "" {
		return s.listSQLRecords(ctx, s.db, "control_plane_accounts", accountEntFields, "WHERE id = ?", accountID)
	}
	return s.listSQLRecords(ctx, s.db, "control_plane_accounts", accountEntFields, "")
}

func (s *postgresEntStateStore) GetAccount(ctx context.Context, id string) (map[string]any, bool, error) {
	return s.getSQLRecord(ctx, s.db, "control_plane_accounts", id, accountEntFields, false)
}

// Multiple rows must fail closed rather than picking one arbitrarily.
func (s *postgresEntStateStore) GetAccountBySub2APIUserID(ctx context.Context, subjectID int64) (map[string]any, bool, error) {
	rows, err := s.listSQLRecords(ctx, s.db, "control_plane_accounts", accountEntFields, "WHERE sub2api_user_id = ? LIMIT 2", subjectID)
	if err != nil {
		return nil, false, err
	}
	if len(rows) == 0 {
		return nil, false, nil
	}
	if len(rows) > 1 {
		return nil, false, errSub2APIAccountMappingConflict
	}
	return rows[0], true, nil
}

func (s *postgresEntStateStore) PageAccounts(ctx context.Context, page tablePageQuery) (tablePage, error) {
	var total int
	if err := s.queryRow(ctx, s.db, "SELECT count(*) FROM control_plane_accounts").Scan(&total); err != nil {
		return tablePage{}, err
	}
	items, err := s.listSQLRecords(ctx, s.db, "control_plane_accounts", accountEntFields, "ORDER BY id ASC LIMIT ? OFFSET ?", page.Limit, page.Offset)
	if err != nil {
		return tablePage{}, err
	}
	return tablePage{Items: items, Total: total}, nil
}

func (s *postgresEntStateStore) CountAccountStatuses(ctx context.Context) (map[string]int, error) {
	rows, err := s.queryRows(ctx, s.db, "SELECT status, count(*) FROM control_plane_accounts GROUP BY status")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

func (s *postgresEntStateStore) SaveAccount(ctx context.Context, row map[string]any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		if int64(numberField(row, "sub2apiUserId", 0)) > 0 && isSQLConstraintError(err) {
			return errSub2APIAccountMappingConflict
		}
		return err
	}
	accountRows, err := s.listSQLRecords(ctx, tx, "control_plane_accounts", accountEntFields, "")
	if err != nil {
		return rollback(err)
	}
	if err := validateSub2APIAccountMapping(accountRows, row); err != nil {
		return rollback(err)
	}
	id := stringValue(row["id"])
	if id == "" {
		return rollback(errors.New("missing_record_id"))
	}
	if err := s.deleteSQLByID(ctx, tx, "control_plane_accounts", id); err != nil {
		return rollback(err)
	}
	if err := s.insertSQLRecord(ctx, tx, "control_plane_accounts", id, row, accountEntFields); err != nil {
		return rollback(err)
	}
	return tx.Commit()
}

func (s *postgresEntStateStore) ApplyWorkspacePurchaseEligibility(ctx context.Context, mutation workspacePurchaseEligibilityMutation) (map[string]any, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	rollback := func(err error) (map[string]any, error) {
		_ = tx.Rollback()
		return nil, err
	}
	auditID := stringValue(mutation.AuditEvent["id"])
	if auditID == "" || mutation.AccountID == "" {
		return rollback(errIdempotencyConflict)
	}
	if existing, auditFound, auditErr := s.getSQLRecord(ctx, tx, "control_plane_admin_audit_events", auditID, auditEntFields, true); auditErr != nil {
		return rollback(auditErr)
	} else if auditFound {
		accountRow, accountFound, accountErr := s.getSQLRecord(ctx, tx, "control_plane_accounts", mutation.AccountID, accountEntFields, true)
		if accountErr != nil || !accountFound || !workspacePurchaseEligibilityAuditMatches(existing, mutation.AuditEvent) ||
			boolValue(accountRow["workspacePurchaseEnabled"]) != mutation.Enabled {
			return rollback(errIdempotencyConflict)
		}
		_ = tx.Rollback()
		return accountRow, nil
	}

	accountRow, accountFound, err := s.getSQLRecord(ctx, tx, "control_plane_accounts", mutation.AccountID, accountEntFields, true)
	if err != nil {
		return rollback(err)
	}
	if !accountFound {
		return rollback(sql.ErrNoRows)
	}
	if existing, auditFound, auditErr := s.getSQLRecord(ctx, tx, "control_plane_admin_audit_events", auditID, auditEntFields, true); auditErr != nil {
		return rollback(auditErr)
	} else if auditFound {
		if !workspacePurchaseEligibilityAuditMatches(existing, mutation.AuditEvent) || boolValue(accountRow["workspacePurchaseEnabled"]) != mutation.Enabled {
			return rollback(errIdempotencyConflict)
		}
		_ = tx.Rollback()
		return accountRow, nil
	}

	audit := cloneMap(mutation.AuditEvent)
	audit["before"] = map[string]any{"workspacePurchaseEnabled": boolValue(accountRow["workspacePurchaseEnabled"])}
	if _, err := s.execSQL(ctx, tx, "UPDATE control_plane_accounts SET workspace_purchase_enabled = ?, updated_at = ? WHERE id = ?", mutation.Enabled, time.Now().UTC(), mutation.AccountID); err != nil {
		return rollback(err)
	}
	if err := s.insertSQLRecord(ctx, tx, "control_plane_admin_audit_events", auditID, audit, auditEntFields); err != nil {
		if isSQLConstraintError(err) {
			_ = tx.Rollback()
			return s.replayWorkspacePurchaseEligibility(ctx, mutation)
		}
		return rollback(err)
	}
	saved, savedFound, err := s.getSQLRecord(ctx, tx, "control_plane_accounts", mutation.AccountID, accountEntFields, false)
	if err != nil {
		return rollback(err)
	}
	if !savedFound {
		return rollback(sql.ErrNoRows)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return saved, nil
}

func (s *postgresEntStateStore) replayWorkspacePurchaseEligibility(ctx context.Context, mutation workspacePurchaseEligibilityMutation) (map[string]any, error) {
	existing, auditFound, err := s.getSQLRecord(ctx, s.db, "control_plane_admin_audit_events", stringValue(mutation.AuditEvent["id"]), auditEntFields, false)
	if err != nil || !auditFound {
		return nil, errIdempotencyConflict
	}
	accountRow, accountFound, err := s.getSQLRecord(ctx, s.db, "control_plane_accounts", mutation.AccountID, accountEntFields, false)
	if err != nil || !accountFound || !workspacePurchaseEligibilityAuditMatches(existing, mutation.AuditEvent) || boolValue(accountRow["workspacePurchaseEnabled"]) != mutation.Enabled {
		return nil, errIdempotencyConflict
	}
	return accountRow, nil
}

func (s *postgresEntStateStore) CreateProvisionedAccount(ctx context.Context, accountRow, userRow map[string]any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}
	replayAfterConstraint := func(conflict error) error {
		_ = tx.Rollback()
		readback, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		finish := func(err error) error {
			_ = readback.Rollback()
			return err
		}
		accounts, err := s.loadSQLRecordSet(ctx, readback, "control_plane_accounts", accountEntFields, "")
		if err != nil {
			return finish(err)
		}
		users, err := s.loadSQLRecordSet(ctx, readback, "control_plane_users", userEntFields, "")
		if err != nil {
			return finish(err)
		}
		if accounts[stringValue(accountRow["id"])] == nil || users[stringValue(userRow["id"])] == nil ||
			stageProvisionedAccount(accounts, users, accountRow, userRow) != nil {
			return finish(conflict)
		}
		return finish(nil)
	}
	accountID := stringValue(accountRow["id"])
	_, accountExists, err := s.getSQLRecord(ctx, tx, "control_plane_accounts", accountID, accountEntFields, true)
	if err != nil {
		return rollback(err)
	}
	accounts, err := s.loadSQLRecordSet(ctx, tx, "control_plane_accounts", accountEntFields, "")
	if err != nil {
		return rollback(err)
	}
	users, err := s.loadSQLRecordSet(ctx, tx, "control_plane_users", userEntFields, "")
	if err != nil {
		return rollback(err)
	}
	userID := stringValue(userRow["id"])
	_, userExists := users[userID]
	if err := stageProvisionedAccount(accounts, users, accountRow, userRow); err != nil {
		return rollback(err)
	}

	if accountExists {
		if _, err := s.execSQL(ctx, tx, `
			UPDATE control_plane_accounts
			SET owner_user_id = ?, sub2api_user_id = ?, updated_at = ?
			WHERE id = ?`,
			stringValue(accounts[accountID]["ownerUserId"]), int64(numberField(accounts[accountID], "sub2apiUserId", 0)), time.Now().UTC(), accountID,
		); err != nil {
			if isSQLConstraintError(err) {
				return replayAfterConstraint(errSub2APIAccountMappingConflict)
			}
			return rollback(err)
		}
	} else if err := s.insertSQLRecord(ctx, tx, "control_plane_accounts", accountID, accounts[accountID], accountEntFields); err != nil {
		if isSQLConstraintError(err) {
			return replayAfterConstraint(errSub2APIAccountMappingConflict)
		}
		return rollback(err)
	}
	if !userExists {
		if err := s.insertSQLRecord(ctx, tx, "control_plane_users", userID, users[userID], userEntFields); err != nil {
			if isSQLConstraintError(err) {
				return replayAfterConstraint(errUserExists)
			}
			return rollback(err)
		}
	}
	return tx.Commit()
}

func (s *postgresEntStateStore) ApplyUserLifecycle(ctx context.Context, user map[string]any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}
	userID := stringValue(user["id"])
	if err := s.updateSQLRecord(ctx, tx, "control_plane_users", userID, user, userEntFields, true); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return rollback(errUserNotFound)
		}
		return rollback(err)
	}
	if _, err := s.execSQL(ctx, tx, "DELETE FROM control_plane_sessions WHERE user_id = ?", userID); err != nil {
		return rollback(err)
	}
	if stringValue(user["role"]) == "owner" {
		accountID := stringValue(user["accountId"])
		computes, err := s.listSQLRecords(ctx, tx, "control_plane_compute_allocations", computeEntFields, "WHERE account_id = ? ORDER BY id ASC", accountID)
		if err != nil {
			return rollback(err)
		}
		for _, compute := range computes {
			current, found, err := s.getSQLRecord(ctx, tx, "control_plane_compute_allocations", stringValue(compute["id"]), computeEntFields, true)
			if err != nil {
				return rollback(err)
			}
			if !found || current["autoRenew"] != true {
				continue
			}
			current["autoRenew"] = false
			billingState, err := encodeMonthlyBillingState(current)
			if err != nil {
				return rollback(err)
			}
			if _, err := s.execSQL(ctx, tx, "UPDATE control_plane_compute_allocations SET billing_state_json = ?, updated_at = ? WHERE id = ?", billingState, time.Now().UTC(), stringValue(compute["id"])); err != nil {
				return rollback(err)
			}
		}
		storages, err := s.listSQLRecords(ctx, tx, "control_plane_storage_volumes", storageEntFields, "WHERE account_id = ? ORDER BY id ASC", accountID)
		if err != nil {
			return rollback(err)
		}
		for _, storage := range storages {
			current, found, err := s.getSQLRecord(ctx, tx, "control_plane_storage_volumes", stringValue(storage["id"]), storageEntFields, true)
			if err != nil {
				return rollback(err)
			}
			if !found || current["autoRenew"] != true {
				continue
			}
			current["autoRenew"] = false
			billingState, err := encodeMonthlyBillingState(current)
			if err != nil {
				return rollback(err)
			}
			if _, err := s.execSQL(ctx, tx, "UPDATE control_plane_storage_volumes SET billing_state_json = ?, updated_at = ? WHERE id = ?", billingState, time.Now().UTC(), stringValue(storage["id"])); err != nil {
				return rollback(err)
			}
		}
		workspaces, err := s.listSQLRecords(ctx, tx, "control_plane_workspaces", workspaceEntFields, "WHERE owner_user_id = ? ORDER BY id ASC"+s.forUpdateClause(), userID)
		if err != nil {
			return rollback(err)
		}
		for _, row := range workspaces {
			if row["autoRenew"] != true {
				continue
			}
			row["autoRenew"] = false
			billingState, err := encodeWorkspaceBillingState(row)
			if err != nil {
				return rollback(err)
			}
			if _, err := s.execSQL(ctx, tx, "UPDATE control_plane_workspaces SET billing_state_json = ?, updated_at = ? WHERE id = ?", billingState, time.Now().UTC(), stringValue(row["id"])); err != nil {
				return rollback(err)
			}
		}
	}
	return tx.Commit()
}
