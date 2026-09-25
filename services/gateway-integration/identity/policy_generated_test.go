package identity

import (
	"testing"

	api "opl-cloud/packages/contracts/go/api"
)

func policyFor(t *testing.T, action string) actionPolicy {
	t.Helper()
	value, ok := api.AuthorizationActionEnum_value["AUTHORIZATION_ACTION_ENUM_"+action]
	if !ok {
		t.Fatalf("action %s is not in the wire contract", action)
	}
	policy, ok := actions[api.AuthorizationActionEnum(value)]
	if !ok {
		t.Fatalf("the policy has no row for %s", action)
	}
	return policy
}

// TestGeneratedPolicyCoversAssignedOwners fixes the generated surface to the
// contract's per-owner operation counts and to the exact audience/role of the
// operations the coordinator assigned. A hand-added row, a missing owner or a
// changed role changes the surface this asserts, so the table cannot drift from
// the single canonical permission source.
func TestGeneratedPolicyCoversAssignedOwners(t *testing.T) {
	counts := map[api.OwnerEnum]int{}
	for action, policy := range actions {
		if action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UNSPECIFIED {
			t.Fatal("the policy contains the unspecified action")
		}
		if policy.owner == api.OwnerEnum_OWNER_ENUM_UNSPECIFIED || len(policy.roles) == 0 {
			t.Fatalf("%v has no audience or no role", action)
		}
		for _, role := range policy.roles {
			if role == "" {
				t.Fatalf("%v has an empty role", action)
			}
		}
		counts[policy.owner]++
	}
	for owner, want := range map[api.OwnerEnum]int{
		api.OwnerEnum_OWNER_ENUM_CAPABILITY:       25,
		api.OwnerEnum_OWNER_ENUM_BUILD:            5,
		api.OwnerEnum_OWNER_ENUM_TENANT:           18,
		api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL:  5,
		api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG: 14,
		api.OwnerEnum_OWNER_ENUM_SERVE:            4,
	} {
		if counts[owner] != want {
			t.Fatalf("owner %v has %d policy rows, expected %d", owner, counts[owner], want)
		}
	}
	if len(counts) != 6 {
		t.Fatalf("the policy names %d owners, expected 6", len(counts))
	}
}

// TestGeneratedPolicyAudienceAndRoles pins the audience and roles of the
// operations the resource catalog and Serve work packages asked this owner to
// authorize. A platform administrator action must stay platform-scoped and never
// be granted to a tenant role, and a member read must stay tenant-scoped.
func TestGeneratedPolicyAudienceAndRoles(t *testing.T) {
	for _, action := range []string{
		"CREATECOMPUTEPLAN", "SETCOMPUTEPLANAVAILABILITY", "CREATESTORAGEPLAN", "SETSTORAGEPLANAVAILABILITY",
		"LISTPRICEPOLICYVERSIONS", "CREATEPRICEPOLICYVERSION", "LISTREFUNDPOLICYVERSIONS", "CREATEREFUNDPOLICYVERSION",
		"LISTRETENTIONPOLICYVERSIONS", "CREATERETENTIONPOLICYVERSION",
	} {
		policy := policyFor(t, action)
		if policy.owner != api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG || len(policy.roles) != 1 || policy.roles[0] != "platform_admin" {
			t.Fatalf("%s is %v/%v, expected the resource_catalog platform administrator", action, policy.owner, policy.roles)
		}
	}
	for _, action := range []string{"LISTCOMPUTEPLANS", "LISTSTORAGEPLANS"} {
		policy := policyFor(t, action)
		if policy.owner != api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG || len(policy.roles) != 1 || policy.roles[0] != "member" {
			t.Fatalf("%s is %v/%v, expected the resource_catalog member read", action, policy.owner, policy.roles)
		}
	}
	for _, action := range []string{"LISTDEPLOYMENTS", "GETDEPLOYMENT"} {
		policy := policyFor(t, action)
		if policy.owner != api.OwnerEnum_OWNER_ENUM_SERVE || len(policy.roles) != 1 || policy.roles[0] != "member" {
			t.Fatalf("%s is %v/%v, expected the serve member read", action, policy.owner, policy.roles)
		}
	}
	// The invitee-bound operation is deliberately not a grantable role row.
	if _, ok := actions[api.AuthorizationActionEnum(api.AuthorizationActionEnum_value["AUTHORIZATION_ACTION_ENUM_ACCEPTINVITATION"])]; ok {
		t.Fatal("acceptInvitation must not be a tenant role row")
	}
	// getWorkspaceAccess is contract-owned by the workspace owner, which this
	// policy does not serve, so no row may authorize it here under a serve audience.
	if _, ok := actions[api.AuthorizationActionEnum(api.AuthorizationActionEnum_value["AUTHORIZATION_ACTION_ENUM_GETWORKSPACEACCESS"])]; ok {
		t.Fatal("getWorkspaceAccess is not a serve-owned action and must not be rowed here")
	}
}
