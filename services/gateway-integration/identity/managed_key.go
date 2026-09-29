package identity

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

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

// InsertManagedKeyBinding records the confirmed managed key. It fails closed when
// a confirmed binding has no opaque secret reference.
func (s *GatewayStore) InsertManagedKeyBinding(ctx context.Context, binding ManagedKeyBinding) (ManagedKeyBinding, error) {
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
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO gateway.key_bindings (id, tenant_id, workspace_id, actor_id, external_key_id, fingerprint, secret_ref, purpose,
			model_ids, observation_result, name, expires_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)`,
		binding.ID, binding.TenantID, binding.WorkspaceID, nullString(binding.ActorID), binding.ExternalKeyID, binding.Fingerprint,
		binding.SecretRef, binding.Purpose, binding.ModelIDs, binding.ObservationResult, binding.ID, nullTime(binding.ExpiresAt), now); err != nil {
		return ManagedKeyBinding{}, err
	}
	binding.CreatedAt = now
	return binding, nil
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
			&binding.SecretRef, &binding.Purpose, &binding.ModelIDs, &binding.ObservationResult, &expiresAt, &revokedAt, &binding.CreatedAt)
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

// ManagedKeyIssuer mints and retires a Workspace-scoped Gateway key at the
// external Gateway, returning the raw value only to the caller that immediately
// hands it to the approved Secret store. A key issuer never persists the raw key.
type ManagedKeyIssuer interface {
	IssueWorkspaceKey(ctx context.Context, subject, workspaceID string, modelIDs []string) (raw, externalKeyID string, err error)
	RevokeWorkspaceKey(ctx context.Context, subject, externalKeyID string) error
}
