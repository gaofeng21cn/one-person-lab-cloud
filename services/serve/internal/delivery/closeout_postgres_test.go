package delivery_test

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
)

func TestRuntimeRetirementRejectsTheOriginalDeployReplay(t *testing.T) {
	service, request, _ := reservationFixture(t)
	ctx := workspaceContext()
	original, err := service.Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	service.Resources, service.Runtime = &resourcesForServe{confirmed: true}, &runtimeForServe{}
	deploy := deployReserved(request, original)
	if _, err = service.Deploy(ctx, deploy); err != nil {
		t.Fatal(err)
	}
	retired, err := service.Retire(ctx, &api.RuntimeStopCommand{Context: request.Context, RuntimeInstanceId: original.RuntimeInstanceId, DeploymentId: original.DeploymentId, RetainedDataAttachmentId: request.DataAttachmentId})
	if err != nil {
		t.Fatal(err)
	}
	if retired.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("runtime retirement unresolved: %v", retired)
	}
	if _, err = service.Deploy(ctx, deploy); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("retired runtime accepted a late deployment: %v", err)
	}
}

type closingResourceAuthority struct{ resourcesForServe }

func (r *closingResourceAuthority) ReadResources(ctx context.Context, request *api.ResourceReadbackRequest, opts ...grpc.CallOption) (*api.ResourceReadback, error) {
	if request.GetContext().GetActorId() == "" {
		return nil, status.Error(codes.PermissionDenied, "resource observation requires the closing command authority")
	}
	return r.resourcesForServe.ReadResources(ctx, request, opts...)
}

func TestRuntimeRetirementCarriesTheClosingAuthorityToFabric(t *testing.T) {
	service, request, _ := reservationFixture(t)
	ctx := workspaceContext()
	original, err := service.Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	service.Resources, service.Runtime = &resourcesForServe{confirmed: true}, &runtimeForServe{}
	if _, err = service.Deploy(ctx, deployReserved(request, original)); err != nil {
		t.Fatal(err)
	}
	service.Resources = &closingResourceAuthority{resourcesForServe{confirmed: true}}
	if _, err = service.Retire(ctx, &api.RuntimeStopCommand{Context: request.Context, RuntimeInstanceId: original.RuntimeInstanceId, DeploymentId: original.DeploymentId, RetainedDataAttachmentId: request.DataAttachmentId}); err != nil {
		t.Fatalf("retirement lost its caller authority before provider cleanup: %v", err)
	}
}

func TestRuntimeRetirementRemovesThePublishedRoute(t *testing.T) {
	service, request, _ := reservationFixture(t)
	ctx := workspaceContext()
	original, err := service.Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	service.Resources, service.Runtime = &resourcesForServe{confirmed: true}, &runtimeForServe{}
	if _, err = service.Deploy(ctx, deployReserved(request, original)); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Retire(ctx, &api.RuntimeStopCommand{Context: request.Context, RuntimeInstanceId: original.RuntimeInstanceId, DeploymentId: original.DeploymentId, RetainedDataAttachmentId: request.DataAttachmentId}); err != nil {
		t.Fatal(err)
	}
	route, err := service.ObserveRoute(ctx, &api.RouteObserveRequest{Context: request.Context, WorkspaceId: request.WorkspaceId})
	if err != nil {
		t.Fatal(err)
	}
	if route.GetTargetRuntimeInstanceId() != "" || route.GetTargetExecutionResourceId() != "" || route.GetTargetDeploymentId() != "" {
		t.Fatalf("retired application remains selected by Serve: %v", route)
	}
}

func TestRuntimeRetirementPersistsItsAbsentReadback(t *testing.T) {
	service, request, _ := reservationFixture(t)
	ctx := workspaceContext()
	original, err := service.Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	service.Resources, service.Runtime = &resourcesForServe{confirmed: true}, &runtimeForServe{}
	if _, err = service.Deploy(ctx, deployReserved(request, original)); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Retire(ctx, &api.RuntimeStopCommand{Context: request.Context, RuntimeInstanceId: original.RuntimeInstanceId, DeploymentId: original.DeploymentId, RetainedDataAttachmentId: request.DataAttachmentId}); err != nil {
		t.Fatal(err)
	}
	read, err := service.ObserveRuntime(servePeerContext(), &api.RuntimeReadbackRequest{RuntimeInstanceId: original.RuntimeInstanceId, DeploymentId: original.DeploymentId})
	if err != nil || read.GetState() != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATED || read.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || read.GetApplicationAvailable() || read.GetProcessReady() {
		t.Fatalf("Serve did not retain its confirmed retirement: %v %v", read, err)
	}
}

func TestRuntimeRetirementClosesAnUndeployedReservation(t *testing.T) {
	service, request, _ := reservationFixture(t)
	ctx := workspaceContext()
	original, err := service.Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	service.Resources, service.Runtime = &resourcesForServe{confirmed: true}, &runtimeForServe{}
	retired, err := service.Retire(ctx, &api.RuntimeStopCommand{Context: request.Context, RuntimeInstanceId: original.RuntimeInstanceId, DeploymentId: original.DeploymentId, RetainedDataAttachmentId: request.DataAttachmentId})
	if err != nil || retired.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("reserved original delivery cannot close: %v %v", retired, err)
	}
	if _, err = service.Deploy(ctx, deployReserved(request, original)); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("late start of the retired reservation: %v", err)
	}
}

func TestRuntimeRetirementRetainsFulfillmentHistory(t *testing.T) {
	for _, delivered := range []bool{false, true} {
		t.Run(map[bool]string{false: "never delivered", true: "delivered before acknowledgement"}[delivered], func(t *testing.T) {
			service, request, _ := reservationFixture(t)
			ctx := workspaceContext()
			original, err := service.Reserve(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			runtime := &runtimeForServe{state: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED}
			if delivered {
				runtime.state = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY
			}
			service.Resources, service.Runtime = &resourcesForServe{confirmed: true}, runtime
			if _, err = service.Deploy(ctx, deployReserved(request, original)); err != nil {
				t.Fatal(err)
			}
			if _, err = service.Retire(ctx, &api.RuntimeStopCommand{Context: request.Context, RuntimeInstanceId: original.RuntimeInstanceId, DeploymentId: original.DeploymentId, RetainedDataAttachmentId: request.DataAttachmentId}); err != nil {
				t.Fatal(err)
			}
			read, err := service.ReadRuntime(ctx, &api.RuntimeReadbackRequest{Context: request.Context, RuntimeInstanceId: original.RuntimeInstanceId, DeploymentId: original.DeploymentId})
			if err != nil || read.GetState() != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATED || read.GetNeverDeliveredConfirmed() == delivered || (read.GetFirstFulfilledAt() != nil) != delivered {
				t.Fatalf("retirement lost the original delivery fact: %v %v", read, err)
			}
		})
	}
}

func TestRuntimeRetirementFencesAnOriginalReservationBeforeItArrives(t *testing.T) {
	service, request, _ := reservationFixture(t)
	ctx := workspaceContext()
	service.Resources, service.Runtime = &resourcesForServe{confirmed: true}, &runtimeForServe{}
	closed, err := service.Retire(ctx, &api.RuntimeStopCommand{Context: request.Context, WorkspaceId: request.WorkspaceId, OriginalReservationIdempotencyKey: request.GetContext().GetIdempotencyKey()})
	if err != nil || closed.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("Serve could not close the original unstarted reservation: %v %v", closed, err)
	}
	read, err := service.ReadRuntime(ctx, &api.RuntimeReadbackRequest{Context: request.Context, WorkspaceId: request.WorkspaceId, OriginalReservationIdempotencyKey: request.GetContext().GetIdempotencyKey()})
	if err != nil || read.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || !read.GetNeverDeliveredConfirmed() || read.GetWorkspaceId() != request.WorkspaceId {
		t.Fatalf("Serve did not confirm its fenced delivery absence: %v %v", read, err)
	}
	if _, err = service.Reserve(ctx, request); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a late original reservation crossed closeout: %v", err)
	}
}

func TestRuntimeRetirementRejectsALateAdapterStart(t *testing.T) {
	service, request, _ := reservationFixture(t)
	ctx := workspaceContext()
	original, err := service.Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	service.Resources, service.Runtime = &resourcesForServe{confirmed: true}, &runtimeForServe{}
	command := deployReserved(request, original)
	if _, err = service.Deploy(ctx, command); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Retire(ctx, &api.RuntimeStopCommand{Context: request.Context, RuntimeInstanceId: original.RuntimeInstanceId, DeploymentId: original.DeploymentId, RetainedDataAttachmentId: request.DataAttachmentId}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.StartRuntime(servePeerContext(), command); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a late execution-adapter start crossed retirement: %v", err)
	}
}

func TestRuntimeRetirementRejectsALateReadyObservation(t *testing.T) {
	service, request, _ := reservationFixture(t)
	ctx := workspaceContext()
	original, err := service.Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	service.Resources, service.Runtime = &resourcesForServe{confirmed: true}, &runtimeForServe{}
	command := deployReserved(request, original)
	if _, err = service.Deploy(ctx, command); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Retire(ctx, &api.RuntimeStopCommand{Context: request.Context, RuntimeInstanceId: original.RuntimeInstanceId, DeploymentId: original.DeploymentId, RetainedDataAttachmentId: request.DataAttachmentId}); err != nil {
		t.Fatal(err)
	}
	_, err = service.RecordDeploymentObservation(ctx, command, runtimeReady(applicationEntry(), "https://ws-res.example/app", "readiness://late"))
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a late ready observation crossed retirement: %v", err)
	}
	read, err := service.ReadRuntime(ctx, &api.RuntimeReadbackRequest{Context: request.Context, RuntimeInstanceId: original.RuntimeInstanceId, DeploymentId: original.DeploymentId})
	if err != nil || read.GetState() != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATED || read.GetApplicationAvailable() {
		t.Fatalf("late observation changed the retired readback: %v %v", read, err)
	}
}

// Workspace retains its original order identity after Serve upgrades the application.
// Its deletion must close the successors without substituting that order identity.
func TestWorkspaceRetirementAfterVersionSwitch(t *testing.T) {
	s, request, catalog := reservationFixture(t)
	ctx := workspaceContext()
	next := replacementVersion(t, "cv-retire-next", strings.Repeat("2", 64))
	catalog.versions = map[string]*api.CapabilityVersion{next.GetId(): next}
	runtime := &runtimeForServe{}
	s.Resources, s.Runtime = &resourcesForServe{confirmed: true}, runtime
	first, err := s.Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Deploy(ctx, deployReserved(request, first)); err != nil {
		t.Fatal(err)
	}
	update := call("tenant-alpha", false)
	update.IdempotencyKey = "upgrade-before-delete"
	operation, err := s.UpdateWorkspaceVersion(serveContext(), &api.UpdateWorkspaceVersionRpcRequest{
		Context: update, WorkspaceId: request.WorkspaceId,
		Body: &api.UpdateWorkspaceVersionRequest{CapabilityVersionId: proto.String(next.GetId()), ExpectedCurrentAgentDeploymentId: first.DeploymentId},
	})
	if err != nil || operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("upgrade: %v %v", operation, err)
	}
	successor, _, _, _ := currentDeploymentFacts(t, s, request.WorkspaceId)
	if successor == first.DeploymentId {
		t.Fatal("upgrade did not replace the original deployment")
	}
	command := &api.RuntimeStopCommand{Context: request.Context, RuntimeInstanceId: first.RuntimeInstanceId, DeploymentId: first.DeploymentId, RetainedDataAttachmentId: request.DataAttachmentId}
	retired, err := s.Retire(ctx, command)
	if err != nil {
		t.Fatalf("delete after upgrade: %v", err)
	}
	if retired.GetResourceId() != first.RuntimeInstanceId || retired.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || retired.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED {
		t.Fatalf("original retirement: %v", retired)
	}
	var live int
	if err = s.DB.QueryRow(`SELECT count(*) FROM serve.agent_runtime_instances WHERE workspace_id=$1 AND status <> 'terminated'`, request.WorkspaceId).Scan(&live); err != nil || live != 0 {
		t.Fatalf("live runtimes after deletion: %d %v", live, err)
	}
	generation, target := routeBinding(t, s, request.WorkspaceId)
	if target != "" {
		t.Fatalf("retired successor remains routed: %s", target)
	}
	before := len(runtime.lifecycle)
	replay, err := s.Retire(ctx, command)
	if err != nil || replay.GetOperationId() != retired.GetOperationId() {
		t.Fatalf("retirement replay: %v %v", replay, err)
	}
	if after, _ := routeBinding(t, s, request.WorkspaceId); after != generation || len(runtime.lifecycle) != before {
		t.Fatal("retirement replay repeated a confirmed effect")
	}
}

func TestWorkspaceRetirementAfterDefaultAppUpgrade(t *testing.T) {
	s, original, _, runtime, resources, gateway := nativeUpgradeFixture(t)
	first, _ := frozenUpgradeCommand(t, s, original.GetDeploymentId())
	request := nativeUpgradeRequest(original.GetDeploymentId(), "native-upgrade-before-delete")
	upgraded, err := s.UpdateWorkspaceVersion(serveContext(), request)
	if err != nil || upgraded.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("default App upgrade: %v %v", upgraded, err)
	}
	bound, keys := resources.binds, gateway.calls
	closing := call("tenant-alpha", false)
	closing.IdempotencyKey = "native-delete:retire_runtime"
	result, err := s.Retire(workspaceContext(), &api.RuntimeStopCommand{Context: closing, RuntimeInstanceId: original.GetRuntimeInstanceId(), DeploymentId: original.GetDeploymentId(), RetainedDataAttachmentId: first.GetDataAttachmentId()})
	if err != nil || result.GetResourceId() != original.GetRuntimeInstanceId() || result.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("default App retirement: %v %v", result, err)
	}
	var absent int
	for _, event := range runtime.events {
		if event == "absent" {
			absent++
		}
	}
	if absent != 2 || resources.binds != bound || gateway.calls != keys {
		t.Fatalf("retirement effects: events=%v Secret binds=%d Gateway calls=%d", runtime.events, resources.binds, gateway.calls)
	}
	read, err := s.ReadRuntime(workspaceContext(), &api.RuntimeReadbackRequest{Context: closing, RuntimeInstanceId: original.GetRuntimeInstanceId(), DeploymentId: original.GetDeploymentId()})
	if err != nil || read.GetState() != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATED || read.GetFirstFulfilledAt() == nil || read.GetNeverDeliveredConfirmed() {
		t.Fatalf("original fulfillment was lost: %v %v", read, err)
	}
}
