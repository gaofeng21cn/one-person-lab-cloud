package delivery_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/serve/internal/delivery"
)

type canceledStartRuntime struct {
	runtimeForServe
	cancel context.CancelFunc
}

func (f *canceledStartRuntime) Start(context.Context, *api.RuntimeDeployCommand, delivery.ExecutionTarget) (delivery.RuntimeObservation, error) {
	f.cancel()
	return delivery.RuntimeObservation{}, status.Error(codes.Canceled, "request canceled during provider call")
}

func TestServeStartFailurePersistsOutsideTheExecutionTransaction(t *testing.T) {
	s, request, _ := reservationFixture(t)
	ctx := workspaceContext()
	reservation, err := s.Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	s.Resources = &resourcesForServe{confirmed: true}
	s.Runtime = &progressionBoundaryRuntime{startErr: status.Error(codes.Unavailable, "provider response unavailable")}
	if _, err = s.Deploy(ctx, deployReserved(request, reservation)); status.Code(err) != codes.Unavailable {
		t.Fatalf("deploy error = %v", err)
	}
	var result, actionCode, deploymentCode, runtimeCode string
	if err = s.DB.QueryRowContext(context.Background(), `SELECT a.observation_result, COALESCE(a.error_code,''), COALESCE(d.error_code,''), COALESCE(i.error_code,'') FROM serve.agent_runtime_actions a JOIN serve.agent_deployments d ON d.id=a.expected_deployment_id JOIN serve.agent_runtime_instances i ON i.id=a.runtime_instance_id WHERE d.id=$1`, reservation.GetDeploymentId()).Scan(&result, &actionCode, &deploymentCode, &runtimeCode); err != nil {
		t.Fatal(err)
	}
	if result != strings.ToLower(strings.TrimPrefix(api.Observation_OBSERVATION_REJECTED.String(), "OBSERVATION_")) || actionCode != strings.TrimPrefix(api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE.String(), "ERROR_CODE_ENUM_") || deploymentCode != actionCode || runtimeCode != actionCode {
		t.Fatalf("failure was not retained: result=%q action=%q deployment=%q runtime=%q", result, actionCode, deploymentCode, runtimeCode)
	}
	s.Runtime = &runtimeForServe{}
	if _, err = s.Deploy(ctx, deployReserved(request, reservation)); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRowContext(context.Background(), `SELECT COALESCE(a.error_code,''), COALESCE(d.error_code,''), COALESCE(i.error_code,'') FROM serve.agent_runtime_actions a JOIN serve.agent_deployments d ON d.id=a.expected_deployment_id JOIN serve.agent_runtime_instances i ON i.id=a.runtime_instance_id WHERE d.id=$1`, reservation.GetDeploymentId()).Scan(&actionCode, &deploymentCode, &runtimeCode); err != nil {
		t.Fatal(err)
	}
	if actionCode != "" || deploymentCode != "" || runtimeCode != "" {
		t.Fatalf("ready retained stale failure: action=%q deployment=%q runtime=%q", actionCode, deploymentCode, runtimeCode)
	}
}

func TestServePreAdmissionFailureDoesNotPoisonAnAdmittedStart(t *testing.T) {
	for _, change := range []string{"reservation", "frozen_key", "caller_secret_identity", "epoch", "runtime", "activated_retry"} {
		t.Run(change, func(t *testing.T) {
			s, request, reservation, _, resources := managedKeyFixture(t)
			s.Runtime = &runtimeForServe{state: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_PENDING}
			if change == "activated_retry" {
				s.Runtime = &runtimeForServe{}
			}
			ctx := workspaceContext()
			command := func() *api.RuntimeDeployCommand {
				c := deployReserved(request, reservation)
				c.ManagedKeyBinding = launchManagedKeyHandover(request.GetWorkspaceId(), "key-1")
				return c
			}
			if _, err := s.Deploy(ctx, command()); err != nil {
				t.Fatal(err)
			}
			invalid := command()
			switch change {
			case "reservation":
				invalid.ResourceSetId = "foreign-resource-set"
			case "frozen_key":
				invalid.ManagedKeyBinding.KeyBindingId = "other-key"
				invalid.ManagedKeyBinding.TargetSlot = "other-slot"
			case "caller_secret_identity":
				invalid.ManagedKeyBinding.SecretBindingId = "forged-binding"
			case "epoch":
				invalid.ExecutionEpoch++
			case "runtime":
				invalid.RuntimeInstanceId = "other-runtime"
			case "activated_retry":
				resources.conflict = true
			}
			if _, err := s.Deploy(ctx, invalid); err == nil {
				t.Fatalf("invalid reservation error = %v", err)
			}
			var result, actionCode, deploymentCode, runtimeCode string
			if err := s.DB.QueryRowContext(context.Background(), `SELECT a.observation_result, COALESCE(a.error_code,''), COALESCE(d.error_code,''), COALESCE(i.error_code,'') FROM serve.agent_runtime_actions a JOIN serve.agent_deployments d ON d.id=a.expected_deployment_id JOIN serve.agent_runtime_instances i ON i.id=a.runtime_instance_id WHERE d.id=$1`, reservation.GetDeploymentId()).Scan(&result, &actionCode, &deploymentCode, &runtimeCode); err != nil {
				t.Fatal(err)
			}
			if result != strings.ToLower(strings.TrimPrefix(api.Observation_OBSERVATION_CONFIRMED.String(), "OBSERVATION_")) || actionCode != "" || deploymentCode != "" || runtimeCode != "" {
				t.Fatalf("rejected foreign input poisoned original: result=%q action=%q deployment=%q runtime=%q", result, actionCode, deploymentCode, runtimeCode)
			}
		})
	}
}

func TestServeCanceledStartRetainsDiagnosticAndOriginalError(t *testing.T) {
	s, request, _ := reservationFixture(t)
	ctx, cancel := context.WithCancel(workspaceContext())
	defer cancel()
	reservation, err := s.Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	s.Resources = &resourcesForServe{confirmed: true}
	s.Runtime = &canceledStartRuntime{cancel: cancel}
	if _, err = s.Deploy(ctx, deployReserved(request, reservation)); status.Code(err) != codes.Canceled {
		t.Fatalf("cancellation was masked: %v", err)
	}
	var code string
	if err = s.DB.QueryRowContext(context.Background(), `SELECT COALESCE(error_code,'') FROM serve.agent_deployments WHERE id=$1`, reservation.GetDeploymentId()).Scan(&code); err != nil {
		t.Fatal(err)
	}
	if code != strings.TrimPrefix(api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE.String(), "ERROR_CODE_ENUM_") {
		t.Fatalf("canceled start diagnostic=%q", code)
	}
}

func TestServePreAdmissionFailureRetainsOnlyTheReservedFailure(t *testing.T) {
	s, request, reservation, _, resources := managedKeyFixture(t)
	resources.conflict = true
	ctx := workspaceContext()
	command := func() *api.RuntimeDeployCommand {
		c := deployReserved(request, reservation)
		c.ManagedKeyBinding = launchManagedKeyHandover(request.GetWorkspaceId(), "key-1")
		return c
	}
	if _, err := s.Deploy(ctx, command()); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Secret refusal changed: %v", err)
	}
	var actionCount int
	var deploymentCode, runtimeCode string
	if err := s.DB.QueryRowContext(context.Background(), `SELECT (SELECT count(*) FROM serve.agent_runtime_actions WHERE expected_deployment_id=d.id),COALESCE(d.error_code,''),COALESCE(i.error_code,'') FROM serve.agent_deployments d JOIN serve.agent_runtime_instances i ON i.id=d.runtime_instance_id WHERE d.id=$1`, reservation.GetDeploymentId()).Scan(&actionCount, &deploymentCode, &runtimeCode); err != nil {
		t.Fatal(err)
	}
	if actionCount != 0 || deploymentCode != strings.TrimPrefix(api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR.String(), "ERROR_CODE_ENUM_") || runtimeCode != deploymentCode {
		t.Fatalf("reserved diagnostic=%d %q %q", actionCount, deploymentCode, runtimeCode)
	}
	resources.conflict = false
	if _, err := s.Deploy(ctx, command()); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRowContext(context.Background(), `SELECT COALESCE(d.error_code,''),COALESCE(i.error_code,'') FROM serve.agent_deployments d JOIN serve.agent_runtime_instances i ON i.id=d.runtime_instance_id WHERE d.id=$1`, reservation.GetDeploymentId()).Scan(&deploymentCode, &runtimeCode); err != nil {
		t.Fatal(err)
	}
	if deploymentCode != "" || runtimeCode != "" {
		t.Fatalf("ready retained pre-admission failure=%q %q", deploymentCode, runtimeCode)
	}
}

func TestServeReadProgressionFailureIsDurableWithoutRestartOnPoll(t *testing.T) {
	s, request, _ := reservationFixture(t)
	ctx := workspaceContext()
	reservation, err := s.Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	s.Resources = &resourcesForServe{confirmed: true}
	runtime := &progressionBoundaryRuntime{runtimeForServe: runtimeForServe{state: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING}}
	s.Runtime = runtime
	if _, err = s.Deploy(ctx, deployReserved(request, reservation)); err != nil {
		t.Fatal(err)
	}
	runtime.startErr = status.Error(codes.Unavailable, "provider unavailable during convergence")
	read := &api.RuntimeReadbackRequest{Context: request.Context, RuntimeInstanceId: reservation.GetRuntimeInstanceId(), DeploymentId: reservation.GetDeploymentId()}
	if _, err = s.ReadRuntime(ctx, read); status.Code(err) != codes.Unavailable {
		t.Fatalf("progression error=%v", err)
	}
	var code string
	if err = s.DB.QueryRowContext(context.Background(), `SELECT COALESCE(error_code,'') FROM serve.agent_deployments WHERE id=$1`, reservation.GetDeploymentId()).Scan(&code); err != nil {
		t.Fatal(err)
	}
	if code != strings.TrimPrefix(api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE.String(), "ERROR_CODE_ENUM_") {
		t.Fatalf("progression failure was not retained: %q", code)
	}
	if _, err = s.ReadRuntime(ctx, read); err != nil {
		t.Fatal(err)
	}
	if runtime.starts != 2 {
		t.Fatalf("poll retried a rejected provider call: starts=%d", runtime.starts)
	}
}
