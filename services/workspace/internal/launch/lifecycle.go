package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

// Suspension and authorized recovery are two Workspace lifecycle events that
// move one Workspace's standing and, with it, the exact managed key the launch
// delivered:
//
//   - suspension closes the entitlement at a confirmed boundary (an unpaid
//     period end or a provider-confirmed loss of the original resources) and
//     then asks the Gateway owner to reversibly disable that binding;
//   - authorized recovery re-enables the same binding before it restores the
//     Workspace, because the customer's original order is still the one that
//     owns the key.
//
// Both are this owner's own decisions, and both reuse the owner route the
// deletion established instead of introducing a second key writer: this owner
// resolves the exact live binding from its own confirmed records, names it to
// Gateway Integration under the original launch grant, and records the typed
// answer. It never mints, repurchases or rewrites a key, never disables a key it
// cannot name, and never turns a refusal or an unanswered call into a claimed
// key state. A key answer that is not confirmed is a recorded fact of this
// event; it never blocks deletion, settlement or the recovery of the Workspace
// itself.
//
// Each event is a durable operation of this owner, so the two triggers that can
// fire on the same Workspace at the same time (the expiry sweep and the provider
// resource reconcile) serialize on one event identity and one idempotency key
// instead of sending two effects for one binding.
const (
	// lifecycleSuspend and lifecycleResume are this owner's operation kinds. They
	// are internal lifecycle events: no product RPC accepts them from a caller.
	lifecycleSuspend = "suspend_workspace"
	lifecycleResume  = "resume_workspace"

	// The contract's own stage vocabulary names both transitions.
	lifecycleStageSuspension = "provider_suspend"
	lifecycleStageResumption = "provider_resume"
	lifecycleStageSuperseded = "reconciliation"

	// lifecycleStep is the one managed-key step of every lifecycle event. Its
	// idempotency key is the step identity the Gateway owner replays.
	lifecycleStep = "apply_managed_key_lifecycle"

	lifecycleReasonUnpaidExpiry   = "unpaid_expiry"
	lifecycleReasonResourceAbsent = "provider_resource_absent"

	lifecycleTriggerExpiry    = "expiry_sweep"
	lifecycleTriggerReconcile = "resource_reconcile"
	lifecycleTriggerRecovery  = "paid_period_recovery"

	// The typed outcome vocabulary of one lifecycle key effect. It is the same
	// vocabulary the deletion closeout records, so a refusal, a lost answer and a
	// missing binding are never confused with a confirmed key state.
	lifecycleConfirmed     = "confirmed"
	lifecycleRejected      = "rejected"
	lifecycleUnknown       = "unknown"
	lifecycleNotApplicable = "not_applicable"

	// A non-confirmed answer is retried a bounded number of times and then left
	// visible as needs_attention. The Workspace's own standing never waits for it.
	lifecycleRecoveryLimit = 20
	lifecycleSweepLimit    = 20
)

// lifecycleRetryDelay spaces the bounded retries of an unanswered or refused key
// effect so a permanently unavailable dependency is not replayed every pass.
const lifecycleRetryDelay = 15 * time.Second

// lifecycleOrder is the committed input of one lifecycle event. It names the
// launch whose grant authorizes the continuation and the boundary that produced
// the event; it never carries a key, a secret or a session.
type lifecycleOrder struct {
	LaunchOperationID string `json:"launchOperationId"`
	GrantID           string `json:"grantId,omitempty"`
	Reason            string `json:"reason"`
	Trigger           string `json:"trigger"`
	SubscriptionID    string `json:"subscriptionId,omitempty"`
	PeriodEnd         string `json:"periodEnd,omitempty"`
	ResourceSetID     string `json:"resourceSetId,omitempty"`
}

// lifecycleResult is the durable outcome of one lifecycle event. The managed key
// fields are the typed record of what the Gateway owner answered, independent of
// whether the event itself landed.
type lifecycleResult struct {
	PendingAttempts     int    `json:"pendingAttempts,omitempty"`
	ManagedKeyBindingID string `json:"managedKeyBindingId,omitempty"`
	KeyLifecycleAction  string `json:"keyLifecycleAction,omitempty"`
	KeyLifecycleOutcome string `json:"keyLifecycleOutcome,omitempty"`
	KeyLifecycleCode    string `json:"keyLifecycleCode,omitempty"`
	KeyLifecycleVersion int64  `json:"keyLifecycleVersion,omitempty"`
	KeyLifecycleKey     string `json:"keyLifecycleIdempotencyKey,omitempty"`
	KeyReadOutcome      string `json:"keyReadOutcome,omitempty"`
	SuspendedAt         string `json:"suspendedAt,omitempty"`
	ResumedAt           string `json:"resumedAt,omitempty"`
}

// lifecycleEffect is one answer of the Gateway owner, before it is recorded.
type lifecycleEffect struct {
	Outcome string
	Code    string
	Action  string
	Binding string
	Version int64
	// Terminal reports an answer that no retry can change: a revoked binding has
	// no reversible lifecycle state, so neither direction may claim it.
	Terminal bool
}

// workspaceStanding is this owner's own confirmed view of one Workspace: its
// stored standing, the confirmed paid period that bounds it, and the succeeded
// launch whose granted continuation authorizes its lifecycle effects.
type workspaceStanding struct {
	Status          string
	TenantID        string
	SubscriptionID  string
	PeriodStart     sql.NullTime
	PeriodEnd       sql.NullTime
	PeriodConfirmed bool
	Launch          ownerstore.Operation
	LaunchGrantID   string
	ResourceSetID   string
}

// paidTerm reports whether the confirmed paid period contains now.
func (s workspaceStanding) paidTerm(now time.Time) bool {
	return s.PeriodConfirmed && s.PeriodStart.Valid && s.PeriodEnd.Valid &&
		!now.Before(s.PeriodStart.Time) && now.Before(s.PeriodEnd.Time)
}

// expiredTerm reports whether the confirmed paid period ended without a
// confirmed successor.
func (s workspaceStanding) expiredTerm(now time.Time) bool {
	return s.PeriodConfirmed && s.PeriodEnd.Valid && !now.Before(s.PeriodEnd.Time)
}

// lifecycleNow is the single clock of the lifecycle worker. Tests set accessNow
// so a boundary is decided by the owner's own facts, not by wall time.
func (s *Service) lifecycleNow() time.Time {
	if s.accessNow != nil {
		return s.accessNow().UTC()
	}
	return time.Now().UTC()
}

// readWorkspaceStanding reads the Workspace rows this owner already owns. A
// confirmed period is the same predicate ReadWorkspaceAccess uses, so suspension
// can never be decided on a term Serve would not honor.
func (s *Service) readWorkspaceStanding(ctx context.Context, workspaceID string) (workspaceStanding, error) {
	return readWorkspaceStandingFrom(ctx, s.Store.DB(), workspaceID)
}

// standingQuerier is the narrow read surface both the worker and the committed
// event transaction use, so the decision and the durable record read the same
// facts.
type standingQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func readWorkspaceStandingFrom(ctx context.Context, db standingQuerier, workspaceID string) (workspaceStanding, error) {
	var st workspaceStanding
	err := db.QueryRowContext(ctx, `SELECT w.status,COALESCE(w.tenant_id,''),COALESCE(s.id,''),
		s.current_period_start,s.current_period_end,
		EXISTS(SELECT 1 FROM workspace.subscription_periods p WHERE p.subscription_id=s.id
		AND p.period_start=s.current_period_start AND p.period_end=s.current_period_end
		AND p.charge_wallet_operation_id=s.last_charge_wallet_operation_id
		AND COALESCE(p.charge_receipt_id,'')<>'')
		FROM workspace.workspaces w LEFT JOIN workspace.subscriptions s ON s.workspace_id=w.id
		WHERE w.id=$1`, workspaceID).Scan(&st.Status, &st.TenantID, &st.SubscriptionID, &st.PeriodStart, &st.PeriodEnd, &st.PeriodConfirmed)
	if err != nil {
		return st, dbError(err)
	}
	var raw []byte
	var launch orderResult
	err = db.QueryRowContext(ctx, `SELECT id,COALESCE(tenant_id,''),actor_id,request_id,COALESCE(result,'{}'::jsonb)
		FROM workspace.operations WHERE resource_id=$1 AND kind='create_workspace' AND status='succeeded'
		ORDER BY created_at DESC,id DESC LIMIT 1`, workspaceID).Scan(&st.Launch.ID, &st.Launch.TenantID, &st.Launch.ActorID, &st.Launch.RequestID, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return st, dbError(err)
	}
	st.Launch.Kind, st.Launch.ResourceID = "create_workspace", workspaceID
	if json.Unmarshal(raw, &launch) != nil {
		return st, status.Error(codes.DataLoss, "stored Workspace launch result is invalid")
	}
	st.LaunchGrantID, st.ResourceSetID = launch.GrantID, launch.ResourceSetID
	return st, nil
}

// lifecycleApplicable decides whether one event still describes this Workspace's
// standing. It is re-read under the same lock that creates the event, so a
// deletion or a concurrent recovery cannot be overtaken by a stale trigger.
func lifecycleApplicable(kind, reason string, st workspaceStanding, now time.Time) bool {
	if st.Launch.ID == "" {
		return false
	}
	switch kind {
	case lifecycleSuspend:
		if st.Status != "active" {
			return false
		}
		switch reason {
		case lifecycleReasonUnpaidExpiry:
			return st.expiredTerm(now)
		case lifecycleReasonResourceAbsent:
			return st.ResourceSetID != ""
		}
		return false
	case lifecycleResume:
		// Recovery is only an authorized recovery of a still-recoverable
		// Workspace: it must still be suspended, it must still own its original
		// resources, and the customer must hold a confirmed paid term again.
		return st.Status == "suspended" && st.paidTerm(now) && st.ResourceSetID != ""
	}
	return false
}

// openLifecycleOperation returns the identity of the one open event of its kind
// for this Workspace, creating it when the trigger still describes the standing.
// The advisory lock serializes the triggers, so they share one event and one
// idempotency key instead of sending two effects for one binding.
func (s *Service) openLifecycleOperation(ctx context.Context, workspaceID, kind, reason, trigger string) (string, error) {
	now := s.lifecycleNow()
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return "", dbError(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "workspace.lifecycle:"+workspaceID); err != nil {
		return "", dbError(err)
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT id FROM workspace.operations WHERE resource_id=$1 AND kind=$2
		AND status IN ('accepted','running','awaiting_confirmation','needs_attention')
		ORDER BY created_at DESC,id DESC LIMIT 1`, workspaceID, kind).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", dbError(err)
	}
	st, err := readWorkspaceStandingFrom(ctx, tx, workspaceID)
	if err != nil {
		return "", err
	}
	if !lifecycleApplicable(kind, reason, st, now) {
		return "", nil
	}
	stage, request := lifecycleStageSuspension, trigger+"/"+reason+"/"+workspaceID
	if kind == lifecycleResume {
		stage, request = lifecycleStageResumption, trigger+"/resume/"+workspaceID
	}
	order := lifecycleOrder{LaunchOperationID: st.Launch.ID, GrantID: st.LaunchGrantID, Reason: reason, Trigger: trigger,
		SubscriptionID: st.SubscriptionID, ResourceSetID: st.ResourceSetID}
	if st.PeriodEnd.Valid {
		order.PeriodEnd = st.PeriodEnd.Time.UTC().Format(time.RFC3339Nano)
	}
	accepted, err := json.Marshal(order)
	if err != nil {
		return "", status.Error(codes.Internal, "Workspace lifecycle event cannot be encoded")
	}
	op, err := s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{
		ID: id("operation_"), TenantID: st.TenantID, ActorID: st.Launch.ActorID, Kind: kind,
		ResourceID: workspaceID, Stage: stage, RequestID: request, AcceptedInput: accepted,
	})
	if err != nil {
		return "", dbError(err)
	}
	if err = tx.Commit(); err != nil {
		return "", dbError(err)
	}
	return op.ID, nil
}

// eventLaunchOperation returns the launch the committed event names. The event
// freezes its authority when it is accepted, so a later launch for the same
// Workspace cannot silently change the actor, tenant or grant of this effect.
func (s *Service) eventLaunchOperation(ctx context.Context, st workspaceStanding, order lifecycleOrder) (ownerstore.Operation, error) {
	if order.LaunchOperationID == "" || order.LaunchOperationID == st.Launch.ID {
		return st.Launch, nil
	}
	launch := st.Launch
	var raw []byte
	err := s.Store.DB().QueryRowContext(ctx, `SELECT id,COALESCE(tenant_id,''),actor_id,request_id,kind,resource_id,COALESCE(result,'{}'::jsonb)
		FROM workspace.operations WHERE id=$1 AND kind='create_workspace' AND status='succeeded'`, order.LaunchOperationID).
		Scan(&launch.ID, &launch.TenantID, &launch.ActorID, &launch.RequestID, &launch.Kind, &launch.ResourceID, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ownerstore.Operation{}, status.Error(codes.FailedPrecondition, "the accepted Workspace lifecycle event has no delivered launch")
	}
	if err != nil {
		return ownerstore.Operation{}, dbError(err)
	}
	return launch, nil
}

// lifecycleContinuation builds the call context of one lifecycle effect. The
// authority is the original launch's own grant, actor and tenant scope: suspension
// and recovery continue the accepted order that issued the key instead of asking
// a background worker to invent an authorization of its own. The identity and the
// idempotency key belong to this event, so one binding never receives two
// differently-keyed effects.
func lifecycleContinuation(launch, event ownerstore.Operation, grant, step string) *api.CallContext {
	return &api.CallContext{
		RequestId:                event.RequestID + ":" + step,
		IdempotencyKey:           event.ID + ":" + step,
		ActorId:                  launch.ActorID,
		AcceptedOperationGrantId: proto.String(grant),
		Scope:                    &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: launch.TenantID}}},
	}
}

// suspendWorkspaceLifecycle records one suspension event and advances it. The
// entitlement closes first, exactly as the target state machine requires: the
// Workspace's own standing and its refused access never wait for the key owner.
func (s *Service) suspendWorkspaceLifecycle(ctx context.Context, workspaceID, reason, trigger string) (string, error) {
	operationID, err := s.openLifecycleOperation(ctx, workspaceID, lifecycleSuspend, reason, trigger)
	if err != nil || operationID == "" {
		return operationID, err
	}
	return operationID, s.applyWorkspaceLifecycle(ctx, operationID)
}

// resumeWorkspaceLifecycle records one authorized recovery and advances it. A
// recovery that is no longer justified by the owner's own facts records
// not_applicable instead of reopening a Workspace.
func (s *Service) resumeWorkspaceLifecycle(ctx context.Context, workspaceID string) (string, error) {
	operationID, err := s.openLifecycleOperation(ctx, workspaceID, lifecycleResume, "", lifecycleTriggerRecovery)
	if err != nil || operationID == "" {
		return operationID, err
	}
	return operationID, s.applyWorkspaceLifecycle(ctx, operationID)
}

// applyWorkspaceLifecycle continues one committed lifecycle event under its own
// worker lease.
func (s *Service) applyWorkspaceLifecycle(ctx context.Context, operationID string) error {
	op, err := s.Store.ReadOperation(ctx, operationID)
	if err != nil {
		return dbError(err)
	}
	if op.Terminal() || op.Status == "needs_attention" {
		// A spent attempt budget is a visible, frozen state: the sweep does not
		// reopen it and the worker does not replay it on every pass.
		return nil
	}
	token := id("lease_")
	var leased string
	err = s.Store.DB().QueryRowContext(ctx, `UPDATE workspace.operations SET worker_lease_token=$2,worker_lease_until=now()+interval '60 seconds'
		WHERE id=$1 AND status NOT IN ('succeeded','failed','cancelled') AND (worker_lease_until IS NULL OR worker_lease_until<now()) RETURNING id`, operationID, token).Scan(&leased)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return dbError(err)
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer releaseCancel()
		_, _ = s.Store.DB().ExecContext(releaseCtx, `UPDATE workspace.operations SET worker_lease_token=NULL,worker_lease_until=NULL WHERE id=$1 AND worker_lease_token=$2`, operationID, token)
	}()
	op, err = s.Store.ReadOperation(ctx, operationID)
	if err != nil {
		return dbError(err)
	}
	var order lifecycleOrder
	var result lifecycleResult
	if json.Unmarshal(op.AcceptedInput, &order) != nil || (len(op.Result) > 0 && json.Unmarshal(op.Result, &result) != nil) {
		return status.Error(codes.DataLoss, "stored Workspace lifecycle event is invalid")
	}
	switch op.Kind {
	case lifecycleSuspend:
		return s.applySuspension(ctx, op, token, order, result)
	case lifecycleResume:
		return s.applyRecovery(ctx, op, token, order, result)
	}
	return status.Error(codes.FailedPrecondition, "operation is not a Workspace lifecycle event")
}

// applySuspension closes the Workspace's standing and then asks the Gateway owner
// for the reversible disable of the exact binding. The two are separate facts: a
// refused or unanswered key answer is recorded as this event's own outcome, never
// converted into a disabled key, and never leaves the entitlement open.
func (s *Service) applySuspension(ctx context.Context, op ownerstore.Operation, token string, order lifecycleOrder, result lifecycleResult) error {
	st, err := s.readWorkspaceStanding(ctx, op.ResourceID)
	if err != nil {
		return err
	}
	switch st.Status {
	case "active", "suspended":
		if st.Status == "active" {
			// Suspend first: the Workspace's own standing records that it no
			// longer offers its obligation, before any external owner is asked
			// anything. Its access readback already refuses a non-active
			// Workspace, so Serve observes the closure immediately.
			if _, err = s.Store.DB().ExecContext(ctx, `UPDATE workspace.workspaces SET status='suspended',version=version+1,updated_at=now()
				WHERE id=$1 AND status='active'`, op.ResourceID); err != nil {
				return dbError(err)
			}
		}
		if result.SuspendedAt == "" {
			result.SuspendedAt = s.lifecycleNow().Format(time.RFC3339Nano)
		}
	default:
		// A deleting, deleted or failed Workspace is no longer suspended by this
		// event; its own closure owns whatever key remains.
		result.KeyLifecycleAction, result.KeyLifecycleOutcome, result.KeyLifecycleCode = "none", lifecycleNotApplicable, "WORKSPACE_NOT_ACTIVE"
		return s.checkpointLifecycle(ctx, op, token, lifecycleStageSuperseded, lifecycleSucceeded, lifecycleConfirmed, result)
	}
	launch, err := s.eventLaunchOperation(ctx, st, order)
	if err != nil {
		return err
	}
	effect, err := s.applyManagedKeyLifecycle(ctx, op, token, launch, order, &result, api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_DISABLED)
	if err != nil {
		return err
	}
	result.KeyLifecycleAction, result.KeyLifecycleOutcome, result.KeyLifecycleCode = effect.Action, effect.Outcome, effect.Code
	switch effect.Outcome {
	case lifecycleConfirmed, lifecycleNotApplicable, lifecycleRejected:
		// The suspension itself is settled. A definite refusal is a recorded fact
		// of this event and repeating the same command cannot change the Gateway
		// owner's decision, so the event completes with that answer instead of
		// claiming a key state it does not have.
		return s.checkpointLifecycle(ctx, op, token, lifecycleStageSuspension, lifecycleSucceeded, effect.Outcome, result)
	default:
		// An unanswered call leaves the event open and is retried with the
		// Gateway's authoritative state re-read first; the Workspace stays
		// suspended meanwhile and nothing downstream waits for it.
		return s.checkpointLifecycle(ctx, op, token, lifecycleStageSuspension, lifecycleAwaiting, lifecycleUnknown, result)
	}
}

// applyRecovery re-enables the Workspace's own binding before it restores the
// Workspace. The key is read first, so a prior unknown outcome is settled against
// the Gateway's authoritative state instead of a second blind command, and a
// revoked binding stays terminal instead of being replaced by a new key.
func (s *Service) applyRecovery(ctx context.Context, op ownerstore.Operation, token string, order lifecycleOrder, result lifecycleResult) error {
	st, err := s.readWorkspaceStanding(ctx, op.ResourceID)
	if err != nil {
		return err
	}
	now := s.lifecycleNow()
	if st.Status != "suspended" || !st.paidTerm(now) {
		result.KeyLifecycleAction, result.KeyLifecycleOutcome, result.KeyLifecycleCode = "none", lifecycleNotApplicable, "RECOVERY_NOT_APPLICABLE"
		return s.checkpointLifecycle(ctx, op, token, lifecycleStageSuperseded, lifecycleSucceeded, lifecycleConfirmed, result)
	}
	launch, err := s.eventLaunchOperation(ctx, st, order)
	if err != nil {
		return err
	}
	effect, err := s.applyManagedKeyLifecycle(ctx, op, token, launch, order, &result, api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_ACTIVE)
	if err != nil {
		return err
	}
	result.KeyLifecycleAction, result.KeyLifecycleOutcome, result.KeyLifecycleCode = effect.Action, effect.Outcome, effect.Code
	switch {
	case effect.Outcome == lifecycleConfirmed || effect.Outcome == lifecycleNotApplicable:
		// Recovery restores the Workspace only after the original binding is
		// confirmed usable again, so access never reopens without its key.
		if _, err = s.Store.DB().ExecContext(ctx, `UPDATE workspace.workspaces SET status='active',version=version+1,updated_at=now()
			WHERE id=$1 AND status='suspended'`, op.ResourceID); err != nil {
			return dbError(err)
		}
		if result.ResumedAt == "" {
			result.ResumedAt = now.Format(time.RFC3339Nano)
		}
		return s.checkpointLifecycle(ctx, op, token, lifecycleStageResumption, lifecycleSucceeded, lifecycleConfirmed, result)
	case effect.Terminal:
		// A revoked binding cannot be revived and this owner will not issue a
		// replacement, so the recovery ends as a recorded terminal refusal that
		// stays visible for an operator; the Workspace stays suspended instead of
		// reopening without a credential.
		return s.checkpointLifecycle(ctx, op, token, lifecycleStageResumption, "needs_attention", lifecycleRejected, result)
	default:
		return s.checkpointLifecycle(ctx, op, token, lifecycleStageResumption, lifecycleAwaiting, lifecycleUnknown, result)
	}
}

// applyManagedKeyLifecycle asks the Gateway owner for one reversible state of the
// exact binding this Workspace still owns and records its typed answer.
//
// The Gateway's state is read before anything is written: an already-desired
// binding is confirmed without a second effect, a revoked binding is terminal,
// and an unreadable binding is unknown. That is what lets an event that lost its
// answer recover by re-reading instead of replaying a different effect, and what
// keeps two concurrent triggers from sending two effects for one binding.
func (s *Service) applyManagedKeyLifecycle(ctx context.Context, op ownerstore.Operation, token string, launch ownerstore.Operation, order lifecycleOrder, result *lifecycleResult, desired api.ManagedKeyLifecycleState) (lifecycleEffect, error) {
	bindingID, unresolved, err := s.managedKeyBindingForCloseout(ctx, op.ResourceID)
	if err != nil {
		return lifecycleEffect{}, status.Error(codes.DataLoss, "Stored Workspace managed key binding is invalid")
	}
	if unresolved {
		return lifecycleEffect{Action: "none", Outcome: lifecycleUnknown, Code: "KEY_BINDING_UNRESOLVED"}, nil
	}
	if bindingID == "" {
		// This Workspace never obtained a managed key, so there is nothing to
		// suspend or restore.
		return lifecycleEffect{Action: "none", Outcome: lifecycleNotApplicable, Code: "KEY_BINDING_ABSENT"}, nil
	}
	result.ManagedKeyBindingID, result.KeyLifecycleKey = bindingID, op.ID+":"+lifecycleStep
	if s.Gateway == nil {
		return lifecycleEffect{Action: "none", Binding: bindingID, Outcome: lifecycleUnknown, Code: "DEPENDENCY_UNAVAILABLE"}, nil
	}
	readStep := "read_managed_key_lifecycle"
	read := &api.ManagedKeyLifecycleCommand{Context: lifecycleContinuation(launch, op, order.GrantID, readStep), KeyBindingId: bindingID, WorkspaceId: op.ResourceID}
	if err = s.beginStep(ctx, op, token, readStep, 1, "gateway", lifecycleStageOf(op.Kind), read); err != nil {
		return lifecycleEffect{}, err
	}
	observed, err := s.Gateway.ReadManagedKeyLifecycle(ctx, read)
	if err != nil {
		outcome, code := managedKeyCloseoutFailure(err)
		result.KeyReadOutcome = "unknown"
		return lifecycleEffect{Action: "read", Binding: bindingID, Outcome: outcome, Code: code}, nil
	}
	result.KeyReadOutcome = lifecycleConfirmed
	if observed.GetKeyBindingId() != bindingID || observed.GetWorkspaceId() != op.ResourceID {
		return lifecycleEffect{Action: "read", Binding: bindingID, Outcome: lifecycleUnknown, Code: "KEY_LIFECYCLE_UNIDENTIFIED"}, nil
	}
	switch observed.GetState() {
	case api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_REVOKED:
		// Revocation is terminal in both directions: the binding cannot be
		// disabled or re-enabled, and a replacement key is never issued here.
		return lifecycleEffect{Action: "read", Binding: bindingID, Outcome: lifecycleRejected, Code: "KEY_REVOKED", Version: observed.GetLifecycleVersion(), Terminal: true}, nil
	case desired:
		effect := lifecycleEffect{Action: "readback", Binding: bindingID, Outcome: lifecycleConfirmed, Version: observed.GetLifecycleVersion()}
		if desired != api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_ACTIVE {
			return effect, nil
		}
		return s.confirmManagedKeyReadable(ctx, op, launch, order, bindingID, effect)
	}
	command := &api.ManagedKeyLifecycleCommand{Context: lifecycleContinuation(launch, op, order.GrantID, lifecycleStep), KeyBindingId: bindingID, WorkspaceId: op.ResourceID}
	if err = s.beginStep(ctx, op, token, lifecycleStep, 2, "gateway", lifecycleStageOf(op.Kind), command); err != nil {
		return lifecycleEffect{}, err
	}
	action := "disable"
	var readback *api.ManagedKeyLifecycleReadback
	if desired == api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_ACTIVE {
		action = "enable"
		readback, err = s.Gateway.EnableManagedKey(ctx, command)
	} else {
		readback, err = s.Gateway.DisableManagedKey(ctx, command)
	}
	if err != nil {
		outcome, code := managedKeyCloseoutFailure(err)
		return lifecycleEffect{Action: action, Binding: bindingID, Outcome: outcome, Code: code}, nil
	}
	if readback.GetKeyBindingId() != bindingID || readback.GetWorkspaceId() != op.ResourceID ||
		readback.GetState() != desired || readback.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED {
		return lifecycleEffect{Action: action, Binding: bindingID, Outcome: lifecycleUnknown, Code: "KEY_LIFECYCLE_UNCONFIRMED"}, nil
	}
	effect := lifecycleEffect{Action: action, Binding: bindingID, Outcome: lifecycleConfirmed, Version: readback.GetLifecycleVersion()}
	if desired != api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_ACTIVE {
		return effect, nil
	}
	return s.confirmManagedKeyReadable(ctx, op, launch, order, bindingID, effect)
}

// confirmManagedKeyReadable requires the restored binding to serve the original
// operation again before recovery restores the Workspace: the key owner's read
// surface refuses a revoked or disabled binding, so one successful read of the
// same binding proves the credential the launch delivered is usable again and
// that no replacement key was introduced. A binding that cannot be read is an
// unanswered recovery, never a restored Workspace.
func (s *Service) confirmManagedKeyReadable(ctx context.Context, op ownerstore.Operation, launch ownerstore.Operation, order lifecycleOrder, bindingID string, effect lifecycleEffect) (lifecycleEffect, error) {
	if s.Gateway == nil {
		return lifecycleEffect{Action: effect.Action, Binding: bindingID, Outcome: lifecycleUnknown, Code: "DEPENDENCY_UNAVAILABLE"}, nil
	}
	step := "verify_managed_key_readable"
	read := &api.ManagedKeyReadCommand{Context: lifecycleContinuation(launch, op, order.GrantID, step), KeyBindingId: bindingID, WorkspaceId: op.ResourceID}
	readable, err := s.Gateway.ReadManagedKey(ctx, read)
	if err != nil {
		outcome, code := managedKeyCloseoutFailure(err)
		return lifecycleEffect{Action: effect.Action, Binding: bindingID, Outcome: outcome, Code: "KEY_NOT_READABLE_" + code, Version: effect.Version}, nil
	}
	if readable.GetKeyBindingId() != bindingID || readable.GetWorkspaceId() != op.ResourceID {
		return lifecycleEffect{Action: effect.Action, Binding: bindingID, Outcome: lifecycleUnknown, Code: "KEY_READBACK_UNIDENTIFIED"}, nil
	}
	return effect, nil
}

const (
	lifecycleSucceeded = "succeeded"
	lifecycleAwaiting  = "awaiting_confirmation"
)

func lifecycleStageOf(kind string) string {
	if kind == lifecycleResume {
		return lifecycleStageResumption
	}
	return lifecycleStageSuspension
}

// checkpointLifecycle persists one fenced lifecycle outcome under this event's
// own operation identity. It deliberately does not touch the Workspace status
// beyond the transition the caller already made, so an event can never resurrect
// or re-close a Workspace as a side effect of writing its result.
func (s *Service) checkpointLifecycle(ctx context.Context, op ownerstore.Operation, token, stage, state, observation string, result lifecycleResult) error {
	result.KeyLifecycleKey = op.ID + ":" + lifecycleStep
	if state == lifecycleAwaiting {
		result.PendingAttempts++
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return status.Error(codes.Internal, "Workspace lifecycle result cannot be encoded")
	}
	stepObservation := result.KeyLifecycleOutcome
	if stepObservation == "" {
		stepObservation = observation
	}
	if state == lifecycleAwaiting && result.PendingAttempts >= lifecycleRecoveryLimit {
		// The bounded budget is spent. The event stays visible for an operator and
		// stops consuming attempts; the Workspace's own standing is unaffected.
		state = "needs_attention"
	}
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return dbError(err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE workspace.operations SET result=$3,status=$4,stage=$5,observation_result=$6,
		error_code=NULLIF($7,''),updated_at=now(),completed_at=CASE WHEN $4 IN ('succeeded','failed','cancelled') THEN now() ELSE NULL END
		WHERE id=$1 AND worker_lease_token=$2 AND worker_lease_until>now() AND status NOT IN ('succeeded','failed','cancelled')`,
		op.ID, token, raw, state, stage, observation, result.KeyLifecycleCode)
	if err = fenced(res, err); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE workspace.saga_steps SET observation_result=$3,owner_result_ref=NULLIF($4,''),error_code=NULLIF($5,''),
		confirmed_at=CASE WHEN $3='confirmed' THEN now() ELSE NULL END,
		next_attempt_at=CASE WHEN $3 IN ('unknown','rejected') THEN now()+make_interval(secs=>$6) ELSE NULL END,updated_at=now()
		WHERE operation_id=$1 AND step_key=$2`,
		op.ID, lifecycleStep, stepObservation, result.ManagedKeyBindingID, result.KeyLifecycleCode, lifecycleRetryDelay.Seconds())
	if err != nil {
		return dbError(err)
	}
	// The readiness read is this event's own step too. It settles with the event
	// instead of staying unknown forever.
	readObservation := result.KeyReadOutcome
	if readObservation == "" {
		readObservation = lifecycleUnknown
	}
	_, err = tx.ExecContext(ctx, `UPDATE workspace.saga_steps SET observation_result=$3,
		confirmed_at=CASE WHEN $3='confirmed' THEN now() ELSE NULL END,next_attempt_at=NULL,updated_at=now()
		WHERE operation_id=$1 AND step_key=$2`,
		op.ID, "read_managed_key_lifecycle", readObservation)
	if err != nil {
		return dbError(err)
	}
	if err = tx.Commit(); err != nil {
		return dbError(err)
	}
	return nil
}

// sweepWorkspaceLifecycle advances the Workspace owner's own lifecycle facts: a
// confirmed paid term that ended suspends the Workspace, a provider-confirmed
// loss of the original resources suspends it, and a Workspace whose customer
// holds a confirmed paid term again recovers its original key and standing.
func (s *Service) sweepWorkspaceLifecycle(ctx context.Context) error {
	now := s.lifecycleNow()
	var failures []error
	if err := s.suspendExpiredWorkspaces(ctx, now); err != nil {
		failures = append(failures, err)
	}
	if s.providerReconcileDue(now) {
		if err := s.suspendLostWorkspaces(ctx, now); err != nil {
			failures = append(failures, err)
		}
	}
	if err := s.recoverRestoredWorkspaces(ctx, now); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

// providerReconcileInterval is how often this owner re-reads provider resources
// for its active Workspaces. Recovery is not delayed by it: it reads only the
// Workspaces that are already candidates for recovery.
const providerReconcileInterval = 10 * time.Minute

// providerReconcileDue reports whether the provider reconcile may run on this
// pass and records that it did.
func (s *Service) providerReconcileDue(now time.Time) bool {
	if !s.providerReconciledAt.IsZero() && now.Sub(s.providerReconciledAt) < providerReconcileInterval {
		return false
	}
	s.providerReconciledAt = now
	return true
}

// suspendExpiredWorkspaces suspends Workspaces whose confirmed paid period ended
// without a confirmed successor. It never reads a quote, a pending renewal or a
// provider state: an unpaid term is the owner's own confirmed fact.
func (s *Service) suspendExpiredWorkspaces(ctx context.Context, now time.Time) error {
	rows, err := s.Store.DB().QueryContext(ctx, `SELECT w.id FROM workspace.workspaces w
		JOIN workspace.subscriptions s ON s.workspace_id=w.id
		WHERE w.status='active' AND s.current_period_end <= $1
		AND EXISTS (SELECT 1 FROM workspace.operations l WHERE l.resource_id=w.id AND l.kind='create_workspace' AND l.status='succeeded')
		AND NOT EXISTS (SELECT 1 FROM workspace.operations o WHERE o.resource_id=w.id AND o.kind IN ('suspend_workspace','resume_workspace')
			AND o.status IN ('accepted','running','awaiting_confirmation'))
		ORDER BY s.current_period_end,w.id LIMIT $2`, now, lifecycleSweepLimit)
	if err != nil {
		return dbError(err)
	}
	ids, err := scanIDs(rows)
	if err != nil {
		return err
	}
	var failures []error
	for _, workspaceID := range ids {
		if _, err = s.suspendWorkspaceLifecycle(ctx, workspaceID, lifecycleReasonUnpaidExpiry, lifecycleTriggerExpiry); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// suspendLostWorkspaces suspends a Workspace whose original resource set Fabric
// confirms absent. An unanswered or present readback is a wait, never a
// suspension, so a provider outage can never close a customer's Workspace.
func (s *Service) suspendLostWorkspaces(ctx context.Context, now time.Time) error {
	if s.Fabric == nil {
		return nil
	}
	rows, err := s.Store.DB().QueryContext(ctx, `SELECT w.id FROM workspace.workspaces w
		JOIN workspace.operations l ON l.resource_id=w.id AND l.kind='create_workspace' AND l.status='succeeded'
		WHERE w.status='active'
		AND NOT EXISTS (SELECT 1 FROM workspace.operations o WHERE o.resource_id=w.id AND o.kind IN ('suspend_workspace','resume_workspace')
			AND o.status IN ('accepted','running','awaiting_confirmation'))
		ORDER BY l.created_at,w.id LIMIT $1`, lifecycleSweepLimit)
	if err != nil {
		return dbError(err)
	}
	ids, err := scanIDs(rows)
	if err != nil {
		return err
	}
	var failures []error
	for _, workspaceID := range ids {
		st, readErr := s.readWorkspaceStanding(ctx, workspaceID)
		if readErr != nil {
			failures = append(failures, readErr)
			continue
		}
		if st.ResourceSetID == "" || st.Launch.ID == "" {
			continue
		}
		readback, fabricErr := s.Fabric.ReadResources(ctx, &api.ResourceReadbackRequest{
			Context: lifecycleContinuation(st.Launch, st.Launch, st.LaunchGrantID, "observe_resources"), ResourceSetId: st.ResourceSetID})
		if fabricErr != nil {
			// An unanswered provider read proves nothing about the resources.
			continue
		}
		if readback.GetResourceSetId() != st.ResourceSetID || readback.GetWorkspaceId() != workspaceID {
			continue
		}
		if readback.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || !readback.GetAbsenceConfirmed() {
			continue
		}
		if _, err = s.suspendWorkspaceLifecycle(ctx, workspaceID, lifecycleReasonResourceAbsent, lifecycleTriggerReconcile); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// recoverRestoredWorkspaces recovers a suspended Workspace only when the owner
// holds a confirmed paid term again and the original resources are present. The
// original binding is re-enabled by the event itself; no new key is issued.
func (s *Service) recoverRestoredWorkspaces(ctx context.Context, now time.Time) error {
	rows, err := s.Store.DB().QueryContext(ctx, `SELECT w.id FROM workspace.workspaces w
		JOIN workspace.subscriptions s ON s.workspace_id=w.id
		WHERE w.status='suspended' AND s.current_period_start <= $1 AND s.current_period_end > $1
		AND EXISTS (SELECT 1 FROM workspace.subscription_periods p WHERE p.subscription_id=s.id
			AND p.period_start=s.current_period_start AND p.period_end=s.current_period_end
			AND p.charge_wallet_operation_id=s.last_charge_wallet_operation_id
			AND COALESCE(p.charge_receipt_id,'')<>'')
		AND EXISTS (SELECT 1 FROM workspace.operations l WHERE l.resource_id=w.id AND l.kind='create_workspace' AND l.status='succeeded')
		AND NOT EXISTS (SELECT 1 FROM workspace.operations o WHERE o.resource_id=w.id AND o.kind IN ('suspend_workspace','resume_workspace')
			AND o.status IN ('accepted','running','awaiting_confirmation'))
		ORDER BY s.current_period_end,w.id LIMIT $2`, now, lifecycleSweepLimit)
	if err != nil {
		return dbError(err)
	}
	ids, err := scanIDs(rows)
	if err != nil {
		return err
	}
	var failures []error
	for _, workspaceID := range ids {
		st, readErr := s.readWorkspaceStanding(ctx, workspaceID)
		if readErr != nil {
			failures = append(failures, readErr)
			continue
		}
		if st.ResourceSetID != "" && s.Fabric != nil {
			readback, fabricErr := s.Fabric.ReadResources(ctx, &api.ResourceReadbackRequest{
				Context: lifecycleContinuation(st.Launch, st.Launch, st.LaunchGrantID, "observe_resources"), ResourceSetId: st.ResourceSetID})
			if fabricErr != nil {
				continue
			}
			if readback.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || readback.GetAbsenceConfirmed() {
				continue
			}
		}
		if _, err = s.resumeWorkspaceLifecycle(ctx, workspaceID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func scanIDs(rows *sql.Rows) ([]string, error) {
	var ids []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			rows.Close()
			return nil, dbError(err)
		}
		ids = append(ids, value)
	}
	err := rows.Err()
	rows.Close()
	if err != nil {
		return nil, dbError(err)
	}
	return ids, nil
}
