package identity

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

// GatewayStore is the owner-local persistence for the `gateway` schema. It is a
// second data owner inside the Gateway Integration deployment unit: it never
// touches the tenant schema and the tenant store never touches this one.
type GatewayStore struct {
	db    *sql.DB
	store *ownerstore.Store
}

// ErrGatewayWalletUnknown reports an unknown wallet operation in this owner.
var ErrGatewayWalletUnknown = errors.New("gateway wallet operation not found")

// ErrGatewayWalletConflict reports a stored wallet operation that differs from
// the submitted command identity.
var ErrGatewayWalletConflict = errors.New("gateway wallet operation conflicts with stored evidence")

// NewGatewayStore opens the gateway owner store over an already-migrated
// opl_gateway connection.
func NewGatewayStore(db *sql.DB) (*GatewayStore, error) {
	if db == nil {
		return nil, errors.New("gateway database connection required")
	}
	store, err := ownerstore.New(db, "gateway")
	if err != nil {
		return nil, err
	}
	return &GatewayStore{db: db, store: store}, nil
}

// Store exposes the policy-free ownerstore handle for owner Operations and
// command idempotency.
func (s *GatewayStore) Store() *ownerstore.Store { return s.store }

// DB exposes the owner connection for transactional call sites.
func (s *GatewayStore) DB() *sql.DB { return s.db }

// Ready reports whether the gateway database answers.
func (s *GatewayStore) Ready(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("gateway database is not open")
	}
	return s.db.PingContext(ctx)
}

// WalletBinding is one active tenant-wallet binding.
type WalletBinding struct {
	ID                   string
	TenantID             string
	BillingSub2APIUserID string
	DelegationRef        string
	VerificationRef      string
	BoundAt              time.Time
	Version              int64
}

// ErrWalletBindingAbsent reports that the Tenant has no active wallet binding.
var ErrWalletBindingAbsent = errors.New("tenant has no active wallet binding")

// ErrWalletBindingVersionConflict reports a stale expected binding version.
var ErrWalletBindingVersionConflict = errors.New("wallet binding version conflict")

// ReadActiveWalletBinding returns the one active binding for a tenant. An absent
// binding is reported, never synthesized.
func (s *GatewayStore) ReadActiveWalletBinding(ctx context.Context, tenantID string) (WalletBinding, error) {
	var binding WalletBinding
	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, billing_sub2api_user_id, delegation_ref, verification_evidence_ref, bound_at
		FROM gateway.tenant_wallet_bindings
		WHERE tenant_id = $1 AND revoked_at IS NULL`, tenantID).
		Scan(&binding.ID, &binding.TenantID, &binding.BillingSub2APIUserID, &binding.DelegationRef, &binding.VerificationRef, &binding.BoundAt)
	if errors.Is(err, sql.ErrNoRows) {
		return WalletBinding{}, ErrWalletBindingAbsent
	}
	if err != nil {
		return WalletBinding{}, err
	}
	version, err := s.bindingVersion(ctx, tenantID)
	if err != nil {
		return WalletBinding{}, err
	}
	binding.Version = version
	return binding, nil
}

func (s *GatewayStore) bindingVersion(ctx context.Context, tenantID string) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM gateway.tenant_wallet_bindings WHERE tenant_id = $1`, tenantID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// BindWalletBinding atomically revokes the previous active binding and records the
// new one at the next monotonically increasing version. The expected version is a
// compare-and-set: a stale caller never overwrites a newer binding.
func (s *GatewayStore) BindWalletBinding(ctx context.Context, binding WalletBinding, expectedVersion int64) (WalletBinding, error) {
	binding = WalletBinding{ID: binding.ID, TenantID: binding.TenantID, BillingSub2APIUserID: binding.BillingSub2APIUserID, DelegationRef: binding.DelegationRef, VerificationRef: binding.VerificationRef}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WalletBinding{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "gateway.wallet_binding:"+binding.TenantID); err != nil {
		return WalletBinding{}, err
	}
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM gateway.tenant_wallet_bindings WHERE tenant_id = $1`, binding.TenantID).Scan(&current); err != nil {
		return WalletBinding{}, err
	}
	if current != expectedVersion {
		return WalletBinding{}, ErrWalletBindingVersionConflict
	}
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, `UPDATE gateway.tenant_wallet_bindings SET revoked_at = $2, updated_at = $2 WHERE tenant_id = $1 AND revoked_at IS NULL`, binding.TenantID, now); err != nil {
		return WalletBinding{}, err
	}
	binding.BoundAt = now
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO gateway.tenant_wallet_bindings (id, tenant_id, billing_sub2api_user_id, delegation_ref, verification_evidence_ref, bound_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $6, $6)`,
		binding.ID, binding.TenantID, binding.BillingSub2APIUserID, binding.DelegationRef, binding.VerificationRef, binding.BoundAt); err != nil {
		return WalletBinding{}, err
	}
	if err = tx.Commit(); err != nil {
		return WalletBinding{}, err
	}
	binding.Version = current + 1
	return binding, nil
}

// WalletRecord is one owner-local wallet operation.
type WalletRecord struct {
	ID                   string
	TenantID             string
	WorkspaceID          string
	WalletBindingID      string
	Kind                 string
	Status               string
	AmountUSDMicros      int64
	OriginalOperationID  string
	BusinessKey          string
	RequestFingerprint   string
	ExternalReference    string
	ReceiptID            string
	RefundEntitlementRef string
	ErrorCode            string
	Purpose              string
	ConfirmedAt          time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// ReadWalletOperation reads one wallet operation by its own id.
func (s *GatewayStore) ReadWalletOperation(ctx context.Context, id string) (WalletRecord, error) {
	return s.scanWallet(ctx, s.db.QueryRowContext(ctx, walletSelect+` WHERE id = $1`, id))
}

// ReadWalletOperationByBusinessKey reads one wallet operation by its immutable
// business idempotency key (the Workspace order step key).
func (s *GatewayStore) ReadWalletOperationByBusinessKey(ctx context.Context, key string) (WalletRecord, error) {
	return s.scanWallet(ctx, s.db.QueryRowContext(ctx, walletSelect+` WHERE business_idempotency_key = $1`, key))
}

const walletSelect = `SELECT id, tenant_id, COALESCE(workspace_id,''), wallet_binding_id, kind, status, amount_usd_micros,
	COALESCE(original_wallet_operation_id,''), business_idempotency_key, request_fingerprint,
	COALESCE(external_reference,''), COALESCE(receipt_id,''), COALESCE(refund_entitlement_ref,''), COALESCE(error_code,''),
	COALESCE(purpose,''), confirmed_at, created_at, updated_at
	FROM gateway.wallet_operations`

type rowScanner interface{ Scan(...any) error }

func (s *GatewayStore) scanWallet(ctx context.Context, row rowScanner) (WalletRecord, error) {
	var record WalletRecord
	var confirmed sql.NullTime
	err := row.Scan(&record.ID, &record.TenantID, &record.WorkspaceID, &record.WalletBindingID, &record.Kind, &record.Status, &record.AmountUSDMicros,
		&record.OriginalOperationID, &record.BusinessKey, &record.RequestFingerprint, &record.ExternalReference, &record.ReceiptID,
		&record.RefundEntitlementRef, &record.ErrorCode, &record.Purpose, &confirmed, &record.CreatedAt, &record.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return WalletRecord{}, ErrGatewayWalletUnknown
	}
	if err != nil {
		return WalletRecord{}, err
	}
	if confirmed.Valid {
		record.ConfirmedAt = confirmed.Time
	}
	return record, nil
}

// ReserveWalletOperation inserts the requested wallet operation and returns the
// existing row when the same business key already exists, so a replayed command
// resolves to the one original operation instead of a second one.
func (s *GatewayStore) ReserveWalletOperation(ctx context.Context, record WalletRecord) (WalletRecord, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WalletRecord{}, false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "gateway.wallet_operation:"+record.BusinessKey); err != nil {
		return WalletRecord{}, false, err
	}
	existing, err := s.scanWallet(ctx, tx.QueryRowContext(ctx, walletSelect+` WHERE business_idempotency_key = $1`, record.BusinessKey))
	switch {
	case err == nil:
		if existing.RequestFingerprint != record.RequestFingerprint {
			return WalletRecord{}, false, ErrGatewayWalletConflict
		}
		return existing, true, nil
	case !errors.Is(err, ErrGatewayWalletUnknown):
		return WalletRecord{}, false, err
	}
	now := time.Now().UTC()
	record.Status = "requested"
	record.CreatedAt, record.UpdatedAt = now, now
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO gateway.wallet_operations (id, tenant_id, wallet_binding_id, workspace_id, kind, status, amount_usd_micros,
			original_wallet_operation_id, business_idempotency_key, request_fingerprint, refund_entitlement_ref, purpose, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,'requested',$6,$7,$8,$9,$10,$11,$12,$12)`,
		record.ID, record.TenantID, record.WalletBindingID, nullString(record.WorkspaceID), record.Kind, record.AmountUSDMicros,
		nullString(record.OriginalOperationID), record.BusinessKey, record.RequestFingerprint, nullString(record.RefundEntitlementRef), nullString(record.Purpose), now); err != nil {
		return WalletRecord{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return WalletRecord{}, false, err
	}
	return record, false, nil
}

// SettleWalletOperation records the terminal answer for a dispatched operation.
// A confirmed operation must name its external reference and confirmation instant,
// so a caller can never read "confirmed" without that evidence.
func (s *GatewayStore) SettleWalletOperation(ctx context.Context, id, status, externalReference, receiptID, errorCode string) (WalletRecord, error) {
	now := time.Now().UTC()
	var confirmedAt any
	if status == "confirmed" {
		if strings.TrimSpace(externalReference) == "" {
			return WalletRecord{}, errors.New("a confirmed wallet operation requires an external reference")
		}
		confirmedAt = now
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE gateway.wallet_operations
		SET status = $2, external_reference = $3, receipt_id = $4, error_code = $5, confirmed_at = $6, updated_at = $7
		WHERE id = $1`, id, status, nullString(externalReference), nullString(receiptID), nullString(errorCode), confirmedAt, now)
	if err != nil {
		return WalletRecord{}, err
	}
	return s.ReadWalletOperation(ctx, id)
}

func nullString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

// WalletOperationMessage converts the stored operation to its typed contract form.
func WalletOperationMessage(record WalletRecord) *api.WalletOperation {
	out := &api.WalletOperation{
		Id:              record.ID,
		Kind:            walletKindEnum(record.Kind),
		AmountUsdMicros: record.AmountUSDMicros,
		Status:          walletStatusEnum(record.Status),
		CreatedAt:       stampOf(record.CreatedAt),
		UpdatedAt:       stampOf(record.UpdatedAt),
	}
	if record.WorkspaceID != "" {
		out.WorkspaceId = &record.WorkspaceID
	}
	if record.ExternalReference != "" {
		out.ExternalReference = &record.ExternalReference
	}
	if record.ReceiptID != "" {
		out.ReceiptId = &record.ReceiptID
	}
	if record.ErrorCode != "" {
		if code, ok := api.ErrorCodeEnum_value["ERROR_CODE_ENUM_"+record.ErrorCode]; ok {
			out.ErrorCode = api.ErrorCodeEnum(code).Enum()
		}
	}
	if record.OriginalOperationID != "" {
		out.OriginalChargeOperationId = &record.OriginalOperationID
	}
	if record.Purpose != "" {
		if purpose, ok := api.WalletOperationPurposeEnum_value["WALLET_OPERATION_PURPOSE_ENUM_"+strings.ToUpper(record.Purpose)]; ok {
			out.Purpose = api.WalletOperationPurposeEnum(purpose).Enum()
		}
	}
	return out
}

func walletKindEnum(kind string) api.WalletOperationKindEnum {
	switch kind {
	case "charge":
		return api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_CHARGE
	case "refund":
		return api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_REFUND
	case "recharge":
		return api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_RECHARGE
	default:
		return api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_UNSPECIFIED
	}
}

func walletStatusEnum(status string) api.WalletOperationStatusEnum {
	switch status {
	case "requested":
		return api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_REQUESTED
	case "confirmed":
		return api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED
	case "rejected":
		return api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_REJECTED
	default:
		return api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_UNKNOWN
	}
}
