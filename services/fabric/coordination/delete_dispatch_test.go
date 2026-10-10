package coordination_test

import (
	"context"
	"testing"

	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/fabric/internal/fabric"
)

// TestLocalDispatcherDeletionNeverReportsAbsenceWithoutOwnerReadback proves the
// provider adapter's release step: a handle the owning surface cannot identify or
// read back stays an error — never absent — so the coordination layer can only
// record it as unknown or pending. No provider mutation is dispatched for a
// handle the owner never confirmed.
func TestLocalDispatcherDeletionNeverReportsAbsenceWithoutOwnerReadback(t *testing.T) {
	t.Setenv("OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON", `{"schemaVersion":1,"profileId":"profile","capabilityVersion":"v1","region":"local","accountBindings":[{"tenantId":"tenant","accountId":"account"}],"packages":[{"id":"package","name":"Package","available":true,"compute":{"id":"compute-sku","server":"2c4g","cpu":2,"memoryGb":4,"instanceType":"local-2c4g"},"storage":{"id":"storage-sku","sizeGb":10,"quotaPolicy":"linux-project"}}]}`)
	provider := fabric.NewLocalDockerProvider()
	service := fabric.NewService(provider)
	dispatcher := coordination.NewLocalDispatcher(service, provider)
	intent := coordination.ResourceDeletionIntent{TenantID: "tenant", WorkspaceID: "workspace-unreadback", OperationID: "op-unreadback", ResourceSetID: "rset-unreadback", AccountID: "account", ComputeID: "res_missing_compute", StorageID: "res_missing_storage"}
	for _, kind := range []string{coordination.DeletionKindStorage, coordination.DeletionKindCompute} {
		fact, err := dispatcher.DeleteResource(context.Background(), intent, kind)
		if err == nil || fact.State == coordination.DeletionStateAbsent {
			t.Fatalf("kind %s reported %q with err=%v", kind, fact.State, err)
		}
	}
	// The mount binding has no provider-confirmed identity in this intent, so the
	// adapter refuses instead of assuming the binding is gone.
	fact, err := dispatcher.DeleteResource(context.Background(), intent, coordination.DeletionKindAttachment)
	if err == nil || fact.State == coordination.DeletionStateAbsent {
		t.Fatalf("an unowned attachment reported %q with err=%v", fact.State, err)
	}
}
