package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"

	api "opl-cloud/packages/contracts/go/api"
)

// managedKeyTTL bounds a Workspace-managed Gateway key's validity. The contract
// fixes ManagedKeyBinding.expiresAt but names no window, so the owner records an
// explicit positive lifetime rather than an open-ended credential.
const managedKeyTTL = 30 * 24 * time.Hour

// ErrSecretStoreUnavailable reports that no approved Secret store is wired, so a
// managed key cannot be delivered. The raw key is never persisted as a fallback.
var ErrSecretStoreUnavailable = errors.New("approved Secret store is not configured")

// SecretDelivery is one written Secret handle. The Value is the raw credential;
// it is returned only here and never persisted in a Gateway table.
type SecretDelivery struct {
	Reference   string
	TenantID    string
	WorkspaceID string
	Purpose     string
	Fingerprint string
	// Raw is the credential value. A SecretStore reads it to write the approved
	// store; it is never returned to a caller or persisted in a Gateway table.
	Raw string
	// ExternalKeyID is the external key identity the issuer produced for this
	// delivery. An approved SecretStore maps it to the provider-side key id the
	// stored Secret is bound to; it is an opaque identity, never the raw key.
	ExternalKeyID string
	// Call is the derived owner-to-owner continuation context of the approved
	// store write. It preserves the original command's tenant scope, actor,
	// session or accepted-obligation grant and request identity, clears the
	// caller's authorization context, and carries the bounded idempotency key a
	// retry of this write must repeat. The store presents it to its own owner, so
	// the store authorizes through its own audience rather than the caller's.
	Call *api.CallContext
}

// SecretStore writes a raw credential into the deployment's approved Secret store
// and returns only an opaque delivery reference. Every implementation must keep
// the raw value out of normal databases, logs, Outbox and receipts.
type SecretStore interface {
	PutSecret(ctx context.Context, delivery SecretDelivery) (SecretDelivery, error)
}

// ManagedKeyBinding is one recorded Workspace-managed key. It stores only the
// opaque delivery reference and fingerprint, never the raw key.
type ManagedKeyBinding struct {
	ID                      string
	TenantID                string
	WorkspaceID             string
	ActorID                 string
	ExternalKeyID           string
	Fingerprint             string
	SecretRef               string
	Purpose                 string
	ModelIDs                []string
	ObservationResult       string
	TargetRuntimeInstanceID string
	TTL                     time.Duration
	ExpiresAt               time.Time
	RevokedAt               time.Time
	CreatedAt               time.Time
}

// There is exactly one writer that records a confirmed managed key - the
// transactional InsertConfirmedManagedKeyBinding below - so a binding can never
// become durable without its original command answer. A standalone binding insert
// is deliberately not offered.
// execer is the subset of *sql.DB and *sql.Tx one owner write needs, so a
// binding can be recorded alone or together with its original command answer in
// one transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (s *GatewayStore) insertManagedKeyBinding(ctx context.Context, exec execer, binding ManagedKeyBinding) (ManagedKeyBinding, error) {
	if strings.TrimSpace(binding.ExternalKeyID) == "" || strings.TrimSpace(binding.SecretRef) == "" || strings.TrimSpace(binding.Fingerprint) == "" {
		return ManagedKeyBinding{}, errors.New("external key id, secret reference and fingerprint are required")
	}
	if binding.Purpose != "workspace_managed" || strings.TrimSpace(binding.WorkspaceID) == "" {
		return ManagedKeyBinding{}, errors.New("a workspace-managed key requires a workspace")
	}
	now := time.Now().UTC()
	binding.ID = "gateway-key-" + shortDigest(binding.TenantID+":"+binding.WorkspaceID+":"+binding.ExternalKeyID)
	binding.ExpiresAt = now.Add(binding.TTL)
	if binding.TTL <= 0 {
		binding.ExpiresAt = time.Time{}
	}
	binding.ObservationResult = "confirmed"
	if _, err := exec.ExecContext(ctx, `
		INSERT INTO gateway.key_bindings (id, tenant_id, workspace_id, actor_id, external_key_id, fingerprint, secret_ref, purpose,
			model_ids, observation_result, name, expires_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)`,
		binding.ID, binding.TenantID, binding.WorkspaceID, nullString(binding.ActorID), binding.ExternalKeyID, binding.Fingerprint,
		binding.SecretRef, binding.Purpose, pq.Array(binding.ModelIDs), binding.ObservationResult, binding.ID, nullTime(binding.ExpiresAt), now); err != nil {
		return ManagedKeyBinding{}, err
	}
	binding.CreatedAt = now
	return binding, nil
}

// InsertConfirmedManagedKeyBinding records the confirmed key binding and the
// original command's confirmed answer in one transaction. The recorded effect
// and the command identity therefore commit together: a crash can never leave a
// binding whose original command is still pending, or a pending command that
// already minted a key.
func (s *GatewayStore) InsertConfirmedManagedKeyBinding(ctx context.Context, binding ManagedKeyBinding, reservation ManagedKeyCommandReservation, answer []byte) (ManagedKeyBinding, error) {
	if len(answer) == 0 || !json.Valid(answer) {
		return ManagedKeyBinding{}, errors.New("a confirmed managed key command answer must be a JSON document")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ManagedKeyBinding{}, err
	}
	defer tx.Rollback()
	recorded, err := s.insertManagedKeyBinding(ctx, tx, binding)
	if err != nil {
		return ManagedKeyBinding{}, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE gateway.idempotency_records
		SET response_status = $5, response_body = $6, resource_id = $7
		WHERE tenant_scope = $1 AND actor_scope = $2 AND operation_name = $3 AND idempotency_key = $4
		  AND request_sha256 = $8 AND response_status = $9`,
		reservation.TenantID, reservation.ActorID, managedKeyCommandOperation, reservation.IdempotencyKey,
		managedKeyCommandConfirmedStatus, answer, recorded.ID, reservation.CommandFingerprint, managedKeyCommandPendingStatus)
	if err != nil {
		return ManagedKeyBinding{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ManagedKeyBinding{}, err
	}
	if rows != 1 {
		return ManagedKeyBinding{}, errors.New("managed key command is not pending in this owner")
	}
	if err = tx.Commit(); err != nil {
		return ManagedKeyBinding{}, err
	}
	return recorded, nil
}

// AdvanceManagedKeyCommandPending records the opaque external identity an
// issuance actually produced while the command is still unresolved, so a lost
// Secret delivery is read back as the one original key instead of a second
// issuance. It never overwrites an already-recorded identity or answer.
func (s *GatewayStore) AdvanceManagedKeyCommandPending(ctx context.Context, reservation ManagedKeyCommandReservation, externalKeyID, fingerprint string) error {
	if strings.TrimSpace(externalKeyID) == "" || strings.TrimSpace(fingerprint) == "" {
		return errors.New("an observed managed key effect requires its external key id and fingerprint")
	}
	state, err := json.Marshal(managedKeyCommandState{Outcome: "pending", ExternalKeyID: externalKeyID, Fingerprint: fingerprint})
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE gateway.idempotency_records
		SET response_body = $5
		WHERE tenant_scope = $1 AND actor_scope = $2 AND operation_name = $3 AND idempotency_key = $4
		  AND request_sha256 = $6 AND response_status = $7
		  AND response_body->>'outcome' = 'pending' AND response_body->>'externalKeyId' IS NULL`,
		reservation.TenantID, reservation.ActorID, managedKeyCommandOperation, reservation.IdempotencyKey,
		state, reservation.CommandFingerprint, managedKeyCommandPendingStatus)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("managed key command already carries an observed effect")
	}
	return nil
}

// ReadManagedKeyBinding reads one recorded managed key by its own binding id.
func (s *GatewayStore) ReadManagedKeyBinding(ctx context.Context, id string) (ManagedKeyBinding, error) {
	var binding ManagedKeyBinding
	var expires, revoked interface{}
	var expiresAt, revokedAt *time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, COALESCE(workspace_id,''), COALESCE(actor_id,''), external_key_id, fingerprint, COALESCE(secret_ref,''),
			purpose, model_ids, observation_result, expires_at, revoked_at, created_at
		FROM gateway.key_bindings WHERE id = $1`, id).
		Scan(&binding.ID, &binding.TenantID, &binding.WorkspaceID, &binding.ActorID, &binding.ExternalKeyID, &binding.Fingerprint,
			&binding.SecretRef, &binding.Purpose, pq.Array(&binding.ModelIDs), &binding.ObservationResult, &expiresAt, &revokedAt, &binding.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedKeyBinding{}, ErrGatewayWalletUnknown
	}
	if err != nil {
		return ManagedKeyBinding{}, err
	}
	_ = expires
	_ = revoked
	if expiresAt != nil {
		binding.ExpiresAt = *expiresAt
	}
	if revokedAt != nil {
		binding.RevokedAt = *revokedAt
	}
	return binding, nil
}

// MarkManagedKeyRevoked records the confirmed key revocation.
func (s *GatewayStore) MarkManagedKeyRevoked(ctx context.Context, id string) (ManagedKeyBinding, error) {
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `UPDATE gateway.key_bindings SET revoked_at = $2, updated_at = $2, observation_result = 'confirmed' WHERE id = $1 AND revoked_at IS NULL`, id, now); err != nil {
		return ManagedKeyBinding{}, err
	}
	return s.ReadManagedKeyBinding(ctx, id)
}

func nullTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}

// GatewayKeyContract maps a stored managed key binding to the public GatewayKey
// read model, without the raw value.
func (b ManagedKeyBinding) GatewayKeyContract() *api.GatewayKey {
	out := &api.GatewayKey{Id: b.ID, Fingerprint: b.Fingerprint, ModelIds: b.ModelIDs, CreatedAt: stampOf(b.CreatedAt)}
	if b.WorkspaceID != "" {
		out.WorkspaceId = &b.WorkspaceID
	}
	if b.Purpose == "workspace_managed" {
		out.Purpose = api.GatewayKeyPurposeEnum_GATEWAY_KEY_PURPOSE_ENUM_WORKSPACE_MANAGED
	} else {
		out.Purpose = api.GatewayKeyPurposeEnum_GATEWAY_KEY_PURPOSE_ENUM_PERSONAL
	}
	if b.RevokedAt.IsZero() {
		out.Status = api.GatewayKeyStatusEnum_GATEWAY_KEY_STATUS_ENUM_ACTIVE
	} else {
		out.Status = api.GatewayKeyStatusEnum_GATEWAY_KEY_STATUS_ENUM_REVOKED
	}
	if !b.ExpiresAt.IsZero() {
		out.ExpiresAt = stampOf(b.ExpiresAt)
	}
	return out
}

// ManagedKeyIssueRequest is the declared issuance intent of one Workspace-managed
// Gateway key. ModelIDs is the declared model scope; an empty list is the default
// App's declared scope and never means "unrestricted": the issuer resolves the
// approved concrete scope for the bound group and returns it. LaunchOperationID
// and IdempotencyKey are the original command's bounded idempotency key, so an
// ambiguous outcome is read back by its original issue identity instead of
// minting a second key.
type ManagedKeyIssueRequest struct {
	Subject           string
	WorkspaceID       string
	LaunchOperationID string
	ExactName         string
	GroupName         string
	ModelIDs          []string
	IdempotencyKey    string
}

// ManagedKeyIssueResult is the issuer-confirmed identity of one issued key. The
// raw value is handed straight to the approved Secret store and never persisted
// in a Gateway table; ModelIDs is the resolved concrete scope, never empty.
type ManagedKeyIssueResult struct {
	Raw           string
	ExternalKeyID string
	GroupID       int64
	ModelIDs      []string
	Replayed      bool
}

// ManagedKeyIssuer mints and retires a Workspace-scoped Gateway key at the
// external Gateway, returning the raw value only to the caller that immediately
// hands it to the approved Secret store. A key issuer never persists the raw key.
type ManagedKeyIssuer interface {
	IssueWorkspaceKey(ctx context.Context, request ManagedKeyIssueRequest) (ManagedKeyIssueResult, error)
	RevokeWorkspaceKey(ctx context.Context, subject, externalKeyID string) error
}
