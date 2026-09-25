//go:build !linux

package coordination_test

import (
	"context"
	"strings"
	"testing"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/fabric/internal/fabric"
)

// The real provider must reject an unsupported host before creating a network;
// Docker Desktop's Linux daemon is not the Fabric host's project-quota backend.
func TestLocalDispatcherRejectsUnsupportedHostQuota(t *testing.T) {
	t.Setenv("OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON", `{"schemaVersion":1,"profileId":"profile","capabilityVersion":"v1","region":"local","accountBindings":[{"tenantId":"tenant","accountId":"account"}],"packages":[{"id":"package","name":"Package","available":true,"compute":{"id":"compute-sku","server":"2c4g","cpu":2,"memoryGb":4,"instanceType":"local-2c4g"},"storage":{"id":"storage-sku","sizeGb":10,"quotaPolicy":"linux-project"}}]}`)
	provider := fabric.NewLocalDockerProvider()
	service := fabric.NewService(provider)
	dispatcher := coordination.NewLocalDispatcher(service, provider)
	_, err := dispatcher.EnsureLocal(context.Background(), coordination.LocalResourceIntent{TenantID: "tenant", WorkspaceID: "quota-guard-never-created", OperationID: "quota-guard", ComputeID: "quota-compute-never-created", StorageID: "quota-storage-never-created", Plan: &api.ResourcePlanSnapshot{Provider: "local-docker", ProviderProfileId: "profile", ProviderCapabilityVersion: "v1", Region: "local", BillingMode: "LOCAL_NO_CHARGE", ProviderComputeSkuId: "compute-sku", ProviderStorageSkuId: "storage-sku", Vcpus: 2, MemoryMib: 4096, CapacityGib: 10}})
	if err == nil || !strings.Contains(err.Error(), "local_docker_storage_quota_unavailable") {
		t.Fatalf("host quota guard=%v", err)
	}
	if _, found := service.GetComputeAllocation(context.Background(), "quota-compute-never-created"); found {
		t.Fatal("compute was dispatched before quota admission")
	}
}
