package owneridentity

import "testing"

// TestDeploymentUnitTransportPrincipal pins the transport principal of each
// deployment unit. CloudIdentity (tenant) and Gateway Integration (gateway) are
// two data owners served by one process whose single certificate identifies
// `tenant`; a caller that reaches the Gateway owner must verify that principal,
// because no process can present a `gateway` certificate. Every other owner is
// its own principal, so this mapping is the only exception.
func TestDeploymentUnitTransportPrincipal(t *testing.T) {
	for _, owner := range []Owner{Tenant, Capability, Build, Workspace, RuntimeControl, Serve, Fabric, Gateway, ResourceCatalog, Ledger} {
		principal := DeploymentUnitTransportPrincipal(owner)
		if !principal.Valid() {
			t.Fatalf("transport principal for %s is not a valid service: %q", owner, principal)
		}
		if owner == Gateway {
			if principal != Tenant.Service() {
				t.Fatalf("Gateway transport principal = %q, want %q", principal, Tenant.Service())
			}
			continue
		}
		if principal != owner.Service() {
			t.Fatalf("%s transport principal = %q, want its own service identity", owner, principal)
		}
	}
	if DeploymentUnitTransportPrincipal(Tenant) != DeploymentUnitTransportPrincipal(Gateway) {
		t.Fatal("CloudIdentity and Gateway Integration must share the deployment unit's transport principal")
	}
}
