package composition

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func projectedInstallation(t *testing.T, installation environment, service string) map[string]string {
	t.Helper()
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(topology, installation.getenv)
	if err != nil {
		t.Fatal(err)
	}
	parent := []string{}
	for k, v := range installation {
		parent = append(parent, k+"="+v)
	}
	child, err := ChildEnvironment(stepFor(t, plan, service).Process, parent, installation.getenv)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, entry := range child {
		k, v, _ := strings.Cut(entry, "=")
		got[k] = v
	}
	return got
}

func TestChildPreservesConfiguredListeners(t *testing.T) {
	for _, tc := range []struct{ service, name, value string }{
		{"fabric", "FABRIC_ADDR", "127.0.0.1:19111"},
		{"fabric", "FABRIC_ADDR", "[::]:19113"},
		{"fabric", "FABRIC_ADDR", "127.0.0.2:19114"},
		{"capability", "OPL_CAPABILITY_ADDR", "[::1]:19121"},
		{"console-bff", "OPL_BFF_ADDR", "127.0.0.1:19190"},
		{"control-plane", "CONTROL_PLANE_ADDR", "127.0.0.1:19787"},
		{"control-plane", "PORT", "9000"},
		{"ledger", "LEDGER_ADDR", "127.0.0.1:19081"},
		{"serve", "OPL_SERVE_ACCESS_ADDR", "127.0.0.1:19192"},
		{"capability", "OPL_CAPABILITY_OBJECT_LISTEN_ADDR", "127.0.0.1:19191"},
	} {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			env := environment{"OPL_GRPC_INSECURE_LOCAL": "1", tc.name: tc.value}
			got := projectedInstallation(t, env, tc.service)
			if got[tc.name] != tc.value {
				t.Fatalf("%s %s: configured=%q child=%q", tc.service, tc.name, tc.value, got[tc.name])
			}
		})
	}
}

func TestHTTPCallerAndReadinessReachTheRealTargetListener(t *testing.T) {
	// The unchanged contract's two real consumers of Fabric HTTP must reach
	// the address actually delivered to the Fabric owner, including IPv6 and
	// an explicit bind. All requests below are local reads of a test server.
	for _, address := range []string{"127.0.0.1:19111", "[::]:19113"} {
		t.Run(address, func(t *testing.T) {
			installation := environment{"OPL_GRPC_INSECURE_LOCAL": "1", "FABRIC_ADDR": address,
				"FABRIC_URL": "http://fabric:8082", "OPL_FABRIC_APPLICATION_URL": "http://fabric:8082"}
			owner := projectedInstallation(t, installation, "fabric")
			listener, err := net.Listen("tcp", owner["FABRIC_ADDR"])
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"serviceReady":true}`))
			})}
			defer server.Close()
			go func() { _ = server.Serve(listener) }()
			topology, err := Load(contractPath(t))
			if err != nil {
				t.Fatal(err)
			}
			plan, err := NewPlan(topology, installation.getenv)
			if err != nil {
				t.Fatal(err)
			}
			checker := NewChecker(installation.getenv)
			checker.httpClient.Transport = &http.Transport{Proxy: nil}
			fabric := stepFor(t, plan, "fabric")
			// Exercise every declared HTTP check against the real HTTP listener;
			// gRPC health is exercised against a real gRPC server separately.
			checks := []Check{}
			for _, check := range fabric.Criterion.Checks {
				if check.Transport == "http" {
					checks = append(checks, check)
				}
			}
			if len(checks) == 0 {
				t.Fatal("Fabric declares no HTTP checks")
			}
			fabric.Criterion.Checks = checks
			if err := checker.Check(context.Background(), fabric); err != nil {
				t.Fatalf("readiness did not reach the owner's effective bind: %v", err)
			}
			client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
			for _, tc := range []struct{ service, name string }{{"control-plane", "FABRIC_URL"}, {"serve", "OPL_FABRIC_APPLICATION_URL"}} {
				caller := projectedInstallation(t, installation, tc.service)
				response, err := client.Get(caller[tc.name] + "/readyz")
				if err != nil {
					t.Errorf("%s: Fabric binds %q, caller %s=%q: %v", tc.service, owner["FABRIC_ADDR"], tc.name, caller[tc.name], err)
					continue
				}
				response.Body.Close()
				if response.StatusCode != 200 {
					t.Errorf("unexpected HTTP status %d", response.StatusCode)
				}
			}
		})
	}
}

func TestChildCarriesResolvedDefaultsWhenInstallationOmitsAnAddress(t *testing.T) {
	installation := environment{"OPL_GRPC_INSECURE_LOCAL": "1", "OPL_FABRIC_DATABASE_URL": "postgres://coordination"}
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(topology, installation.getenv)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range plan.Steps {
		child := projectedInstallation(t, installation, step.Service())
		for name, address := range step.Process.ListenerEnvironment {
			if child[name] != address {
				t.Errorf("%s bind %s = %q, want effective %q", step.Service(), name, child[name], address)
			}
		}
		for name, address := range step.Process.PeerEnvironment {
			if child[name] != address {
				t.Errorf("%s peer %s = %q, want effective %q", step.Service(), name, child[name], address)
			}
		}
	}
}
