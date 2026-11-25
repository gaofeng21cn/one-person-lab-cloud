package fabric

import (
	"strings"
	"testing"

	api "opl-cloud/packages/contracts/go/api"
)

const acceptedPlanTencentPackages = `"packages":[{"id":"basic","name":"Basic Workspace","available":true,"compute":{"id":"pool-basic-2c4g","server":"2c4g","cpu":2,"memoryGb":4,"diskGb":10,"instanceType":"SA5.MEDIUM4"},"nodePoolId":"np-basic","maxReplicas":20,"zone":"ap-guangzhou-3","storage":{"sizeGb":10,"diskType":"CLOUD_BSSD"},"billing":{"chargeType":"PREPAID","periodMonths":1,"renewFlag":"NOTIFY_AND_MANUAL_RENEW"}}]}`

const acceptedPlanTencentProfile = `{"schemaVersion":1,"accountBindings":[{"tenantId":"tenant-a","accountId":"acct-a"}],` + acceptedPlanTencentPackages

// acceptedPlanTencentProfileWithDefault is the same profile with the account
// this installation declares for every tenant that no binding names.
const acceptedPlanTencentProfileWithDefault = `{"schemaVersion":1,"defaultAccountId":"acct-default","accountBindings":[{"tenantId":"tenant-a","accountId":"acct-a"}],` + acceptedPlanTencentPackages

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
	if _, err := provider.ResolveAcceptedResourcePlan("tenant-b", acceptedPlanSnapshot()); err == nil || !strings.Contains(err.Error(), "tencent_account_binding_required") {
		t.Fatalf("unbound tenant resolved a provider account: %v", err)
	}
	ambiguous := `{"schemaVersion":1,"accountBindings":[{"tenantId":"tenant-a","accountId":"acct-a"},{"tenantId":"tenant-a","accountId":"acct-b"}],"packages":[{"id":"basic","name":"Basic Workspace","available":true,"compute":{"id":"pool-basic-2c4g","server":"2c4g","cpu":2,"memoryGb":4,"diskGb":10,"instanceType":"SA5.MEDIUM4"},"nodePoolId":"np-basic","maxReplicas":20,"zone":"ap-guangzhou-3","storage":{"sizeGb":10,"diskType":"CLOUD_BSSD"},"billing":{"chargeType":"PREPAID","periodMonths":1,"renewFlag":"NOTIFY_AND_MANUAL_RENEW"}}]}`
	if _, _, err := decodeTencentProviderProfile([]byte(ambiguous)); err == nil {
		t.Fatal("ambiguous tenant binding was decoded")
	}
}

// TestTencentResolveAcceptedResourcePlanUsesTheInstallationDefaultAccount
// proves the operator deadlock is gone: a tenant that no binding names runs in
// the account this installation declares, so onboarding a customer needs no
// profile edit, no restart and no per-tenant account list.
func TestTencentResolveAcceptedResourcePlanUsesTheInstallationDefaultAccount(t *testing.T) {
	provider := acceptedPlanProvider(t, acceptedPlanTencentProfileWithDefault, "ap-guangzhou")
	bound, err := provider.ResolveAcceptedResourcePlan("tenant-b", acceptedPlanSnapshot())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if bound.AccountID != "acct-default" || bound.PackageID != "basic" || bound.NodePoolID != "np-basic" {
		t.Fatalf("bound=%#v", bound)
	}
	// The binding that names the tenant still wins over the installation
	// default, so declaring a default never changes an existing tenant.
	bound, err = provider.ResolveAcceptedResourcePlan("tenant-a", acceptedPlanSnapshot())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if bound.AccountID != "acct-a" {
		t.Fatalf("bound=%#v", bound)
	}
}

// TestTencentProviderProfileRejectsAPaddedDefaultAccount proves the declared
// default is read exactly as the profile states it: a padded account is a
// malformed profile, not a silently trimmed one.
func TestTencentProviderProfileRejectsAPaddedDefaultAccount(t *testing.T) {
	padded := strings.Replace(acceptedPlanTencentProfileWithDefault, `"defaultAccountId":"acct-default"`, `"defaultAccountId":" acct-default "`, 1)
	if _, _, err := decodeTencentProviderProfile([]byte(padded)); err == nil {
		t.Fatal("a padded default account was decoded")
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
