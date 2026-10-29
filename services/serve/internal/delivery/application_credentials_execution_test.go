package delivery

import (
	contracts "opl-cloud/packages/contracts/go"
	"testing"
)

func TestApplicationCredentialExecutorPreservesProviderRuntimeIdentity(t *testing.T) {
	command := executionCommand(t)
	target := executionTargetFixture(command)
	providerID := contracts.WorkspaceApplicationRuntimeID(command.GetRuntimeInstanceId())
	executor := &fakeExecutor{credentials: contracts.WorkspaceApplicationRuntimeCredentials{WorkspaceID: command.WorkspaceId, RuntimeID: providerID, WebUIUsername: "declared-admin", WebUIPassword: "fixture-password"}}
	adapter := executionAdapter(executor, RouteOrigin{})
	value, err := adapter.Credentials(t.Context(), command, target)
	if err != nil {
		t.Fatal(err)
	}
	if value.GetRuntimeInstanceId() != providerID || executor.input.RuntimeOperationID != command.RuntimeInstanceId {
		t.Fatal("provider identity does not follow the runtime input contract")
	}
}
