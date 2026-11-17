package composition

import (
	"strings"
	"testing"
)

// TestPlanKeepsTheContractDeclaredOrder proves that the composition starts the
// owners in the order the owner topology contract declares.
func TestPlanKeepsTheContractDeclaredOrder(t *testing.T) {
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatalf("load the owner topology contract: %v", err)
	}
	plan, err := NewPlan(topology, environment{}.getenv)
	if err != nil {
		t.Fatalf("build the composition plan: %v", err)
	}
	if len(plan.Steps) != len(topology.Processes) {
		t.Fatalf("the plan composes %d owners, the contract declares %d", len(plan.Steps), len(topology.Processes))
	}
	for index, step := range plan.Steps {
		if step.Process.Service != topology.Processes[index].Service {
			t.Fatalf("owner %d is %s, the contract declares %s", index, step.Process.Service, topology.Processes[index].Service)
		}
		if step.Criterion.Process != step.Process.Service {
			t.Fatalf("owner %s is verified against the criterion of %s", step.Process.Service, step.Criterion.Process)
		}
	}
}

// TestPlanDerivesTheDependenciesFromTheContractEdges proves that the owners an
// owner waits for come from the contract's own communication edges.
func TestPlanDerivesTheDependenciesFromTheContractEdges(t *testing.T) {
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatalf("load the owner topology contract: %v", err)
	}
	plan, err := NewPlan(topology, environment{}.getenv)
	if err != nil {
		t.Fatalf("build the composition plan: %v", err)
	}
	cases := map[string][]string{
		// The Capability owner requires the Build owner and the Runtime
		// Control owner, and both are declared after it, so it waits for
		// neither of them.
		"capability": {},
		// The Build owner requires the Capability owner, which the contract
		// declares before it.
		"build": {"capability"},
		// The Runtime Control owner requires the Capability owner.
		"runtime-control": {"capability"},
		// The Control Plane transport to the CloudIdentity deployment unit is
		// a configured one, so an installation that does not configure it
		// waits for nothing.
		"control-plane": {},
	}
	for service, expected := range cases {
		dependencies := dependenciesOf(t, plan, service)
		if strings.Join(dependencies, ",") != strings.Join(expected, ",") {
			t.Fatalf("owner %s waits for %v, the contract edges make it wait for %v", service, dependencies, expected)
		}
	}
}

// TestPlanMakesTheFabricOwnerWaitForTheOwnersItRequires proves that the
// required transports of the Fabric owner become its dependencies in an
// installation that starts the Fabric owner's typed surface.
func TestPlanMakesTheFabricOwnerWaitForTheOwnersItRequires(t *testing.T) {
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatalf("load the owner topology contract: %v", err)
	}
	plan, err := NewPlan(topology, environment{
		"OPL_FABRIC_DATABASE_URL": "postgres://fabric@fabric/fabric",
		"OPL_FABRIC_ADDR":         ":8197",
	}.getenv)
	if err != nil {
		t.Fatalf("build the composition plan: %v", err)
	}
	dependencies := dependenciesOf(t, plan, "fabric")
	if strings.Join(dependencies, ",") != "resource-catalog,ledger,gateway-integration" {
		t.Fatalf("the Fabric owner waits for %v, the contract edges make it wait for the Resource Catalog owner, the Ledger owner and the CloudIdentity deployment unit", dependencies)
	}
}

// TestPlanActivatesAConfiguredEdgeOnlyWhenTheInstallationConfiguresIt proves
// that a configured transport becomes a dependency when the installation
// configures the address the contract declares for it.
func TestPlanActivatesAConfiguredEdgeOnlyWhenTheInstallationConfiguresIt(t *testing.T) {
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatalf("load the owner topology contract: %v", err)
	}
	quiet, err := NewPlan(topology, environment{}.getenv)
	if err != nil {
		t.Fatalf("build the composition plan: %v", err)
	}
	if dependencies := dependenciesOf(t, quiet, "workspace"); len(dependencies) != 0 {
		t.Fatalf("the Workspace owner waits for %v, no configured transport is active", dependencies)
	}
	configured, err := NewPlan(topology, environment{"OPL_CAPABILITY_ADDR": ":8181"}.getenv)
	if err != nil {
		t.Fatalf("build the composition plan: %v", err)
	}
	dependencies := dependenciesOf(t, configured, "workspace")
	if strings.Join(dependencies, ",") != "capability" {
		t.Fatalf("the Workspace owner waits for %v, the configured Capability transport makes it wait for capability", dependencies)
	}
	identity, err := NewPlan(topology, environment{"OPL_CLOUD_IDENTITY_URL": "https://identity.example"}.getenv)
	if err != nil {
		t.Fatalf("build the composition plan: %v", err)
	}
	dependencies = dependenciesOf(t, identity, "control-plane")
	if strings.Join(dependencies, ",") != "gateway-integration" {
		t.Fatalf("the Control Plane waits for %v, the configured CloudIdentity transport makes it wait for the CloudIdentity deployment unit", dependencies)
	}
}

// TestPlanRefusesTwoOwnersThatWouldShareOneAddress proves that a composition
// that would bind one address twice is refused, which is the collision the
// contract itself records.
func TestPlanRefusesTwoOwnersThatWouldShareOneAddress(t *testing.T) {
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatalf("load the owner topology contract: %v", err)
	}
	_, err = NewPlan(topology, environment{"OPL_LEDGER_ADDR": ":8186"}.getenv)
	if err == nil {
		t.Fatal("the Ledger owner and the Resource Catalog owner would share :8186 in one network namespace")
	}
	for _, fragment := range []string{"ledger", "resource-catalog", ":8186"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("the refusal must name %q, got %q", fragment, err.Error())
		}
	}
	if _, err := NewPlan(topology, environment{"OPL_LEDGER_ADDR": ":8189"}.getenv); err != nil {
		t.Fatalf("a composition with distinct listen addresses must be accepted: %v", err)
	}
}

// TestPlanAcceptsThePublishedInstallationAddresses proves that the addresses
// the published Compose installation gives the declared owners compose without
// a shared listener.
func TestPlanAcceptsThePublishedInstallationAddresses(t *testing.T) {
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatalf("load the owner topology contract: %v", err)
	}
	installation := environment{
		"OPL_TENANT_ADDR":                   ":8187",
		"OPL_RESOURCE_CATALOG_ADDR":         ":8186",
		"OPL_LEDGER_ADDR":                   ":8189",
		"LEDGER_ADDR":                       ":8081",
		"OPL_CAPABILITY_ADDR":               ":8181",
		"OPL_CAPABILITY_OBJECT_LISTEN_ADDR": ":8191",
		"OPL_BUILD_ADDR":                    ":8182",
		"OPL_RUNTIME_CONTROL_ADDR":          ":8183",
		"OPL_WORKSPACE_ADDR":                ":8184",
		"OPL_SERVE_ADDR":                    ":8185",
		"OPL_SERVE_ACCESS_ADDR":             ":8192",
		"FABRIC_ADDR":                       ":8082",
		"OPL_FABRIC_ADDR":                   ":8188",
		"OPL_FABRIC_DATABASE_URL":           "postgres://fabric@fabric/fabric",
		"CONTROL_PLANE_ADDR":                ":8787",
		"OPL_BFF_ADDR":                      ":8190",
	}
	if _, err := NewPlan(topology, installation.getenv); err != nil {
		t.Fatalf("the published installation addresses must compose: %v", err)
	}
}

// dependenciesOf returns the owners one step waits for.
func dependenciesOf(t *testing.T, plan Plan, service string) []string {
	t.Helper()
	dependencies := []string{}
	for _, dependency := range stepFor(t, plan, service).Dependencies {
		dependencies = append(dependencies, dependency.Process.Service)
	}
	return dependencies
}
