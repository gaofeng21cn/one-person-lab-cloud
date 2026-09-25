package fabric

import (
	"encoding/json"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"testing"
)

func TestLocalDockerAcceptedResourcePlanExactBinding(t *testing.T) {
	profile := localDockerProviderProfile{SchemaVersion: 1, ProfileID: "profile", CapabilityVersion: "v1", Region: "local", AccountBindings: []localDockerAccountBinding{{TenantID: "tenant", AccountID: "legacy-account"}}, Packages: []localDockerPackageProfile{{ID: "package-is-not-the-sku", Name: "Local", Available: true, Compute: ComputePlan{ID: "compute-sku", Server: "2c4g", CPU: 2, MemoryGB: 4, InstanceType: "local-2c4g"}, Storage: localDockerStoragePlan{ID: "storage-sku", SizeGB: 10, QuotaPolicy: "linux-project"}}}}
	raw, _ := json.Marshal(profile)
	provider := newLocalDockerProvider(LocalDockerProviderConfig{ProviderProfileJSON: raw}, &recordingDockerRunner{})
	plan := &api.ResourcePlanSnapshot{Provider: "local-docker", ProviderProfileId: "profile", ProviderCapabilityVersion: "v1", Region: "local", BillingMode: "LOCAL_NO_CHARGE", ProviderComputeSkuId: "compute-sku", ProviderStorageSkuId: "storage-sku", Vcpus: 2, MemoryMib: 4096, CapacityGib: 10}
	got, err := provider.ResolveAcceptedResourcePlan("tenant", plan)
	if err != nil || got.PackageID != "package-is-not-the-sku" || got.AccountID != "legacy-account" || got.NodePoolID != "local-docker" {
		t.Fatalf("exact binding=%+v %v", got, err)
	}
	for name, mutate := range map[string]func(*api.ResourcePlanSnapshot){"profile": func(p *api.ResourcePlanSnapshot) { p.ProviderProfileId = "other" }, "capability": func(p *api.ResourcePlanSnapshot) { p.ProviderCapabilityVersion = "other" }, "region": func(p *api.ResourcePlanSnapshot) { p.Region = "other" }, "storage SKU": func(p *api.ResourcePlanSnapshot) { p.ProviderStorageSkuId = "other" }, "compute SKU": func(p *api.ResourcePlanSnapshot) { p.ProviderComputeSkuId = "package-is-not-the-sku" }, "size": func(p *api.ResourcePlanSnapshot) { p.CapacityGib = 20 }, "memory": func(p *api.ResourcePlanSnapshot) { p.MemoryMib = 2048 }, "provider": func(p *api.ResourcePlanSnapshot) { p.Provider = "tencent-tke" }, "billing": func(p *api.ResourcePlanSnapshot) { p.BillingMode = "PREPAID_MONTHLY" }} {
		t.Run(name, func(t *testing.T) {
			changed := proto.Clone(plan).(*api.ResourcePlanSnapshot)
			mutate(changed)
			if _, err := provider.ResolveAcceptedResourcePlan("tenant", changed); err == nil {
				t.Fatal("mismatch accepted")
			}
		})
	}
	if _, err := provider.ResolveAcceptedResourcePlan("unknown", plan); err == nil {
		t.Fatal("tenant was guessed as legacy account")
	}
	provider.profile.AccountBindings = append(provider.profile.AccountBindings, provider.profile.AccountBindings[0])
	if _, err := provider.ResolveAcceptedResourcePlan("tenant", plan); err == nil {
		t.Fatal("ambiguous account mapping accepted")
	}
}
