package delivery_test

import (
	"context"
	"reflect"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
)

// fabricMutationGuard proves a retirement never reaches Fabric's resource
// mutation surface: disks, PVC/PV, attachments and secrets stay with the owner
// that owns them, so a deletion deletes no data here.
type fabricMutationGuard struct {
	resourcesForServe
	deletes []string
}

func (f *fabricMutationGuard) DeleteResources(_ context.Context, c *api.MutateResourcesCommand, _ ...grpc.CallOption) (*api.Operation, error) {
	f.deletes = append(f.deletes, c.GetResourceSetId())
	return &api.Operation{OperationId: "fabric-delete-" + c.GetResourceSetId()}, nil
}

// contextualFabricReadback is a Fabric readback that validates the call context
// exactly as the real owner does: Fabric refuses a read whose context names no
// actor, request, scope or authority. It exists so a retirement that derived its
// owner calls from the persisted command's stripped context fails here instead of
// only against a live Fabric.
type contextualFabricReadback struct {
	api.FabricCoordinationClient
	reads int
	last  *api.CallContext
}

func (c *contextualFabricReadback) ReadResources(_ context.Context, r *api.ResourceReadbackRequest, _ ...grpc.CallOption) (*api.ResourceReadback, error) {
	c.reads++
	c.last = r.GetContext()
	if r.GetContext().GetActorId() == "" || r.GetContext().GetRequestId() == "" || r.GetContext().GetScope() == nil ||
		(r.GetContext().GetSessionId() == "" && r.GetContext().GetAcceptedOperationGrantId() == "") {
		return nil, status.Error(codes.Unauthenticated, "actor, request, scope and session or accepted grant are required")
	}
	return &api.ResourceReadback{ResourceSetId: r.GetResourceSetId(), WorkspaceId: "ws-first", Outcome: api.Observation_OBSERVATION_CONFIRMED,
		ExecutionResources:   &api.ResourceExecutionBinding{AccountId: "account-original", ComputeAllocationId: "compute-original", StorageVolumeId: "volume-original", DataAttachmentId: "attachment-original", DataAttachmentOperationId: "attach-operation-original"},
		ApplicationPlacement: &api.ApplicationExecutionPlacement{ComputeNodeName: "node-original", ComputePackageId: "basic", StoragePvcName: "pvc-original"}}, nil
}

func (c *contextualFabricReadback) DeleteResources(_ context.Context, r *api.MutateResourcesCommand, _ ...grpc.CallOption) (*api.Operation, error) {
	return &api.Operation{OperationId: "fabric-delete-" + r.GetResourceSetId()}, nil
}

// retirementCall is the Workspace owner's own call context for the deletion step
// that retires the application runtime.
func retirementCall(r *api.RuntimeReservationCommand) *api.CallContext {
	c := call(r.GetContext().GetScope().GetTenant().GetTenantId(), false)
	c.RequestId = "delete-workspace-1"
	c.IdempotencyKey = "delete-workspace-1"
	return c
}

// TestRetireStopsExactRuntimeWithoutSecondStopOrDataDeletion proves the
// coordination entry point the Workspace deletion chain calls: the admitted
// Workspace owner stops exactly the declared runtime once, every replay resolves
// to the recorded result instead of a second stop, and neither Serve nor Fabric
// loses any fact the caller retains.
func TestRetireStopsExactRuntimeWithoutSecondStopOrDataDeletion(t *testing.T) {
	s, r, _ := reservationFixture(t)
	ctx := workspaceContext()
	resources := &fabricMutationGuard{resourcesForServe: resourcesForServe{confirmed: true, workspace: "ws-first", dataAttachment: "attachment-original"}}
	runtime := &runtimeForServe{state: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY, readiness: "ready-original"}
	s.Resources = resources
	s.Runtime = runtime
	reservation, err := s.Reserve(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Deploy(ctx, deployReserved(r, reservation)); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	// The application is reachable before the deletion: Serve's own readiness and
	// access facts say so.
	if _, err = s.GetWorkspaceAccess(serveContext(), &api.GetWorkspaceAccessRpcRequest{Context: r.GetContext(), WorkspaceId: r.GetWorkspaceId()}); err != nil {
		t.Fatalf("delivered application reported no access: %v", err)
	}
	stop := &api.RuntimeStopCommand{Context: retirementCall(r), RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId, RetainedDataAttachmentId: "attachment-original"}
	// Only the Workspace owner reaches the coordination entry point.
	if _, err = s.Retire(serveContext(), stop); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign peer err=%v want permission denied", err)
	}
	if len(runtime.lifecycle) != 0 {
		t.Fatalf("a refused caller still reached the execution boundary: %v", runtime.lifecycle)
	}
	operation, err := s.Retire(ctx, stop)
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED ||
		operation.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_RUNTIME_RETIRE ||
		operation.GetStage() != api.OperationStageEnum_OPERATION_STAGE_ENUM_RETIREMENT ||
		operation.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE ||
		operation.GetResourceId() != reservation.RuntimeInstanceId ||
		operation.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED ||
		operation.GetOperationId() == "" {
		t.Fatalf("retire operation=%v", operation)
	}
	if !reflect.DeepEqual(runtime.lifecycle, []string{"suspended"}) {
		t.Fatalf("lifecycle=%v, want the single stop this retire applied", runtime.lifecycle)
	}
	var state, attachment string
	if err = s.DB.QueryRowContext(ctx, `SELECT status,COALESCE(data_attachment_contract->>'attachmentId','') FROM serve.agent_runtime_instances WHERE id=$1 AND deployment_id=$2`, reservation.RuntimeInstanceId, reservation.DeploymentId).Scan(&state, &attachment); err != nil {
		t.Fatal(err)
	}
	if state != "stopped" || attachment != "attachment-original" {
		t.Fatalf("runtime state=%q retained attachment=%q", state, attachment)
	}
	if len(resources.deletes) != 0 {
		t.Fatalf("the retire reached Fabric's resource deletion surface: %v", resources.deletes)
	}
	// Serve's own read surface follows its runtime record: a stopped application
	// reports no access entry, and the access decision is not composed from history.
	if access, err := s.GetWorkspaceAccess(serveContext(), &api.GetWorkspaceAccessRpcRequest{Context: r.GetContext(), WorkspaceId: r.GetWorkspaceId()}); status.Code(err) != codes.FailedPrecondition || access != nil {
		t.Fatalf("stopped application reported access=%v err=%v", access, err)
	}
	// A replay of the same deletion resumes its own recorded retire. It reports the
	// recorded operation and never issues a second stop.
	replayed, err := s.Retire(ctx, stop)
	if err != nil {
		t.Fatalf("replayed retire: %v", err)
	}
	if replayed.GetOperationId() != operation.GetOperationId() || replayed.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("replayed retire operation=%v first=%v", replayed, operation)
	}
	if !reflect.DeepEqual(runtime.lifecycle, []string{"suspended"}) {
		t.Fatalf("replayed retire issued another lifecycle call: %v", runtime.lifecycle)
	}
	// The retirement is recorded exactly once, under the schema's stop vocabulary,
	// with the provider-confirmed suspension as its evidence.
	var stops int
	var evidence string
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*),COALESCE(max(evidence_ref),'') FROM serve.agent_runtime_actions WHERE runtime_instance_id=$1 AND action='stop'`, reservation.RuntimeInstanceId).Scan(&stops, &evidence); err != nil {
		t.Fatal(err)
	}
	if stops != 1 || evidence != "suspended:"+reservation.DeploymentId {
		t.Fatalf("recorded stops=%d evidence=%q", stops, evidence)
	}
	// The declared identity is a precondition, never a hint: a substitute runtime or
	// deployment is refused and never resolved onto another runtime.
	for _, substitute := range []*api.RuntimeStopCommand{
		{RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: "dep-substitute", Context: retirementCall(r)},
		{RuntimeInstanceId: reservation.RuntimeInstanceId + "-other", DeploymentId: reservation.DeploymentId, Context: retirementCall(r)},
	} {
		if _, err = s.Retire(ctx, substitute); status.Code(err) != codes.NotFound {
			t.Fatalf("substitute %v err=%v want not found", substitute, err)
		}
	}
	if !reflect.DeepEqual(runtime.lifecycle, []string{"suspended"}) {
		t.Fatalf("a refused substitute reached the execution boundary: %v", runtime.lifecycle)
	}
}

// TestRetireReportsOwnerConfirmedAbsenceOnly proves a retire completes against
// Serve's own readback that the exact runtime is gone, and that an unreachable
// execution boundary is never reported as that absence: the unknown outcome stays
// unknown until an owner fact confirms it.
func TestRetireReportsOwnerConfirmedAbsenceOnly(t *testing.T) {
	s, r, _ := reservationFixture(t)
	ctx := workspaceContext()
	s.Resources = &resourcesForServe{confirmed: true, workspace: "ws-first", dataAttachment: "attachment-original"}
	runtime := &runtimeForServe{state: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY, readiness: "ready-original"}
	s.Runtime = runtime
	reservation, err := s.Reserve(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Deploy(ctx, deployReserved(r, reservation)); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	stop := &api.RuntimeStopCommand{Context: retirementCall(r), RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId, RetainedDataAttachmentId: "attachment-retained"}
	// No execution boundary and no Fabric readback are reachable. The retire must
	// not turn that into a stopped runtime or a claimed absence.
	s.Runtime = nil
	s.Resources = nil
	if _, err = s.Retire(ctx, stop); status.Code(err) != codes.Unavailable {
		t.Fatalf("unreachable execution boundary err=%v want unavailable", err)
	}
	var retirements int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM serve.operations WHERE kind='runtime_retire'`).Scan(&retirements); err != nil {
		t.Fatal(err)
	}
	if retirements != 0 {
		t.Fatalf("an unconfirmed retire recorded %d completed operations", retirements)
	}
	// Serve's own runtime record is the readback of the provider-confirmed
	// termination: the exact runtime is gone and the retire completes against it
	// without any further provider or Fabric call.
	if _, err = s.DB.ExecContext(ctx, `UPDATE serve.agent_runtime_instances SET status='terminated',observed_at=now(),updated_at=now() WHERE id=$1 AND deployment_id=$2`, reservation.RuntimeInstanceId, reservation.DeploymentId); err != nil {
		t.Fatal(err)
	}
	operation, err := s.Retire(ctx, stop)
	if err != nil {
		t.Fatalf("retire over a confirmed absent runtime: %v", err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED ||
		operation.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_RUNTIME_RETIRE ||
		operation.GetStage() != api.OperationStageEnum_OPERATION_STAGE_ENUM_RETIREMENT ||
		operation.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED ||
		operation.GetOperationId() == "" {
		t.Fatalf("absent retire operation=%v", operation)
	}
	var action, result, evidence, desired string
	if err = s.DB.QueryRowContext(ctx, `SELECT action,observation_result,COALESCE(evidence_ref,''),input_snapshot->>'desired' FROM serve.agent_runtime_actions WHERE runtime_instance_id=$1 AND action='stop'`, reservation.RuntimeInstanceId).Scan(&action, &result, &evidence, &desired); err != nil {
		t.Fatal(err)
	}
	if action != "stop" || result != "confirmed" || evidence != "absent:"+reservation.DeploymentId || desired != "suspended" {
		t.Fatalf("retire action=%q result=%q evidence=%q desired=%q", action, result, evidence, desired)
	}
	if len(runtime.lifecycle) != 0 {
		t.Fatalf("a confirmed absent runtime was stopped again: %v", runtime.lifecycle)
	}
	replayed, err := s.Retire(ctx, stop)
	if err != nil {
		t.Fatalf("replayed absent retire: %v", err)
	}
	if replayed.GetOperationId() != operation.GetOperationId() {
		t.Fatalf("replayed absent retire allocated a second operation: %v", replayed)
	}
}

// TestRetireDerivesOwnerCallsFromTheAdmittedWorkspaceContext proves the retirement
// reads the exact Fabric resource binding through the Workspace owner's own
// admitted call context. The frozen start command carries no context, so a
// retirement that forwarded the persisted command's stripped context would present
// no actor, request, scope or authority and Fabric would refuse it; this test fails
// in exactly that case instead of passing against a permissive stub.
func TestRetireDerivesOwnerCallsFromTheAdmittedWorkspaceContext(t *testing.T) {
	s, r, _ := reservationFixture(t)
	ctx := workspaceContext()
	resources := &contextualFabricReadback{}
	runtime := &runtimeForServe{state: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY, readiness: "ready-original"}
	s.Resources = resources
	s.Runtime = runtime
	reservation, err := s.Reserve(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Deploy(ctx, deployReserved(r, reservation)); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	stop := &api.RuntimeStopCommand{Context: retirementCall(r), RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId, RetainedDataAttachmentId: "attachment-original"}
	operation, err := s.Retire(ctx, stop)
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || operation.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED {
		t.Fatalf("retire operation=%v", operation)
	}
	if resources.reads == 0 || resources.last.GetActorId() != stop.GetContext().GetActorId() || resources.last.GetRequestId() != stop.GetContext().GetRequestId() ||
		resources.last.GetAcceptedOperationGrantId() != stop.GetContext().GetAcceptedOperationGrantId() || resources.last.GetScope().GetTenant().GetTenantId() != stop.GetContext().GetScope().GetTenant().GetTenantId() {
		t.Fatalf("Fabric readback was not derived from the admitted retirement context: %v", resources.last)
	}
	if !reflect.DeepEqual(runtime.lifecycle, []string{"suspended"}) {
		t.Fatalf("lifecycle=%v", runtime.lifecycle)
	}
}
