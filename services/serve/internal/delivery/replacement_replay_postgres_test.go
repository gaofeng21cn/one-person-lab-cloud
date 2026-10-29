package delivery_test

import (
	"reflect"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
)

// These are the pre-upgrade owner records: an accepted Agent replacement had no
// replacementState/exclusiveWriter, but its exact start was already committed.
func TestServeRetainedAgentReplacementReplaysCommittedStart(t *testing.T) {
	for _, kind := range []string{"update", "rollback"} {
		for _, stop := range []string{"confirmed", "unknown", "absent"} {
			t.Run(kind+"/"+stop, func(t *testing.T) {
				s, _, original, second, runtime := exclusiveSwitchFixture(t)
				predecessor := original.DeploymentId
				var invoke func() (*api.Operation, error)
				if kind == "update" {
					invoke = func() (*api.Operation, error) {
						return switchVersion(t, s, "retained-update", second.GetId(), predecessor)
					}
				} else {
					if _, err := switchVersion(t, s, "before-retained-rollback", second.GetId(), predecessor); err != nil {
						t.Fatal(err)
					}
					predecessor, _, _, _ = currentDeploymentFacts(t, s, "ws-first")
					c := call("tenant-alpha", false)
					c.IdempotencyKey = "retained-rollback"
					invoke = func() (*api.Operation, error) {
						return s.RollbackWorkspace(serveContext(), &api.RollbackWorkspaceRpcRequest{Context: c, WorkspaceId: "ws-first", Body: &api.RollbackWorkspaceRequest{TargetDeploymentId: original.DeploymentId, ExpectedCurrentAgentDeploymentId: predecessor}})
					}
				}
				runtime.state = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_PENDING
				pending, err := invoke()
				if err != nil || pending.GetStatus() == api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
					t.Fatalf("pending=%v err=%v", pending, err)
				}
				var replacement string
				if err = s.DB.QueryRow(`SELECT resource_id FROM serve.operations WHERE id=$1`, pending.OperationId).Scan(&replacement); err != nil {
					t.Fatal(err)
				}
				if _, err = s.DB.Exec(`UPDATE serve.operations SET accepted_input=accepted_input-'replacementState'-'exclusiveWriter' WHERE id=$1`, pending.OperationId); err != nil {
					t.Fatal(err)
				}
				// The old builder omitted DataCompatibility from the runtime command.
				// Rebuilding from today's plan would change these committed bytes.
				if _, err = s.DB.Exec(`UPDATE serve.agent_runtime_actions SET input_snapshot=input_snapshot#-'{command,dataCompatibility}' WHERE expected_deployment_id=$1 AND action='start'`, replacement); err != nil {
					t.Fatal(err)
				}
				if stop == "confirmed" {
					// Both retained formats are decoded by the existing owner decoder.
					if _, err = s.DB.Exec(`UPDATE serve.agent_runtime_actions SET input_snapshot=input_snapshot->'command' WHERE expected_deployment_id=$1 AND action='start'`, replacement); err != nil {
						t.Fatal(err)
					}
				}
				var frozenBefore []byte
				if err = s.DB.QueryRow(`SELECT input_snapshot FROM serve.agent_runtime_actions WHERE expected_deployment_id=$1 AND action='start'`, replacement).Scan(&frozenBefore); err != nil {
					t.Fatal(err)
				}
				if stop == "absent" {
					_, err = s.DB.Exec(`DELETE FROM serve.agent_runtime_actions WHERE action='stop' AND input_snapshot->>'replacementDeploymentId'=$1`, replacement)
				} else if stop == "unknown" {
					_, err = s.DB.Exec(`UPDATE serve.agent_runtime_actions SET observation_result='unknown' WHERE action='stop' AND input_snapshot->>'replacementDeploymentId'=$1`, replacement)
				}
				if err != nil {
					t.Fatal(err)
				}
				runtime.lifecycle = nil
				runtime.state = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY
				result, err := invoke()
				if err != nil || result.GetOperationId() != pending.GetOperationId() || result.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
					t.Fatalf("retained replay=%v err=%v", result, err)
				}
				var frozenAfter []byte
				if err = s.DB.QueryRow(`SELECT input_snapshot FROM serve.agent_runtime_actions WHERE expected_deployment_id=$1 AND action='start'`, replacement).Scan(&frozenAfter); err != nil {
					t.Fatal(err)
				}
				if string(frozenBefore) != string(frozenAfter) {
					t.Fatal("replay rewrote the committed runtime start")
				}
				if stop == "confirmed" && len(runtime.lifecycle) != 0 || stop != "confirmed" && !reflect.DeepEqual(runtime.lifecycle, []string{"suspended"}) {
					t.Fatalf("replay stop=%s lifecycle=%v", stop, runtime.lifecycle)
				}
				if _, target := routeBinding(t, s, "ws-first"); target != replacement {
					t.Fatal("ready retained replacement was not selected")
				}
			})
		}
	}
}

func TestNativeReplacementWithoutStartRequiresAcceptedPredecessor(t *testing.T) {
	for _, retained := range []bool{false, true} {
		name := "missing predecessor is refused"
		if retained {
			name = "accepted predecessor resumes"
		}
		t.Run(name, func(t *testing.T) {
			s, original, _, runtime, resources, gateway := nativeUpgradeFixture(t)
			first, credential := frozenUpgradeCommand(t, s, original.DeploymentId)
			request := nativeUpgradeRequest(original.DeploymentId, "before-native-start")
			runtime.stopFailure = true
			if _, err := s.UpdateWorkspaceVersion(serveContext(), request); err == nil {
				t.Fatal("unconfirmed stop reported success")
			}
			var operation, replacement string
			if err := s.DB.QueryRow(`SELECT id,resource_id FROM serve.operations WHERE kind='update_workspace'`).Scan(&operation, &replacement); err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.Exec(`DELETE FROM serve.agent_runtime_actions WHERE expected_deployment_id=$1 AND action='start'`, replacement); err != nil {
				t.Fatal(err)
			}
			if !retained {
				if _, err := s.DB.Exec(`UPDATE serve.operations SET accepted_input=accepted_input-'replacementState' WHERE id=$1`, operation); err != nil {
					t.Fatal(err)
				}
			}
			runtime.stopFailure = false
			before := len(runtime.events)
			result, err := s.UpdateWorkspaceVersion(serveContext(), request)
			if !retained {
				if status.Code(err) != codes.DataLoss || len(runtime.events) != before || runtime.starts != 1 {
					t.Fatalf("missing native predecessor was guessed: err=%v events=%v", err, runtime.events)
				}
				return
			}
			if err != nil || result.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || result.GetOperationId() != operation {
				t.Fatalf("accepted predecessor replay=%v err=%v", result, err)
			}
			command, nextCredential := frozenUpgradeCommand(t, s, replacement)
			if nextCredential != credential || !proto.Equal(command.GetManagedKeyBinding(), first.GetManagedKeyBinding()) || resources.binds != 1 || resources.rebinds != 0 || gateway.calls != 0 {
				t.Fatal("resumed native start changed the original credentials")
			}
		})
	}
}
