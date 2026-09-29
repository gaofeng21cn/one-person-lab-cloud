package delivery

// Serve's own access/route facts: the route generation a Workspace's Agent is
// served at, and the route switches Serve commits to.
//
// The frozen schema makes the provider conditional revision the only thing that
// may advance a route generation: a fence confirms a new execution epoch without
// changing the target, an activate or rollback CAS the target and advance the
// generation by exactly one, and any switch left requested or unknown blocks every
// new switch for that binding. Serve therefore never advances a generation from
// its own intent. When the route provider is not configured, Serve records the
// exact switch it committed to and refuses, naming the missing capability, instead
// of reporting an activated route it never confirmed.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

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
	// ReasonRouteEpochNotFenced: the execution epoch has no confirmed provider
	// fence, so its route generation must not advance.
	ReasonRouteEpochNotFenced = "route_epoch_not_fenced"
	// ReasonRouteProviderUnavailable: no installation route provider is configured,
	// so no provider conditional revision can be confirmed.
	ReasonRouteProviderUnavailable = "route_provider_unavailable"
	// ReasonRoutePreconditionInvalid: the presented provider precondition does not
	// match the binding's current provider revision.
	ReasonRoutePreconditionInvalid = "route_precondition_invalid"
	// ReasonRouteSwitchNotFound: the requested original switch does not belong to
	// this workspace.
	ReasonRouteSwitchNotFound = "route_switch_not_found"
)

// RouteProvider is Serve's port to the installation route provider. It performs
// provider conditional-revision CAS on one route object and reports what the
// provider actually confirmed. A provider that cannot observe the object reports
// an unknown outcome rather than guessing, and Serve then blocks further switches.
type RouteProvider interface {
	FenceRoute(context.Context, RouteProviderRequest) (RouteProviderResult, error)
	ActivateRoute(context.Context, RouteProviderRequest) (RouteProviderResult, error)
	RollbackRoute(context.Context, RouteProviderRequest) (RouteProviderResult, error)
	ObserveRoute(context.Context, RouteProviderObserveRequest) (RouteProviderResult, error)
}

// RouteProviderRequest is the exact identity one route switch executes at the
// provider. The provider command id is deterministic, so a retry after a lost
// response names the same provider object instead of allocating a second one.
type RouteProviderRequest struct {
	WorkspaceID                       string
	SwitchID                          string
	ProviderCommandID                 string
	ActionKind                        string
	ExecutionEpoch                    int64
	ExpectedRouteGeneration           int64
	ExpectedProviderRevision          string
	ExpectedAbsenceReceiptID          string
	ExpectedAbsenceObservedAt         time.Time
	TargetExecutionResourceID         string
	PreviousTargetExecutionResourceID string
}

// RouteProviderObserveRequest reads back one provider command identity, so a lost
// activation response resumes the original switch instead of a new one.
type RouteProviderObserveRequest struct {
	WorkspaceID       string
	SwitchID          string
	ProviderCommandID string
}

// RouteProviderResult is what the provider actually confirmed, observed or
// rejected. An empty Outcome means the provider could not decide.
type RouteProviderResult struct {
	Outcome                 api.Observation
	ProviderRevision        string
	ObservedRouteGeneration int64
	ObservedExecutionEpoch  int64
	EvidenceRef             string
	ProviderCommandID       string
	ErrorCode               string
}

// FenceRouteEpoch confirms a new execution epoch against the Workspace's route
// binding without changing its target. The provider CAS confirms the epoch and
// advances its own revision; the route generation is unchanged.
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
	precondition, err := routePrecondition(command.GetExpectedRouteGeneration(), command.GetProviderPrecondition())
	if err != nil {
		return nil, err
	}
	return s.commitRouteSwitch(ctx, routeSwitchPlan{
		workspaceID:               workspaceID,
		operationID:               strings.TrimSpace(command.GetOperationId()),
		actionKind:                "fence",
		executionEpoch:            command.GetExecutionEpoch(),
		expectedRouteGeneration:   command.GetExpectedRouteGeneration(),
		expectedProviderRevision:  precondition.expectedRevision,
		expectedAbsenceReceiptID:  precondition.absenceReceiptID,
		expectedAbsenceObservedAt: precondition.absenceObservedAt,
	})
}

// ActivateRoute makes one ready deployment the Workspace's current route target.
// The provider CAS advances the route generation by exactly one; Serve commits its
// own current-selection fact only after that confirmed readback.
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
	precondition, err := routePrecondition(command.GetExpectedRouteGeneration(), command.GetProviderPrecondition())
	if err != nil {
		return nil, err
	}
	return s.commitRouteSwitch(ctx, routeSwitchPlan{
		workspaceID:               workspaceID,
		operationID:               strings.TrimSpace(command.GetOperationId()),
		actionKind:                "activate",
		executionEpoch:            command.GetExecutionEpoch(),
		expectedRouteGeneration:   command.GetExpectedRouteGeneration(),
		expectedProviderRevision:  precondition.expectedRevision,
		expectedAbsenceReceiptID:  precondition.absenceReceiptID,
		expectedAbsenceObservedAt: precondition.absenceObservedAt,
		target:                    target,
		targetRuntimeInstanceID:   strings.TrimSpace(command.GetTargetRuntimeInstanceId()),
		targetDeploymentID:        strings.TrimSpace(command.GetTargetDeploymentId()),
		readinessReceiptID:        strings.TrimSpace(command.GetConfirmedReadinessReceiptId()),
	})
}

// RollbackRoute restores the Workspace's previously confirmed route target after a
// new deployment failed. It requires the original switch Serve committed and a
// confirmed provider revision, and it never reports a rollback it did not confirm.
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
	precondition, err := routePrecondition(command.GetExpectedRouteGeneration(), command.GetProviderPrecondition())
	if err != nil {
		return nil, err
	}
	if err := s.requireConfirmedOriginalSwitch(ctx, workspaceID, originalSwitchID); err != nil {
		return nil, err
	}
	return s.commitRouteSwitch(ctx, routeSwitchPlan{
		workspaceID:               workspaceID,
		operationID:               strings.TrimSpace(command.GetOperationId()),
		actionKind:                "rollback",
		executionEpoch:            command.GetExecutionEpoch(),
		expectedRouteGeneration:   command.GetExpectedRouteGeneration(),
		expectedProviderRevision:  precondition.expectedRevision,
		expectedAbsenceReceiptID:  precondition.absenceReceiptID,
		expectedAbsenceObservedAt: precondition.absenceObservedAt,
		target:                    target,
		targetRuntimeInstanceID:   strings.TrimSpace(command.GetTargetRuntimeInstanceId()),
		targetDeploymentID:        strings.TrimSpace(command.GetTargetDeploymentId()),
		readinessReceiptID:        strings.TrimSpace(command.GetCompatibilityReceiptId()),
	})
}

// ObserveRoute returns Serve's own route fact for one Workspace. A switch that is
// still requested is reported unknown, because Serve holds no provider readback
// for it; only a confirmed provider readback is reported confirmed.
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
		bindingID          string
		generation, epoch  int64
		target, lastSwitch sql.NullString
		providerRevision   sql.NullString
		observedAt         sql.NullTime
	)
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, route_generation, accepted_execution_epoch, target_execution_resource_id,
		       last_confirmed_switch_id, provider_revision, observed_at
		FROM serve.access_bindings WHERE workspace_id = $1`, workspaceID).
		Scan(&bindingID, &generation, &epoch, &target, &lastSwitch, &providerRevision, &observedAt)
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
	if providerRevision.Valid {
		readback.ProviderRevision = providerRevision.String
	}
	if observedAt.Valid {
		readback.ObservedAt = timestamppb.New(observedAt.Time.UTC())
	}

	var (
		switchID, providerCommandID, switchStatus string
		switchEpoch                               int64
		switchTarget                              sql.NullString
		switchEvidence                            sql.NullString
	)
	query := `SELECT id, provider_command_id, status, execution_epoch, target_execution_resource_id, evidence_ref FROM serve.access_switches WHERE route_binding_id = $1`
	args := []any{bindingID}
	if requested := strings.TrimSpace(request.GetSwitchId()); requested != "" {
		query += ` AND id = $2`
		args = append(args, requested)
	} else {
		query += ` AND status IN ('requested','unknown')`
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT 1`
	err = s.DB.QueryRowContext(ctx, query, args...).Scan(&switchID, &providerCommandID, &switchStatus, &switchEpoch, &switchTarget, &switchEvidence)
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
	readback.ProviderCommandId = providerCommandID
	if switchTarget.Valid {
		readback.TargetExecutionResourceId = &switchTarget.String
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
	workspaceID               string
	operationID               string
	actionKind                string
	executionEpoch            int64
	expectedRouteGeneration   int64
	expectedProviderRevision  string
	expectedAbsenceReceiptID  string
	expectedAbsenceObservedAt time.Time
	target                    string
	targetRuntimeInstanceID   string
	targetDeploymentID        string
	readinessReceiptID        string
}

// commitRouteSwitch serializes one route switch under the workspace route lock,
// records the switch before it calls the provider, then persists only what the
// provider confirmed. A lost provider response leaves the switch unknown, which
// blocks every later switch until the original provider command is read back.
func (s *Service) commitRouteSwitch(ctx context.Context, plan routeSwitchPlan) (*api.RouteReadback, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, dbError(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockRouteBinding(ctx, tx, plan.workspaceID); err != nil {
		return nil, err
	}
	binding, err := s.ensureRouteBinding(ctx, tx, plan.workspaceID)
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
	providerCommandID := routeProviderCommandID(switchID)
	// An unresolved earlier switch blocks every new switch for this binding before
	// any other check: the provider may already have applied it, so a new switch
	// would run on an object whose state is unknown.
	pending, err := pendingRouteSwitch(ctx, tx, binding.ID)
	if err != nil {
		return nil, err
	}
	if pending.Valid && pending.String != providerCommandID {
		return nil, status.Errorf(codes.FailedPrecondition, "%s: route switch %s of workspace %s must be read back before a new switch is committed", ReasonRouteSwitchUnresolved, pending.String, plan.workspaceID)
	}
	if err := s.validateRouteTarget(ctx, tx, plan); err != nil {
		return nil, err
	}
	previousTarget, target := binding.TargetExecutionResource, binding.TargetExecutionResource
	if plan.actionKind != "fence" {
		target = sql.NullString{String: plan.target, Valid: true}
	}
	absenceReceipt, absenceObservedAt := absenceColumns(plan)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO serve.access_switches
			(id, route_binding_id, workspace_id, operation_owner, operation_id, expected_route_generation,
			 execution_epoch, target_execution_resource_id, previous_target_execution_resource_id,
			 provider_command_id, status, action_kind, expected_provider_revision,
			 expected_absence_receipt_id, expected_absence_observed_at)
		VALUES ($1,$2,$3,'serve',$4,$5,$6,$7,$8,$9,'requested',$10,$11,$12,$13)
		ON CONFLICT (provider_command_id) DO NOTHING`,
		switchID, binding.ID, plan.workspaceID, plan.operationID, plan.expectedRouteGeneration,
		plan.executionEpoch, target, previousTarget, providerCommandID, plan.actionKind,
		nullable(plan.expectedProviderRevision), absenceReceipt, absenceObservedAt); err != nil {
		return nil, dbError(err)
	}
	if s.Route == nil {
		if err := tx.Commit(); err != nil {
			return nil, dbError(err)
		}
		return nil, owneridentity.WithErrorCode(status.Errorf(codes.Unavailable, "%s: no installation route provider is configured, so workspace %s switch %s cannot be confirmed", ReasonRouteProviderUnavailable, plan.workspaceID, switchID), api.ErrorCodeEnum_ERROR_CODE_ENUM_APP_ACCESS_UNAVAILABLE)
	}
	if err := tx.Commit(); err != nil {
		return nil, dbError(err)
	}
	request := RouteProviderRequest{
		WorkspaceID: plan.workspaceID, SwitchID: switchID, ProviderCommandID: providerCommandID,
		ActionKind: plan.actionKind, ExecutionEpoch: plan.executionEpoch, ExpectedRouteGeneration: plan.expectedRouteGeneration,
		ExpectedProviderRevision: plan.expectedProviderRevision, ExpectedAbsenceReceiptID: plan.expectedAbsenceReceiptID,
		ExpectedAbsenceObservedAt: plan.expectedAbsenceObservedAt,
		TargetExecutionResourceID: target.String, PreviousTargetExecutionResourceID: previousTarget.String,
	}
	var result RouteProviderResult
	var callErr error
	switch plan.actionKind {
	case "fence":
		result, callErr = s.Route.FenceRoute(ctx, request)
	case "activate":
		result, callErr = s.Route.ActivateRoute(ctx, request)
	case "rollback":
		result, callErr = s.Route.RollbackRoute(ctx, request)
	}
	return s.settleRouteSwitch(ctx, plan, binding, switchID, providerCommandID, result, callErr)
}

// settleRouteSwitch persists the provider's own outcome for one switch. Only a
// confirmed readback with the exact revision, observed generation and epoch may
// advance the binding; anything else leaves the switch blocking.
func (s *Service) settleRouteSwitch(ctx context.Context, plan routeSwitchPlan, binding routeBinding, switchID, providerCommandID string, result RouteProviderResult, callErr error) (*api.RouteReadback, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, dbError(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockRouteBinding(ctx, tx, plan.workspaceID); err != nil {
		return nil, err
	}
	expectedGeneration := plan.expectedRouteGeneration
	if plan.actionKind != "fence" {
		expectedGeneration++
	}
	outcome := result.Outcome
	if callErr != nil {
		outcome = api.Observation_OBSERVATION_UNKNOWN
	}
	switch outcome {
	case api.Observation_OBSERVATION_CONFIRMED:
		if strings.TrimSpace(result.EvidenceRef) == "" || result.ObservedExecutionEpoch != plan.executionEpoch || result.ObservedRouteGeneration != expectedGeneration || strings.TrimSpace(result.ProviderRevision) == "" {
			outcome = api.Observation_OBSERVATION_UNKNOWN
		}
	case api.Observation_OBSERVATION_REJECTED:
	default:
		outcome = api.Observation_OBSERVATION_UNKNOWN
	}
	readback := &api.RouteReadback{
		WorkspaceId: plan.workspaceID, SwitchId: switchID, ProviderCommandId: providerCommandID,
		CurrentGeneration: binding.RouteGeneration, AcceptedExecutionEpoch: plan.executionEpoch,
		TargetExecutionResourceId: protoStringOrNil(plan.target), ProviderRevision: strings.TrimSpace(result.ProviderRevision),
	}
	if readback.ProviderRevision == "" && binding.ProviderRevision.Valid && outcome != api.Observation_OBSERVATION_CONFIRMED {
		readback.ProviderRevision = binding.ProviderRevision.String
	}
	switch outcome {
	case api.Observation_OBSERVATION_CONFIRMED:
		readback.CurrentGeneration = expectedGeneration
		if _, err := tx.ExecContext(ctx, `
			UPDATE serve.access_switches
			SET status='confirmed', observed_route_generation=$2, observed_execution_epoch=$3,
			    observed_provider_revision=$4, evidence_ref=$5, updated_at=now()
			WHERE id=$1`,
			switchID, result.ObservedRouteGeneration, result.ObservedExecutionEpoch, result.ProviderRevision, result.EvidenceRef); err != nil {
			return nil, dbError(err)
		}
		target := binding.TargetExecutionResource
		if plan.actionKind != "fence" {
			target = sql.NullString{String: plan.target, Valid: true}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE serve.access_bindings
			SET route_generation=$2, accepted_execution_epoch=$3, target_execution_resource_id=$4,
			    last_confirmed_switch_id=$5, provider_revision=$6, observed_at=now(), updated_at=now()
			WHERE id=$1`,
			binding.ID, result.ObservedRouteGeneration, result.ObservedExecutionEpoch, target, switchID, result.ProviderRevision); err != nil {
			return nil, dbError(err)
		}
		readback.Observation = api.Observation_OBSERVATION_CONFIRMED
		readback.RouteReceiptId = protoStringOrNil(result.EvidenceRef)
		readback.ObservedAt = timestamppb.Now()
		if plan.targetDeploymentID != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE serve.agent_deployments SET confirmed_route_switch_id=$2, updated_at=now() WHERE id=$1 AND workspace_id=$3`, plan.targetDeploymentID, switchID, plan.workspaceID); err != nil {
				return nil, dbError(err)
			}
		}
	case api.Observation_OBSERVATION_REJECTED:
		if _, err := tx.ExecContext(ctx, `UPDATE serve.access_switches SET status='rejected', error_code=$2, updated_at=now() WHERE id=$1`, switchID, nullable(result.ErrorCode)); err != nil {
			return nil, dbError(err)
		}
		readback.Observation = api.Observation_OBSERVATION_REJECTED
		if value, ok := api.ErrorCodeEnum_value["ERROR_CODE_ENUM_"+strings.ToUpper(result.ErrorCode)]; ok {
			code := api.ErrorCodeEnum(value)
			readback.ErrorCode = &code
		}
	default:
		if _, err := tx.ExecContext(ctx, `UPDATE serve.access_switches SET status='unknown', error_code=$2, updated_at=now() WHERE id=$1`, switchID, nullable(result.ErrorCode)); err != nil {
			return nil, dbError(err)
		}
		readback.Observation = api.Observation_OBSERVATION_UNKNOWN
	}
	if err := tx.Commit(); err != nil {
		return nil, dbError(err)
	}
	return readback, nil
}

// validateRouteTarget enforces the facts a switch depends on before any switch row
// is written: an activation must name a deployment whose own runtime instance is
// ready with the presented readiness receipt, and its epoch must already be
// covered by a confirmed provider fence.
func (s *Service) validateRouteTarget(ctx context.Context, tx *sql.Tx, plan routeSwitchPlan) error {
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
			return status.Errorf(codes.FailedPrecondition, "%s: execution epoch %d of workspace %s is not covered by a confirmed provider fence", ReasonRouteEpochNotFenced, plan.executionEpoch, plan.workspaceID)
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
	LastConfirmedSwitchID   sql.NullString
	ProviderRevision        sql.NullString
	ObservedAt              sql.NullTime
}

// ensureRouteBinding creates the workspace's route binding on first use and
// returns it, serialized against every other route action of that workspace.
func (s *Service) ensureRouteBinding(ctx context.Context, tx *sql.Tx, workspaceID string) (routeBinding, error) {
	var binding routeBinding
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO serve.access_bindings (id, workspace_id) VALUES ($1, $2)
		ON CONFLICT (workspace_id) DO NOTHING`, routeBindingID(workspaceID), workspaceID); err != nil {
		return binding, dbError(err)
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT id, workspace_id, route_generation, accepted_execution_epoch, target_execution_resource_id,
		       last_confirmed_switch_id, provider_revision, observed_at
		FROM serve.access_bindings WHERE workspace_id = $1 FOR UPDATE`, workspaceID).
		Scan(&binding.ID, &binding.WorkspaceID, &binding.RouteGeneration, &binding.AcceptedExecutionEpoch,
			&binding.TargetExecutionResource, &binding.LastConfirmedSwitchID, &binding.ProviderRevision, &binding.ObservedAt); err != nil {
		return binding, dbError(err)
	}
	return binding, nil
}

// pendingRouteSwitch reports the binding's unresolved switch, the one piece of
// state that blocks every new route action for that binding.
func pendingRouteSwitch(ctx context.Context, tx *sql.Tx, bindingID string) (sql.NullString, error) {
	var providerCommandID sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT provider_command_id FROM serve.access_switches
		WHERE route_binding_id = $1 AND status IN ('requested','unknown')
		ORDER BY created_at DESC, id DESC LIMIT 1`, bindingID).Scan(&providerCommandID)
	if errors.Is(err, sql.ErrNoRows) {
		return providerCommandID, nil
	}
	if err != nil {
		return providerCommandID, dbError(err)
	}
	return providerCommandID, nil
}

// lockRouteBinding serializes route actions for one workspace.
func lockRouteBinding(ctx context.Context, tx *sql.Tx, workspaceID string) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "serve.route:"+workspaceID); err != nil {
		return dbError(err)
	}
	return nil
}

// routePreconditionShape projects the presented provider precondition onto the
// shape serve.access_switches can record: an exact revision, or a confirmed
// absence that is only recordable at generation zero.
type routePreconditionShape struct {
	expectedRevision  string
	absenceReceiptID  string
	absenceObservedAt time.Time
}

func routePrecondition(expectedGeneration int64, precondition *api.ProviderRevisionPrecondition) (routePreconditionShape, error) {
	if precondition == nil {
		return routePreconditionShape{}, status.Error(codes.InvalidArgument, "a provider revision precondition (exact revision or confirmed absence) is required")
	}
	switch condition := precondition.GetCondition().(type) {
	case *api.ProviderRevisionPrecondition_ExactRevision:
		revision := strings.TrimSpace(condition.ExactRevision)
		if revision == "" {
			return routePreconditionShape{}, status.Error(codes.InvalidArgument, "an exact provider revision must not be empty")
		}
		return routePreconditionShape{expectedRevision: revision}, nil
	case *api.ProviderRevisionPrecondition_RequireAbsent:
		absence := condition.RequireAbsent
		if absence == nil || strings.TrimSpace(absence.GetReceiptId()) == "" || absence.GetObservedAt() == nil || !absence.GetObservedAt().IsValid() {
			return routePreconditionShape{}, status.Error(codes.InvalidArgument, "a confirmed absence requires its receipt and observation time")
		}
		if expectedGeneration != 0 {
			return routePreconditionShape{}, status.Errorf(codes.InvalidArgument, "%s: a confirmed-absence precondition is only recordable at route generation zero", ReasonRouteGenerationConflict)
		}
		return routePreconditionShape{absenceReceiptID: strings.TrimSpace(absence.GetReceiptId()), absenceObservedAt: absence.GetObservedAt().AsTime().UTC()}, nil
	default:
		return routePreconditionShape{}, status.Error(codes.InvalidArgument, "a provider revision precondition (exact revision or confirmed absence) is required")
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

func routeProviderCommandID(switchID string) string { return "serve-route:" + switchID }

// sha256Hex is the stable hex digest used to derive owner-local route identities
// from exact inputs, so a replay names the same row instead of allocating one.
func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func absenceColumns(plan routeSwitchPlan) (any, any) {
	if plan.expectedAbsenceReceiptID == "" {
		return nil, nil
	}
	return plan.expectedAbsenceReceiptID, plan.expectedAbsenceObservedAt.UTC()
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
