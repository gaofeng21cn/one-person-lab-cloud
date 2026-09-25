package fabric

import (
	"fmt"
	api "opl-cloud/packages/contracts/go/api"
)

// AcceptedLocalResourcePlan is the exact existing provider package selected by
// Catalog's frozen SKU pair and the deployment's explicit tenant-account binding.
// It is a read of this provider's one profile, never a second package registry.
type AcceptedLocalResourcePlan struct {
	PackageID  string
	AccountID  string
	NodePoolID string
}

func (p *LocalDockerProvider) ResolveAcceptedResourcePlan(tenant string, plan *api.ResourcePlanSnapshot) (AcceptedLocalResourcePlan, error) {
	if p == nil || p.profileErr != nil || plan == nil || tenant == "" || plan.Provider != "local-docker" || plan.BillingMode != "LOCAL_NO_CHARGE" || p.profile.ProfileID == "" || p.profile.CapabilityVersion == "" || p.profile.Region == "" || plan.ProviderProfileId != p.profile.ProfileID || plan.ProviderCapabilityVersion != p.profile.CapabilityVersion || plan.Region != p.profile.Region {
		return AcceptedLocalResourcePlan{}, fmt.Errorf("local_docker_accepted_profile_mismatch")
	}
	var account string
	for _, binding := range p.profile.AccountBindings {
		if binding.TenantID == tenant {
			if account != "" || binding.AccountID == "" {
				return AcceptedLocalResourcePlan{}, fmt.Errorf("local_docker_account_binding_ambiguous")
			}
			account = binding.AccountID
		}
	}
	if account == "" {
		return AcceptedLocalResourcePlan{}, fmt.Errorf("local_docker_account_binding_required")
	}
	var packageID string
	for _, candidate := range p.profile.Packages {
		if candidate.Available && candidate.Compute.ID == plan.ProviderComputeSkuId && candidate.Storage.ID != "" && candidate.Storage.ID == plan.ProviderStorageSkuId && candidate.Compute.CPU == int(plan.Vcpus) && candidate.Compute.MemoryGB*1024 == int(plan.MemoryMib) && candidate.Storage.SizeGB == int(plan.CapacityGib) {
			if packageID != "" {
				return AcceptedLocalResourcePlan{}, fmt.Errorf("local_docker_accepted_plan_ambiguous")
			}
			packageID = candidate.ID
		}
	}
	if packageID == "" {
		return AcceptedLocalResourcePlan{}, fmt.Errorf("local_docker_accepted_plan_mismatch")
	}
	return AcceptedLocalResourcePlan{PackageID: packageID, AccountID: account, NodePoolID: "local-docker"}, nil
}
