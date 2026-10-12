package identity

import (
	"slices"
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
		api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL:  6,
		api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG: 14,
		api.OwnerEnum_OWNER_ENUM_SERVE:            5,
		api.OwnerEnum_OWNER_ENUM_WORKSPACE:        18,
		api.OwnerEnum_OWNER_ENUM_GATEWAY:          9,
	} {
		if counts[owner] != want {
			t.Fatalf("owner %v has %d policy rows, expected %d", owner, counts[owner], want)
		}
	}
	if len(counts) != 8 {
		t.Fatalf("the policy names %d owners, expected 8", len(counts))
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
	// getWorkspaceAccess reports Serve's access facts, so its audience is Serve even
	// though the operation routes through the Workspace product path. A workspace
	// audience must not authorize it, or the fact would be authorized by the wrong
	// owner.
	access := policyFor(t, "GETWORKSPACEACCESS")
	if access.owner != api.OwnerEnum_OWNER_ENUM_SERVE || len(access.roles) != 1 || access.roles[0] != "member" {
		t.Fatalf("GETWORKSPACEACCESS is %v/%v, expected the serve access owner", access.owner, access.roles)
	}
	// The invitee-bound operation is deliberately not a grantable role row.
	if _, ok := actions[api.AuthorizationActionEnum(api.AuthorizationActionEnum_value["AUTHORIZATION_ACTION_ENUM_ACCEPTINVITATION"])]; ok {
		t.Fatal("acceptInvitation must not be a tenant role row")
	}
	// An Operation is read through an explicit owner route, so the contract names
	// no single Operation writer and the table has no getOperation row: CloudIdentity
	// decides it as a route-resolved session read whose audience is the route the
	// caller chose. A row here would pin every owner's Operation read to one owner.
	if _, ok := actions[api.AuthorizationActionEnum(api.AuthorizationActionEnum_value["AUTHORIZATION_ACTION_ENUM_GETOPERATION"])]; ok {
		t.Fatal("getOperation must not be one owner's role row")
	}
	// The composed BFF read guards the Workspace identity fact, so the workspace
	// owner needs its own row for that read; the count is pinned by the surface test.
	if policy := policyFor(t, "GETWORKSPACE"); policy.owner != api.OwnerEnum_OWNER_ENUM_WORKSPACE || policy.roles[0] != "member" {
		t.Fatalf("GETWORKSPACE is %v/%v, expected the workspace member read", policy.owner, policy.roles)
	}
	// The effective build policy carries the one owner fact a Publisher session must
	// read to freeze a new Build's inputs: the Runtime Control owner's own
	// runtime_control.catalog_policies row, including the default WebUI half. It is
	// deliberately a separate read-only action from the administrator's policy read,
	// so granting a member this fact cannot also hand that member the administrator
	// boundary. Both name Runtime Control, and neither may be widened into the other.
	effective := policyFor(t, "GETEFFECTIVEBUILDRUNTIMEPOLICY")
	if effective.owner != api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL || len(effective.roles) != 1 || effective.roles[0] != "member" {
		t.Fatalf("GETEFFECTIVEBUILDRUNTIMEPOLICY is %v/%v, expected the runtime_control member read", effective.owner, effective.roles)
	}
	administrator := policyFor(t, "GETBUILDRUNTIMEPOLICY")
	if administrator.owner != api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL || len(administrator.roles) != 1 || administrator.roles[0] != "platform_admin" {
		t.Fatalf("GETBUILDRUNTIMEPOLICY is %v/%v, expected the runtime_control platform administrator read", administrator.owner, administrator.roles)
	}
	// The publisher route authorizes by action alone, so the two reads are only a
	// real boundary while they are distinct actions. Aliasing them, or granting
	// member the administrator action, would make /api/v2/catalog/build-policy and
	// /api/v2/admin/catalog/build-policy the same permission.
	if effective.owner == administrator.owner && len(effective.roles) == len(administrator.roles) &&
		slices.Equal(effective.roles, administrator.roles) {
		t.Fatal("the member effective-policy read and the administrator policy read must be distinct actions")
	}
	for _, role := range administrator.roles {
		if role == "member" {
			t.Fatal("member must not hold the administrator build-policy read")
		}
	}
	// A serve-audience row must not exist for a workspace-only action, so the
	// corrected ownership cannot be read as "serve may do anything workspace may".
	for action, policy := range actions {
		if policy.owner != api.OwnerEnum_OWNER_ENUM_SERVE {
			continue
		}
		switch action {
		case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETDEPLOYMENT,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEACCESS,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEVERSION,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ROLLBACKWORKSPACE:
		default:
			t.Fatalf("unexpected serve audience row: %v", action)
		}
	}
}
