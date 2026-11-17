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
	if err := checker.Check(context.Background(), ledgerStep(t, checker)); err != nil {
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
	err := checker.Check(context.Background(), ledgerStep(t, checker))
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

// ledgerStep returns the Ledger step of the composition plan.
func ledgerStep(t *testing.T, checker *Checker) Step {
	t.Helper()
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatalf("load the owner topology contract: %v", err)
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
