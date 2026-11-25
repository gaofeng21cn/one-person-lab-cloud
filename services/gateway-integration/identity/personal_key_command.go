package identity

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"opl-cloud/services/internal/ownerstore"
)

// A personal Gateway Key command is registered in this owner's existing
// gateway.idempotency_records before any provider write, under the caller's
// immutable idempotency key and a fingerprint bound to the personal actor and
// the provider subject. A replay of the same command reads the recorded answer
// back instead of writing to the provider a second time; a replayed command
// with a different request is refused.
//
// The reservation exists because a provider write is not idempotent on its own.
// An update that resets the Key's usage would clear the usage that accrued
// after the first request if the retry reached the provider, and a create would
// mint a second Key for one accepted command. The stored answer is a
// de-identified identity: the create answer carries the Key summary and never
// the provider's plaintext, which is handed back only by the explicit reveal
// command and only after its audit write.
//
// The owner's own readback is the only evidence that settles an interrupted
// write, and it settles only what it can prove: a Key that is still present was
// not revoked, and a Key whose configuration matches was updated. A usage reset
// cannot be read back, so an interrupted update that asked for one stays
// unresolved rather than being dispatched a second time.
const (
	personalKeyCreateOperation = "CreatePersonalGatewayKey"
	personalKeyUpdateOperation = "UpdatePersonalGatewayKey"
	personalKeyRevokeOperation = "RevokePersonalGatewayKey"
)

// personalKeyCommandScope is the tenant scope of a personal Key command. A
// personal Key belongs to the User, not to a Tenant, so the command is scoped
// to the personal account space and the actor scope carries the personal actor.
const personalKeyCommandScope = "personal"

// Answer states recorded in the original command row: pending while the
// provider's outcome is unconfirmed, confirmed only for the recorded answer,
// and rejected for a definite refusal. A terminal answer is read back, never
// re-derived.
const (
	personalKeyCommandPendingStatus   = 202
	personalKeyCommandConfirmedStatus = 200
	personalKeyCommandRejectedStatus  = 409
)

// ErrPersonalKeyCommandConflict reports one idempotency key reused for a
// different personal Key command.
var ErrPersonalKeyCommandConflict = errors.New("personal key command differs from its original execution")

// PersonalKeyCommandReservation is the immutable identity of one original
// personal Key command, fixed before any provider write.
type PersonalKeyCommandReservation struct {
	ActorID            string
	ProviderSubject    string
	OperationName      string
	IdempotencyKey     string
	CommandFingerprint string
	ResourceID         string
}

// PersonalKeyCommandRecord is the durable answer of one command.
type PersonalKeyCommandRecord struct {
	ResponseStatus int
	Body           []byte
}

// personalKeyCommandState is the safe stored answer shape of one command. It
// carries the de-identified answer message and never the provider's plaintext.
type personalKeyCommandState struct {
	Outcome   string          `json:"outcome"`
	ErrorCode string          `json:"errorCode,omitempty"`
	Answer    json.RawMessage `json:"answer,omitempty"`
}

// ReservePersonalKeyCommand registers the original command before any provider
// write and returns the existing record when the same command already exists.
// A different fingerprint under the same key is a conflict: the owner never
// reinterprets one command identity as another command. The registration is a
// pending answer, so an interrupted write leaves the original command findable
// instead of writing to the provider again.
func (s *GatewayStore) ReservePersonalKeyCommand(ctx context.Context, reservation PersonalKeyCommandReservation) (PersonalKeyCommandRecord, bool, error) {
	if strings.TrimSpace(reservation.ActorID) == "" || strings.TrimSpace(reservation.ProviderSubject) == "" ||
		strings.TrimSpace(reservation.OperationName) == "" || strings.TrimSpace(reservation.IdempotencyKey) == "" ||
		strings.TrimSpace(reservation.ResourceID) == "" || len(reservation.CommandFingerprint) != 64 {
		return PersonalKeyCommandRecord{}, false, errors.New("personal key command requires actor, provider subject, operation, idempotency key, resource and request hash")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PersonalKeyCommandRecord{}, false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "gateway.personal_key_command:"+reservation.ActorID+":"+reservation.OperationName+":"+reservation.IdempotencyKey); err != nil {
		return PersonalKeyCommandRecord{}, false, err
	}
	input := ownerstore.IdempotencyInput{
		ID:             "idem_personal_key_" + shortDigest(reservation.ActorID+"\x00"+reservation.OperationName+"\x00"+reservation.IdempotencyKey),
		TenantScope:    personalKeyCommandScope,
		ActorScope:     reservation.ActorID,
		OperationName:  reservation.OperationName,
		IdempotencyKey: reservation.IdempotencyKey,
		RequestSHA256:  reservation.CommandFingerprint,
	}
	previous, found, err := s.store.LookupIdempotency(ctx, tx, input)
	if errors.Is(err, ownerstore.ErrIdempotencyConflict) {
		return PersonalKeyCommandRecord{}, false, ErrPersonalKeyCommandConflict
	}
	if err != nil {
		return PersonalKeyCommandRecord{}, false, err
	}
	if found {
		record := PersonalKeyCommandRecord{ResponseStatus: previous.ResponseStatus, Body: append([]byte(nil), previous.ResponseBody...)}
		if err = tx.Commit(); err != nil {
			return PersonalKeyCommandRecord{}, false, err
		}
		return record, true, nil
	}
	body, marshalErr := json.Marshal(personalKeyCommandState{Outcome: "pending"})
	if marshalErr != nil {
		return PersonalKeyCommandRecord{}, false, marshalErr
	}
	input.ResourceID = reservation.ResourceID
	input.ResponseStatus = personalKeyCommandPendingStatus
	input.ResponseBody = body
	if err = s.store.RecordIdempotency(ctx, tx, input); err != nil {
		return PersonalKeyCommandRecord{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return PersonalKeyCommandRecord{}, false, err
	}
	return PersonalKeyCommandRecord{ResponseStatus: personalKeyCommandPendingStatus, Body: body}, false, nil
}

// SettlePersonalKeyCommand records the terminal answer of one original command.
// Only the pending answer may be advanced: a settled command can never be
// overwritten by a replay, so the first recorded effect stays authoritative.
// resourceID is the recorded Key identity when the answer is confirmed.
func (s *GatewayStore) SettlePersonalKeyCommand(ctx context.Context, reservation PersonalKeyCommandReservation, responseStatus int, body []byte, resourceID string) error {
	switch responseStatus {
	case personalKeyCommandConfirmedStatus, personalKeyCommandRejectedStatus:
	default:
		// A pending answer is written only by the reservation itself. Settling can
		// never rewrite it back to pending, so an observed effect recorded on the
		// pending answer cannot be erased.
		return errors.New("a settled personal key command answer must be confirmed or rejected")
	}
	if !json.Valid(body) {
		return errors.New("personal key command answer must be a JSON document")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE gateway.idempotency_records
		SET response_status = $5, response_body = $6, resource_id = COALESCE(NULLIF($7, ''), resource_id)
		WHERE tenant_scope = $1 AND actor_scope = $2 AND operation_name = $3 AND idempotency_key = $4
		  AND request_sha256 = $8 AND response_status = $9`,
		personalKeyCommandScope, reservation.ActorID, reservation.OperationName, reservation.IdempotencyKey,
		responseStatus, body, resourceID, reservation.CommandFingerprint, personalKeyCommandPendingStatus)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("personal key command is not pending in this owner")
	}
	return nil
}

// personalKeyCommandFingerprint fixes the immutable identity of one personal Key
// command. It binds the personal actor and the provider subject to the
// requested facts, so a command issued for one account can never answer for
// another, and a different request under the same idempotency key is a
// different command rather than a second write. The request is canonical JSON,
// so the same instruction always hashes to the same identity.
func personalKeyCommandFingerprint(operation, actorID, providerSubject, resourceID string, request map[string]any) string {
	canonical, err := json.Marshal(request)
	if err != nil {
		canonical = []byte("{}")
	}
	parts := []string{"personal_key_command", operation, actorID, providerSubject, resourceID, string(canonical)}
	return hash(strings.Join(parts, "\x00"))
}

// personalKeyCommandAnswer reads the durable answer of one original command. A
// confirmed answer is replayed as recorded; a rejected answer replays its
// refusal; a pending answer reports the command as unresolved, because the
// owner's readback cannot settle every interrupted provider write and the
// command is never dispatched a second time.
func personalKeyCommandAnswer(record PersonalKeyCommandRecord, answer proto.Message) error {
	var state personalKeyCommandState
	if json.Unmarshal(record.Body, &state) != nil {
		return status.Error(codes.Unavailable, "the personal Gateway key command answer is unreadable")
	}
	switch state.Outcome {
	case "confirmed":
		if len(state.Answer) == 0 || protojson.Unmarshal(state.Answer, answer) != nil {
			return status.Error(codes.Unavailable, "the personal Gateway key command answer is unreadable")
		}
		return nil
	case "rejected":
		return status.Error(codes.FailedPrecondition, "the personal Gateway key command was refused")
	case "pending":
		return status.Error(codes.Unavailable, "the personal Gateway key command outcome is unknown")
	default:
		return status.Error(codes.Unavailable, "the personal Gateway key command answer is unreadable")
	}
}

// personalKeyCommandPending reports whether the recorded answer is still
// pending, which means the provider's outcome was never confirmed.
func personalKeyCommandPending(record PersonalKeyCommandRecord) bool {
	var state personalKeyCommandState
	if json.Unmarshal(record.Body, &state) != nil {
		return false
	}
	return state.Outcome == "pending"
}

// settlePersonalKeyCommand records the confirmed answer of one original command.
func settlePersonalKeyCommand(ctx context.Context, s *Service, reservation PersonalKeyCommandReservation, answer proto.Message, resourceID string) error {
	body, err := protojson.Marshal(answer)
	if err != nil {
		return status.Error(codes.Unavailable, "the personal Gateway key answer is unreadable")
	}
	state, err := json.Marshal(personalKeyCommandState{Outcome: "confirmed", Answer: body})
	if err != nil {
		return status.Error(codes.Unavailable, "the personal Gateway key answer is unreadable")
	}
	if err := s.GatewayStore.SettlePersonalKeyCommand(ctx, reservation, personalKeyCommandConfirmedStatus, state, resourceID); err != nil {
		return status.Error(codes.Unavailable, "the personal Gateway key command settlement is unavailable")
	}
	return nil
}

// refusePersonalKeyCommand records a definite refusal of one original command.
// The command stays answered: a retry replays the refusal instead of writing to
// the provider a second time.
func refusePersonalKeyCommand(ctx context.Context, s *Service, reservation PersonalKeyCommandReservation, cause error) {
	state, err := json.Marshal(personalKeyCommandState{Outcome: "rejected", ErrorCode: status.Code(cause).String()})
	if err != nil {
		return
	}
	_ = s.GatewayStore.SettlePersonalKeyCommand(ctx, reservation, personalKeyCommandRejectedStatus, state, "")
}

// definitePersonalKeyRefusal reports whether the provider answered with a
// definite refusal. Any other failure is an unconfirmed external outcome and is
// never re-dispatched: a create could mint a second Key and an update that
// resets the Key's usage could clear the usage that accrued after the first
// request.
func definitePersonalKeyRefusal(err error) bool {
	switch status.Code(err) {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.AlreadyExists,
		codes.NotFound, codes.PermissionDenied, codes.ResourceExhausted,
		codes.OutOfRange, codes.DataLoss, codes.Unimplemented:
		return true
	default:
		return false
	}
}

// personalKeyCommandUsageReset reports whether an update asked the provider to
// reset the Key's usage. Such a write cannot be read back, because the usage
// that accrued after the first request is indistinguishable from a usage that
// was never reset.
func personalKeyCommandUsageReset(request map[string]any) bool {
	for _, field := range []string{"reset_quota", "reset_rate_limit_usage"} {
		if value, present := request[field]; present {
			if reset, ok := value.(bool); ok && reset {
				return true
			}
		}
	}
	return false
}
