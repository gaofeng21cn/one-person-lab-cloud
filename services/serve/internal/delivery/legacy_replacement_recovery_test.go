package delivery_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/serve/internal/delivery"
)

type legacyRecoveryNoCatalog struct {
	api.CapabilityProductServiceClient
}

func (legacyRecoveryNoCatalog) GetCapabilityVersion(context.Context, *api.GetCapabilityVersionRpcRequest, ...grpc.CallOption) (*api.CapabilityVersion, error) {
	return nil, status.Error(codes.Unavailable, "legacy recovery must not consult a live catalog")
}

// BindReference fails after admission commits and before any replacement start or
// stop exists. Removing only the two new fields restores the actual base format;
// no runtime action is deleted to manufacture the interruption.
func legacyPreStartFailure(t *testing.T, s *delivery.Service, invoke func() (*api.Operation, error), kind string) (string, string, []byte) {
	t.Helper()
	claims := s.References.(*capabilityForServe)
	claims.bindFail = true
	if _, err := invoke(); status.Code(err) != codes.Unavailable {
		t.Fatalf("pre-start BindReference failure=%v", err)
	}
	var operation, deployment string
	if err := s.DB.QueryRow(`SELECT id,resource_id FROM serve.operations WHERE kind=$1 AND status='accepted'`, kind).Scan(&operation, &deployment); err != nil {
		t.Fatal(err)
	}
	var actions int
	if err := s.DB.QueryRow(`SELECT count(*) FROM serve.agent_runtime_actions WHERE expected_deployment_id=$1`, deployment).Scan(&actions); err != nil || actions != 0 {
		t.Fatalf("replacement already executed: actions=%d err=%v", actions, err)
	}
	var accepted []byte
	if err := s.DB.QueryRow(`UPDATE serve.operations SET accepted_input=accepted_input-'replacementState'-'exclusiveWriter' WHERE id=$1 RETURNING accepted_input`, operation).Scan(&accepted); err != nil {
		t.Fatal(err)
	}
	claims.bindFail = false
	s.Capability = legacyRecoveryNoCatalog{}
	return operation, deployment, accepted
}

func TestLegacyAgentReplacementRecoversBeforeStart(t *testing.T) {
	for _, kind := range []string{"update", "rollback"} {
		for _, format := range []string{"wrapped", "legacy command"} {
			t.Run(kind+"/"+format, func(t *testing.T) {
				s, _, original, second, runtime := exclusiveSwitchFixture(t)
				predecessor := original.DeploymentId
				invoke := func() (*api.Operation, error) {
					return switchVersion(t, s, "legacy-before-start", second.GetId(), predecessor)
				}
				operationKind := "update_workspace"
				if kind == "rollback" {
					if _, err := switchVersion(t, s, "before-legacy-rollback", second.GetId(), predecessor); err != nil {
						t.Fatal(err)
					}
					predecessor, _, _, _ = currentDeploymentFacts(t, s, "ws-first")
					c := call("tenant-alpha", false)
					c.IdempotencyKey = "legacy-before-start"
					invoke = func() (*api.Operation, error) {
						return s.RollbackWorkspace(serveContext(), &api.RollbackWorkspaceRpcRequest{Context: c, WorkspaceId: "ws-first", Body: &api.RollbackWorkspaceRequest{TargetDeploymentId: original.DeploymentId, ExpectedCurrentAgentDeploymentId: predecessor}})
					}
					operationKind = "rollback_workspace"
				}
				if format == "legacy command" {
					if _, err := s.DB.Exec(`UPDATE serve.agent_runtime_actions SET input_snapshot=input_snapshot->'command' WHERE expected_deployment_id=$1 AND action='start'`, predecessor); err != nil {
						t.Fatal(err)
					}
				}
				runtime.lifecycle = nil
				starts := runtime.starts
				operation, deployment, accepted := legacyPreStartFailure(t, s, invoke, operationKind)
				claims := s.References.(*capabilityForServe)
				acquired := claims.acquired
				// A second lost bind happens after migration commits. The next retry
				// must use the frozen migration, with no provider call before binding.
				claims.bindFail = true
				if _, err := invoke(); status.Code(err) != codes.Unavailable || runtime.starts != starts || len(runtime.lifecycle) != 0 {
					t.Fatalf("migrated bind retry=%v starts=%d lifecycle=%v", err, runtime.starts-starts, runtime.lifecycle)
				}
				claims.bindFail = false
				var predecessorStart []byte
				if err := s.DB.QueryRow(`SELECT input_snapshot FROM serve.agent_runtime_actions WHERE expected_deployment_id=$1 AND action='start'`, predecessor).Scan(&predecessorStart); err != nil {
					t.Fatal(err)
				}
				result, err := invoke()
				if err != nil || result.GetOperationId() != operation || result.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
					t.Fatalf("legacy pre-start recovery=%v err=%v", result, err)
				}
				if runtime.starts != starts+1 || !reflect.DeepEqual(runtime.lifecycle, []string{"suspended"}) || claims.acquired != acquired {
					t.Fatalf("starts=%d lifecycle=%v claims=%d", runtime.starts-starts, runtime.lifecycle, claims.acquired-acquired)
				}
				if id, _, _, previous := currentDeploymentFacts(t, s, "ws-first"); id != deployment || previous != predecessor || activeDeployments(t, s, "ws-first") != 1 {
					t.Fatal("recovery changed the admitted replacement lineage")
				}
				if _, target := routeBinding(t, s, "ws-first"); target != deployment {
					t.Fatal("recovered replacement was not selected")
				}
				command, _ := frozenUpgradeCommand(t, s, deployment)
				if command.GetResourceSetId() != "resource-set-original" || command.GetDataAttachmentId() != "attachment-original" {
					t.Fatal("recovery changed original resources")
				}
				var migrated, predecessorAfter []byte
				if err := s.DB.QueryRow(`SELECT accepted_input FROM serve.operations WHERE id=$1`, operation).Scan(&migrated); err != nil {
					t.Fatal(err)
				}
				var before, after map[string]json.RawMessage
				if json.Unmarshal(accepted, &before) != nil || json.Unmarshal(migrated, &after) != nil || len(after["replacementState"]) == 0 || string(after["exclusiveWriter"]) != "true" {
					t.Fatal("legacy recovery did not freeze the migrated predecessor decision")
				}
				delete(after, "replacementState")
				delete(after, "exclusiveWriter")
				if !reflect.DeepEqual(before, after) {
					t.Fatal("migration rewrote original admission evidence")
				}
				if err := s.DB.QueryRow(`SELECT input_snapshot FROM serve.agent_runtime_actions WHERE expected_deployment_id=$1 AND action='start'`, predecessor).Scan(&predecessorAfter); err != nil || string(predecessorAfter) != string(predecessorStart) {
					t.Fatalf("migration rewrote predecessor start: %v", err)
				}
			})
		}
	}
}

func TestLegacyReplacementRecoveryRejectsAmbiguousOwnerState(t *testing.T) {
	for _, name := range []string{"new Agent missing state only", "changed admitted request", "predecessor reloaded", "changed predecessor start"} {
		t.Run(name, func(t *testing.T) {
			s, _, original, second, runtime := exclusiveSwitchFixture(t)
			invoke := func() (*api.Operation, error) {
				return switchVersion(t, s, "ambiguous-legacy", second.GetId(), original.DeploymentId)
			}
			operation, _, _ := legacyPreStartFailure(t, s, invoke, "update_workspace")
			var err error
			switch name {
			case "new Agent missing state only":
				_, err = s.DB.Exec(`UPDATE serve.operations SET accepted_input=accepted_input || '{"exclusiveWriter":true}'::jsonb WHERE id=$1`, operation)
			case "changed admitted request":
				_, err = s.DB.Exec(`UPDATE serve.operations SET accepted_input=jsonb_set(accepted_input,'{request,resource_set_id}','"foreign-resources"') WHERE id=$1`, operation)
			case "predecessor reloaded":
				_, err = s.DB.Exec(`INSERT INTO serve.agent_runtime_actions(id,command_id,runtime_instance_id,expected_deployment_id,action,input_snapshot,observation_result) VALUES('ambiguous-reload','ambiguous-reload',$1,$2,'reload','{"targetVersion":"7","selections":"digest-only"}','confirmed')`, original.RuntimeInstanceId, original.DeploymentId)
			case "changed predecessor start":
				_, err = s.DB.Exec(`UPDATE serve.agent_runtime_actions SET input_snapshot=jsonb_set(input_snapshot,'{command,resource_set_id}','"foreign-resources"') WHERE expected_deployment_id=$1 AND action='start'`, original.DeploymentId)
			}
			if err != nil {
				t.Fatal(err)
			}
			starts := runtime.starts
			if _, err = invoke(); status.Code(err) != codes.DataLoss || runtime.starts != starts || len(runtime.lifecycle) != 0 {
				t.Fatalf("ambiguous predecessor reached execution: err=%v starts=%d lifecycle=%v", err, runtime.starts-starts, runtime.lifecycle)
			}
			var migrated bool
			if err = s.DB.QueryRow(`SELECT accepted_input ? 'replacementState' FROM serve.operations WHERE id=$1`, operation).Scan(&migrated); err != nil || migrated {
				t.Fatalf("rejected migration persisted a guessed state: migrated=%v err=%v", migrated, err)
			}
		})
	}
}

func TestLegacyReplacementRecoveryRejectsMissingNativeState(t *testing.T) {
	for _, removeBoth := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing state", true: "legacy-shaped native"}[removeBoth], func(t *testing.T) {
			s, original, _, runtime, _, _ := nativeUpgradeFixture(t)
			request := nativeUpgradeRequest(original.DeploymentId, "native-before-start")
			claims := s.References.(*capabilityForServe)
			claims.bindFail = true
			if _, err := s.UpdateWorkspaceVersion(serveContext(), request); status.Code(err) != codes.Unavailable {
				t.Fatalf("native pre-start fault=%v", err)
			}
			if _, err := s.DB.Exec(`UPDATE serve.operations SET accepted_input=accepted_input-'replacementState' WHERE kind='update_workspace'`); err != nil {
				t.Fatal(err)
			}
			if removeBoth {
				if _, err := s.DB.Exec(`UPDATE serve.operations SET accepted_input=accepted_input-'exclusiveWriter' WHERE kind='update_workspace'`); err != nil {
					t.Fatal(err)
				}
			}
			claims.bindFail = false
			starts := runtime.starts
			if _, err := s.UpdateWorkspaceVersion(serveContext(), request); status.Code(err) != codes.DataLoss || runtime.starts != starts || len(runtime.events) != 0 {
				t.Fatalf("native missing state reached execution: err=%v starts=%d events=%v", err, runtime.starts-starts, runtime.events)
			}
		})
	}
}
