package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"opl-cloud/services/internal/ownerstore"
)

// One revocation attempt of one Workspace-managed key is recorded here, in this
// owner's own gateway.idempotency_records under its own API operation name.
//
// It deliberately does NOT reuse gateway.key_bindings.observation_result. That
// column is already written by two different owners of meaning - 'confirmed'
// there says the *issuance* was observed confirmed on insert, and says the
// *revocation* was confirmed in MarkManagedKeyRevoked - and it is CHECK
// constrained to exactly ('confirmed','rejected','unknown'). Neither a cause code
// nor a per-attempt history can be expressed in it. Writing a third meaning into
// it would make 'confirmed' ambiguous for every reader, so the attempt is
// recorded as its own command row instead: one row per attempt identity, its own
// vocabulary, and free-form enough to carry the cause that produced it.
const managedKeyRevocationOperation = "RevokeManagedKey"

// The recorded outcome of one revocation attempt.
//
//	confirmed - the issuer retired the key and this owner's record committed.
//	            The key really is revoked.
//	refused   - a definite answer that no revocation happened. The authority said
//	            no before any key effect; this is the actionable fact, and it is
//	            never overwritten by a later, vaguer attempt.
//	unknown   - the outcome could not be observed: the issuer's answer was lost
//	            or unreadable, or this owner's readback did not commit. The key may
//	            or may not be retired; nothing here claims either way.
const (
	managedKeyRevocationOutcomeConfirmed = "confirmed"
	managedKeyRevocationOutcomeRefused   = "refused"
	managedKeyRevocationOutcomeUnknown   = "unknown"
)

// Stored response statuses for one recorded attempt. The table admits 100..599.
const (
	managedKeyRevocationConfirmedStatus = 200
	managedKeyRevocationRefusedStatus   = 409
	managedKeyRevocationUnknownStatus   = 202
)

// ManagedKeyRevocationAttempt is one recorded attempt to retire one key binding.
// ReasonCode is the gRPC code that ended the attempt, so 'Unimplemented' (this
// owner has no approved revocation route) stays distinguishable from 'Unavailable'
// (the route may have taken effect and lost its answer) after the fact. The
// provider's free-form message is not stored: the existing command-record
// convention keeps de-identified codes only, and the code is what distinguishes
// the outcomes.
type ManagedKeyRevocationAttempt struct {
	KeyBindingID  string
	WorkspaceID   string
	ExternalKeyID string
	Outcome       string
	ReasonCode    string
	RecordedAt    time.Time
}

// managedKeyRevocationState is the safe stored body of one attempt. It carries
// only opaque identities and the recorded cause; no credential material.
type managedKeyRevocationState struct {
	Outcome       string `json:"outcome"`
	ReasonCode    string `json:"reasonCode,omitempty"`
	KeyBindingID  string `json:"keyBindingId"`
	WorkspaceID   string `json:"workspaceId,omitempty"`
	ExternalKeyID string `json:"externalKeyId,omitempty"`
}

// RecordManagedKeyRevocation records one revocation attempt. It is the durable
// answer to "what does this system know after it was asked to revoke a key it
// could not revoke?".
//
// The attempt identity is the caller's idempotency key, so a replayed command
// replays its first recorded answer instead of overwriting it: a definite
// refusal is never erased by a later unknown outcome. An absent idempotency key
// falls back to the binding id, which pins the same guarantee to the key.
//
// A recording failure is returned to the caller rather than swallowed. It is the
// one case where this owner knows something it could not write down, and a
// silent failure would leave the binding exactly as indistinguishable as before.
func (s *GatewayStore) RecordManagedKeyRevocation(ctx context.Context, tenantID, actorID, idempotencyKey string, attempt ManagedKeyRevocationAttempt) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(actorID) == "" || strings.TrimSpace(attempt.KeyBindingID) == "" {
		return errors.New("a managed key revocation attempt requires tenant, actor and key binding")
	}
	responseStatus, ok := managedKeyRevocationStatus(attempt.Outcome)
	if !ok {
		return fmt.Errorf("an unrecorded managed key revocation outcome %q", attempt.Outcome)
	}
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		key = attempt.KeyBindingID
	}
	body, err := json.Marshal(managedKeyRevocationState{Outcome: attempt.Outcome, ReasonCode: attempt.ReasonCode,
		KeyBindingID: attempt.KeyBindingID, WorkspaceID: attempt.WorkspaceID, ExternalKeyID: attempt.ExternalKeyID})
	if err != nil {
		return err
	}
	input := ownerstore.IdempotencyInput{
		ID:             "idem_key_revoke_" + shortDigest(tenantID+"\x00"+actorID+"\x00"+key),
		TenantScope:    tenantID,
		ActorScope:     actorID,
		OperationName:  managedKeyRevocationOperation,
		IdempotencyKey: key,
		RequestSHA256:  ownerstore.HashRequestBody([]byte(attempt.KeyBindingID + "\x00" + attempt.WorkspaceID)),
		ResourceID:     attempt.KeyBindingID,
		ResponseStatus: responseStatus,
		ResponseBody:   body,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// A repeated attempt under the same command identity is the same command, so
	// it keeps the answer it already has. This is what stops a definite refusal
	// from being re-decided - or erased - by a later, vaguer attempt.
	if _, found, lookupErr := s.store.LookupIdempotency(ctx, tx, input); lookupErr != nil {
		return lookupErr
	} else if found {
		return tx.Commit()
	}
	if err = s.store.RecordIdempotency(ctx, tx, input); err != nil {
		return err
	}
	return tx.Commit()
}

// ListUnconfirmedManagedKeyRevocations returns every recorded revocation attempt
// this owner could not confirm, newest first. This is the readback that answers
// "which keys were asked to die and did not?", which neither the binding read nor
// the call that failed can answer afterwards. Each attempt is its own row, so a
// refusal and a later unknown attempt are both visible rather than one replacing
// the other.
func (s *GatewayStore) ListUnconfirmedManagedKeyRevocations(ctx context.Context, tenantID string) ([]ManagedKeyRevocationAttempt, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, errors.New("listing unconfirmed revocations requires a tenant")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT resource_id, response_body, created_at
		FROM gateway.idempotency_records
		WHERE tenant_scope = $1 AND operation_name = $2
		  AND response_body->>'outcome' IN ('refused','unknown')
		ORDER BY created_at DESC, id`, tenantID, managedKeyRevocationOperation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var attempts []ManagedKeyRevocationAttempt
	for rows.Next() {
		var body []byte
		var recordedAt time.Time
		var state managedKeyRevocationState
		if err = rows.Scan(&state.KeyBindingID, &body, &recordedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(body, &state); err != nil {
			return nil, err
		}
		attempts = append(attempts, ManagedKeyRevocationAttempt{KeyBindingID: state.KeyBindingID, WorkspaceID: state.WorkspaceID,
			ExternalKeyID: state.ExternalKeyID, Outcome: state.Outcome, ReasonCode: state.ReasonCode, RecordedAt: recordedAt})
	}
	return attempts, rows.Err()
}

// managedKeyRevocationStatus maps a recorded outcome to its stored status.
func managedKeyRevocationStatus(outcome string) (int, bool) {
	switch outcome {
	case managedKeyRevocationOutcomeConfirmed:
		return managedKeyRevocationConfirmedStatus, true
	case managedKeyRevocationOutcomeRefused:
		return managedKeyRevocationRefusedStatus, true
	case managedKeyRevocationOutcomeUnknown:
		return managedKeyRevocationUnknownStatus, true
	}
	return 0, false
}

// managedKeyRevocationOutcome classifies why an attempt is unconfirmed. A listed
// code is a definite answer - the authority refused before any key effect - so it
// is recorded as 'refused'. Anything else, including a lost or unreadable answer,
// may have taken effect without being observed, so it is recorded as 'unknown' and
// is never mistaken for a settled refusal.
func managedKeyRevocationOutcome(cause error) string {
	switch status.Code(cause) {
	case codes.Unimplemented, codes.FailedPrecondition, codes.PermissionDenied, codes.Unauthenticated,
		codes.InvalidArgument, codes.AlreadyExists, codes.NotFound:
		return managedKeyRevocationOutcomeRefused
	default:
		return managedKeyRevocationOutcomeUnknown
	}
}

// noteRevocationAttempt records one attempt under the caller's own tenant,
// actor and command identity, and reports whether it could be recorded. The
// caller's original error is never replaced by this one: the issuer's refusal is
// the fact the caller must act on, so a recording failure is returned separately
// for the caller to decide what to do with it.
//
// When the attempt could not be written down, the returned error is wrapped onto
// the caller's own error by the caller. This owner has no logger wired (nothing
// in this module does), so the caller's error is the only channel available -
// silently dropping it would leave the binding exactly as indistinguishable as
// before, which is the bug this record exists to fix.
func (s *Service) noteRevocationAttempt(ctx context.Context, tenantID, actorID, idempotencyKey string, key ManagedKeyBinding, outcome string, cause error) error {
	if s.GatewayStore == nil {
		// There is no store to record against. The caller's own failure already
		// says the revocation did not happen; nothing here can add to it.
		return nil
	}
	attempt := ManagedKeyRevocationAttempt{KeyBindingID: key.ID, WorkspaceID: key.WorkspaceID,
		ExternalKeyID: key.ExternalKeyID, Outcome: outcome}
	if cause != nil {
		attempt.ReasonCode = status.Code(cause).String()
	}
	return s.GatewayStore.RecordManagedKeyRevocation(ctx, tenantID, actorID, idempotencyKey, attempt)
}
