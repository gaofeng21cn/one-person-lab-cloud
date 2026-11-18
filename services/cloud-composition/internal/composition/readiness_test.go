package composition

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// contractPath is the owner topology contract the entrypoint reads at run
// time. The tests read the contract the repository ships, so they prove the
// behaviour against the declared facts and not against a copy of them.
func contractPath(t *testing.T) string {
	t.Helper()
	return "../../../../deploy/portable/opl-cloud-owner-topology.json"
}

// environment is an installation's environment for one test.
type environment map[string]string

func (e environment) getenv(name string) string { return e[name] }

// stepFor returns the composition step of one declared owner.
func stepFor(t *testing.T, plan Plan, service string) Step {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Process.Service == service {
			return step
		}
	}
	t.Fatalf("the contract declares no owner %s", service)
	return Step{}
}

// TestLedgerReadinessUsesTheDeclaredHTTPCriterion proves that the Ledger
// owner is verified over its declared HTTP readiness route and not over a gRPC
// health service it does not register.
func TestLedgerReadinessUsesTheDeclaredHTTPCriterion(t *testing.T) {
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatalf("load the owner topology contract: %v", err)
	}
	plan, err := NewPlan(topology, environment{}.getenv)
	if err != nil {
		t.Fatalf("build the composition plan: %v", err)
	}
	ledger := stepFor(t, plan, "ledger")
	for _, check := range ledger.Criterion.Checks {
		if check.Transport != "http" {
			t.Fatalf("the Ledger criterion must use the HTTP surface, found a %s check", check.Transport)
		}
	}
	routes := []string{}
	for _, check := range ledger.Criterion.Checks {
		routes = append(routes, check.Path)
	}
	if !contains(routes, "/readyz") {
		t.Fatalf("the Ledger criterion must read /readyz, found %v", routes)
	}
}

// TestLedgerReadyPasses proves that a ready Ledger satisfies its criterion.
func TestLedgerReadyPasses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/readyz":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ready"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	checker := ledgerChecker(t, server.URL)
	if err := checker.Check(context.Background(), ledgerStep(t, checker, server.URL)); err != nil {
		t.Fatalf("a ready Ledger must satisfy its criterion: %v", err)
	}
}

// TestLedgerUnreadyFails proves that an unready Ledger does not satisfy its
// criterion, and that the failure names the owner and the route.
func TestLedgerUnreadyFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/readyz":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unavailable"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	checker := ledgerChecker(t, server.URL)
	err := checker.Check(context.Background(), ledgerStep(t, checker, server.URL))
	if err == nil {
		t.Fatal("an unready Ledger must not satisfy its criterion")
	}
	message := err.Error()
	for _, fragment := range []string{"ledger", "/readyz", "503"} {
		if !strings.Contains(message, fragment) {
			t.Fatalf("the failure must name %q, got %q", fragment, message)
		}
	}
}

// TestChildEnvironmentProjectsOwnerSecretsAndAliases proves that the shared
// composition container does not recreate the former one-secret-per-owner
// boundary by passing its complete parent environment to every child. The
// Ledger receives its own database and service token aliases under the names
// the existing Ledger binary reads, while an unrelated owner's secret is
// absent.
func TestChildEnvironmentProjectsOwnerSecretsAndAliases(t *testing.T) {
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatalf("load the owner topology contract: %v", err)
	}
	installation := environment{
		"OPL_GRPC_INSECURE_LOCAL":    "1",
		"DATABASE_URL":               "postgres://shared",
		"OPL_LEDGER_DATABASE_URL":    "postgres://ledger",
		"OPL_LEDGER_SERVICE_TOKEN":   "ledger-token",
		"OPL_INTERNAL_SERVICE_TOKEN": "global-token",
		"OPL_GATEWAY_DATABASE_URL":   "postgres://gateway",
		"TENCENTCLOUD_SECRET_KEY":    "unrelated-secret",
		"PATH":                       "/usr/bin",
	}
	plan, err := NewPlan(topology, installation.getenv)
	if err != nil {
		t.Fatalf("build the composition plan: %v", err)
	}
	ledger := stepFor(t, plan, "ledger")
	parent := []string{}
	for name, value := range installation {
		parent = append(parent, name+"="+value)
	}
	projected, err := ChildEnvironment(ledger.Process, parent, installation.getenv)
	if err != nil {
		t.Fatalf("project the Ledger environment: %v", err)
	}
	got := environment{}
	for _, entry := range projected {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			got[name] = value
		}
	}
	if got["DATABASE_URL"] != "postgres://ledger" {
		t.Fatalf("Ledger must receive its aliased database, got %q", got["DATABASE_URL"])
	}
	if got["OPL_INTERNAL_SERVICE_TOKEN"] != "ledger-token" {
		t.Fatalf("Ledger must receive its own service token alias, got %q", got["OPL_INTERNAL_SERVICE_TOKEN"])
	}
	if _, ok := got["TENCENTCLOUD_SECRET_KEY"]; ok {
		t.Fatal("Ledger must not receive Fabric/provider credentials")
	}
	if _, ok := got["OPL_GATEWAY_DATABASE_URL"]; ok {
		t.Fatal("Ledger must not receive Gateway database credentials")
	}
}

func TestChildEnvironmentKeepsEveryDeclaredListenerAndPeerInput(t *testing.T) {
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatal(err)
	}
	installation := environment{"OPL_GRPC_INSECURE_LOCAL": "1", "DATABASE_URL": "shared-owner-dsn", "PGSSLMODE": "disable", "SSL_CERT_FILE": "/installation/root-ca.pem", "HTTPS_PROXY": "http://proxy.example:8080", "no_proxy": "127.0.0.1,localhost"}
	for _, process := range topology.Processes {
		for _, name := range []string{process.AddressEnv, process.AddressPortEnv, process.PeerTokensEnv, process.RPCAddressEnv, process.RPCDatabaseEnv, process.RPCEnabledByEnv} {
			if name != "" {
				installation[name] = "declared-" + name
			}
		}
		for _, listener := range process.AdditionalListeners {
			installation[listener.AddressEnv] = "declared-" + listener.AddressEnv
		}
	}
	for _, edge := range topology.Communication.Edges {
		for _, name := range append([]string{edge.AddressEnv, edge.TokenEnv}, edge.ActivationEnvs...) {
			if name != "" {
				installation[name] = "declared-" + name
			}
		}
	}
	// The plan reads the installation the owner processes actually run in, so
	// the resolved listeners and the active edges are the ones this test
	// inspects.
	plan, err := NewPlan(topology, installation.getenv)
	if err != nil {
		t.Fatal(err)
	}
	parent := []string{}
	for name, value := range installation {
		parent = append(parent, name+"="+value)
	}
	for _, step := range plan.Steps {
		t.Run(step.Service(), func(t *testing.T) {
			projected, err := ChildEnvironment(step.Process, parent, installation.getenv)
			if err != nil {
				t.Fatal(err)
			}
			got := environment{}
			for _, entry := range projected {
				name, value, ok := strings.Cut(entry, "=")
				if ok {
					got[name] = value
				}
			}
			// The listener environments carry the listener the contract
			// declares, and a peer address environment carries the target's
			// declared listener reached through the shared container's
			// loopback interface. Every other declared input keeps the
			// installation's own value.
			resolved := map[string]string{}
			for _, name := range step.Process.listenerAddressEnvs() {
				declared, ok := step.Process.defaultAddressFor(name)
				if !ok {
					t.Fatalf("owner %s declares no default address for %s", step.Process.Service, name)
				}
				resolved[name] = declared
			}
			if port := step.Process.AddressPortEnv; port != "" {
				if _, number, err := net.SplitHostPort(resolved[step.Process.AddressEnv]); err == nil {
					resolved[port] = number
				}
			}
			for _, edge := range topology.Communication.Edges {
				if edge.Caller != step.Process.TransportPrincipal {
					continue
				}
				target, ok := processByPrincipal(topology, edge.Target)
				if !ok {
					t.Fatalf("the contract declares no owner %q", edge.Target)
				}
				declared, ok := target.defaultAddressFor(target.grpcAddressEnv())
				if !ok {
					t.Fatalf("owner %s declares no default address for its owner transport", target.Service)
				}
				resolved[edge.AddressEnv] = loopbackAddress(declared)
			}
			expected := []string{step.Process.AddressEnv, step.Process.AddressPortEnv, step.Process.PeerTokensEnv, step.Process.RPCAddressEnv, step.Process.RPCDatabaseEnv, step.Process.RPCEnabledByEnv}
			for _, listener := range step.Process.AdditionalListeners {
				expected = append(expected, listener.AddressEnv)
			}
			for _, edge := range topology.Communication.Edges {
				if edge.Caller == step.Process.TransportPrincipal {
					expected = append(expected, edge.AddressEnv, edge.TokenEnv)
					expected = append(expected, edge.ActivationEnvs...)
				}
			}
			for _, name := range expected {
				if name == "" {
					continue
				}
				if _, present := got[name]; !present {
					t.Errorf("declared listener/peer input %s was dropped", name)
					continue
				}
				want, resolvedHere := resolved[name]
				if !resolvedHere {
					want = installation[name]
				}
				if got[name] != want {
					t.Errorf("declared listener/peer input %s carries %q, want %q", name, got[name], want)
				}
			}
			if step.Process.DatabaseEnv == "" {
				if _, present := got["DATABASE_URL"]; present {
					t.Fatal("a process without a database consumer must not receive a database DSN")
				}
				if _, present := got["PGSSLMODE"]; present {
					t.Fatal("a process without a database consumer must not receive PostgreSQL settings")
				}
			} else if got["PGSSLMODE"] != "disable" {
				t.Fatal("database consumers must retain the installation's PostgreSQL TLS policy")
			}
			if got["SSL_CERT_FILE"] != installation["SSL_CERT_FILE"] {
				t.Fatal("owner HTTP clients must retain the installation's root CA file")
			}
			for _, name := range []string{"HTTPS_PROXY", "no_proxy"} {
				if got[name] != installation[name] {
					t.Errorf("owner HTTP clients must retain the installation's proxy input %s", name)
				}
			}
		})
	}
}

// loopbackAddress is the address a listener that binds every interface of the
// shared container is reached at.
func loopbackAddress(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return net.JoinHostPort(host, port)
}

// TestOwnerGRPCHealthIsVerifiedAgainstTheOwnerHealthService proves that an
// owner with a gRPC criterion is verified against a real health service.
func TestOwnerGRPCHealthIsVerifiedAgainstTheOwnerHealthService(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer()
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(server, healthServer)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatalf("load the owner topology contract: %v", err)
	}
	// The owner binds the listener the contract declares, so the contract
	// declares the address this test serves on.
	for index := range topology.Processes {
		if topology.Processes[index].Service == "capability" {
			topology.Processes[index].DefaultAddress = listener.Addr().String()
		}
	}
	environment := environment{
		"OPL_CAPABILITY_ADDR":     listener.Addr().String(),
		"OPL_GRPC_INSECURE_LOCAL": "1",
	}
	plan, err := NewPlan(topology, environment.getenv)
	if err != nil {
		t.Fatalf("build the composition plan: %v", err)
	}
	checker := NewChecker(environment.getenv)
	capability := stepFor(t, plan, "capability")
	if err := checker.Check(context.Background(), capability); err != nil {
		t.Fatalf("a serving owner must satisfy its criterion: %v", err)
	}

	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	err = checker.Check(context.Background(), capability)
	if err == nil {
		t.Fatal("an owner that does not serve must not satisfy its criterion")
	}
	if !strings.Contains(err.Error(), "capability") {
		t.Fatalf("the failure must name the owner, got %q", err.Error())
	}
}

// ledgerChecker returns a checker for an installation whose Ledger HTTP
// listener answers on the given URL.
func ledgerChecker(t *testing.T, url string) *Checker {
	t.Helper()
	environment := environment{
		"LEDGER_ADDR":             url,
		"OPL_GRPC_INSECURE_LOCAL": "1",
	}
	return NewChecker(environment.getenv)
}

// ledgerStep returns the Ledger step of the composition plan. The Ledger
// binds the listener the contract declares, so the contract declares the
// address this test serves on.
func ledgerStep(t *testing.T, checker *Checker, url string) Step {
	t.Helper()
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatalf("load the owner topology contract: %v", err)
	}
	for index := range topology.Processes {
		if topology.Processes[index].Service != "ledger" {
			continue
		}
		for listener := range topology.Processes[index].AdditionalListeners {
			if topology.Processes[index].AdditionalListeners[listener].AddressEnv == "LEDGER_ADDR" {
				topology.Processes[index].AdditionalListeners[listener].DefaultAddress = url
			}
		}
	}
	plan, err := NewPlan(topology, checker.getenv)
	if err != nil {
		t.Fatalf("build the composition plan: %v", err)
	}
	return stepFor(t, plan, "ledger")
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
