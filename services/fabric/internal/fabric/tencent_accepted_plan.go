package fabric

import (
	"fmt"
	"strings"

	api "opl-cloud/packages/contracts/go/api"
)

// AcceptedTencentResourcePlan is the exact existing provider package selected by
// Catalog's frozen SKU pair and this deployment's explicit tenant-account
// binding. It is a read of the provider's one profile, never a second package
// registry, and it never carries a billing mode other than prepaid monthly.
type AcceptedTencentResourcePlan struct {
	PackageID  string
	AccountID  string
	NodePoolID string
	Zone       string
	Region     string
}

// ResolveAcceptedResourcePlan maps one accepted Catalog resource plan back to the
// provider profile entry that priced it. The accepted plan must already be a
// prepaid monthly plan for this deployment's region; anything else, including a
// missing tenant-account binding, fails before any capacity check or mutation.
func (p *TencentProvider) ResolveAcceptedResourcePlan(tenant string, plan *api.ResourcePlanSnapshot) (AcceptedTencentResourcePlan, error) {
	if p == nil || p.profileErr != nil || p.installationErr != nil || plan == nil {
		return AcceptedTencentResourcePlan{}, fmt.Errorf("tencent_accepted_profile_mismatch")
	}
	if plan.GetProvider() != "tencent-tke" || plan.GetProviderProfileId() != "tencent-tke" ||
		plan.GetBillingMode() != "PREPAID_MONTHLY" || plan.GetPrepaidMonths() <= 0 ||
		!validTencentProviderRegion(p.region) || plan.GetRegion() != p.region ||
		plan.GetProviderComputeSkuId() == "" || plan.GetProviderStorageSkuId() == "" ||
		plan.GetVcpus() <= 0 || plan.GetMemoryMib() <= 0 || plan.GetCapacityGib() < 10 || int(plan.GetCapacityGib())%10 != 0 {
		return AcceptedTencentResourcePlan{}, fmt.Errorf("tencent_accepted_profile_mismatch")
	}
	account, err := p.acceptedTencentAccount(tenant)
	if err != nil {
		return AcceptedTencentResourcePlan{}, err
	}
	var matched *tencentPackageProfile
	for index := range p.profile.Packages {
		item := p.profile.Packages[index]
		if !item.Available || item.Compute.ID != plan.GetProviderComputeSkuId() || item.Storage.SizeGB != int(plan.GetCapacityGib()) ||
			item.Storage.DiskType != plan.GetProviderStorageSkuId() || item.Compute.CPU != int(plan.GetVcpus()) || item.Compute.MemoryGB*1024 != int(plan.GetMemoryMib()) ||
			item.Billing.ChargeType != "PREPAID" || item.Billing.PeriodMonths != int64(plan.GetPrepaidMonths()) || item.Billing.RenewFlag == "" {
			continue
		}
		if matched != nil {
			return AcceptedTencentResourcePlan{}, fmt.Errorf("tencent_accepted_plan_ambiguous")
		}
		candidate := item
		matched = &candidate
	}
	if matched == nil {
		return AcceptedTencentResourcePlan{}, fmt.Errorf("tencent_accepted_plan_mismatch")
	}
	return AcceptedTencentResourcePlan{PackageID: matched.ID, AccountID: account, NodePoolID: matched.NodePoolID, Zone: matched.Zone, Region: p.region}, nil
}

func (p *TencentProvider) acceptedTencentAccount(tenant string) (string, error) {
	if strings.TrimSpace(tenant) == "" || tenant != strings.TrimSpace(tenant) {
		return "", fmt.Errorf("tencent_account_binding_required")
	}
	var account string
	for _, binding := range p.profile.AccountBindings {
		if binding.TenantID != tenant {
			continue
		}
		if account != "" || binding.AccountID == "" {
			return "", fmt.Errorf("tencent_account_binding_ambiguous")
		}
		account = binding.AccountID
	}
	if account == "" {
		return "", fmt.Errorf("tencent_account_binding_required")
	}
	return account, nil
}
