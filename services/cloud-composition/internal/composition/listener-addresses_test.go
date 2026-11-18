package composition

import (
	"strings"
	"testing"
)

func TestOwnerAndGRPCPeerUseTheEffectiveListener(t *testing.T) {
	for _, tc := range []struct{ bind, peer string }{
		{":19189", "127.0.0.1:19189"},
		{"0.0.0.0:19189", "127.0.0.1:19189"},
		{"[::]:19189", "[::1]:19189"},
		{"[::1]:19189", "[::1]:19189"},
		{"127.0.0.2:19189", "127.0.0.2:19189"},
		{"ledger.example:19189", "ledger.example:19189"},
	} {
		t.Run(tc.bind, func(t *testing.T) {
			topology, err := Load(contractPath(t))
			if err != nil {
				t.Fatal(err)
			}
			installation := environment{"OPL_LEDGER_ADDR": tc.bind, "LEDGER_ADDR": "127.0.0.2:19081"}
			plan, err := NewPlan(topology, installation.getenv)
			if err != nil {
				t.Fatal(err)
			}
			ledger := stepFor(t, plan, "ledger").Process
			if got := ledger.ListenerEnvironment["OPL_LEDGER_ADDR"]; got != tc.bind {
				t.Fatalf("owner bind = %q, want %q", got, tc.bind)
			}
			if got := ledger.ListenerEnvironment["LEDGER_ADDR"]; got != installation["LEDGER_ADDR"] {
				t.Fatalf("HTTP bind = %q", got)
			}
			if got := stepFor(t, plan, "capability").Process.PeerEnvironment["OPL_LEDGER_ADDR"]; got != tc.peer {
				t.Fatalf("gRPC peer = %q, want %q", got, tc.peer)
			}
			got, err := checkAddress(ledger, "OPL_LEDGER_ADDR", installation.getenv)
			if err != nil || got != tc.peer {
				t.Fatalf("readiness address = %q, %v; want %q", got, err, tc.peer)
			}
		})
	}
}

func TestOwnerListenerEnvironmentUsesTheEffectivePort(t *testing.T) {
	for _, tc := range []struct {
		input         environment
		address, port string
	}{
		{environment{}, ":8787", ""},
		{environment{"PORT": "9000"}, ":9000", "9000"},
		{environment{"PORT": "9000", "CONTROL_PLANE_ADDR": "localhost:19000"}, "localhost:19000", "9000"},
	} {
		tc.input["OPL_GRPC_INSECURE_LOCAL"] = "1"
		child := projectedInstallation(t, tc.input, "control-plane")
		if child["CONTROL_PLANE_ADDR"] != tc.address || child["PORT"] != tc.port {
			t.Fatalf("child address and raw PORT = %q / %q, want %q / %q", child["CONTROL_PLANE_ADDR"], child["PORT"], tc.address, tc.port)
		}
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

func TestResolveAddressHonorsListenerValueSemantics(t *testing.T) {
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ service, name, input, want string }{
		{"capability", "OPL_CAPABILITY_ADDR", " 127.0.0.1:19181 \t", "127.0.0.1:19181"},
		{"capability", "OPL_CAPABILITY_OBJECT_LISTEN_ADDR", " 127.0.0.1:19191 \t", " 127.0.0.1:19191 \t"},
		{"capability", "OPL_CAPABILITY_OBJECT_LISTEN_ADDR", " \t", " \t"},
		{"serve", "OPL_SERVE_ACCESS_ADDR", " 127.0.0.1:19192 \t", "127.0.0.1:19192"},
		{"serve", "OPL_SERVE_ACCESS_ADDR", " \t", ":8192"},
		{"ledger", "LEDGER_ADDR", " 127.0.0.1:19081 \t", " 127.0.0.1:19081 \t"},
		{"fabric", "FABRIC_ADDR", " 127.0.0.1:19082 \t", " 127.0.0.1:19082 \t"},
		{"fabric", "OPL_FABRIC_ADDR", " 127.0.0.1:19188 \t", "127.0.0.1:19188"},
		{"control_plane", "CONTROL_PLANE_ADDR", " \t", " \t"},
		{"console_bff", "OPL_BFF_ADDR", " \t", " \t"},
	} {
		t.Run(tc.name+"="+tc.input, func(t *testing.T) {
			process, ok := processByPrincipal(topology, tc.service)
			if !ok {
				t.Fatalf("missing process %s", tc.service)
			}
			got, err := ResolveAddress(process, tc.name, environment{tc.name: tc.input}.getenv)
			if err != nil || got != tc.want {
				t.Fatalf("ResolveAddress = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	controlPlane, _ := processByPrincipal(topology, "control_plane")
	got, err := ResolveAddress(controlPlane, "CONTROL_PLANE_ADDR", environment{"PORT": " \t"}.getenv)
	if err != nil || got != ": \t" {
		t.Fatalf("HTTP PORT must retain raw semantics: %q, %v", got, err)
	}
	capability, _ := processByPrincipal(topology, "capability")
	capability.AdditionalListeners[0].ValueSemantics = "unknown"
	if _, err := ResolveAddress(capability, "OPL_CAPABILITY_OBJECT_LISTEN_ADDR", environment{}.getenv); err == nil {
		t.Fatal("unknown listener semantics must be refused")
	}
}
