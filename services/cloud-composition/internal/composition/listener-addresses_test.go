package composition

import (
	"strings"
	"testing"
)

// TestOwnerListenerEnvironmentCarriesTheDeclaredListener proves that an owner
// binds the listener the contract declares, even when the shared environment
// names the same variable with the published per-container address of another
// owner's service. That service name does not exist inside one container, so
// an owner that inherited it would bind an address nothing reaches.
func TestOwnerListenerEnvironmentCarriesTheDeclaredListener(t *testing.T) {
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatalf("load the owner topology contract: %v", err)
	}
	// The published installation reaches the Ledger owner at its service name.
	installation := environment{
		"OPL_LEDGER_ADDR":           "ledger:8189",
		"LEDGER_ADDR":               ":8081",
		"OPL_FABRIC_ADDR":           "fabric:8188",
		"FABRIC_ADDR":               ":8082",
		"OPL_FABRIC_DATABASE_URL":   "postgres://fabric",
		"OPL_CLOUD_IDENTITY_URL":    "tenant:8187",
		"OPL_CAPABILITY_ADDR":       "capability:8181",
		"OPL_RESOURCE_CATALOG_ADDR": "resource-catalog:8186",
		"OPL_WORKSPACE_ADDR":        "workspace:8184",
		"OPL_SERVE_ADDR":            "serve:8185",
		"OPL_RUNTIME_CONTROL_ADDR":  "runtime-control:8183",
		"OPL_BUILD_ADDR":            "build:8182",
		"OPL_TENANT_ADDR":           ":8187",
	}
	ledger, ok := processByPrincipal(topology, "ledger")
	if !ok {
		t.Fatal("the contract declares no Ledger owner")
	}
	resolved, err := ownerListenerEnvironment(ledger, installation.getenv)
	if err != nil {
		t.Fatalf("resolve the Ledger listeners: %v", err)
	}
	if resolved["OPL_LEDGER_ADDR"] != ":8189" {
		t.Fatalf("the Ledger owner must bind the declared listener, got %q", resolved["OPL_LEDGER_ADDR"])
	}
	if resolved["LEDGER_ADDR"] != ":8081" {
		t.Fatalf("the Ledger owner must bind its declared HTTP listener, got %q", resolved["LEDGER_ADDR"])
	}
	fabric, ok := processByPrincipal(topology, "fabric")
	if !ok {
		t.Fatal("the contract declares no Fabric owner")
	}
	resolved, err = ownerListenerEnvironment(fabric, installation.getenv)
	if err != nil {
		t.Fatalf("resolve the Fabric listeners: %v", err)
	}
	if resolved["OPL_FABRIC_ADDR"] != ":8188" {
		t.Fatalf("the Fabric owner must bind its declared owner transport listener, got %q", resolved["OPL_FABRIC_ADDR"])
	}
	if resolved["FABRIC_ADDR"] != ":8082" {
		t.Fatalf("the Fabric owner must bind its declared HTTP listener, got %q", resolved["FABRIC_ADDR"])
	}
}

// TestOwnerListenerEnvironmentCarriesTheDeclaredPort proves that the owner's
// own port environment names the port of the listener the contract declares,
// so the two agree.
func TestOwnerListenerEnvironmentCarriesTheDeclaredPort(t *testing.T) {
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatalf("load the owner topology contract: %v", err)
	}
	controlPlane, ok := processByPrincipal(topology, "control_plane")
	if !ok {
		t.Fatal("the contract declares no Control Plane owner")
	}
	resolved, err := ownerListenerEnvironment(controlPlane, environment{"PORT": "9000"}.getenv)
	if err != nil {
		t.Fatalf("resolve the Control Plane listeners: %v", err)
	}
	if resolved["CONTROL_PLANE_ADDR"] != ":8787" {
		t.Fatalf("the Control Plane owner must bind the declared listener, got %q", resolved["CONTROL_PLANE_ADDR"])
	}
	if resolved["PORT"] != "8787" {
		t.Fatalf("the Control Plane port environment must name the declared port, got %q", resolved["PORT"])
	}
}

// TestConfiguredOwnerPeerEnvironmentCarriesTheTargetListener proves that a
// caller reaches its target at the listener the target declares, reached
// through the loopback interface of the one shared container. The published
// per-container service name does not resolve there.
func TestConfiguredOwnerPeerEnvironmentCarriesTheTargetListener(t *testing.T) {
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatalf("load the owner topology contract: %v", err)
	}
	installation := environment{
		"OPL_LEDGER_ADDR":        "ledger:8189",
		"OPL_CLOUD_IDENTITY_URL": "tenant:8187",
	}
	capability, ok := processByPrincipal(topology, "capability")
	if !ok {
		t.Fatal("the contract declares no Capability owner")
	}
	resolved, err := configuredOwnerPeerEnvironment(topology, capability, installation.getenv)
	if err != nil {
		t.Fatalf("resolve the Capability peers: %v", err)
	}
	if resolved["OPL_LEDGER_ADDR"] != "127.0.0.1:8189" {
		t.Fatalf("the Capability owner must reach the Ledger owner at its declared listener, got %q", resolved["OPL_LEDGER_ADDR"])
	}
	if resolved["OPL_CLOUD_IDENTITY_URL"] != "127.0.0.1:8187" {
		t.Fatalf("the Capability owner must reach the Tenant owner at its declared listener, got %q", resolved["OPL_CLOUD_IDENTITY_URL"])
	}
}

// TestConfiguredOwnerPeerEnvironmentRefusesATargetWithoutAListener proves that
// a peer whose target declares no listener is refused instead of resolved
// through the installation's value or an invented port.
func TestConfiguredOwnerPeerEnvironmentRefusesATargetWithoutAListener(t *testing.T) {
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatalf("load the owner topology contract: %v", err)
	}
	topology.Communication.Edges = append(topology.Communication.Edges, Edge{
		Caller:     "capability",
		Target:     "control_plane",
		AddressEnv: "OPL_CONTROL_PLANE_PEER",
		TokenEnv:   "OPL_CONTROL_PLANE_TOKEN",
		Activation: "required",
	})
	capability, ok := processByPrincipal(topology, "capability")
	if !ok {
		t.Fatal("the contract declares no Capability owner")
	}
	_, err = configuredOwnerPeerEnvironment(topology, capability, environment{}.getenv)
	if err == nil {
		t.Fatal("a peer whose target declares no listener must be refused")
	}
	for _, fragment := range []string{"capability", "OPL_CONTROL_PLANE_PEER", "control_plane"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("the refusal must name %q, got %q", fragment, err.Error())
		}
	}
}
