package coordination

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// RebindSecret is Fabric's explicit replacement path for a changed Gateway
// Secret. Serve is the only approved caller, exactly as for the initial bind.
// BindSecret stays initial-bind and same-request replay only, so a second
// model configuration that changes the managed key cannot be expressed as another
// BindSecret; it must name the exact predecessor it replaces. Fabric compares that
// predecessor under the same per-Workspace lock it binds under, proves it belongs
// to the exact runtime instance the replacement names, has the provider confirm
// the new approved-store Secret, then retires the predecessor binding and writes
// the replacement as the one active binding, preserving the
// (execution_resource_id, purpose) WHERE revoked_at IS NULL invariant without ever
// holding two active bindings.
//
// The readback names the predecessor so the caller can prove which binding it
// replaced. Fabric never revokes the predecessor Gateway key: only Workspace does
// that, and only after Serve confirms the new applied model version. A provider
// failure rolls the whole transaction back, so the predecessor binding stays
// active and no replacement is recorded.
func (s *Service) RebindSecret(ctx context.Context, r *api.SecretBindingRebindCommand) (*api.SecretBindingRebindReadback, error) {
	if err := peer(ctx, owneridentity.Serve.Service()); err != nil {
		return nil, err
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	if strings.TrimSpace(r.GetWorkspaceId()) == "" || strings.TrimSpace(r.GetRuntimeInstanceId()) == "" ||
		strings.TrimSpace(r.GetExpectedCurrentSecretBindingId()) == "" || strings.TrimSpace(r.GetKeyBindingId()) == "" ||
		strings.TrimSpace(r.GetSecretDeliveryReference()) == "" || strings.TrimSpace(r.GetContext().GetIdempotencyKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace, runtime instance, expected current binding, key binding, delivery reference and idempotency key are required")
	}
	if s.Dispatcher == nil {
		return nil, status.Error(codes.FailedPrecondition, "no provider secret binding capability is configured")
	}
	tenant := r.GetContext().GetScope().GetTenant().GetTenantId()
	if err := s.authorizeWorkspace(ctx, r.GetContext(), r.GetWorkspaceId(), tenant, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDMANAGEDSECRET); err != nil {
		return nil, err
	}
	// The idempotency identity is the replacement intent without the CallContext:
	// the same runtime, predecessor, key binding and Secret must replay one
	// durable readback instead of retiring a second binding.
	normalized, err := protojson.Marshal(&api.SecretBindingRebindCommand{
		WorkspaceId: r.GetWorkspaceId(), RuntimeInstanceId: r.GetRuntimeInstanceId(),
		ExpectedCurrentSecretBindingId: r.GetExpectedCurrentSecretBindingId(), KeyBindingId: r.GetKeyBindingId(),
		SecretDeliveryReference: r.GetSecretDeliveryReference(), TargetSlot: r.GetTargetSlot(), Fingerprint: r.GetFingerprint(),
	})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "secret rebind command cannot be encoded")
	}
	idem := ownerstore.IdempotencyInput{ID: "idem_" + uuid.NewString(), TenantScope: tenant, ActorScope: r.GetContext().GetActorId(), OperationName: "RebindSecret", IdempotencyKey: r.GetContext().GetIdempotencyKey(), RequestSHA256: ownerstore.HashRequestBody(normalized), ResponseStatus: 200}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, persistenceError(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "fabric:workspace:"+r.WorkspaceId); err != nil {
		return nil, persistenceError(err)
	}
	// Re-check authorization at the write boundary, after any lock wait.
	if err = s.authorizeWorkspace(ctx, r.GetContext(), r.WorkspaceId, tenant, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDMANAGEDSECRET); err != nil {
		return nil, err
	}
	replayed, found, err := s.Store.LookupIdempotency(ctx, tx, idem)
	if errors.Is(err, ownerstore.ErrIdempotencyConflict) {
		return nil, status.Error(codes.AlreadyExists, "idempotency key has a different Secret replacement")
	}
	if err != nil {
		return nil, persistenceError(err)
	}
	if found {
		out := &api.SecretBindingRebindReadback{}
		if len(replayed.ResponseBody) == 0 || protojson.Unmarshal(replayed.ResponseBody, out) != nil {
			return nil, status.Error(codes.DataLoss, "stored Secret rebind readback is invalid")
		}
		if err = tx.Commit(); err != nil {
			return nil, persistenceError(err)
		}
		return out, nil
	}
	setID, execID, err := s.runtimeExecutionResource(ctx, tx, r.WorkspaceId)
	if err != nil {
		return nil, err
	}
	purpose := strings.TrimSpace(r.GetTargetSlot())
	if purpose == "" {
		purpose = "workspace_gateway_key"
	}
	// The predecessor must be the exact active binding the caller expects. Two
	// writers serialize on the workspace lock, so a concurrent replacement cannot
	// slip between the compare and the supersede.
	var currentID, currentRef, currentVersion, currentFingerprint, currentResult string
	err = tx.QueryRowContext(ctx, `SELECT id,secret_ref,version,fingerprint,observation_result FROM fabric.secret_bindings WHERE execution_resource_id=$1 AND purpose=$2 AND revoked_at IS NULL FOR UPDATE`, execID, purpose).Scan(&currentID, &currentRef, &currentVersion, &currentFingerprint, &currentResult)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Error(codes.FailedPrecondition, "the runtime has no active Secret binding to replace")
	}
	if err != nil {
		return nil, persistenceError(err)
	}
	if currentID != r.GetExpectedCurrentSecretBindingId() {
		return nil, status.Errorf(codes.FailedPrecondition, "the active Secret binding is %s, not the expected %s", currentID, r.GetExpectedCurrentSecretBindingId())
	}
	if currentResult != "confirmed" {
		return nil, status.Error(codes.FailedPrecondition, "the active Secret binding is not confirmed")
	}
	// The predecessor must belong to the exact runtime instance this replacement
	// names. Its originating bind/rebind command is the durable runtime identity
	// of the active binding, so a replacement can never retire another runtime
	// instance's binding or relabel it in the readback.
	origin, err := secretBindingOriginOf(ctx, tx, currentID)
	if err != nil {
		return nil, err
	}
	if origin.RuntimeInstanceID != r.GetRuntimeInstanceId() {
		return nil, status.Error(codes.FailedPrecondition, "the active Secret binding belongs to a different runtime instance")
	}
	if currentRef == r.GetSecretDeliveryReference() && currentFingerprint == r.GetFingerprint() {
		// The predecessor may answer the replacement as itself only when the
		// request names the exact Gateway key binding the active binding was
		// created from. Identical Secret content does not prove the same Gateway
		// binding identity: another binding can neither be labeled confirmed by
		// the predecessor nor silently swapped in without the provider
		// confirmation and predecessor retirement a real replacement requires.
		if origin.KeyBindingID != r.GetKeyBindingId() {
			return nil, status.Error(codes.FailedPrecondition, "the active Secret binding belongs to a different Gateway key binding")
		}
		// The predecessor already carries the requested Secret identity and
		// Gateway binding: the replacement names itself, so replay the current
		// binding instead of retiring and re-writing the same content.
		out := secretRebindReadback(currentID, currentID, r.GetRuntimeInstanceId(), currentVersion, currentFingerprint, "confirmed", currentID)
		if err = recordSecretRebindIdempotency(ctx, tx, s, idem, setID, out); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, persistenceError(err)
		}
		return out, nil
	}
	// The provider confirms the exact new approved-store Secret. A binding is never
	// written from a caller claim alone, and the raw key is never read into Fabric.
	bound, err := s.Dispatcher.BindSecret(ctx, SecretBindIntent{TenantID: tenant, WorkspaceID: r.GetWorkspaceId(), RuntimeInstanceID: r.GetRuntimeInstanceId(), SecretRef: r.GetSecretDeliveryReference(), Fingerprint: r.GetFingerprint(), TargetSlot: purpose})
	if err != nil {
		return nil, status.Error(codes.Unavailable, "the approved Secret store could not confirm the replacement binding")
	}
	if bound.SecretRef != r.GetSecretDeliveryReference() || bound.Version == "" || bound.Fingerprint == "" || (r.GetFingerprint() != "" && bound.Fingerprint != r.GetFingerprint()) {
		return nil, status.Error(codes.FailedPrecondition, "the approved Secret store returned a different identity")
	}
	bindingID := "sbx_" + shortDigest(tenant, r.GetWorkspaceId(), r.GetRuntimeInstanceId(), purpose, r.GetKeyBindingId())
	// Retire the predecessor first so the partial unique index never sees two
	// active bindings; the unique index and this order together preserve the
	// one-active-binding invariant.
	if _, err = tx.ExecContext(ctx, `UPDATE fabric.secret_bindings SET revoked_at=now(),updated_at=now() WHERE id=$1 AND revoked_at IS NULL`, currentID); err != nil {
		return nil, persistenceError(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO fabric.secret_bindings (id,resource_set_id,execution_resource_id,secret_ref,purpose,version,fingerprint,observation_result) VALUES ($1,$2,$3,$4,$5,$6,$7,'confirmed')`, bindingID, setID, execID, bound.SecretRef, purpose, bound.Version, bound.Fingerprint); err != nil {
		if isUniqueViolation(err) {
			return nil, status.Error(codes.AlreadyExists, "runtime already has an active Secret binding for this purpose")
		}
		return nil, persistenceError(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO fabric.resource_actions (id,resource_set_id,command_id,action,provider_idempotency_key,approved_input,observation_result,evidence_ref) VALUES ($1,$2,$3,'inject_secret',$4,$5,'confirmed',$6)`, "raction_"+shortDigest(bindingID, "rebind"), setID, bindingID, "rebind_secret:"+bindingID, normalized, "secret-rebind://"+currentID+"/"+bindingID); err != nil {
		return nil, persistenceError(err)
	}
	out := secretRebindReadback(currentID, bindingID, r.GetRuntimeInstanceId(), bound.Version, bound.Fingerprint, "confirmed", bindingID)
	if err = recordSecretRebindIdempotency(ctx, tx, s, idem, setID, out); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, persistenceError(err)
	}
	return out, nil
}

// recordSecretRebindIdempotency stores the readback with the replacement so a lost
// response replays the same predecessor and replacement instead of retiring a
// second binding.
func recordSecretRebindIdempotency(ctx context.Context, tx *sql.Tx, s *Service, idem ownerstore.IdempotencyInput, resourceID string, out *api.SecretBindingRebindReadback) error {
	body, err := protojson.Marshal(out)
	if err != nil {
		return status.Error(codes.Internal, "Secret rebind readback cannot be encoded")
	}
	idem.ResourceID = resourceID
	idem.ResponseBody = body
	if err = s.Store.RecordIdempotency(ctx, tx, idem); err != nil {
		return persistenceError(err)
	}
	return nil
}

func secretRebindReadback(previousID, bindingID, runtimeInstanceID, version, fingerprint, result, receiptID string) *api.SecretBindingRebindReadback {
	outcome := api.Observation_OBSERVATION_UNKNOWN
	switch result {
	case "confirmed":
		outcome = api.Observation_OBSERVATION_CONFIRMED
	case "rejected":
		outcome = api.Observation_OBSERVATION_REJECTED
	}
	return &api.SecretBindingRebindReadback{PreviousSecretBindingId: previousID, SecretBindingId: bindingID, RuntimeInstanceId: runtimeInstanceID, Fingerprint: fingerprint, Version: version, Outcome: outcome, ReceiptId: receiptID}
}
