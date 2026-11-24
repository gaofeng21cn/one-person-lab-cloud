package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"opl-cloud/services/control-plane/internal/server/sqlcgen"
	controlplanemigrations "opl-cloud/services/control-plane/migrations"
	"opl-cloud/services/internal/postgresmigrate"
)

const (
	controlPlaneMaxOpenDBConnections = 20
)

var errIdempotencyConflict = errors.New("idempotency_conflict")
var errInvalidWorkspaceBillingState = errors.New("invalid_workspace_billing_state")

type controlPlaneRecord = map[string]any
type controlPlaneRecordSet = map[string]controlPlaneRecord

type StateStore interface {
	controlPlaneTableStore
}

func StateStoreFromEnv() (StateStore, error) {
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		return NewPostgresEntStateStore(databaseURL)
	}
	return nil, errors.New("DATABASE_URL is required for control-plane persistence")
}

type sqlQueryRunner interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type postgresEntStateStore struct {
	db                    *sql.DB
	pool                  *pgxpool.Pool
	queries               *sqlcgen.Queries
	dialect               string
	onQuery               func(string)
	beforeUpdateRuntimeOp func(ctx context.Context, id, result, status string) error
}

func (s *postgresEntStateStore) Close() error {
	var err error
	if s.db != nil {
		err = s.db.Close()
	}
	if s.pool != nil {
		s.pool.Close()
	}
	return err
}

func NewPostgresEntStateStore(databaseURL string) (StateStore, error) {
	if err := postgresmigrate.ValidateTLS(databaseURL); err != nil {
		return nil, err
	}
	return newPostgresEntStateStore(databaseURL)
}

func newTestPostgresEntStateStore(databaseURL string) (StateStore, error) {
	return newPostgresEntStateStore(databaseURL)
}

func newPostgresEntStateStore(databaseURL string) (StateStore, error) {
	ctx := context.Background()
	pool, db, err := postgresmigrate.OpenPool(ctx, databaseURL, func(cfg *pgxpool.Config) {
		// ponytail: fixed for the single Pod; revisit only with measured DB and replica capacity.
		cfg.MaxConns = controlPlaneMaxOpenDBConnections
	})
	if err != nil {
		return nil, err
	}
	store := &postgresEntStateStore{db: db, pool: pool, queries: sqlcgen.New(pool), dialect: "postgres"}
	if err := postgresmigrate.Apply(ctx, db, "control-plane", controlPlaneMigrations(controlplanemigrations.OpenDriver(db))); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func controlPlaneMigrations(driver controlplanemigrations.Driver) []postgresmigrate.Migration {
	return []postgresmigrate.Migration{
		{Version: "202607140001_sub2api_monthly_hard_cut", Run: func(ctx context.Context) error {
			return controlplanemigrations.Apply(ctx, driver)
		}},
		{Version: "202608170002_legacy_identity_table_custody", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyLegacyIdentityTableCustody(ctx, driver)
		}},
		{Version: "202607150001_legacy_membership_normalize", Run: func(ctx context.Context) error {
			return validateAndNormalizeLegacyMemberships(ctx, driver)
		}},
		{Version: "202607150002_pre_schema_backfill", Run: func(ctx context.Context) error {
			return backfillControlPlaneMigrationNulls(ctx, driver)
		}},
		{Version: "202607150003_ent_schema", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyCoreSchema(ctx, driver)
		}},
		{Version: "202607150004_post_schema_backfill", Run: func(ctx context.Context) error {
			return backfillControlPlaneMigrationNulls(ctx, driver)
		}},
		{Version: "202607160001_sub2api_user_unique", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplySub2APIUserUniqueness(ctx, driver)
		}},
		{Version: "202607160002_primary_workspace", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyPrimaryWorkspace(ctx, driver)
		}},
		{Version: "202607170001_invited_account_identity", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyInvitedAccountIdentity(ctx, driver)
		}},
		{Version: "202607170002_workspace_renewal", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyWorkspaceRenewal(ctx, driver)
		}},
		{Version: "202607170003_workspace_auto_renew_audit", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyAutoRenewAudit(ctx, driver)
		}},
		{Version: "202607180001_customer_identity_hard_cut", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyCustomerIdentityHardCut(ctx, driver)
		}},
		{Version: "202607190001_workspace_api_key_id", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyWorkspaceAPIKeyID(ctx, driver)
		}},
		{Version: "202607190002_pilot_announcements", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyPilotAnnouncements(ctx, driver)
		}},
		{Version: "202607230001_workspace_purchase_receipt_id", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyWorkspacePurchaseReceiptID(ctx, driver)
		}},
		{Version: "202607240001_multi_workspace_pagination", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyMultiWorkspacePagination(ctx, driver)
		}},
		{Version: "202607250001_control_plane_capacity_indexes", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyControlPlaneCapacityIndexes(ctx, driver)
		}},
		{Version: "202608180001_workspace_purchase_eligibility", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyWorkspacePurchaseEligibility(ctx, driver)
		}},
		{Version: "202609120001_workspace_application_binding", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyWorkspaceApplicationBinding(ctx, driver)
		}},
		{Version: "202609120002_application_revisions", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyApplicationRevisions(ctx, driver)
		}},
		{Version: "202609120003_workspace_application_binding_version", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyWorkspaceApplicationBindingVersion(ctx, driver)
		}},
		{Version: "202609130001_application_data_materials", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyApplicationDataMaterials(ctx, driver)
		}},
		{Version: "202609130001_workspace_application_selection", Run: func(ctx context.Context) error {
			return controlplanemigrations.ApplyWorkspaceApplicationSelection(ctx, driver)
		}},
	}
}

func validateAndNormalizeLegacyMemberships(ctx context.Context, driver controlplanemigrations.Driver) error {
	const query = `
DO $$
BEGIN
	IF to_regclass('control_plane_memberships') IS NULL THEN
		RETURN;
	END IF;
	IF NOT EXISTS (SELECT 1 FROM control_plane_memberships) THEN
    RETURN;
  END IF;
  IF to_regclass('control_plane_accounts') IS NULL
    OR to_regclass('control_plane_organizations') IS NULL
    OR to_regclass('control_plane_users') IS NULL
  THEN
    RAISE EXCEPTION 'legacy membership truth tables are missing';
  END IF;
  IF EXISTS (
    SELECT 1
    FROM control_plane_memberships memberships
    LEFT JOIN control_plane_accounts accounts ON accounts.id = memberships.account_id
    LEFT JOIN control_plane_organizations organizations ON organizations.id = memberships.organization_id
    LEFT JOIN control_plane_users users ON users.id = memberships.user_id
	WHERE memberships.role IS NULL
	  OR btrim(memberships.role) = ''
	  OR lower(btrim(memberships.role)) NOT IN ('owner', 'admin', 'member')
      OR accounts.id IS NULL
      OR organizations.id IS NULL
      OR users.id IS NULL
      OR organizations.billing_account_id <> memberships.account_id
      OR users.account_id <> memberships.account_id
  ) THEN
    RAISE EXCEPTION 'legacy membership cannot be mapped without guessing';
  END IF;
  UPDATE control_plane_memberships SET role = lower(btrim(role));
END $$;`
	tx, err := driver.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin legacy membership migration: %w", err)
	}
	if _, err := tx.ExecContext(ctx, query); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("validate legacy memberships: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy membership migration: %w", err)
	}
	return nil
}

func backfillControlPlaneMigrationNulls(ctx context.Context, driver controlplanemigrations.Driver) error {
	const query = `
DO $$
DECLARE
  target_schema text;
  target_table text;
  target_column text;
  target_type text;
BEGIN
  FOR target_schema, target_table IN
    SELECT table_schema, table_name
    FROM information_schema.tables
    WHERE table_schema = 'public'
      AND table_name LIKE 'control_plane_%'
      AND table_type = 'BASE TABLE'
  LOOP
    EXECUTE format('ALTER TABLE %I.%I ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ', target_schema, target_table);
    EXECUTE format('ALTER TABLE %I.%I ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ', target_schema, target_table);
    EXECUTE format(
      'UPDATE %I.%I SET created_at = COALESCE(created_at, NOW()), updated_at = COALESCE(updated_at, created_at, NOW()) WHERE created_at IS NULL OR updated_at IS NULL',
      target_schema,
      target_table
    );
  END LOOP;

  IF to_regclass('public.control_plane_storage_attachments') IS NOT NULL
    AND EXISTS (
      SELECT 1 FROM information_schema.columns
      WHERE table_schema = 'public' AND table_name = 'control_plane_storage_attachments' AND column_name = 'account_id'
    )
  THEN
    IF to_regclass('public.control_plane_workspaces') IS NOT NULL
      AND EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'control_plane_storage_attachments' AND column_name = 'workspace_id'
      )
      AND EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'control_plane_workspaces' AND column_name = 'account_id'
      )
    THEN
      UPDATE control_plane_storage_attachments attachments
      SET account_id = workspaces.account_id
      FROM control_plane_workspaces workspaces
      WHERE COALESCE(attachments.account_id, '') = ''
        AND COALESCE(attachments.workspace_id, '') <> ''
        AND attachments.workspace_id = workspaces.id
        AND COALESCE(workspaces.account_id, '') <> '';
    END IF;

    IF to_regclass('public.control_plane_storage_volumes') IS NOT NULL
      AND EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'control_plane_storage_attachments' AND column_name = 'storage_id'
      )
      AND EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'control_plane_storage_volumes' AND column_name = 'account_id'
      )
    THEN
      UPDATE control_plane_storage_attachments attachments
      SET account_id = volumes.account_id
      FROM control_plane_storage_volumes volumes
      WHERE COALESCE(attachments.account_id, '') = ''
        AND COALESCE(attachments.storage_id, '') <> ''
        AND attachments.storage_id = volumes.id
        AND COALESCE(volumes.account_id, '') <> '';
    END IF;

    IF to_regclass('public.control_plane_storage_volumes') IS NOT NULL
      AND EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'control_plane_storage_attachments' AND column_name = 'volume_id'
      )
      AND EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'control_plane_storage_volumes' AND column_name = 'account_id'
      )
    THEN
      UPDATE control_plane_storage_attachments attachments
      SET account_id = volumes.account_id
      FROM control_plane_storage_volumes volumes
      WHERE COALESCE(attachments.account_id, '') = ''
        AND COALESCE(attachments.volume_id, '') <> ''
        AND attachments.volume_id = volumes.id
        AND COALESCE(volumes.account_id, '') <> '';
    END IF;

    IF to_regclass('public.control_plane_compute_allocations') IS NOT NULL
      AND EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'control_plane_storage_attachments' AND column_name = 'compute_allocation_id'
      )
      AND EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'control_plane_compute_allocations' AND column_name = 'account_id'
      )
    THEN
      UPDATE control_plane_storage_attachments attachments
      SET account_id = computes.account_id
      FROM control_plane_compute_allocations computes
      WHERE COALESCE(attachments.account_id, '') = ''
        AND COALESCE(attachments.compute_allocation_id, '') <> ''
        AND attachments.compute_allocation_id = computes.id
        AND COALESCE(computes.account_id, '') <> '';
    END IF;
  END IF;

  FOR target_schema, target_table, target_column, target_type IN
    SELECT c.table_schema, c.table_name, c.column_name, c.data_type
    FROM information_schema.columns c
    JOIN information_schema.tables t
      ON t.table_schema = c.table_schema
      AND t.table_name = c.table_name
      AND t.table_type = 'BASE TABLE'
    WHERE c.table_schema = 'public'
      AND c.table_name LIKE 'control_plane_%'
      AND c.column_name NOT IN ('id', 'created_at', 'updated_at')
      AND c.data_type IN ('text', 'character varying', 'character', 'boolean', 'bigint', 'integer', 'double precision', 'numeric', 'real')
  LOOP
    IF target_type IN ('text', 'character varying', 'character') THEN
      EXECUTE format('UPDATE %I.%I SET %I = '''' WHERE %I IS NULL', target_schema, target_table, target_column, target_column);
    ELSIF target_type = 'boolean' THEN
      EXECUTE format('UPDATE %I.%I SET %I = false WHERE %I IS NULL', target_schema, target_table, target_column, target_column);
    ELSE
      EXECUTE format('UPDATE %I.%I SET %I = 0 WHERE %I IS NULL', target_schema, target_table, target_column, target_column);
    END IF;
  END LOOP;
END $$;`
	if _, err := driver.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("backfill control-plane migration nulls: %w", err)
	}
	return nil
}

type sqlRecordField struct {
	Column string
	Path   []string
	Kind   string
}

func sqlTextField(column string, path ...string) sqlRecordField {
	return sqlRecordField{Column: column, Path: path, Kind: "text"}
}

func sqlIntField(column string, path ...string) sqlRecordField {
	return sqlRecordField{Column: column, Path: path, Kind: "int"}
}

func sqlNullableIntField(column string, path ...string) sqlRecordField {
	return sqlRecordField{Column: column, Path: path, Kind: "nullable_int"}
}

func sqlFloatField(column string, path ...string) sqlRecordField {
	return sqlRecordField{Column: column, Path: path, Kind: "float"}
}

func sqlBoolField(column string, path ...string) sqlRecordField {
	return sqlRecordField{Column: column, Path: path, Kind: "bool"}
}

func sqlBillingJSONField(column string) sqlRecordField {
	return sqlRecordField{Column: column, Kind: "billing_json"}
}

func sqlJSONTextField(column string, path ...string) sqlRecordField {
	return sqlRecordField{Column: column, Path: path, Kind: "json_text"}
}

func (s *postgresEntStateStore) rebind(query string) string {
	if s.dialect != "postgres" {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 16)
	index := 1
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(index))
			index++
		} else {
			b.WriteByte(query[i])
		}
	}
	return b.String()
}

func (s *postgresEntStateStore) forUpdateClause() string {
	if s.dialect == "postgres" {
		return " FOR UPDATE"
	}
	return ""
}

func (s *postgresEntStateStore) logQuery(bound string, args []any) {
	if s.onQuery != nil {
		s.onQuery(fmt.Sprintf("query=%s args=%v", bound, args))
	}
}

func (s *postgresEntStateStore) execSQL(ctx context.Context, runner sqlQueryRunner, query string, args ...any) (sql.Result, error) {
	bound := s.rebind(query)
	s.logQuery(bound, args)
	return runner.ExecContext(ctx, bound, args...)
}

func (s *postgresEntStateStore) queryRows(ctx context.Context, runner sqlQueryRunner, query string, args ...any) (*sql.Rows, error) {
	bound := s.rebind(query)
	s.logQuery(bound, args)
	return runner.QueryContext(ctx, bound, args...)
}

func (s *postgresEntStateStore) queryRow(ctx context.Context, runner sqlQueryRunner, query string, args ...any) *sql.Row {
	bound := s.rebind(query)
	s.logQuery(bound, args)
	return runner.QueryRowContext(ctx, bound, args...)
}

func isSQLConstraintError(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return strings.HasPrefix(pgErr.Code, "23")
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed") ||
		strings.Contains(msg, "check constraint failed") ||
		strings.Contains(msg, "not null constraint failed") ||
		strings.Contains(msg, "foreign key constraint failed") ||
		strings.Contains(msg, "duplicate key value violates unique constraint")
}

func selectColumnsSQL(fields []sqlRecordField) string {
	cols := make([]string, 0, len(fields)+3)
	cols = append(cols, "id", "created_at", "updated_at")
	for _, field := range fields {
		cols = append(cols, field.Column)
	}
	return strings.Join(cols, ", ")
}

func scanTimestampValue(raw any) time.Time {
	switch typed := raw.(type) {
	case time.Time:
		return typed.UTC()
	case *time.Time:
		if typed != nil {
			return typed.UTC()
		}
	case []byte:
		return parseSQLTimestampText(string(typed))
	case string:
		return parseSQLTimestampText(typed)
	}
	return time.Time{}
}

func parseSQLTimestampText(text string) time.Time {
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSQLRecord(scanner rowScanner, fields []sqlRecordField) (controlPlaneRecord, error) {
	var id string
	var createdRaw, updatedRaw any
	dest := make([]any, len(fields)+3)
	dest[0] = &id
	dest[1] = &createdRaw
	dest[2] = &updatedRaw
	holders := make([]any, len(fields))
	for i, field := range fields {
		switch field.Kind {
		case "int":
			holders[i] = new(sql.NullInt64)
		case "nullable_int":
			holders[i] = new(sql.NullInt64)
		case "float":
			holders[i] = new(sql.NullFloat64)
		case "bool":
			holders[i] = new(sql.NullBool)
		default:
			holders[i] = new(sql.NullString)
		}
		dest[i+3] = holders[i]
	}
	if err := scanner.Scan(dest...); err != nil {
		return nil, err
	}
	row := controlPlaneRecord{"id": id}
	if createdAt := scanTimestampValue(createdRaw); !createdAt.IsZero() {
		row["createdAt"] = createdAt.Format(time.RFC3339Nano)
	}
	if updatedAt := scanTimestampValue(updatedRaw); !updatedAt.IsZero() {
		row["updatedAt"] = updatedAt.Format(time.RFC3339Nano)
	}
	var workspaceBillingJSON string
	for i, field := range fields {
		switch field.Kind {
		case "int":
			val := holders[i].(*sql.NullInt64)
			if val.Valid && val.Int64 != 0 {
				setPath(row, field.Path, val.Int64)
			}
		case "nullable_int":
			val := holders[i].(*sql.NullInt64)
			if val.Valid && val.Int64 != 0 {
				setPath(row, field.Path, val.Int64)
			}
		case "float":
			val := holders[i].(*sql.NullFloat64)
			if val.Valid && val.Float64 != 0 {
				setPath(row, field.Path, val.Float64)
			}
		case "bool":
			val := holders[i].(*sql.NullBool)
			setPath(row, field.Path, val.Valid && val.Bool)
		case "billing_json":
			text := holders[i].(*sql.NullString).String
			var billing map[string]any
			if text != "" && json.Unmarshal([]byte(text), &billing) == nil {
				for key, value := range billing {
					row[key] = value
				}
			}
		case "workspace_billing_json":
			workspaceBillingJSON = holders[i].(*sql.NullString).String
		case "json_text":
			text := holders[i].(*sql.NullString).String
			var decoded any
			if text != "" && json.Unmarshal([]byte(text), &decoded) == nil {
				setPath(row, field.Path, decoded)
			}
		default:
			text := holders[i].(*sql.NullString).String
			if text != "" {
				setPath(row, field.Path, text)
			}
		}
	}
	if billing, err := decodeWorkspaceBillingState(workspaceBillingJSON, row); err == nil {
		for key, value := range billing {
			row[key] = value
		}
	}
	return row, nil
}

func encodeSQLFieldValue(row controlPlaneRecord, field sqlRecordField, includeEmptyText bool) (any, bool, error) {
	if field.Kind == "billing_json" {
		encoded, err := encodeMonthlyBillingState(row)
		return encoded, err == nil, err
	}
	if field.Kind == "workspace_billing_json" {
		encoded, err := encodeWorkspaceBillingState(row)
		return encoded, err == nil, err
	}
	value, ok := valueAtPath(row, field.Path)
	if !ok {
		return nil, false, nil
	}
	switch field.Kind {
	case "int", "nullable_int":
		return int64(numberValue(value)), true, nil
	case "float":
		return numberValue(value), true, nil
	case "bool":
		return boolValue(value), true, nil
	case "json_text":
		encoded, err := json.Marshal(value)
		return string(encoded), err == nil, err
	default:
		text := stringValue(value)
		if text == "" && !includeEmptyText {
			return nil, false, nil
		}
		return text, true, nil
	}
}

func (s *postgresEntStateStore) insertSQLRecord(ctx context.Context, runner sqlQueryRunner, table, id string, row controlPlaneRecord, fields []sqlRecordField) error {
	now := time.Now().UTC()
	createdAt := now
	updatedAt := now
	if parsed, ok := parseRecordTime(row); ok {
		createdAt = parsed
		updatedAt = parsed
	}
	columns := make([]string, 0, len(fields)+3)
	placeholders := make([]string, 0, len(fields)+3)
	args := make([]any, 0, len(fields)+3)
	columns = append(columns, "id", "created_at", "updated_at")
	placeholders = append(placeholders, "?", "?", "?")
	args = append(args, id, createdAt, updatedAt)
	for _, field := range fields {
		val, include, err := encodeSQLFieldValue(row, field, false)
		if err != nil || !include {
			continue
		}
		columns = append(columns, field.Column)
		placeholders = append(placeholders, "?")
		args = append(args, val)
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(columns, ", "), strings.Join(placeholders, ", "))
	_, err := s.execSQL(ctx, runner, query, args...)
	return err
}

func (s *postgresEntStateStore) updateSQLRecord(ctx context.Context, runner sqlQueryRunner, table, id string, row controlPlaneRecord, fields []sqlRecordField, includeEmptyText bool) error {
	assignments := make([]string, 0, len(fields)+1)
	args := make([]any, 0, len(fields)+2)
	assignments = append(assignments, "updated_at = ?")
	args = append(args, time.Now().UTC())
	for _, field := range fields {
		val, include, err := encodeSQLFieldValue(row, field, includeEmptyText)
		if err != nil || !include {
			continue
		}
		assignments = append(assignments, field.Column+" = ?")
		args = append(args, val)
	}
	args = append(args, id)
	query := fmt.Sprintf("UPDATE %s SET %s WHERE id = ?", table, strings.Join(assignments, ", "))
	res, err := s.execSQL(ctx, runner, query, args...)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err == nil && affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *postgresEntStateStore) getSQLRecord(ctx context.Context, runner sqlQueryRunner, table, id string, fields []sqlRecordField, forUpdate bool) (controlPlaneRecord, bool, error) {
	query := fmt.Sprintf("SELECT %s FROM %s WHERE id = ?", selectColumnsSQL(fields), table)
	if forUpdate {
		query += s.forUpdateClause()
	}
	row, err := scanSQLRecord(s.queryRow(ctx, runner, query, id), fields)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return row, true, nil
}

func (s *postgresEntStateStore) listSQLRecords(ctx context.Context, runner sqlQueryRunner, table string, fields []sqlRecordField, suffix string, args ...any) ([]controlPlaneRecord, error) {
	query := fmt.Sprintf("SELECT %s FROM %s", selectColumnsSQL(fields), table)
	if suffix != "" {
		query += " " + suffix
	}
	rows, err := s.queryRows(ctx, runner, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []controlPlaneRecord{}
	for rows.Next() {
		record, err := scanSQLRecord(rows, fields)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *postgresEntStateStore) loadSQLRecordSet(ctx context.Context, runner sqlQueryRunner, table string, fields []sqlRecordField, suffix string, args ...any) (controlPlaneRecordSet, error) {
	rows, err := s.listSQLRecords(ctx, runner, table, fields, suffix, args...)
	if err != nil {
		return nil, err
	}
	out := make(controlPlaneRecordSet, len(rows))
	for _, row := range rows {
		out[stringValue(row["id"])] = row
	}
	return out, nil
}

func (s *postgresEntStateStore) deleteSQLByID(ctx context.Context, runner sqlQueryRunner, table, id string) error {
	_, err := s.execSQL(ctx, runner, fmt.Sprintf("DELETE FROM %s WHERE id = ?", table), id)
	return err
}

// ponytail: keep low-cardinality monthly state in one JSON column at the current
// scale; promote fields to indexed columns only when renewal scans become measurable.
var monthlyBillingStateKeys = []string{
	"resourceType",
	"billingOperationStartedAt",
	"sub2apiRedeemCode",
	"sub2apiRefundCode",
	"priceVersion",
	"currency",
	"priceSnapshot",
	"monthlyPriceCnyCents",
	"chargeUsdMicros",
	"billingAnchorDay",
	"periodStart",
	"paidThrough",
	"autoRenew",
	"lastRenewalAttemptAt",
	"lastBillingError",
	"manualReviewReason",
	"lastReceiptId",
	"sub2apiChargeConfirmation",
	"postChargeBalanceUsdMicros",
	"postChargeBalanceKnown",
	"computeAllocationId",
	"zone",
	"chargeType",
	"renewFlag",
	"deadline",
	"cbsStatus",
	"diskType",
	"providerData",
	"costTags",
	"nodePoolId",
	"instanceType",
	"requestedPeriodMonths",
	"periodMonths",
	"verificationSlotId",
	"customerProduct",
	"pvName",
	"persistentVolumeName",
	"reviewResolutionKey",
	"reviewResolutionFingerprint",
	"reviewResolutionDecision",
	"reviewResolutionEvidenceRef",
	"reviewResolutionReviewer",
	"reviewResolutionPhase",
	"reviewResolutionReceiptId",
	"reviewOriginalReceiptId",
	"reviewResolutionResolvedAt",
	"reviewResolutionResult",
}

var (
	runtimeOpEntFields = []sqlRecordField{
		sqlTextField("operation_id", "operationId"),
		sqlTextField("account_id", "accountId"),
		sqlTextField("workspace_id", "workspaceId"),
		sqlTextField("period_start", "periodStart"),
		sqlTextField("resource_id", "resourceId"),
		sqlTextField("resource_kind", "resourceKind"),
		sqlTextField("action", "action"),
		sqlTextField("provider", "provider"),
		sqlTextField("provider_request_id", "providerRequestId"),
		sqlTextField("status", "status"),
		sqlTextField("result", "result"),
		sqlTextField("compute_allocation_id", "computeAllocationId"),
		sqlTextField("storage_id", "storageId"),
		sqlTextField("attachment_id", "attachmentId"),
		sqlTextField("runtime_service_name", "runtimeServiceName"),
	}
	auditEntFields = []sqlRecordField{
		sqlTextField("actor_user_id", "actorUserId"),
		sqlTextField("actor_role", "actorRole"),
		sqlTextField("actor_account_id", "actorAccountId"),
		sqlTextField("target_account_id", "targetAccountId"),
		sqlTextField("action", "action"),
		sqlTextField("resource_kind", "resourceKind"),
		sqlTextField("resource_id", "resourceId"),
		sqlTextField("ip_address", "ipAddress"),
		sqlTextField("user_agent", "userAgent"),
		sqlJSONTextField("before_json", "before"),
		sqlJSONTextField("after_json", "after"),
		sqlTextField("result", "result"),
	}
	announcementEntFields = []sqlRecordField{
		sqlTextField("title", "title"),
		sqlTextField("body", "body"),
		sqlTextField("status", "status"),
		sqlTextField("starts_at", "startsAt"),
		sqlTextField("ends_at", "endsAt"),
		sqlTextField("published_at", "publishedAt"),
		sqlTextField("created_by_user_id", "createdByUserId"),
		sqlTextField("updated_by_user_id", "updatedByUserId"),
	}
	announcementReadEntFields = []sqlRecordField{
		sqlTextField("announcement_id", "announcementId"),
		sqlTextField("user_id", "userId"),
		sqlTextField("read_at", "readAt"),
	}
	productionE2EEntFields = []sqlRecordField{
		sqlTextField("account_id", "accountId"),
		sqlTextField("workspace_id", "workspaceId"),
		sqlTextField("status", "status"),
		sqlTextField("result", "result"),
		sqlTextField("reason", "reason"),
		sqlTextField("url", "url"),
	}
	reconcileEntFields = []sqlRecordField{
		sqlTextField("status", "status"),
		sqlTextField("guard_status", "guard", "status"),
		sqlTextField("guard_reason", "guard", "reason"),
		sqlTextField("message_author", "messageAuthor"),
		sqlTextField("message_text", "messageText"),
		sqlTextField("message_created_at", "messageCreatedAt"),
		sqlBoolField("guard_block_new_workspaces", "guard", "blockNewWorkspaces"),
		sqlIntField("reports", "reports"),
	}
)

func (s *postgresEntStateStore) ListAuditEvents(ctx context.Context, accountID string) ([]map[string]any, error) {
	suffix := "ORDER BY created_at ASC, id ASC"
	var args []any
	if accountID != "" {
		suffix = "WHERE (target_account_id = ? OR (target_account_id = '' AND actor_account_id = ?)) ORDER BY created_at ASC, id ASC"
		args = []any{accountID, accountID}
	}
	rows, err := s.listSQLRecords(ctx, s.db, "control_plane_admin_audit_events", auditEntFields, suffix, args...)
	if err != nil {
		return nil, err
	}
	return filteredEvents(rows, accountID), nil
}

func (s *postgresEntStateStore) SaveAuditEvent(ctx context.Context, row map[string]any) error {
	return s.replaceRecord(ctx, "control_plane_admin_audit_events", row, auditEntFields)
}

func (s *postgresEntStateStore) ListAnnouncements(ctx context.Context) ([]map[string]any, error) {
	return s.listSQLRecords(ctx, s.db, "control_plane_announcements", announcementEntFields, "ORDER BY created_at DESC, id DESC")
}

func (s *postgresEntStateStore) ApplyAnnouncementMutation(ctx context.Context, mutation announcementMutation) (map[string]any, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	auditID := stringValue(mutation.AuditEvent["id"])
	if existing, found, auditErr := s.getSQLRecord(ctx, tx, "control_plane_admin_audit_events", auditID, auditEntFields, true); auditErr != nil {
		return nil, auditErr
	} else if found {
		return announcementReplay(existing, mutation)
	}

	current, entityFound, queryErr := s.getSQLRecord(ctx, tx, "control_plane_announcements", mutation.AnnouncementID, announcementEntFields, true)
	if queryErr != nil {
		return nil, queryErr
	}
	if existing, found, auditErr := s.getSQLRecord(ctx, tx, "control_plane_admin_audit_events", auditID, auditEntFields, true); auditErr != nil {
		return nil, auditErr
	} else if found {
		return announcementReplay(existing, mutation)
	}

	desired, err := prepareAnnouncementMutation(current, mutation, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if !entityFound {
		err = s.insertSQLRecord(ctx, tx, "control_plane_announcements", mutation.AnnouncementID, desired, announcementEntFields)
	} else {
		err = s.updateSQLRecord(ctx, tx, "control_plane_announcements", mutation.AnnouncementID, desired, announcementEntFields, true)
	}
	if err != nil {
		if isSQLConstraintError(err) {
			_ = tx.Rollback()
			return s.replayAnnouncementMutation(ctx, mutation)
		}
		return nil, err
	}
	authoritative, found, err := s.getSQLRecord(ctx, tx, "control_plane_announcements", mutation.AnnouncementID, announcementEntFields, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, sql.ErrNoRows
	}
	audit := announcementAudit(mutation, current, authoritative)
	if err := s.insertSQLRecord(ctx, tx, "control_plane_admin_audit_events", auditID, audit, auditEntFields); err != nil {
		if isSQLConstraintError(err) {
			_ = tx.Rollback()
			return s.replayAnnouncementMutation(ctx, mutation)
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return authoritative, nil
}

func (s *postgresEntStateStore) replayAnnouncementMutation(ctx context.Context, mutation announcementMutation) (map[string]any, error) {
	existing, found, err := s.getSQLRecord(ctx, s.db, "control_plane_admin_audit_events", stringValue(mutation.AuditEvent["id"]), auditEntFields, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errIdempotencyConflict
	}
	return announcementReplay(existing, mutation)
}

func (s *postgresEntStateStore) ListAnnouncementReads(ctx context.Context, userID string) ([]map[string]any, error) {
	suffix := "ORDER BY created_at ASC, id ASC"
	var args []any
	if userID != "" {
		suffix = "WHERE user_id = ? ORDER BY created_at ASC, id ASC"
		args = []any{userID}
	}
	return s.listSQLRecords(ctx, s.db, "control_plane_announcement_reads", announcementReadEntFields, suffix, args...)
}

func (s *postgresEntStateStore) MarkAnnouncementRead(ctx context.Context, announcementID, userID, readAt string) (map[string]any, error) {
	if announcementID == "" || userID == "" {
		return nil, errAnnouncementNotActive
	}
	id := announcementReadID(announcementID, userID)
	if existing, found, err := s.getSQLRecord(ctx, s.db, "control_plane_announcement_reads", id, announcementReadEntFields, false); err != nil {
		return nil, err
	} else if found {
		return existing, nil
	}
	readTime, ok := optionalAnnouncementTime(readAt)
	if !ok {
		return nil, errAnnouncementNotActive
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	announcementRow, found, err := s.getSQLRecord(ctx, tx, "control_plane_announcements", announcementID, announcementEntFields, true)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errAnnouncementNotActive
	}
	if existing, foundRead, queryErr := s.getSQLRecord(ctx, tx, "control_plane_announcement_reads", id, announcementReadEntFields, false); queryErr != nil {
		return nil, queryErr
	} else if foundRead {
		return existing, nil
	}
	if !announcementIsActive(announcementRow, readTime) {
		return nil, errAnnouncementNotActive
	}
	row := map[string]any{
		"id": id, "announcementId": announcementID, "userId": userID, "readAt": readAt,
		"createdAt": readAt, "updatedAt": readAt,
	}
	if err := s.insertSQLRecord(ctx, tx, "control_plane_announcement_reads", id, row, announcementReadEntFields); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *postgresEntStateStore) MarkCloudAnnouncementRead(ctx context.Context, announcementID, cloudUserID, legacyUserID, readAt string) (map[string]any, error) {
	canonical, alias := announcementReadReferences(cloudUserID, legacyUserID)
	return s.markAnnouncementReadAliases(ctx, announcementID, canonical, alias, readAt)
}

// Publication mutations use this same announcement row lock, so an existing
// ACK can never bypass withdrawal/window checks. Canonical and exact alias
// references are folded atomically, preserving the original earliest read_at.
func (s *postgresEntStateStore) markAnnouncementReadAliases(ctx context.Context, announcementID, userID, aliasUserID, readAt string) (map[string]any, error) {
	readTime, ok := optionalAnnouncementTime(readAt)
	if announcementID == "" || userID == "" || !ok || readTime.IsZero() {
		return nil, errAnnouncementNotActive
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	publication, found, err := s.getSQLRecord(ctx, tx, "control_plane_announcements", announcementID, announcementEntFields, true)
	if err != nil {
		return nil, err
	}
	if !found || !announcementIsActive(publication, time.Now().UTC()) {
		return nil, errAnnouncementNotActive
	}
	var existing []map[string]any
	if aliasUserID != "" {
		existing, err = s.listSQLRecords(ctx, tx, "control_plane_announcement_reads", announcementReadEntFields, "WHERE announcement_id = ? AND user_id IN (?, ?)", announcementID, userID, aliasUserID)
	} else {
		existing, err = s.listSQLRecords(ctx, tx, "control_plane_announcement_reads", announcementReadEntFields, "WHERE announcement_id = ? AND user_id = ?", announcementID, userID)
	}
	if err != nil {
		return nil, err
	}
	var canonical map[string]any
	for _, entity := range existing {
		if stringValue(entity["userId"]) == userID {
			canonical = entity
		}
	}
	row, err := mergedAnnouncementRead(announcementID, userID, readAt, existing)
	if err != nil {
		return nil, err
	}
	if canonical != nil {
		if stringValue(canonical["readAt"]) != stringValue(row["readAt"]) {
			if _, err := s.execSQL(ctx, tx, "UPDATE control_plane_announcement_reads SET read_at = ?, updated_at = ? WHERE id = ?", stringValue(row["readAt"]), time.Now().UTC(), stringValue(canonical["id"])); err != nil {
				return nil, err
			}
		}
	} else if err := s.insertSQLRecord(ctx, tx, "control_plane_announcement_reads", stringValue(row["id"]), row, announcementReadEntFields); err != nil {
		return nil, err
	}
	if aliasUserID != "" {
		if _, err := s.execSQL(ctx, tx, "DELETE FROM control_plane_announcement_reads WHERE announcement_id = ? AND user_id = ?", announcementID, aliasUserID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *postgresEntStateStore) ListRuntimeOperations(ctx context.Context) ([]map[string]any, error) {
	return s.listSQLRecords(ctx, s.db, "control_plane_runtime_operations", runtimeOpEntFields, "ORDER BY created_at ASC, id ASC")
}

func (s *postgresEntStateStore) GetRuntimeOperation(ctx context.Context, id string) (map[string]any, bool, error) {
	return s.getSQLRecord(ctx, s.db, "control_plane_runtime_operations", id, runtimeOpEntFields, false)
}

func appendINPredicate(clauses *[]string, args *[]any, column string, values []string, negate bool) {
	if len(values) == 0 {
		return
	}
	marks := make([]string, len(values))
	for i, value := range values {
		marks[i] = "?"
		*args = append(*args, value)
	}
	op := "IN"
	if negate {
		op = "NOT IN"
	}
	*clauses = append(*clauses, fmt.Sprintf("%s %s (%s)", column, op, strings.Join(marks, ", ")))
}

func (s *postgresEntStateStore) PageRuntimeOperations(ctx context.Context, page runtimeOperationQuery) (tablePage, error) {
	var clauses []string
	var args []any
	if page.AccountID != "" {
		clauses = append(clauses, "account_id = ?")
		args = append(args, page.AccountID)
	}
	if page.WorkspaceID != "" {
		clauses = append(clauses, "workspace_id = ?")
		args = append(args, page.WorkspaceID)
	}
	if page.Action != "" {
		clauses = append(clauses, "action = ?")
		args = append(args, page.Action)
	}
	appendINPredicate(&clauses, &args, "status", page.Statuses, false)
	appendINPredicate(&clauses, &args, "status", page.ExcludedStatuses, true)
	if page.PeriodStart != "" {
		clauses = append(clauses, "period_start = ?")
		args = append(args, page.PeriodStart)
	}
	if !page.AfterCreatedAt.IsZero() {
		clauses = append(clauses, "(created_at > ? OR (created_at = ? AND id > ?))")
		args = append(args, page.AfterCreatedAt, page.AfterCreatedAt, page.AfterID)
	}
	whereSQL := ""
	if len(clauses) > 0 {
		whereSQL = " WHERE " + strings.Join(clauses, " AND ")
	}
	var total int
	if err := s.queryRow(ctx, s.db, "SELECT count(*) FROM control_plane_runtime_operations"+whereSQL, args...).Scan(&total); err != nil {
		return tablePage{}, err
	}
	pageArgs := append(append([]any(nil), args...), page.Limit, page.Offset)
	rows, err := s.listSQLRecords(ctx, s.db, "control_plane_runtime_operations", runtimeOpEntFields, strings.TrimPrefix(whereSQL+" ORDER BY created_at ASC, id ASC LIMIT ? OFFSET ?", " "), pageArgs...)
	if err != nil {
		return tablePage{}, err
	}
	return tablePage{Items: rows, Total: total}, nil
}

func (s *postgresEntStateStore) SaveRuntimeOperation(ctx context.Context, row map[string]any) error {
	return s.upsertSQLRecord(ctx, "control_plane_runtime_operations", row, runtimeOperationIdentityMatches, runtimeOpEntFields)
}

func (s *postgresEntStateStore) ReserveProductionE2EAttempt(ctx context.Context, claim productionE2EAttemptClaim) (map[string]any, error) {
	now := time.Now().UTC()
	_, err := s.execSQL(ctx, s.db, `
		INSERT INTO control_plane_production_e2e_records (id, created_at, updated_at, account_id, workspace_id, status, result, reason, url)
		VALUES (?, ?, ?, ?, ?, 'attempted', ?, ?, ?)`,
		claim.ID, now, now, claim.AccountID, claim.WorkspaceID, claim.Binding, recoveredWorkspaceE2EAttemptReason, claim.URL,
	)
	if isSQLConstraintError(err) {
		return nil, errProductionE2EAttemptAlreadyExists
	}
	if err != nil {
		return nil, err
	}
	record, found, err := s.GetProductionE2EAttempt(ctx, claim.ID)
	if err != nil || !found {
		return nil, err
	}
	return record, nil
}

func (s *postgresEntStateStore) GetProductionE2EAttempt(ctx context.Context, id string) (map[string]any, bool, error) {
	return s.getSQLRecord(ctx, s.db, "control_plane_production_e2e_records", id, productionE2EEntFields, false)
}

func (s *postgresEntStateStore) CompleteProductionE2EAttempt(ctx context.Context, id, binding string) (map[string]any, error) {
	res, err := s.execSQL(ctx, s.db, `
		UPDATE control_plane_production_e2e_records
		SET status = 'passed', updated_at = ?
		WHERE id = ? AND reason = ? AND result = ? AND status = 'attempted'`,
		time.Now().UTC(), id, recoveredWorkspaceE2EAttemptReason, binding,
	)
	if err != nil {
		return nil, err
	}
	updated, _ := res.RowsAffected()
	record, found, err := s.GetProductionE2EAttempt(ctx, id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errProductionE2EAttemptNotFound
	}
	if stringValue(record["reason"]) != recoveredWorkspaceE2EAttemptReason || stringValue(record["result"]) != binding {
		return nil, errProductionE2EAttemptBindingMismatch
	}
	if updated == 0 && stringValue(record["status"]) != "passed" {
		return nil, errProductionE2EAttemptBindingMismatch
	}
	return record, nil
}

func (s *postgresEntStateStore) BillingReconciliation(ctx context.Context) (map[string]any, bool, error) {
	rows, err := s.listSQLRecords(ctx, s.db, "control_plane_billing_reconciliation", reconcileEntFields, "ORDER BY created_at DESC, id DESC LIMIT 1")
	if err != nil {
		return nil, false, err
	}
	if len(rows) == 0 {
		return nil, false, nil
	}
	return rows[0], true, nil
}

func (s *postgresEntStateStore) ApplyRetention(ctx context.Context, policy retentionPolicy) (map[string]any, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	result := map[string]any{"retentionPolicy": policy.dto()}
	if cutoff := policy.cutoff(policy.AdminAuditDays); !cutoff.IsZero() {
		rows, err := s.listSQLRecords(ctx, tx, "control_plane_admin_audit_events", auditEntFields, "WHERE created_at < ?", cutoff)
		if err != nil {
			return nil, err
		}
		for _, record := range rows {
			if err := s.insertSQLRecord(ctx, tx, "control_plane_archived_admin_audit_events", stringValue(record["id"]), record, auditEntFields); err != nil {
				return nil, err
			}
		}
		if len(rows) > 0 {
			if _, err := s.execSQL(ctx, tx, "DELETE FROM control_plane_admin_audit_events WHERE created_at < ?", cutoff); err != nil {
				return nil, err
			}
		}
		result["adminAuditArchived"] = len(rows)
	}
	if cutoff := policy.cutoff(policy.ProductionE2EDays); !cutoff.IsZero() {
		res, err := s.execSQL(ctx, tx, "DELETE FROM control_plane_production_e2e_records WHERE created_at < ?", cutoff)
		if err != nil {
			return nil, err
		}
		deleted, _ := res.RowsAffected()
		result["productionE2EDeleted"] = int(deleted)
	}
	return result, tx.Commit()
}

func encodeMonthlyBillingState(row map[string]any) (string, error) {
	billing := map[string]any{}
	for _, key := range monthlyBillingStateKeys {
		if value, ok := row[key]; ok {
			billing[key] = value
		}
	}
	encoded, err := json.Marshal(billing)
	return string(encoded), err
}

func (s *postgresEntStateStore) upsertSQLRecord(ctx context.Context, table string, row map[string]any, identityMatches func(map[string]any, map[string]any) bool, fields []sqlRecordField) error {
	id := stringValue(row["id"])
	if id == "" {
		return errors.New("missing_record_id")
	}
	if existing, found, err := s.getSQLRecord(ctx, s.db, table, id, fields, false); err != nil {
		return err
	} else if found {
		if !identityMatches(existing, row) {
			return errIdempotencyConflict
		}
		return s.updateSQLRecord(ctx, s.db, table, id, row, fields, true)
	}
	err := s.insertSQLRecord(ctx, s.db, table, id, row, fields)
	if !isSQLConstraintError(err) {
		return err
	}
	// Another writer inserted the canonical ID between the read and create.
	existing, found, err := s.getSQLRecord(ctx, s.db, table, id, fields, false)
	if err != nil || !found || !identityMatches(existing, row) {
		return errIdempotencyConflict
	}
	return nil
}

func runtimeOperationIdentityMatches(existing, row map[string]any) bool {
	return existing != nil &&
		stringValue(existing["operationId"]) == stringValue(row["operationId"]) &&
		stringValue(existing["accountId"]) == stringValue(row["accountId"]) &&
		stringValue(existing["workspaceId"]) == stringValue(row["workspaceId"]) &&
		stringValue(existing["resourceId"]) == stringValue(row["resourceId"]) &&
		stringValue(existing["resourceKind"]) == stringValue(row["resourceKind"]) &&
		stringValue(existing["action"]) == stringValue(row["action"])
}

func (s *postgresEntStateStore) replaceRecord(ctx context.Context, table string, row map[string]any, fields []sqlRecordField) error {
	id := stringValue(row["id"])
	if id == "" {
		return errors.New("missing_record_id")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.deleteSQLByID(ctx, tx, table, id); err != nil {
		return err
	}
	if err := s.insertSQLRecord(ctx, tx, table, id, row, fields); err != nil {
		return err
	}
	return tx.Commit()
}

func filteredRecords(rows controlPlaneRecordSet, accountID string) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if accountID != "" && firstNonEmpty(stringValue(row["accountId"]), stringValue(row["ownerAccountId"])) != accountID {
			continue
		}
		out = append(out, cloneMap(row))
	}
	return out, nil
}

func filteredEvents(rows []controlPlaneRecord, accountID string) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if accountID != "" && firstNonEmpty(stringValue(row["accountId"]), stringValue(row["targetAccountId"]), stringValue(row["actorAccountId"])) != accountID {
			continue
		}
		out = append(out, cloneMap(row))
	}
	return out
}

func parseRecordTime(row controlPlaneRecord) (time.Time, bool) {
	text := stringValue(row["createdAt"])
	if text == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999999 -0700 MST"} {
		parsed, err := time.Parse(layout, text)
		if err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func valueAtPath(row controlPlaneRecord, path []string) (any, bool) {
	var current any = row
	for _, part := range path {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = asMap[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func setPath(row controlPlaneRecord, path []string, value any) {
	if len(path) == 0 {
		return
	}
	current := row
	for _, part := range path[:len(path)-1] {
		next, _ := current[part].(map[string]any)
		if next == nil {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	current[path[len(path)-1]] = value
}

func numberValue(value any) float64 {
	switch typed := value.(type) {
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case float64:
		return typed
	case float32:
		return float64(typed)
	default:
		parsed, _ := strconv.ParseFloat(stringValue(value), 64)
		return parsed
	}
}

func boolValue(value any) bool {
	if parsed, ok := value.(bool); ok {
		return parsed
	}
	if value == nil || reflect.ValueOf(value).IsZero() {
		return false
	}
	parsed, _ := strconv.ParseBool(stringValue(value))
	return parsed
}
