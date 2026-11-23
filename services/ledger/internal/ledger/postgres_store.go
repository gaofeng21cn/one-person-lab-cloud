package ledger

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/lib/pq"

	"opl-cloud/services/internal/postgresmigrate"
)

//go:embed ent_migrations/*.sql
var ledgerMigrations embed.FS

type embeddedMigration struct {
	version string
	query   string
}

type PostgresStore struct {
	db  *sql.DB
	now func() time.Time
}

func ledgerEmbeddedMigrations() ([]embeddedMigration, error) {
	entries, err := ledgerMigrations.ReadDir("ent_migrations")
	if err != nil {
		return nil, err
	}
	migrations := make([]embeddedMigration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		data, err := ledgerMigrations.ReadFile("ent_migrations/" + entry.Name())
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, embeddedMigration{
			version: strings.TrimSuffix(entry.Name(), ".sql"),
			query:   string(data),
		})
	}
	return migrations, nil
}

func PostgresSchemaSQL() string {
	migrations, err := ledgerEmbeddedMigrations()
	if err != nil {
		return ""
	}
	var out strings.Builder
	for _, migration := range migrations {
		out.WriteString(migration.query)
		out.WriteByte('\n')
	}
	return out.String()
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{
		db:  db,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (s *PostgresStore) Ready(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *PostgresStore) Install(ctx context.Context) error {
	embedded, err := ledgerEmbeddedMigrations()
	if err != nil {
		return err
	}
	migrations := make([]postgresmigrate.Migration, 0, len(embedded))
	for _, migration := range embedded {
		migration := migration
		migrations = append(migrations, postgresmigrate.Migration{
			Version: migration.version,
			Run: func(ctx context.Context) error {
				_, err := s.db.ExecContext(ctx, migration.query)
				return err
			},
		})
	}
	return postgresmigrate.Apply(ctx, s.db, "ledger", migrations)
}

func (s *PostgresStore) RecordReceipt(ctx context.Context, input ReceiptInput) (Receipt, error) {
	if err := validateReceiptInput(input); err != nil {
		return Receipt{}, err
	}
	return s.recordReceipt(ctx, input)
}

func (s *PostgresStore) recordReceipt(ctx context.Context, input ReceiptInput) (Receipt, error) {
	hashInput := input
	hashInput.IdempotencyKey = ""
	requestHash, err := hashJSON(hashInput)
	if err != nil {
		return Receipt{}, err
	}
	if existing, existingHash, err := s.receiptByIdempotencyKey(ctx, input.IdempotencyKey); err == nil {
		if existingHash != requestHash {
			return Receipt{}, ErrIdempotencyConflict
		}
		existing.Replayed = true
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, err
	}
	now := s.now()
	receipt := Receipt{ReceiptInput: hashInput, ReceiptID: postgresID("receipt", now), CreatedAt: now}
	payload, err := json.Marshal(receiptPayload{ReceiptInput: receipt.ReceiptInput, Retention: receipt.Retention})
	if err != nil {
		return Receipt{}, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO evidence_receipts
		(id, receipt_type, status, account_id, organization_id, workspace_id, project_id, task_id, job_id, artifact_id, review_id, payload_json, supersedes_receipt_id, idempotency_key, request_hash, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		receipt.ReceiptID, receipt.Type, receipt.Status, receipt.AccountID, receipt.OrganizationID,
		receipt.WorkspaceID, receipt.ProjectID, receipt.TaskID, receipt.JobID, receipt.ArtifactID,
		receipt.ReviewID, string(payload), receipt.SupersedesReceiptID, input.IdempotencyKey,
		requestHash, receipt.CreatedAt); err != nil {
		if isConstraintViolation(err) {
			if existing, existingHash, replayErr := s.receiptByIdempotencyKey(ctx, input.IdempotencyKey); replayErr == nil {
				if existingHash != requestHash {
					return Receipt{}, ErrIdempotencyConflict
				}
				existing.Replayed = true
				return existing, nil
			}
		}
		return Receipt{}, err
	}
	return receipt, nil
}

func (s *PostgresStore) RecordEvidenceIndex(ctx context.Context, input EvidenceIndexInput) (EvidenceIndexEntry, error) {
	if err := validateEvidenceIndexInput(input); err != nil {
		return EvidenceIndexEntry{}, err
	}
	hashInput := input
	hashInput.IdempotencyKey = ""
	requestHash, err := hashJSON(hashInput)
	if err != nil {
		return EvidenceIndexEntry{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return EvidenceIndexEntry{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "evidence-index:"+input.IdempotencyKey); err != nil {
		return EvidenceIndexEntry{}, err
	}
	if existing, existingHash, err := evidenceIndexByIdempotencyKeyTx(ctx, tx, input.IdempotencyKey); err == nil {
		if existingHash != requestHash {
			return EvidenceIndexEntry{}, ErrIdempotencyConflict
		}
		existing.Replayed = true
		if err := tx.Commit(); err != nil {
			return EvidenceIndexEntry{}, err
		}
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return EvidenceIndexEntry{}, err
	}
	now := s.now()
	entry := EvidenceIndexEntry{EvidenceIndexInput: hashInput, EvidenceID: postgresID("evidence", now), CreatedAt: now}
	_, err = tx.ExecContext(ctx, `INSERT INTO evidence_index_entries
		(id, operation_id, candidate_sha, candidate_tree, image_digest, receipt_id, receipt_type, status, actor, observed_at, identity_digest, redacted_link, idempotency_key, request_hash, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		entry.EvidenceID, entry.OperationID, entry.CandidateSHA, entry.CandidateTree, entry.ImageDigest,
		entry.ReceiptID, entry.ReceiptType, entry.Status, entry.Actor, entry.ObservedAt, entry.IdentityDigest,
		entry.RedactedLink, input.IdempotencyKey, requestHash, entry.CreatedAt)
	if err != nil {
		if isConstraintViolation(err) {
			if existing, existingHash, replayErr := evidenceIndexByIdempotencyKeyTx(ctx, tx, input.IdempotencyKey); replayErr == nil {
				if existingHash != requestHash {
					return EvidenceIndexEntry{}, ErrIdempotencyConflict
				}
				existing.Replayed = true
				if commitErr := tx.Commit(); commitErr != nil {
					return EvidenceIndexEntry{}, commitErr
				}
				return existing, nil
			}
		}
		return EvidenceIndexEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return EvidenceIndexEntry{}, err
	}
	return entry, nil
}

func (s *PostgresStore) ListEvidenceIndex(ctx context.Context, query EvidenceIndexQuery) (EvidenceIndexPage, error) {
	query, cursor, err := normalizeEvidenceIndexQuery(query)
	if err != nil {
		return EvidenceIndexPage{}, err
	}
	conditions := []string{"1 = 1"}
	args := make([]any, 0, 10)
	add := func(column, value string) {
		if value == "" {
			return
		}
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	add("operation_id", query.OperationID)
	add("candidate_sha", query.CandidateSHA)
	add("candidate_tree", query.CandidateTree)
	add("image_digest", query.ImageDigest)
	add("receipt_id", query.ReceiptID)
	add("receipt_type", query.ReceiptType)
	add("status", query.Status)
	if !cursor.ObservedAt.IsZero() {
		args = append(args, cursor.ObservedAt, cursor.EvidenceID)
		conditions = append(conditions, fmt.Sprintf("(observed_at, id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	args = append(args, query.Limit+1)
	queryText := fmt.Sprintf(`SELECT id, operation_id, candidate_sha, candidate_tree, image_digest, receipt_id, receipt_type, status, actor, observed_at, identity_digest, redacted_link, created_at
		FROM evidence_index_entries WHERE %s ORDER BY observed_at DESC, id DESC LIMIT $%d`, strings.Join(conditions, " AND "), len(args))
	rows, err := s.db.QueryContext(ctx, queryText, args...)
	if err != nil {
		return EvidenceIndexPage{}, err
	}
	defer rows.Close()
	entries := make([]EvidenceIndexEntry, 0, query.Limit+1)
	for rows.Next() {
		var entry EvidenceIndexEntry
		if err := rows.Scan(&entry.EvidenceID, &entry.OperationID, &entry.CandidateSHA, &entry.CandidateTree, &entry.ImageDigest, &entry.ReceiptID, &entry.ReceiptType, &entry.Status, &entry.Actor, &entry.ObservedAt, &entry.IdentityDigest, &entry.RedactedLink, &entry.CreatedAt); err != nil {
			return EvidenceIndexPage{}, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return EvidenceIndexPage{}, err
	}
	hasMore := len(entries) > query.Limit
	if hasMore {
		entries = entries[:query.Limit]
	}
	page := EvidenceIndexPage{Entries: entries, HasMore: hasMore}
	if hasMore {
		page.NextCursor = encodeEvidenceIndexCursor(entries[len(entries)-1])
	}
	return page, nil
}

func (s *PostgresStore) ExportEvidenceIndex(ctx context.Context, query EvidenceIndexQuery) (EvidenceIndexExport, error) {
	query.Cursor = ""
	query.Limit = MaxEvidenceIndexPageSize
	entries := make([]EvidenceIndexEntry, 0)
	for {
		page, err := s.ListEvidenceIndex(ctx, query)
		if err != nil {
			return EvidenceIndexExport{}, err
		}
		entries = append(entries, page.Entries...)
		if !page.HasMore {
			break
		}
		if len(entries) >= MaxEvidenceIndexExportSize {
			return EvidenceIndexExport{}, ErrEvidenceIndexExportTooLarge
		}
		query.Cursor = page.NextCursor
	}
	return EvidenceIndexExport{SchemaVersion: 1, Entries: entries}, nil
}

func (s *PostgresStore) ListReceipts(ctx context.Context, query ReceiptQuery) (ReceiptPage, error) {
	query, cursor, err := normalizeReceiptQuery(query)
	if err != nil {
		return ReceiptPage{}, err
	}
	// Typed coordination evidence carries private owner commitments and is read
	// only through the authenticated typed LedgerCoordination boundary, which
	// binds the read to that immutable commit. These kinds have no generic account
	// projection to compare a requested account against, so none is admitted
	// here.
	args := make([]any, 0, 16)
	args = append(args, LocalNoChargeReceiptType, WalletActionReceiptType, ProviderActionReceiptType)
	conditions := []string{"receipt_type NOT IN ($1, $2, $3)"}
	addEq := func(column, value string) {
		if value == "" {
			return
		}
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	addEq("account_id", query.AccountID)
	addEq("organization_id", query.OrganizationID)
	addEq("workspace_id", query.WorkspaceID)
	if query.RequestID != "" {
		args = append(args, query.RequestID)
		conditions = append(conditions, fmt.Sprintf("(payload_json::jsonb ->> 'requestId') = $%d", len(args)))
	}
	addEq("project_id", query.ProjectID)
	addEq("task_id", query.TaskID)
	addEq("job_id", query.JobID)
	var typeClause string
	if query.Type != "" {
		args = append(args, query.Type)
		typeClause = fmt.Sprintf("receipt_type = $%d", len(args))
	} else if query.TypePrefix != "" {
		args = append(args, query.TypePrefix)
		typeClause = fmt.Sprintf("starts_with(receipt_type, $%d)", len(args))
	}
	if query.IncludeType != "" {
		args = append(args, query.IncludeType)
		included := fmt.Sprintf("receipt_type = $%d", len(args))
		if query.IncludeExecutionKind != "" {
			args = append(args, query.IncludeExecutionKind)
			included = fmt.Sprintf("(%s AND (payload_json::jsonb -> 'execution' ->> 'kind') = $%d)", included, len(args))
		}
		if typeClause != "" {
			typeClause = fmt.Sprintf("(%s OR %s)", typeClause, included)
		} else {
			typeClause = included
		}
	}
	if typeClause != "" {
		conditions = append(conditions, typeClause)
	}
	addEq("status", query.Status)
	if !cursor.CreatedAt.IsZero() {
		args = append(args, cursor.CreatedAt, cursor.ReceiptID)
		conditions = append(conditions, fmt.Sprintf("(created_at < $%d OR (created_at = $%d AND id < $%d))", len(args)-1, len(args)-1, len(args)))
	}
	args = append(args, query.Limit+1)
	queryText := fmt.Sprintf(`SELECT id, receipt_type, status, account_id, organization_id, workspace_id, project_id, task_id, job_id, artifact_id, review_id, payload_json, supersedes_receipt_id, created_at
		FROM evidence_receipts WHERE %s ORDER BY created_at DESC, id DESC LIMIT $%d`, strings.Join(conditions, " AND "), len(args))
	sqlRows, err := s.db.QueryContext(ctx, queryText, args...)
	if err != nil {
		return ReceiptPage{}, err
	}
	defer sqlRows.Close()
	rows := make([]evidenceReceiptRow, 0, query.Limit+1)
	for sqlRows.Next() {
		var row evidenceReceiptRow
		if err := sqlRows.Scan(&row.ID, &row.ReceiptType, &row.Status, &row.AccountID, &row.OrganizationID, &row.WorkspaceID, &row.ProjectID, &row.TaskID, &row.JobID, &row.ArtifactID, &row.ReviewID, &row.PayloadJSON, &row.SupersedesReceiptID, &row.CreatedAt); err != nil {
			return ReceiptPage{}, err
		}
		rows = append(rows, row)
	}
	if err := sqlRows.Err(); err != nil {
		return ReceiptPage{}, err
	}
	hasMore := len(rows) > query.Limit
	if hasMore {
		rows = rows[:query.Limit]
	}
	receipts := make([]Receipt, 0, len(rows))
	for _, row := range rows {
		receipt, err := receiptFromRow(row)
		if err != nil {
			return ReceiptPage{}, err
		}
		receipts = append(receipts, receipt)
	}
	page := ReceiptPage{Receipts: receipts, HasMore: hasMore, Lookup: receiptLookupScope(query)}
	if hasMore {
		page.NextCursor = encodeReceiptCursor(receipts[len(receipts)-1])
	}
	return page, nil
}

func (s *PostgresStore) Receipt(ctx context.Context, receiptID string) (Receipt, error) {
	receipt, err := s.receipt(ctx, receiptID)
	if err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func (s *PostgresStore) UpdateReceiptRetention(ctx context.Context, input ReceiptRetentionInput) (ReceiptRetentionResult, error) {
	if input.ReceiptID == "" || input.IdempotencyKey == "" || (input.RetainUntil.IsZero() && !input.LegalHold) {
		return ReceiptRetentionResult{}, ErrInvalidReceiptRetentionInput
	}
	requestHash, err := hashJSON(struct {
		ReceiptID   string    `json:"receiptId"`
		RetainUntil time.Time `json:"retainUntil"`
		LegalHold   bool      `json:"legalHold"`
	}{input.ReceiptID, input.RetainUntil, input.LegalHold})
	if err != nil {
		return ReceiptRetentionResult{}, err
	}
	return s.mutateReceipt(ctx, "receipt-retention", input.IdempotencyKey, requestHash, input.ReceiptID, func(receipt *Receipt) error {
		if !input.RetainUntil.IsZero() && !receipt.Retention.RetainUntil.IsZero() && input.RetainUntil.Before(receipt.Retention.RetainUntil) {
			return ErrReceiptRetentionShortening
		}
		if input.RetainUntil.After(receipt.Retention.RetainUntil) {
			receipt.Retention.RetainUntil = input.RetainUntil
		}
		receipt.Retention.LegalHold = receipt.Retention.LegalHold || input.LegalHold
		return nil
	})
}

func (s *PostgresStore) PrivacyDeleteReceipt(ctx context.Context, input ReceiptPrivacyDeleteInput) (ReceiptRetentionResult, error) {
	if input.ReceiptID == "" || input.Reason == "" || input.IdempotencyKey == "" {
		return ReceiptRetentionResult{}, ErrInvalidReceiptRetentionInput
	}
	requestHash, err := hashJSON(struct {
		ReceiptID string `json:"receiptId"`
		Reason    string `json:"reason"`
	}{input.ReceiptID, input.Reason})
	if err != nil {
		return ReceiptRetentionResult{}, err
	}
	return s.mutateReceipt(ctx, "receipt-privacy", input.IdempotencyKey, requestHash, input.ReceiptID, func(receipt *Receipt) error {
		if receipt.Retention.LegalHold {
			return ErrReceiptLegalHold
		}
		if receipt.Retention.RetainUntil.After(s.now()) {
			return ErrReceiptRetentionActive
		}
		if receipt.Retention.PrivacyRedaction == nil {
			receipt.Actor = nil
			receipt.Owner = nil
			receipt.Continuation = nil
			receipt.Retention.PrivacyRedaction = &PrivacyRedactionEvidence{AppliedAt: s.now(), Reason: input.Reason, Eligible: true}
		}
		return nil
	})
}

func (s *PostgresStore) mutateReceipt(ctx context.Context, service, idempotencyKey, requestHash, receiptID string, mutate func(*Receipt) error) (ReceiptRetentionResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReceiptRetentionResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	lockKey := service + ":" + idempotencyKey
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", lockKey); err != nil {
		return ReceiptRetentionResult{}, err
	}
	idempotencyID := evidenceID("idempotency", lockKey)
	var existingHash, responseJSON string
	err = tx.QueryRowContext(ctx, "SELECT request_hash, response_ref FROM idempotency_keys WHERE id = $1", idempotencyID).Scan(&existingHash, &responseJSON)
	if err == nil {
		if existingHash != requestHash {
			return ReceiptRetentionResult{}, ErrIdempotencyConflict
		}
		var result ReceiptRetentionResult
		if err := json.Unmarshal([]byte(responseJSON), &result); err != nil {
			return ReceiptRetentionResult{}, err
		}
		result.Replayed = true
		return result, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ReceiptRetentionResult{}, err
	}
	var payloadJSON string
	var createdAt time.Time
	if err := tx.QueryRowContext(ctx, "SELECT payload_json, created_at FROM evidence_receipts WHERE id = $1 AND receipt_type <> $2 FOR UPDATE /* ledger_receipt_mutation */", receiptID, LocalNoChargeReceiptType).Scan(&payloadJSON, &createdAt); errors.Is(err, sql.ErrNoRows) {
		return ReceiptRetentionResult{}, ErrReceiptNotFound
	} else if err != nil {
		return ReceiptRetentionResult{}, err
	}
	var stored receiptPayload
	if err := decodeStoredJSON(payloadJSON, &stored); err != nil {
		return ReceiptRetentionResult{}, err
	}
	receipt := Receipt{ReceiptInput: stored.ReceiptInput, ReceiptID: receiptID, CreatedAt: createdAt, Retention: stored.Retention}
	if err := mutate(&receipt); err != nil {
		return ReceiptRetentionResult{}, err
	}
	payload, err := json.Marshal(receiptPayload{ReceiptInput: receipt.ReceiptInput, Retention: receipt.Retention})
	if err != nil {
		return ReceiptRetentionResult{}, err
	}
	result := ReceiptRetentionResult{ReceiptID: receipt.ReceiptID, Retention: receipt.Retention}
	response, err := json.Marshal(result)
	if err != nil {
		return ReceiptRetentionResult{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE evidence_receipts SET payload_json = $1 WHERE id = $2", string(payload), receiptID); err != nil {
		return ReceiptRetentionResult{}, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO idempotency_keys (id, service, idempotency_key, request_hash, response_ref, created_at) VALUES ($1, $2, $3, $4, $5, $6)", idempotencyID, service, idempotencyKey, requestHash, string(response), s.now()); err != nil {
		return ReceiptRetentionResult{}, err
	}
	return result, tx.Commit()
}

func (s *PostgresStore) receipt(ctx context.Context, receiptID string) (Receipt, error) {
	var row evidenceReceiptRow
	err := s.db.QueryRowContext(ctx, `SELECT id, receipt_type, status, account_id, organization_id, workspace_id, project_id, task_id, job_id, artifact_id, review_id, payload_json, supersedes_receipt_id, created_at
		FROM evidence_receipts WHERE id = $1`, receiptID).Scan(
		&row.ID, &row.ReceiptType, &row.Status, &row.AccountID, &row.OrganizationID, &row.WorkspaceID,
		&row.ProjectID, &row.TaskID, &row.JobID, &row.ArtifactID, &row.ReviewID,
		&row.PayloadJSON, &row.SupersedesReceiptID, &row.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, ErrReceiptNotFound
	}
	if err != nil {
		return Receipt{}, err
	}
	// The generic reader cannot admit typed coordination evidence: the caller-facing
	// admission it feeds is an account and Workspace comparison against this row,
	// and these typed kinds store no account. A row with no stored
	// account would leave the caller's requested account unbound, so the private
	// owner commit behind it would be served to any valid capability naming any
	// account. The generic mutation path is deliberately not fenced the same way:
	// retention and the data-subject privacy obligation must still reach the row.
	if row.ReceiptType == LocalNoChargeReceiptType || row.ReceiptType == WalletActionReceiptType || row.ReceiptType == ProviderActionReceiptType {
		return Receipt{}, ErrReceiptNotFound
	}
	return receiptFromRow(row)
}

func (s *PostgresStore) RecordReconciliation(ctx context.Context, input ReconciliationInput) (ReconciliationResult, error) {
	if err := validateReconciliationInput(input); err != nil {
		return ReconciliationResult{}, err
	}
	requestHash, err := hashJSON(input.Report)
	if err != nil {
		return ReconciliationResult{}, err
	}
	if existing, existingHash, err := s.reconciliationByIdempotencyKey(ctx, input.IdempotencyKey); err == nil {
		if existingHash != requestHash {
			return ReconciliationResult{}, ErrIdempotencyConflict
		}
		existing.Replayed = true
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ReconciliationResult{}, err
	}
	id := stringFromAny(input.Report["id"])
	status := stringFromAny(input.Report["status"])
	reportJSON, err := json.Marshal(input.Report)
	if err != nil {
		return ReconciliationResult{}, err
	}
	now := s.now()
	result := ReconciliationResult{ID: id, Status: status, Report: input.Report, BlockNewWorkspaces: status == "mismatch", Reason: "operator_reconciliation", CreatedAt: now}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO reconciliation_reports
		(id, status, report_json, block_new_workspaces, reason, idempotency_key, request_hash, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		result.ID, result.Status, string(reportJSON), result.BlockNewWorkspaces, result.Reason,
		input.IdempotencyKey, requestHash, result.CreatedAt); err != nil {
		if isConstraintViolation(err) {
			if existing, existingHash, replayErr := s.reconciliationByIdempotencyKey(ctx, input.IdempotencyKey); replayErr == nil {
				if existingHash != requestHash {
					return ReconciliationResult{}, ErrIdempotencyConflict
				}
				existing.Replayed = true
				return existing, nil
			}
		}
		return ReconciliationResult{}, err
	}
	return result, nil
}

type evidenceReceiptRow struct {
	ID                  string
	ReceiptType         string
	Status              string
	AccountID           string
	OrganizationID      string
	WorkspaceID         string
	ProjectID           string
	TaskID              string
	JobID               string
	ArtifactID          string
	ReviewID            string
	PayloadJSON         string
	SupersedesReceiptID string
	RequestHash         string
	CreatedAt           time.Time
}

type reconciliationReportRow struct {
	ID                 string
	Status             string
	ReportJSON         string
	BlockNewWorkspaces bool
	Reason             string
	RequestHash        string
	CreatedAt          time.Time
}

func (s *PostgresStore) receiptByIdempotencyKey(ctx context.Context, key string) (Receipt, string, error) {
	var row evidenceReceiptRow
	err := s.db.QueryRowContext(ctx, `SELECT id, receipt_type, status, account_id, organization_id, workspace_id, project_id, task_id, job_id, artifact_id, review_id, payload_json, supersedes_receipt_id, request_hash, created_at
		FROM evidence_receipts WHERE idempotency_key = $1`, key).Scan(
		&row.ID, &row.ReceiptType, &row.Status, &row.AccountID, &row.OrganizationID, &row.WorkspaceID,
		&row.ProjectID, &row.TaskID, &row.JobID, &row.ArtifactID, &row.ReviewID,
		&row.PayloadJSON, &row.SupersedesReceiptID, &row.RequestHash, &row.CreatedAt)
	if err != nil {
		return Receipt{}, "", err
	}
	receipt, err := receiptFromRow(row)
	return receipt, row.RequestHash, err
}

func evidenceIndexByIdempotencyKeyTx(ctx context.Context, tx *sql.Tx, key string) (EvidenceIndexEntry, string, error) {
	var entry EvidenceIndexEntry
	var requestHash string
	err := tx.QueryRowContext(ctx, `SELECT id, operation_id, candidate_sha, candidate_tree, image_digest, receipt_id, receipt_type, status, actor, observed_at, identity_digest, redacted_link, request_hash, created_at
		FROM evidence_index_entries WHERE idempotency_key = $1`, key).Scan(
		&entry.EvidenceID, &entry.OperationID, &entry.CandidateSHA, &entry.CandidateTree, &entry.ImageDigest,
		&entry.ReceiptID, &entry.ReceiptType, &entry.Status, &entry.Actor, &entry.ObservedAt,
		&entry.IdentityDigest, &entry.RedactedLink, &requestHash, &entry.CreatedAt)
	return entry, requestHash, err
}

func receiptFromRow(row evidenceReceiptRow) (Receipt, error) {
	// The payload remains canonical for flexible evidence; indexed identity columns override its projections.
	var stored receiptPayload
	if err := decodeStoredJSON(row.PayloadJSON, &stored); err != nil {
		return Receipt{}, err
	}
	input := stored.ReceiptInput
	input.Type = row.ReceiptType
	input.Status = row.Status
	input.AccountID = row.AccountID
	input.OrganizationID = row.OrganizationID
	input.WorkspaceID = row.WorkspaceID
	input.ProjectID = row.ProjectID
	input.TaskID = row.TaskID
	input.JobID = row.JobID
	input.ArtifactID = row.ArtifactID
	input.ReviewID = row.ReviewID
	input.SupersedesReceiptID = row.SupersedesReceiptID
	return Receipt{ReceiptInput: input, ReceiptID: row.ID, CreatedAt: row.CreatedAt, Retention: stored.Retention}, nil
}

type receiptPayload struct {
	ReceiptInput
	Retention ReceiptRetention `json:"retention"`
}

func (s *PostgresStore) reconciliationByIdempotencyKey(ctx context.Context, key string) (ReconciliationResult, string, error) {
	var row reconciliationReportRow
	err := s.db.QueryRowContext(ctx, `SELECT id, status, report_json, block_new_workspaces, reason, request_hash, created_at
		FROM reconciliation_reports WHERE idempotency_key = $1`, key).Scan(
		&row.ID, &row.Status, &row.ReportJSON, &row.BlockNewWorkspaces, &row.Reason, &row.RequestHash, &row.CreatedAt)
	if err != nil {
		return ReconciliationResult{}, "", err
	}
	result, err := reconciliationFromRow(row)
	return result, row.RequestHash, err
}

func reconciliationFromRow(row reconciliationReportRow) (ReconciliationResult, error) {
	result := ReconciliationResult{ID: row.ID, Status: row.Status, BlockNewWorkspaces: row.BlockNewWorkspaces, Reason: row.Reason, CreatedAt: row.CreatedAt}
	if err := decodeStoredJSON(row.ReportJSON, &result.Report); err != nil || validateReconciliationResult(result) != nil {
		return ReconciliationResult{}, ErrInvalidReconciliationInput
	}
	return result, nil
}

func isConstraintViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code.Class() == "23"
}

func decodeStoredJSON(payload string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("stored JSON contains multiple values")
	}
	return nil
}

func postgresID(prefix string, t time.Time) string {
	return fmt.Sprintf("%s_%d", prefix, t.UnixNano())
}

var _ = errors.Is
