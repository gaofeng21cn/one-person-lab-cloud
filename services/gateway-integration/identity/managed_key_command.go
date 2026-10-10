package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"opl-cloud/services/internal/ownerstore"
)

// managedKeyCommandOperation is the stable API operation name of one managed-key
// command. The contract defines no gateway operation kind for a managed-key
// issuance - createGatewayKey is a synchronous product call - so the original
// command is registered in the existing gateway.idempotency_records under its own
// API operation name, exactly as the table comment requires ("operation_name is
// the API operation name, not a new Operation ID").
const managedKeyCommandOperation = "CreateManagedKey"

// Managed key command answer states recorded in the original command row:
// pending while the external effect is unconfirmed (never re-dispatched because
// the issuer has no idempotent lookup by the original issue identity yet),
// confirmed only for the recorded binding identity, and rejected for a definite
// refusal. A terminal answer is read back, never re-derived.
const (
	managedKeyCommandPendingStatus   = 202
	managedKeyCommandConfirmedStatus = 200
	managedKeyCommandRejectedStatus  = 409
)

// ErrManagedKeyCommandConflict reports one idempotency key reused for a
// different managed-key command.
var ErrManagedKeyCommandConflict = errors.New("managed key command differs from its original execution")

// ManagedKeyCommandReservation is the immutable identity of one original
// managed-key command, fixed before any external key issuance.
type ManagedKeyCommandReservation struct {
	TenantID           string
	ActorID            string
	WorkspaceID        string
	IdempotencyKey     string
	CommandFingerprint string
}

// ManagedKeyCommandRecord is the durable original answer of one command. The
// body carries only opaque identities - binding id, fingerprint, Secret delivery
// reference and expiry, or the recorded cause. The raw key is never stored.
type ManagedKeyCommandRecord struct {
	ResponseStatus int
	Body           []byte
}

// managedKeyCommandState is the safe stored answer shape of one original
// command. It carries no credential material.
type managedKeyCommandState struct {
	Outcome                 string `json:"outcome"`
	ErrorCode               string `json:"errorCode,omitempty"`
	ExternalKeyID           string `json:"externalKeyId,omitempty"`
	Fingerprint             string `json:"fingerprint,omitempty"`
	SecretRef               string `json:"secretRef,omitempty"`
	KeyBindingID            string `json:"keyBindingId,omitempty"`
	TargetRuntimeInstanceID string `json:"targetRuntimeInstanceId,omitempty"`
	ExpiresAt               string `json:"expiresAt,omitempty"`
	// ResolvedModelIDs is the concrete model scope the issuance authority
	// resolved for this command. An empty declared selection means "resolve the
	// approved scope", never "unrestricted": the resolved list is recorded with
	// the confirmed binding and a later readback must match it exactly.
	ResolvedModelIDs []string `json:"resolvedModelIds,omitempty"`
}

// ReserveManagedKeyCommand registers the original command before any external
// effect and returns the existing record when the same command already exists.
// A different fingerprint under the same key is a conflict: the owner never
// reinterprets one command identity as another command. The registration is a
// pending answer, so a lost response or a crash leaves the original command
// findable instead of issuing a second key.
func (s *GatewayStore) ReserveManagedKeyCommand(ctx context.Context, reservation ManagedKeyCommandReservation) (ManagedKeyCommandRecord, bool, error) {
	if strings.TrimSpace(reservation.TenantID) == "" || strings.TrimSpace(reservation.ActorID) == "" ||
		strings.TrimSpace(reservation.WorkspaceID) == "" || strings.TrimSpace(reservation.IdempotencyKey) == "" ||
		len(reservation.CommandFingerprint) != 64 {
		return ManagedKeyCommandRecord{}, false, errors.New("managed key command requires tenant, actor, workspace, idempotency key and request hash")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ManagedKeyCommandRecord{}, false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "gateway.managed_key_command:"+reservation.TenantID+":"+reservation.IdempotencyKey); err != nil {
		return ManagedKeyCommandRecord{}, false, err
	}
	input := ownerstore.IdempotencyInput{
		ID:             "idem_managed_key_" + shortDigest(reservation.TenantID+"\x00"+reservation.ActorID+"\x00"+reservation.IdempotencyKey),
		TenantScope:    reservation.TenantID,
		ActorScope:     reservation.ActorID,
		OperationName:  managedKeyCommandOperation,
		IdempotencyKey: reservation.IdempotencyKey,
		RequestSHA256:  reservation.CommandFingerprint,
	}
	previous, found, err := s.store.LookupIdempotency(ctx, tx, input)
	if errors.Is(err, ownerstore.ErrIdempotencyConflict) {
		return ManagedKeyCommandRecord{}, false, ErrManagedKeyCommandConflict
	}
	if err != nil {
		return ManagedKeyCommandRecord{}, false, err
	}
	if found {
		record := ManagedKeyCommandRecord{ResponseStatus: previous.ResponseStatus, Body: append([]byte(nil), previous.ResponseBody...)}
		if err = tx.Commit(); err != nil {
			return ManagedKeyCommandRecord{}, false, err
		}
		return record, true, nil
	}
	body, marshalErr := json.Marshal(managedKeyCommandState{Outcome: "pending"})
	if marshalErr != nil {
		return ManagedKeyCommandRecord{}, false, marshalErr
	}
	input.ResourceID = reservation.WorkspaceID
	input.ResponseStatus = managedKeyCommandPendingStatus
	input.ResponseBody = body
	if err = s.store.RecordIdempotency(ctx, tx, input); err != nil {
		return ManagedKeyCommandRecord{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return ManagedKeyCommandRecord{}, false, err
	}
	return ManagedKeyCommandRecord{ResponseStatus: managedKeyCommandPendingStatus, Body: body}, false, nil
}

// ReadManagedKeyCommand reads the durable answer of one original command by its
// immutable identity. A missing record is reported, never synthesized.
func (s *GatewayStore) ReadManagedKeyCommand(ctx context.Context, reservation ManagedKeyCommandReservation) (ManagedKeyCommandRecord, error) {
	var status int
	var body []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT response_status, response_body
		FROM gateway.idempotency_records
		WHERE tenant_scope = $1 AND actor_scope = $2 AND operation_name = $3 AND idempotency_key = $4
		  AND request_sha256 = $5`,
		reservation.TenantID, reservation.ActorID, managedKeyCommandOperation, reservation.IdempotencyKey,
		reservation.CommandFingerprint).Scan(&status, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedKeyCommandRecord{}, ErrGatewayWalletUnknown
	}
	if err != nil {
		return ManagedKeyCommandRecord{}, err
	}
	return ManagedKeyCommandRecord{ResponseStatus: status, Body: append([]byte(nil), body...)}, nil
}

// SettleManagedKeyCommand records the terminal answer of one original command.
// Only the pending answer may be advanced: a settled command can never be
// overwritten by a replay, so the first recorded effect stays authoritative.
// resourceID is the recorded key binding when the answer is confirmed.
func (s *GatewayStore) SettleManagedKeyCommand(ctx context.Context, reservation ManagedKeyCommandReservation, responseStatus int, body []byte, resourceID string) error {
	switch responseStatus {
	case managedKeyCommandConfirmedStatus, managedKeyCommandRejectedStatus:
	default:
		// A pending answer is written only by the reservation itself. Settling can
		// never rewrite it back to pending, so an observed external key identity
		// recorded on the pending answer cannot be erased.
		return errors.New("a settled managed key command answer must be confirmed or rejected")
	}
	if !json.Valid(body) {
		return errors.New("managed key command answer must be a JSON document")
	}
	if responseStatus == managedKeyCommandConfirmedStatus && strings.TrimSpace(resourceID) == "" {
		return errors.New("a confirmed managed key command answer requires its recorded binding")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE gateway.idempotency_records
		SET response_status = $5, response_body = $6, resource_id = COALESCE(NULLIF($7, ''), resource_id)
		WHERE tenant_scope = $1 AND actor_scope = $2 AND operation_name = $3 AND idempotency_key = $4
		  AND request_sha256 = $8 AND response_status = $9`,
		reservation.TenantID, reservation.ActorID, managedKeyCommandOperation, reservation.IdempotencyKey,
		responseStatus, body, resourceID, reservation.CommandFingerprint, managedKeyCommandPendingStatus)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("managed key command is not pending in this owner")
	}
	return nil
}

// managedKeyCommandDeclaredDefaultScope marks a command whose declared model
// scope is empty: the default App names no model slot, so the approved scope is
// resolved by the issuance authority. The marker is not a model id and can never
// collide with one, and it keeps the fingerprint a function of the DECLARED
// intent alone, so a replay matches regardless of later allowlist drift.
const managedKeyCommandDeclaredDefaultScope = "<declared-default-scope>"

// managedKeyCommandFingerprint fixes the immutable identity of one managed-key
// command. The model list is a set, so its order is normalized; an empty declared
// list is the canonical default-scope marker rather than "anything". A different
// workspace, runtime or declared model set under the same idempotency key is a
// different command and is refused rather than silently re-interpreted.
func managedKeyCommandFingerprint(workspaceID, runtimeInstanceID string, modelIDs []string) string {
	models := append([]string(nil), modelIDs...)
	sort.Strings(models)
	if len(models) == 0 {
		models = []string{managedKeyCommandDeclaredDefaultScope}
	}
	parts := append([]string{"managed_key", workspaceID, runtimeInstanceID}, models...)
	return hash(strings.Join(parts, "\x00"))
}

// normalizeManagedKeyModelIDs trims the declared model set. Empty entries name no
// model and are dropped rather than folded into an opaque identity.
func normalizeManagedKeyModelIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// definiteManagedKeyRefusal reports whether the issuer answered with a definite
// refusal. Any other failure is an unconfirmed external outcome and is never
// re-dispatched: the owner has no idempotent key lookup by its original issue
// identity, so repeating the issuance could mint a second key.
func definiteManagedKeyRefusal(err error) bool {
	switch status.Code(err) {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.AlreadyExists,
		codes.NotFound, codes.PermissionDenied, codes.Unauthenticated,
		codes.ResourceExhausted, codes.OutOfRange, codes.DataLoss, codes.Unimplemented:
		return true
	default:
		return false
	}
}
