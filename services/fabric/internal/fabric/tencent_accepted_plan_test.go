package fabric

import (
	"strings"
	"testing"

	api "opl-cloud/packages/contracts/go/api"
)

const acceptedPlanTencentProfile = `{"schemaVersion":1,"accountBindings":[{"tenantId":"tenant-a","accountId":"acct-a"}],"packages":[{"id":"basic","name":"Basic Workspace","available":true,"compute":{"id":"pool-basic-2c4g","server":"2c4g","cpu":2,"memoryGb":4,"diskGb":10,"instanceType":"SA5.MEDIUM4"},"nodePoolId":"np-basic","maxReplicas":20,"zone":"ap-guangzhou-3","storage":{"sizeGb":10,"diskType":"CLOUD_BSSD"},"billing":{"chargeType":"PREPAID","periodMonths":1,"renewFlag":"NOTIFY_AND_MANUAL_RENEW"}}]}`

func acceptedPlanProvider(t *testing.T, raw string, region string) *TencentProvider {
	t.Helper()
	profile, plans, err := decodeTencentProviderProfile([]byte(raw))
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	return &TencentProvider{profile: profile, plans: plans, region: region, storageDiskType: "CLOUD_BSSD"}
}

func acceptedPlanSnapshot() *api.ResourcePlanSnapshot {
	return &api.ResourcePlanSnapshot{
		ComputePlanId: "cp", StoragePlanId: "sp", ProviderProfileId: "tencent-tke",
		ProviderComputeSkuId: "pool-basic-2c4g", ProviderStorageSkuId: "CLOUD_BSSD",
		Vcpus: 2, MemoryMib: 4096, CapacityGib: 10, PrepaidMonths: 1,
		ProviderCapabilityVersion: "v1", Provider: "tencent-tke", Region: "ap-guangzhou", BillingMode: "PREPAID_MONTHLY",
	}
}

func TestTencentResolveAcceptedResourcePlanBindsProfileAndTenantAccount(t *testing.T) {
	provider := acceptedPlanProvider(t, acceptedPlanTencentProfile, "ap-guangzhou")
	bound, err := provider.ResolveAcceptedResourcePlan("tenant-a", acceptedPlanSnapshot())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if bound.PackageID != "basic" || bound.AccountID != "acct-a" || bound.NodePoolID != "np-basic" || bound.Zone != "ap-guangzhou-3" || bound.Region != "ap-guangzhou" {
		t.Fatalf("bound=%#v", bound)
	}
}

func TestTencentResolveAcceptedResourcePlanRejectsNonPrepaidMonthlyBilling(t *testing.T) {
	provider := acceptedPlanProvider(t, acceptedPlanTencentProfile, "ap-guangzhou")
	for _, mode := range []string{"POSTPAID_BY_HOUR", "PREPAID", "", "LOCAL_NO_CHARGE"} {
		plan := acceptedPlanSnapshot()
		plan.BillingMode = mode
		if _, err := provider.ResolveAcceptedResourcePlan("tenant-a", plan); err == nil {
			t.Fatalf("billing mode %q was accepted", mode)
		}
	}
	plan := acceptedPlanSnapshot()
	plan.PrepaidMonths = 0
	if _, err := provider.ResolveAcceptedResourcePlan("tenant-a", plan); err == nil {
		t.Fatal("zero prepaid months was accepted")
	}
}

func TestTencentResolveAcceptedResourcePlanRejectsUnboundTenant(t *testing.T) {
	provider := acceptedPlanProvider(t, acceptedPlanTencentProfile, "ap-guangzhou")
	if _, err := provider.ResolveAcceptedResourcePlan("tenant-b", acceptedPlanSnapshot()); err == nil {
		t.Fatal("unbound tenant resolved a provider account")
	}
	ambiguous := `{"schemaVersion":1,"accountBindings":[{"tenantId":"tenant-a","accountId":"acct-a"},{"tenantId":"tenant-a","accountId":"acct-b"}],"packages":[{"id":"basic","name":"Basic Workspace","available":true,"compute":{"id":"pool-basic-2c4g","server":"2c4g","cpu":2,"memoryGb":4,"diskGb":10,"instanceType":"SA5.MEDIUM4"},"nodePoolId":"np-basic","maxReplicas":20,"zone":"ap-guangzhou-3","storage":{"sizeGb":10,"diskType":"CLOUD_BSSD"},"billing":{"chargeType":"PREPAID","periodMonths":1,"renewFlag":"NOTIFY_AND_MANUAL_RENEW"}}]}`
	if _, _, err := decodeTencentProviderProfile([]byte(ambiguous)); err == nil {
		t.Fatal("ambiguous tenant binding was decoded")
	}
}

func TestTencentResolveAcceptedResourcePlanRejectsSubstitutedPlan(t *testing.T) {
	provider := acceptedPlanProvider(t, acceptedPlanTencentProfile, "ap-guangzhou")
	mutations := map[string]func(*api.ResourcePlanSnapshot){
		"provider":       func(p *api.ResourcePlanSnapshot) { p.Provider = "local-docker" },
		"profile":        func(p *api.ResourcePlanSnapshot) { p.ProviderProfileId = "other" },
		"region":         func(p *api.ResourcePlanSnapshot) { p.Region = "na-siliconvalley-1" },
		"compute sku":    func(p *api.ResourcePlanSnapshot) { p.ProviderComputeSkuId = "pool-other" },
		"storage sku":    func(p *api.ResourcePlanSnapshot) { p.ProviderStorageSkuId = "CLOUD_BASIC" },
		"cpu":            func(p *api.ResourcePlanSnapshot) { p.Vcpus = 8 },
		"memory":         func(p *api.ResourcePlanSnapshot) { p.MemoryMib = 16384 },
		"capacity":       func(p *api.ResourcePlanSnapshot) { p.CapacityGib = 100 },
		"prepaid months": func(p *api.ResourcePlanSnapshot) { p.PrepaidMonths = 2 },
	}
	for name, mutate := range mutations {
		plan := acceptedPlanSnapshot()
		mutate(plan)
		if _, err := provider.ResolveAcceptedResourcePlan("tenant-a", plan); err == nil {
			t.Fatalf("%s substitution was accepted", name)
		}
	}
}

func TestTencentResolveAcceptedResourcePlanRejectsUnconfiguredProvider(t *testing.T) {
	provider := acceptedPlanProvider(t, acceptedPlanTencentProfile, "")
	if _, err := provider.ResolveAcceptedResourcePlan("tenant-a", acceptedPlanSnapshot()); err == nil || !strings.Contains(err.Error(), "tencent_accepted_profile_mismatch") {
		t.Fatalf("unconfigured region err=%v", err)
	}
	broken := &TencentProvider{profileErr: ErrProviderPlanUnavailable, region: "ap-guangzhou"}
	if _, err := broken.ResolveAcceptedResourcePlan("tenant-a", acceptedPlanSnapshot()); err == nil {
		t.Fatal("unavailable provider resolved a plan")
	}
}
