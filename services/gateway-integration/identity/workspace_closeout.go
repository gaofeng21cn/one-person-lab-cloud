package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// The guard spans the external call as well as the local command journal. A
// closeout is admitted after earlier issuance finishes and fences later creation.
func (s *GatewayStore) guardWorkspaceKey(ctx context.Context, tenant, workspace string) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	var acquired bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))`, "gateway.workspace_key:"+tenant+":"+workspace).Scan(&acquired); err != nil {
		tx.Rollback()
		return nil, err
	}
	if !acquired {
		tx.Rollback()
		return nil, status.Error(codes.Aborted, "Workspace issuance is already in flight")
	}
	return tx, nil
}

func (s *GatewayStore) workspaceKeyClosing(ctx context.Context, tenant, workspace string) (bool, error) {
	var closing bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM gateway.idempotency_records WHERE tenant_scope=$1 AND operation_name='DisableManagedKey' AND resource_id=$2 AND idempotency_key=$3)`, tenant, workspace, "close:"+workspace).Scan(&closing)
	return closing, err
}

// The barrier is an existing API command journal entry, not a second Key store.
func (s *GatewayStore) fenceWorkspaceKey(ctx context.Context, call *api.CallContext, workspace, original string) error {
	tenant := call.GetScope().GetTenant().GetTenantId()
	body, _ := json.Marshal(map[string]string{"workspaceId": workspace, "originalCreateIdempotencyKey": original})
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous string
	err = tx.QueryRowContext(ctx, `SELECT request_sha256 FROM gateway.idempotency_records WHERE tenant_scope=$1 AND operation_name='DisableManagedKey' AND resource_id=$2 AND idempotency_key=$3`, tenant, workspace, "close:"+workspace).Scan(&previous)
	if err == nil {
		if previous != ownerstore.HashRequestBody(body) {
			return status.Error(codes.FailedPrecondition, "Workspace key closeout identifies another original command")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err = s.store.RecordIdempotency(ctx, tx, ownerstore.IdempotencyInput{ID: "idem_key_close_" + shortDigest(tenant+":"+workspace), TenantScope: tenant, ActorScope: call.GetActorId(), OperationName: "DisableManagedKey", IdempotencyKey: "close:" + workspace, RequestSHA256: ownerstore.HashRequestBody(body), ResourceID: workspace, ResponseStatus: 202, ResponseBody: body}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) closeWorkspaceManagedKey(ctx context.Context, request *api.ManagedKeyLifecycleCommand) (*api.ManagedKeyLifecycleReadback, error) {
	if err := s.coordinationCaller(ctx, owneridentity.Workspace.Service()); err != nil {
		return nil, err
	}
	if err := ownerservice.ValidateCallContext(ctx, request.GetContext()); err != nil {
		return nil, err
	}
	tenant, workspace := request.GetContext().GetScope().GetTenant().GetTenantId(), strings.TrimSpace(request.GetWorkspaceId())
	if tenant == "" || workspace == "" || s.GatewayStore == nil {
		return nil, status.Error(codes.InvalidArgument, "tenant, Workspace and Gateway persistence are required")
	}
	if err := s.authorizeManagedKey(ctx, request.GetContext(), tenant, workspace); err != nil {
		return nil, err
	}
	guard, err := s.GatewayStore.guardWorkspaceKey(ctx, tenant, workspace)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Workspace key closeout guard unavailable")
	}
	defer guard.Rollback()
	if err = s.authorizeManagedKey(ctx, request.GetContext(), tenant, workspace); err != nil {
		return nil, err
	}
	if err = s.GatewayStore.fenceWorkspaceKey(ctx, request.GetContext(), workspace, request.GetOriginalCreateIdempotencyKey()); err != nil {
		return nil, err
	}
	rows, err := s.GatewayStore.db.QueryContext(ctx, `SELECT COALESCE(resource_id,''),response_status,response_body FROM gateway.idempotency_records WHERE tenant_scope=$1 AND operation_name='CreateManagedKey' AND idempotency_key=$2`, tenant, request.GetOriginalCreateIdempotencyKey())
	if err != nil {
		return nil, status.Error(codes.Unavailable, "original key command unavailable")
	}
	var resource string
	var response int
	var raw []byte
	count := 0
	for rows.Next() {
		count++
		if err = rows.Scan(&resource, &response, &raw); err != nil {
			break
		}
	}
	readErr := rows.Err()
	rows.Close()
	if err != nil || readErr != nil {
		return nil, status.Error(codes.Unavailable, "original key command unreadable")
	}
	if count > 1 {
		return nil, status.Error(codes.DataLoss, "original key command is ambiguous")
	}
	if count == 0 || (response == managedKeyCommandRejectedStatus && resource == workspace) {
		var unmatched bool
		if err = s.GatewayStore.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM gateway.key_bindings WHERE tenant_id=$1 AND workspace_id=$2 AND purpose='workspace_managed' AND revoked_at IS NULL) OR EXISTS(SELECT 1 FROM gateway.idempotency_records WHERE tenant_scope=$1 AND operation_name='CreateManagedKey' AND resource_id=$2 AND response_status=$3 AND idempotency_key<>$4)`, tenant, workspace, managedKeyCommandPendingStatus, request.GetOriginalCreateIdempotencyKey()).Scan(&unmatched); err != nil {
			return nil, status.Error(codes.Unavailable, "Workspace key readback unavailable")
		}
		if unmatched {
			return nil, status.Error(codes.FailedPrecondition, "Workspace has a key outside this original command")
		}
		if request.GetKeyBindingId() != "" {
			return nil, status.Error(codes.DataLoss, "original key command does not confirm the supplied binding")
		}
		return &api.ManagedKeyLifecycleReadback{WorkspaceId: workspace, Outcome: api.Observation_OBSERVATION_CONFIRMED, AbsenceConfirmed: true, UpdatedAt: timestamppb.Now()}, nil
	}
	var state managedKeyCommandState
	if json.Unmarshal(raw, &state) != nil {
		return nil, status.Error(codes.DataLoss, "original key command is invalid")
	}
	if response != managedKeyCommandConfirmedStatus {
		return nil, status.Error(codes.Unavailable, "original key issuance remains unknown")
	}
	binding, err := s.GatewayStore.ReadManagedKeyBinding(ctx, state.KeyBindingID)
	if err != nil || binding.TenantID != tenant || binding.WorkspaceID != workspace || resource != binding.ID || (request.GetKeyBindingId() != "" && request.GetKeyBindingId() != binding.ID) {
		return nil, status.Error(codes.DataLoss, "original key binding differs from the closing Workspace")
	}
	return s.setManagedKeyLifecycle(ctx, &api.ManagedKeyLifecycleCommand{Context: request.GetContext(), WorkspaceId: workspace, KeyBindingId: binding.ID}, api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_DISABLED)
}
