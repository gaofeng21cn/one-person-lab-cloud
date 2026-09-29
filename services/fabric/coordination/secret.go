package coordination

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/lib/pq"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

// BindSecret records that one accepted delivery's Gateway Secret is bound into the
// resource set its runtime actually consumes. The caller supplies only opaque
// identities: the runtime instance, the Gateway key binding and the approved
// Secret delivery reference. Fabric resolves the execution resource from its own
// resource set, has the provider confirm the exact stored Secret, then writes the
// immutable fabric.secret_bindings row. A retry for the same runtime and purpose
// replays that same binding instead of minting a second one; a retry that names a
// different Secret, key binding or resource set is a conflicting original.
func (s *Service) BindSecret(ctx context.Context, r *api.SecretBindingCommand) (*api.SecretBindingReadback, error) {
	if err := peer(ctx, owneridentity.Serve.Service(), owneridentity.Workspace.Service()); err != nil {
		return nil, err
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	if strings.TrimSpace(r.GetWorkspaceId()) == "" || strings.TrimSpace(r.GetRuntimeInstanceId()) == "" || strings.TrimSpace(r.GetKeyBindingId()) == "" || strings.TrimSpace(r.GetSecretDeliveryReference()) == "" || strings.TrimSpace(r.GetContext().GetIdempotencyKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace, runtime instance, key binding, delivery reference and idempotency key are required")
	}
	if s.Dispatcher == nil {
		return nil, status.Error(codes.FailedPrecondition, "no provider secret binding capability is configured")
	}
	tenant := r.GetContext().GetScope().GetTenant().GetTenantId()
	if err := s.authorizeWorkspace(ctx, r.GetContext(), r.GetWorkspaceId(), tenant, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDMANAGEDSECRET); err != nil {
		return nil, err
	}
	input := &api.SecretBindingCommand{WorkspaceId: r.GetWorkspaceId(), RuntimeInstanceId: r.GetRuntimeInstanceId(), KeyBindingId: r.GetKeyBindingId(), SecretDeliveryReference: r.GetSecretDeliveryReference(), TargetSlot: r.GetTargetSlot(), Fingerprint: r.GetFingerprint()}
	body, err := protojson.Marshal(input)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "secret binding command cannot be encoded")
	}
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
	setID, execID, err := s.runtimeExecutionResource(ctx, tx, r.WorkspaceId)
	if err != nil {
		return nil, err
	}
	purpose := strings.TrimSpace(r.GetTargetSlot())
	if purpose == "" {
		purpose = "workspace_gateway_key"
	}
	// Every writer locks the original workspace before reading its binding; a
	// concurrent retry cannot bind a second Secret for the same runtime and purpose.
	var existingID, existingRef, existingVersion, existingFingerprint, existingPurpose, existingResult string
	err = tx.QueryRowContext(ctx, `SELECT id,secret_ref,version,fingerprint,purpose,observation_result FROM fabric.secret_bindings WHERE execution_resource_id=$1 AND purpose=$2 AND revoked_at IS NULL FOR UPDATE`, execID, purpose).Scan(&existingID, &existingRef, &existingVersion, &existingFingerprint, &existingPurpose, &existingResult)
	if err == nil {
		if existingRef != r.GetSecretDeliveryReference() {
			return nil, status.Error(codes.AlreadyExists, "runtime already has a bound Secret for this purpose")
		}
		if err = tx.Commit(); err != nil {
			return nil, persistenceError(err)
		}
		return secretBindingReadback(existingID, r.GetRuntimeInstanceId(), existingVersion, existingFingerprint, existingResult, existingID), nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, persistenceError(err)
	}
	// The provider confirms the exact stored Secret. A binding is never written from
	// a caller claim alone, and the raw key is never read into Fabric.
	bound, err := s.Dispatcher.BindSecret(ctx, SecretBindIntent{TenantID: tenant, WorkspaceID: r.GetWorkspaceId(), RuntimeInstanceID: r.GetRuntimeInstanceId(), SecretRef: r.GetSecretDeliveryReference(), Fingerprint: r.GetFingerprint(), TargetSlot: purpose})
	if err != nil {
		return nil, status.Error(codes.Unavailable, "the approved Secret store could not confirm the binding")
	}
	if bound.SecretRef != r.GetSecretDeliveryReference() || bound.Version == "" || bound.Fingerprint == "" || (r.GetFingerprint() != "" && bound.Fingerprint != r.GetFingerprint()) {
		return nil, status.Error(codes.FailedPrecondition, "the approved Secret store returned a different identity")
	}
	bindingID := "sbx_" + shortDigest(tenant, r.WorkspaceId, r.RuntimeInstanceId, purpose)
	_, err = tx.ExecContext(ctx, `INSERT INTO fabric.secret_bindings (id,resource_set_id,execution_resource_id,secret_ref,purpose,version,fingerprint,observation_result) VALUES ($1,$2,$3,$4,$5,$6,$7,'confirmed')`, bindingID, setID, execID, bound.SecretRef, purpose, bound.Version, bound.Fingerprint)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, status.Error(codes.AlreadyExists, "runtime already has a bound Secret for this purpose")
		}
		return nil, persistenceError(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO fabric.resource_actions (id,resource_set_id,command_id,action,provider_idempotency_key,approved_input,observation_result) VALUES ($1,$2,$3,'inject_secret',$4,$5,'confirmed')`, "raction_"+shortDigest(bindingID, "inject"), setID, bindingID, "inject_secret:"+bindingID, body)
	if err != nil {
		return nil, persistenceError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, persistenceError(err)
	}
	return secretBindingReadback(bindingID, r.GetRuntimeInstanceId(), bound.Version, bound.Fingerprint, "confirmed", bindingID), nil
}

// runtimeExecutionResource resolves the exact resource set and execution resource
// for one Workspace's confirmed delivery. A Workspace with no confirmed resource
// set, or one without an execution resource, has nothing to bind into.
func (s *Service) runtimeExecutionResource(ctx context.Context, tx *sql.Tx, workspaceID string) (string, string, error) {
	var setID, observation string
	if err := tx.QueryRowContext(ctx, `SELECT id,observation_result FROM fabric.resource_sets WHERE workspace_id=$1`, workspaceID).Scan(&setID, &observation); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", status.Error(codes.FailedPrecondition, "the Workspace has no accepted resource set to bind a Secret into")
		}
		return "", "", persistenceError(err)
	}
	if observation != "confirmed" {
		return "", "", status.Error(codes.FailedPrecondition, "the Workspace resources are not confirmed")
	}
	var execID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM fabric.resources WHERE resource_set_id=$1 AND kind='execution'`, setID).Scan(&execID); err == nil {
		return setID, execID, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", "", persistenceError(err)
	}
	// The execution resource is the runtime placement the confirmed resource set
	// produced. Its identity is derived deterministically so a retry does not
	// allocate a second one; the billing mode follows the accepted plan.
	var billing string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(approved_specification->>'billingMode','') FROM fabric.resource_sets WHERE id=$1`, setID).Scan(&billing); err != nil {
		return "", "", persistenceError(err)
	}
	if billing != "PREPAID_MONTHLY" && billing != "LOCAL_NO_CHARGE" {
		return "", "", status.Error(codes.FailedPrecondition, "the Workspace resource set has no approved billing mode")
	}
	execID = "res_exec_" + shortDigest(setID)
	if _, err := tx.ExecContext(ctx, `INSERT INTO fabric.resources (id,resource_set_id,kind,provider_purchase_key,billing_mode,requested_specification,observation_result) VALUES ($1,$2,'execution',$3,$4,'{}'::jsonb,'confirmed') ON CONFLICT (id) DO NOTHING`, execID, setID, "execution:"+execID, billing); err != nil {
		return "", "", persistenceError(err)
	}
	return setID, execID, nil
}

func isUniqueViolation(err error) bool {
	var p *pq.Error
	return errors.As(err, &p) && p.Code == "23505"
}

func secretBindingReadback(id, runtimeInstanceID, version, fingerprint, result, receiptID string) *api.SecretBindingReadback {
	outcome := api.Observation_OBSERVATION_UNKNOWN
	switch result {
	case "confirmed":
		outcome = api.Observation_OBSERVATION_CONFIRMED
	case "rejected":
		outcome = api.Observation_OBSERVATION_REJECTED
	}
	return &api.SecretBindingReadback{SecretBindingId: id, RuntimeInstanceId: runtimeInstanceID, Fingerprint: fingerprint, Version: version, Outcome: outcome, ReceiptId: receiptID}
}
