package delivery

// Serve's own access/route facts: the route generation a Workspace's Agent is
// served at, and the route switches Serve commits to.
//
// The route object is Serve's own serve.access_bindings row, so the conditional
// revision that may advance a route generation is the binding's own last
// confirmed route revision: a fence confirms a new execution epoch without
// changing the target, an activate or rollback CAS the target and advance the
// generation by exactly one, and any switch left requested or unknown blocks every
// new switch for that binding. Serve therefore never advances a generation from
// its own intent, and there is no second route object or performer that could
// disagree with the binding it committed.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// Refusal reasons. Each names a distinct, actionable condition.
const (
	// ReasonRouteGenerationConflict: the caller's expected route generation is not
	// the workspace's current one, so the switch would race another writer.
	ReasonRouteGenerationConflict = "route_generation_conflict"
	// ReasonStaleExecutionEpoch: the switch names an execution epoch the binding
	// has already superseded.
	ReasonStaleExecutionEpoch = "stale_execution_epoch"
	// ReasonRouteSwitchUnresolved: an earlier switch is still requested or unknown,
	// so the provider may have applied it; a new switch must not be committed.
	ReasonRouteSwitchUnresolved = "route_switch_unresolved"
	// ReasonDeploymentNotReady: the target deployment has no ready runtime instance
	// with the presented readiness receipt.
	ReasonDeploymentNotReady = "route_target_deployment_not_ready"
	// ReasonRouteEpochNotFenced: the execution epoch has no confirmed fence, so
	// its route generation must not advance.
	ReasonRouteEpochNotFenced = "route_epoch_not_fenced"
	// ReasonRoutePreconditionInvalid: the presented precondition does not match
	// the binding's own confirmed route revision, so another switch already
	// changed the route this caller meant to change.
	ReasonRoutePreconditionInvalid = "route_precondition_invalid"
	// ReasonRouteSwitchNotFound: the requested original switch does not belong to
	// this workspace.
	ReasonRouteSwitchNotFound = "route_switch_not_found"
	// ReasonRouteTargetUnavailable: the confirmed target has no running instance
	// Serve can reach, so the access data plane has nothing to serve.
	ReasonRouteTargetUnavailable = "route_target_unavailable"
)

// The route object is Serve's own access binding in `serve.access_bindings`.
// There is no second Kubernetes route object to keep in step and no separate
// performer to confirm one: the conditional revision this file compares is the
// binding's own last confirmed route revision, and the conditional update is the
// same owner transaction that records the switch. A caller that wants to change
// the route presents the revision it read; a caller that finds none presents a
// confirmed absence at generation zero. Anything that does not match is refused,
// so two writers can never both believe they advanced the same generation.
//
// An earlier switch that is still requested or unknown keeps blocking every new
// switch: it exists only when a crash interrupted the owner transaction, and the
// original switch identity is what a reader resumes from.

// FenceRouteEpoch confirms a new execution epoch against the Workspace's route
// binding without changing its target. The confirmed switch advances the
// binding's own route revision; the route generation is unchanged.
func (s *Service) FenceRouteEpoch(ctx context.Context, command *api.FenceRouteEpochCommand) (*api.RouteReadback, error) {
	if err := requireServePeer(ctx); err != nil {
		return nil, err
	}
	workspaceID := strings.TrimSpace(command.GetWorkspaceId())
	if err := s.authorize(ctx, command.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_FENCEROUTEEPOCH, workspaceID); err != nil {
		return nil, err
	}
	if err := s.requireDeliveredWorkspace(ctx, workspaceID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(command.GetOperationId()) == "" || command.GetExecutionEpoch() < 1 || command.GetExpectedRouteGeneration() < 0 {
		return nil, status.Error(codes.InvalidArgument, "workspace, operation and a positive execution epoch are required")
	}
	precondition, err := routePrecondition(command.GetExpectedRouteGeneration(), command.GetRevisionPrecondition())
	if err != nil {
		return nil, err
	}
	return s.commitRouteSwitch(ctx, routeSwitchPlan{
		workspaceID:             workspaceID,
		operationID:             strings.TrimSpace(command.GetOperationId()),
		actionKind:              "fence",
		executionEpoch:          command.GetExecutionEpoch(),
		expectedRouteGeneration: command.GetExpectedRouteGeneration(),
		expectedRouteRevision:   precondition.expectedRevision,
	})
}

// ActivateRoute makes one ready deployment the Workspace's current route target.
// The conditional update advances the route generation by exactly one and writes
// the confirmed target in the same owner transaction.
func (s *Service) ActivateRoute(ctx context.Context, command *api.RouteActivateCommand) (*api.RouteReadback, error) {
	if err := requireServePeer(ctx); err != nil {
		return nil, err
	}
	workspaceID := strings.TrimSpace(command.GetWorkspaceId())
	if err := s.authorize(ctx, command.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACTIVATEROUTE, workspaceID); err != nil {
		return nil, err
	}
	if err := s.requireDeliveredWorkspace(ctx, workspaceID); err != nil {
		return nil, err
	}
	target := strings.TrimSpace(command.GetTargetExecutionResourceId())
	if strings.TrimSpace(command.GetOperationId()) == "" || command.GetExecutionEpoch() < 1 || command.GetExpectedRouteGeneration() < 0 || target == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace, operation, execution epoch and target execution resource are required")
	}
	precondition, err := routePrecondition(command.GetExpectedRouteGeneration(), command.GetRevisionPrecondition())
	if err != nil {
		return nil, err
	}
	return s.commitRouteSwitch(ctx, routeSwitchPlan{
		workspaceID:             workspaceID,
		operationID:             strings.TrimSpace(command.GetOperationId()),
		actionKind:              "activate",
		executionEpoch:          command.GetExecutionEpoch(),
		expectedRouteGeneration: command.GetExpectedRouteGeneration(),
		expectedRouteRevision:   precondition.expectedRevision,
		target:                  target,
		targetRuntimeInstanceID: strings.TrimSpace(command.GetTargetRuntimeInstanceId()),
		targetDeploymentID:      strings.TrimSpace(command.GetTargetDeploymentId()),
		readinessReceiptID:      strings.TrimSpace(command.GetConfirmedReadinessReceiptId()),
	})
}

// RollbackRoute restores the Workspace's previously confirmed route target after a
// new deployment failed. It requires the original switch Serve committed and a
// precondition that matches the binding's own confirmed route revision, and it
// never reports a rollback it did not confirm.
func (s *Service) RollbackRoute(ctx context.Context, command *api.RouteRollbackCommand) (*api.RouteReadback, error) {
	if err := requireServePeer(ctx); err != nil {
		return nil, err
	}
	workspaceID := strings.TrimSpace(command.GetWorkspaceId())
	if err := s.authorize(ctx, command.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ROLLBACKROUTE, workspaceID); err != nil {
		return nil, err
	}
	if err := s.requireDeliveredWorkspace(ctx, workspaceID); err != nil {
		return nil, err
	}
	originalSwitchID := strings.TrimSpace(command.GetOriginalSwitchId())
	target := strings.TrimSpace(command.GetTargetExecutionResourceId())
	if strings.TrimSpace(command.GetOperationId()) == "" || command.GetExecutionEpoch() < 1 || command.GetExpectedRouteGeneration() < 0 || originalSwitchID == "" || target == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace, operation, execution epoch, original switch and target execution resource are required")
	}
	precondition, err := routePrecondition(command.GetExpectedRouteGeneration(), command.GetRevisionPrecondition())
	if err != nil {
		return nil, err
	}
	if err := s.requireConfirmedOriginalSwitch(ctx, workspaceID, originalSwitchID); err != nil {
		return nil, err
	}
	return s.commitRouteSwitch(ctx, routeSwitchPlan{
		workspaceID:             workspaceID,
		operationID:             strings.TrimSpace(command.GetOperationId()),
		actionKind:              "rollback",
		executionEpoch:          command.GetExecutionEpoch(),
		expectedRouteGeneration: command.GetExpectedRouteGeneration(),
		expectedRouteRevision:   precondition.expectedRevision,
		target:                  target,
		targetRuntimeInstanceID: strings.TrimSpace(command.GetTargetRuntimeInstanceId()),
		targetDeploymentID:      strings.TrimSpace(command.GetTargetDeploymentId()),
		readinessReceiptID:      strings.TrimSpace(command.GetCompatibilityReceiptId()),
	})
}

// ObserveRoute returns Serve's own route fact for one Workspace. A switch that is
// still requested is reported unknown, because the owner never recorded a
// confirmation for it; only a confirmed switch is reported confirmed.
func (s *Service) ObserveRoute(ctx context.Context, request *api.RouteObserveRequest) (*api.RouteReadback, error) {
	workspaceID := strings.TrimSpace(request.GetWorkspaceId())
	if err := s.authorize(ctx, request.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVEROUTE, workspaceID); err != nil {
		return nil, err
	}
	if err := s.requireDeliveredWorkspace(ctx, workspaceID); err != nil {
		return nil, err
	}
	readback := &api.RouteReadback{WorkspaceId: workspaceID, Observation: api.Observation_OBSERVATION_UNKNOWN}
	var (
		bindingID                 string
		generation, epoch         int64
		target, lastSwitch        sql.NullString
		targetRuntime, targetDepl sql.NullString
		confirmedRevision         sql.NullString
		observedAt                sql.NullTime
	)
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, route_generation, accepted_execution_epoch, target_execution_resource_id,
		       target_runtime_instance_id, target_deployment_id,
		       last_confirmed_switch_id, route_revision, observed_at
		FROM serve.access_bindings WHERE workspace_id = $1`, workspaceID).
		Scan(&bindingID, &generation, &epoch, &target, &targetRuntime, &targetDepl, &lastSwitch, &confirmedRevision, &observedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return readback, nil
	case err != nil:
		return nil, dbError(err)
	}
	readback.CurrentGeneration = generation
	readback.AcceptedExecutionEpoch = epoch
	if target.Valid {
		readback.TargetExecutionResourceId = &target.String
	}
	if lastSwitch.Valid {
		readback.SwitchId = lastSwitch.String
	}
	if targetRuntime.Valid {
		readback.TargetRuntimeInstanceId = &targetRuntime.String
	}
	if targetDepl.Valid {
		readback.TargetDeploymentId = &targetDepl.String
	}
	if confirmedRevision.Valid {
		readback.RouteRevision = confirmedRevision.String
	}
	if observedAt.Valid {
		readback.ObservedAt = timestamppb.New(observedAt.Time.UTC())
	}

	var (
		switchID, switchStatus          string
		switchEpoch                     int64
		switchTarget                    sql.NullString
		switchRuntime, switchDeployment sql.NullString
		switchEvidence                  sql.NullString
	)
	query := `SELECT id, status, execution_epoch, target_execution_resource_id, target_runtime_instance_id, target_deployment_id, evidence_ref FROM serve.access_switches WHERE route_binding_id = $1`
	args := []any{bindingID}
	if requested := strings.TrimSpace(request.GetSwitchId()); requested != "" {
		query += ` AND id = $2`
		args = append(args, requested)
	} else {
		query += ` AND status IN ('requested','unknown')`
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT 1`
	err = s.DB.QueryRowContext(ctx, query, args...).Scan(&switchID, &switchStatus, &switchEpoch, &switchTarget, &switchRuntime, &switchDeployment, &switchEvidence)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if requested := strings.TrimSpace(request.GetSwitchId()); requested != "" {
			return nil, status.Errorf(codes.NotFound, "%s: serve has no route switch %s for workspace %s", ReasonRouteSwitchNotFound, requested, workspaceID)
		}
		return readback, nil
	case err != nil:
		return nil, dbError(err)
	}
	readback.SwitchId = switchID
	if switchTarget.Valid {
		readback.TargetExecutionResourceId = &switchTarget.String
	}
	if switchRuntime.Valid {
		readback.TargetRuntimeInstanceId = &switchRuntime.String
	}
	if switchDeployment.Valid {
		readback.TargetDeploymentId = &switchDeployment.String
	}
	readback.AcceptedExecutionEpoch = switchEpoch
	if switchEvidence.Valid {
		readback.RouteReceiptId = &switchEvidence.String
	}
	switch switchStatus {
	case "confirmed":
		readback.Observation = api.Observation_OBSERVATION_CONFIRMED
	case "rejected":
		readback.Observation = api.Observation_OBSERVATION_REJECTED
	default:
		readback.Observation = api.Observation_OBSERVATION_UNKNOWN
	}
	return readback, nil
}

// routeSwitchPlan is the validated shape of one route switch command.
type routeSwitchPlan struct {
	workspaceID             string
	operationID             string
	actionKind              string
	executionEpoch          int64
	expectedRouteGeneration int64
	// expectedRouteRevision is the revision the caller presented: the binding's
	// last confirmed route revision, or empty for the caller's absence assertion.
	expectedRouteRevision   string
	target                  string
	targetRuntimeInstanceID string
	targetDeploymentID      string
	readinessReceiptID      string
}

// commitRouteSwitch applies one route switch to the Workspace's Serve-owned
// access binding. The binding is the route object, so the conditional update is
// this owner's own transaction and the confirmed revision is written with the
// switch that produced it.
func (s *Service) commitRouteSwitch(ctx context.Context, plan routeSwitchPlan) (*api.RouteReadback, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, dbError(err)
	}
	defer func() { _ = tx.Rollback() }()
	readback, err := commitRouteSwitchTx(ctx, tx, plan)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, dbError(err)
	}
	return readback, nil
}

// commitRouteSwitchTx performs the conditional revision CAS and records the
// confirmed switch inside the caller's transaction, so a route change is atomic
// with the state transition that caused it and no acknowledgement can be lost
// between them. The lock is re-entrant within one transaction, so an internal
// caller may commit a fence and its activation under one workspace lock.
func commitRouteSwitchTx(ctx context.Context, tx *sql.Tx, plan routeSwitchPlan) (*api.RouteReadback, error) {
	if err := lockRouteBinding(ctx, tx, plan.workspaceID); err != nil {
		return nil, err
	}
	binding, err := ensureRouteBinding(ctx, tx, plan.workspaceID)
	if err != nil {
		return nil, err
	}
	if binding.RouteGeneration != plan.expectedRouteGeneration {
		return nil, owneridentity.WithErrorCode(status.Errorf(codes.Aborted, "%s: route generation of workspace %s is %d, not the expected %d", ReasonRouteGenerationConflict, plan.workspaceID, binding.RouteGeneration, plan.expectedRouteGeneration), api.ErrorCodeEnum_ERROR_CODE_ENUM_ROUTE_GENERATION_CONFLICT)
	}
	if plan.executionEpoch < binding.AcceptedExecutionEpoch {
		return nil, owneridentity.WithErrorCode(status.Errorf(codes.FailedPrecondition, "%s: execution epoch %d of workspace %s is older than the accepted %d", ReasonStaleExecutionEpoch, plan.executionEpoch, plan.workspaceID, binding.AcceptedExecutionEpoch), api.ErrorCodeEnum_ERROR_CODE_ENUM_STALE_EXECUTION_EPOCH)
	}
	switchID := routeSwitchID(plan.workspaceID, plan.actionKind, plan.expectedRouteGeneration, plan.executionEpoch)
	// An unresolved earlier switch blocks every new switch for this binding before
	// any other check: it exists only when a writer was interrupted between
	// recording and confirming a switch, so a new switch would run against a route
	// whose state nobody knows. The original switch identity is what a reader
	// resumes from, and no precondition can lift the block. A replay of the same
	// logical switch names the same identity, so it is not treated as a conflict.
	pending, err := pendingRouteSwitch(ctx, tx, binding.ID)
	if err != nil {
		return nil, err
	}
	if pending.Valid && pending.String != switchID {
		return nil, status.Errorf(codes.FailedPrecondition, "%s: route switch %s of workspace %s must be read back before a new switch is committed", ReasonRouteSwitchUnresolved, pending.String, plan.workspaceID)
	}
	if err := verifyRouteRevisionPrecondition(binding, plan); err != nil {
		return nil, err
	}
	if err := validateRouteTarget(ctx, tx, plan); err != nil {
		return nil, err
	}
	previousTarget, target := binding.TargetExecutionResource, binding.TargetExecutionResource
	targetRuntimeInstance, targetDeployment := binding.TargetRuntimeInstance, binding.TargetDeployment
	if plan.actionKind != "fence" {
		target = sql.NullString{String: plan.target, Valid: true}
		targetRuntimeInstance = sql.NullString{String: plan.targetRuntimeInstanceID, Valid: plan.targetRuntimeInstanceID != ""}
		targetDeployment = sql.NullString{String: plan.targetDeploymentID, Valid: plan.targetDeploymentID != ""}
	}
	observedGeneration := plan.expectedRouteGeneration
	if plan.actionKind != "fence" {
		observedGeneration++
	}
	// The revision is derived from the exact switch identity, so any reader can
	// recompute the route revision Serve confirmed instead of trusting an opaque
	// token, and a replay of the same switch names the same revision.
	revision := routeRevision(switchID)
	evidence := routeEvidenceRef(switchID)
	// The switch identity is the row identity, so a replay of the same switch
	// conflicts on the primary key and writes no second switch. An empty expected
	// revision is the caller's absence assertion, which the check constraint only
	// admits at generation zero.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO serve.access_switches
			(id, route_binding_id, workspace_id, operation_owner, operation_id, expected_route_generation,
			 execution_epoch, target_execution_resource_id, previous_target_execution_resource_id,
			 status, action_kind, expected_route_revision,
			 observed_route_generation, observed_execution_epoch, observed_route_revision, evidence_ref,
			 target_runtime_instance_id, target_deployment_id, updated_at)
		VALUES ($1,$2,$3,'serve',$4,$5,$6,$7,$8,'confirmed',$9,$10,$11,$12,$13,$14,$15,$16,now())
		ON CONFLICT (id) DO NOTHING`,
		switchID, binding.ID, plan.workspaceID, plan.operationID, plan.expectedRouteGeneration,
		plan.executionEpoch, target, previousTarget, plan.actionKind,
		nullable(plan.expectedRouteRevision),
		observedGeneration, plan.executionEpoch, revision, evidence,
		targetRuntimeInstance, targetDeployment); err != nil {
		return nil, dbError(err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE serve.access_bindings
		SET route_generation=$2, accepted_execution_epoch=$3, target_execution_resource_id=$4,
		    target_runtime_instance_id=$5, target_deployment_id=$6,
		    last_confirmed_switch_id=$7, route_revision=$8, observed_at=now(), updated_at=now()
		WHERE id=$1`,
		binding.ID, observedGeneration, plan.executionEpoch, target,
		targetRuntimeInstance, targetDeployment, switchID, revision); err != nil {
		return nil, dbError(err)
	}
	if plan.targetDeploymentID != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE serve.agent_deployments SET confirmed_route_switch_id=$2, updated_at=now() WHERE id=$1 AND workspace_id=$3`, plan.targetDeploymentID, switchID, plan.workspaceID); err != nil {
			return nil, dbError(err)
		}
	}
	return &api.RouteReadback{
		WorkspaceId: plan.workspaceID, SwitchId: switchID,
		Observation:       api.Observation_OBSERVATION_CONFIRMED,
		CurrentGeneration: observedGeneration, AcceptedExecutionEpoch: plan.executionEpoch,
		TargetExecutionResourceId: protoStringOrNil(target.String),
		TargetRuntimeInstanceId:   protoStringOrNil(targetRuntimeInstance.String),
		TargetDeploymentId:        protoStringOrNil(targetDeployment.String),
		RouteRevision:             revision,
		RouteReceiptId:            protoStringOrNil(evidence),
		ObservedAt:                timestamppb.Now(),
	}, nil
}

// verifyRouteRevisionPrecondition compares the revision the caller presented
// against the binding's own last confirmed route revision. An exact revision
// must be present and equal; a confirmed absence is only truthful while the
// binding has never confirmed a switch.
func verifyRouteRevisionPrecondition(binding routeBinding, plan routeSwitchPlan) error {
	if plan.expectedRouteRevision != "" {
		if !binding.RouteRevision.Valid || binding.RouteRevision.String != plan.expectedRouteRevision {
			return owneridentity.WithErrorCode(status.Errorf(codes.FailedPrecondition, "%s: workspace %s route revision is %q, not the expected %q", ReasonRoutePreconditionInvalid, plan.workspaceID, binding.RouteRevision.String, plan.expectedRouteRevision), api.ErrorCodeEnum_ERROR_CODE_ENUM_VERSION_CONFLICT)
		}
		return nil
	}
	if binding.RouteGeneration != 0 || binding.RouteRevision.Valid || binding.LastConfirmedSwitchID.Valid {
		return owneridentity.WithErrorCode(status.Errorf(codes.FailedPrecondition, "%s: workspace %s already has a confirmed route, so a confirmed-absence precondition cannot apply", ReasonRoutePreconditionInvalid, plan.workspaceID), api.ErrorCodeEnum_ERROR_CODE_ENUM_VERSION_CONFLICT)
	}
	return nil
}

// routeRevision and routeEvidenceRef derive the confirmed route revision and its
// receipt from the exact switch identity. Both are values any reader can
// recompute from the recorded switch, so a route fact is auditable without a
// provider-specific token.
func routeRevision(switchID string) string { return "serve-route/" + switchID }

func routeEvidenceRef(switchID string) string { return "serve-route-receipt://" + switchID }

// validateRouteTarget enforces the facts a switch depends on before any switch row
// is written: an activation must name a deployment whose own runtime instance is
// ready with the presented readiness receipt, and its epoch must already be
// covered by a confirmed fence.
func validateRouteTarget(ctx context.Context, tx *sql.Tx, plan routeSwitchPlan) error {
	if plan.actionKind == "fence" {
		return nil
	}
	if plan.targetDeploymentID == "" {
		return status.Errorf(codes.InvalidArgument, "%s: an activation or rollback must name its target deployment", ReasonDeploymentNotReady)
	}
	var readinessRef string
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(readiness_evidence_ref,'') FROM serve.agent_runtime_instances
		WHERE deployment_id=$1 AND status='ready' AND readiness_evidence_ref IS NOT NULL`, plan.targetDeploymentID).Scan(&readinessRef)
	if errors.Is(err, sql.ErrNoRows) {
		return status.Errorf(codes.FailedPrecondition, "%s: deployment %s has no ready runtime instance in Serve's records", ReasonDeploymentNotReady, plan.targetDeploymentID)
	}
	if err != nil {
		return dbError(err)
	}
	if plan.readinessReceiptID != "" && readinessRef != plan.readinessReceiptID {
		return status.Errorf(codes.FailedPrecondition, "%s: the presented receipt is not the readiness Serve recorded for deployment %s", ReasonDeploymentNotReady, plan.targetDeploymentID)
	}
	if plan.extensibleFenceRequired() {
		var fenced bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM serve.access_switches
				WHERE route_binding_id=$1 AND action_kind='fence' AND execution_epoch=$2 AND status='confirmed')`,
			routeBindingID(plan.workspaceID), plan.executionEpoch).Scan(&fenced); err != nil {
			return dbError(err)
		}
		if !fenced {
			return status.Errorf(codes.FailedPrecondition, "%s: execution epoch %d of workspace %s is not covered by a confirmed fence", ReasonRouteEpochNotFenced, plan.executionEpoch, plan.workspaceID)
		}
	}
	return nil
}

func (plan routeSwitchPlan) extensibleFenceRequired() bool { return plan.actionKind == "activate" }

// requireConfirmedOriginalSwitch refuses a rollback whose original switch does not
// belong to the workspace or was never confirmed.
func (s *Service) requireConfirmedOriginalSwitch(ctx context.Context, workspaceID, switchID string) error {
	var statusText string
	err := s.DB.QueryRowContext(ctx, `
		SELECT status FROM serve.access_switches WHERE id=$1 AND workspace_id=$2`, switchID, workspaceID).Scan(&statusText)
	if errors.Is(err, sql.ErrNoRows) {
		return status.Errorf(codes.NotFound, "%s: serve has no route switch %s for workspace %s", ReasonRouteSwitchNotFound, switchID, workspaceID)
	}
	if err != nil {
		return dbError(err)
	}
	if statusText != "confirmed" {
		return status.Errorf(codes.FailedPrecondition, "%s: route switch %s is %s, so there is no confirmed route to roll back from", ReasonRouteSwitchUnresolved, switchID, statusText)
	}
	return nil
}

// requireDeliveredWorkspace refuses a route action for a workspace Serve has no
// delivery for: a route fact without a delivered Agent would be an invented one.
func (s *Service) requireDeliveredWorkspace(ctx context.Context, workspaceID string) error {
	if workspaceID == "" {
		return status.Error(codes.InvalidArgument, "workspace is required")
	}
	var delivered bool
	if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM serve.agent_deployments WHERE workspace_id=$1)`, workspaceID).Scan(&delivered); err != nil {
		return dbError(err)
	}
	if !delivered {
		return status.Errorf(codes.NotFound, "serve has no delivery for workspace %s", workspaceID)
	}
	return nil
}

// routeBinding is Serve's own observed route fact for one workspace.
type routeBinding struct {
	ID                      string
	WorkspaceID             string
	RouteGeneration         int64
	AcceptedExecutionEpoch  int64
	TargetExecutionResource sql.NullString
	TargetRuntimeInstance   sql.NullString
	TargetDeployment        sql.NullString
	LastConfirmedSwitchID   sql.NullString
	RouteRevision           sql.NullString
	ObservedAt              sql.NullTime
}

// ensureRouteBinding creates the workspace's route binding on first use and
// returns it, serialized against every other route action of that workspace.
func ensureRouteBinding(ctx context.Context, tx *sql.Tx, workspaceID string) (routeBinding, error) {
	var binding routeBinding
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO serve.access_bindings (id, workspace_id) VALUES ($1, $2)
		ON CONFLICT (workspace_id) DO NOTHING`, routeBindingID(workspaceID), workspaceID); err != nil {
		return binding, dbError(err)
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT id, workspace_id, route_generation, accepted_execution_epoch,
		       target_execution_resource_id, target_runtime_instance_id, target_deployment_id,
		       last_confirmed_switch_id, route_revision, observed_at
		FROM serve.access_bindings WHERE workspace_id = $1 FOR UPDATE`, workspaceID).
		Scan(&binding.ID, &binding.WorkspaceID, &binding.RouteGeneration, &binding.AcceptedExecutionEpoch,
			&binding.TargetExecutionResource, &binding.TargetRuntimeInstance, &binding.TargetDeployment,
			&binding.LastConfirmedSwitchID, &binding.RouteRevision, &binding.ObservedAt); err != nil {
		return binding, dbError(err)
	}
	return binding, nil
}

// pendingRouteSwitch reports the binding's unresolved switch identity, the one
// piece of state that blocks every new route action for that binding.
func pendingRouteSwitch(ctx context.Context, tx *sql.Tx, bindingID string) (sql.NullString, error) {
	var switchID sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM serve.access_switches
		WHERE route_binding_id = $1 AND status IN ('requested','unknown')
		ORDER BY created_at DESC, id DESC LIMIT 1`, bindingID).Scan(&switchID)
	if errors.Is(err, sql.ErrNoRows) {
		return switchID, nil
	}
	if err != nil {
		return switchID, dbError(err)
	}
	return switchID, nil
}

// lockRouteBinding serializes route actions for one workspace.
func lockRouteBinding(ctx context.Context, tx *sql.Tx, workspaceID string) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "serve.route:"+workspaceID); err != nil {
		return dbError(err)
	}
	return nil
}

// routePreconditionShape projects the presented route precondition onto the
// shape serve.access_switches can record: the exact route revision the caller
// read from this binding, or the caller's assertion that the binding had no
// confirmed route, which is only recordable at generation zero. An empty expected
// revision is that absence: the parser refuses an empty exact revision, so the
// only way to reach it is the absence variant.
type routePreconditionShape struct {
	expectedRevision string
}

func routePrecondition(expectedGeneration int64, precondition *api.RouteRevisionPrecondition) (routePreconditionShape, error) {
	if precondition == nil {
		return routePreconditionShape{}, status.Error(codes.InvalidArgument, "a route revision precondition (exact_revision or require_absent) is required")
	}
	switch condition := precondition.GetCondition().(type) {
	case *api.RouteRevisionPrecondition_ExactRevision:
		revision := strings.TrimSpace(condition.ExactRevision)
		if revision == "" {
			return routePreconditionShape{}, status.Error(codes.InvalidArgument, "an exact route revision must not be empty")
		}
		return routePreconditionShape{expectedRevision: revision}, nil
	case *api.RouteRevisionPrecondition_RequireAbsent:
		if condition.RequireAbsent == nil {
			return routePreconditionShape{}, status.Error(codes.InvalidArgument, "require_absent must carry the caller's route absence assertion")
		}
		// The route object is Serve's own binding, so absence is a fact Serve reads
		// from it: no confirmed route revision exists yet. Only generation zero can
		// present that, and Serve still verifies it before writing the switch.
		if expectedGeneration != 0 {
			return routePreconditionShape{}, status.Errorf(codes.InvalidArgument, "%s: require_absent is only recordable at route generation zero", ReasonRouteGenerationConflict)
		}
		return routePreconditionShape{}, nil
	default:
		return routePreconditionShape{}, status.Error(codes.InvalidArgument, "a route revision precondition (exact_revision or require_absent) is required")
	}
}

// requireServePeer admits only Serve's own execution of a ServeAccessControl
// action: the route switch is a step of Serve's own delivery operation.
func requireServePeer(ctx context.Context) error {
	if err := requirePeer(ctx, owneridentity.Serve); err != nil {
		return err
	}
	return nil
}

// routeBindingID and routeSwitchID derive route identities from the workspace and
// the switch identity, so a replay of the same route command names the same
// binding and the same switch instead of allocating a second unresolved one.
func routeBindingID(workspaceID string) string {
	raw := sha256Hex("serve.route.binding\x00" + workspaceID)
	return "rtb_" + raw[:32]
}

func routeSwitchID(workspaceID, actionKind string, generation, epoch int64) string {
	raw := sha256Hex(strings.Join([]string{"serve.route.switch", workspaceID, actionKind, strconv.FormatInt(generation, 10), strconv.FormatInt(epoch, 10)}, "\x00"))
	return "rsw_" + raw[:32]
}

// sha256Hex is the stable hex digest used to derive owner-local route identities
// from exact inputs, so a replay names the same row instead of allocating one.
func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// nullablePort records an absent upstream port as SQL NULL, so the column's
// both-or-neither constraint describes the real state instead of a zero port.
func nullablePort(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func protoStringOrNil(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

// commitDeliveryRoute makes one ready deployment the Workspace's current route
// target. It runs inside the transaction that recorded the readiness, because
// Serve owns both the readiness fact and the route fact: the fence and the
// activation are owner-local conditional state transitions, and a reader of
// either row sees them together or not at all.
//
// It is idempotent for the same deployment: a delivery that already serves this
// deployment commits no second switch.
func commitDeliveryRoute(ctx context.Context, tx *sql.Tx, r *api.RuntimeDeployCommand, readinessRef string) error {
	if strings.TrimSpace(readinessRef) == "" || strings.TrimSpace(r.GetRuntimeInstanceId()) == "" {
		return nil
	}
	binding, err := ensureRouteBinding(ctx, tx, r.GetWorkspaceId())
	if err != nil {
		return err
	}
	if binding.TargetDeployment.Valid && binding.TargetDeployment.String == r.GetDeploymentId() {
		return nil
	}
	var operationID string
	if err = tx.QueryRowContext(ctx, `SELECT operation_id FROM serve.agent_deployments WHERE id=$1`, r.GetDeploymentId()).Scan(&operationID); err != nil {
		return dbError(err)
	}
	precondition := routePreconditionShape{}
	switch {
	case binding.RouteRevision.Valid && binding.RouteRevision.String != "":
		precondition.expectedRevision = binding.RouteRevision.String
	case binding.RouteGeneration == 0 && !binding.LastConfirmedSwitchID.Valid:
		// The first route this Workspace ever confirms carries the absence
		// assertion instead of an invented revision: Serve read its own binding and
		// found no confirmed route revision.
	default:
		return status.Errorf(codes.FailedPrecondition, "%s: workspace %s has a route generation but no confirmed revision to condition on", ReasonRoutePreconditionInvalid, r.GetWorkspaceId())
	}
	fenced, err := commitRouteSwitchTx(ctx, tx, routeSwitchPlan{
		workspaceID:             r.GetWorkspaceId(),
		operationID:             operationID,
		actionKind:              "fence",
		executionEpoch:          r.GetExecutionEpoch(),
		expectedRouteGeneration: binding.RouteGeneration,
		expectedRouteRevision:   precondition.expectedRevision,
	})
	if err != nil {
		return err
	}
	_, err = commitRouteSwitchTx(ctx, tx, routeSwitchPlan{
		workspaceID:             r.GetWorkspaceId(),
		operationID:             operationID,
		actionKind:              "activate",
		executionEpoch:          r.GetExecutionEpoch(),
		expectedRouteGeneration: fenced.GetCurrentGeneration(),
		expectedRouteRevision:   fenced.GetRouteRevision(),
		target:                  r.GetRuntimeInstanceId(),
		targetRuntimeInstanceID: r.GetRuntimeInstanceId(),
		targetDeploymentID:      r.GetDeploymentId(),
		readinessReceiptID:      readinessRef,
	})
	return err
}
