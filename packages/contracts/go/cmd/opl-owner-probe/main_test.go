package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opl-cloud/packages/contracts/go/owneridentity"
)

const testProbeTimeout = 5 * time.Second

func TestControlPlaneHealthOnlyDoesNotDependOnProviderReadiness(t *testing.T) {
	const source = "fixture-source"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/healthz" {
			_, _ = w.Write([]byte(`{"productSourceSha":"fixture-source"}`))
			return
		}
		_, _ = w.Write([]byte(`{"serviceReady":false}`))
	}))
	defer server.Close()
	args := []string{"--owner", controlPlaneOwner, "--addr", server.URL, "--expected-source-sha", source}
	getenv := func(string) string { return "" }
	probe, err := probeFromArgs(append(args, "--health-only"), getenv)
	if err != nil || probe.Status != "ok" || probe.ProductSourceSHA != source || probe.FabricReadiness != nil {
		t.Fatalf("health-only probe = %+v, %v", probe, err)
	}
	if _, err := probeFromArgs(args, getenv); err == nil || err.Error() != "fabric_not_service_ready" {
		t.Fatalf("installation readiness probe returned %v", err)
	}
}

// The Console BFF is a deployment unit rather than a data owner, so the probe
// answers for it over the HTTP surface it serves. These cases keep that
// criterion its own and keep the owner criterion closed to identities the
// contracts package does not accept.
func TestProbeConsoleBFFAnswersOverItsOwnSurface(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Errorf("the Console BFF probe requested %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	out, err := probeConsoleBFF(config{Address: server.URL, Timeout: testProbeTimeout}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("the Console BFF probe failed on a healthy surface: %v", err)
	}
	if out.Status != "ok" || out.Health == nil || out.Health.Status != "ok" {
		t.Fatalf("the Console BFF probe did not report a ready Console: %+v", out)
	}
}

func TestProbeConsoleBFFRefusesAnUnreadySurface(t *testing.T) {
	for name, body := range map[string]string{
		"degraded":  `{"status":"degraded"}`,
		"no status": `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()

			out, err := probeConsoleBFF(config{Address: server.URL, Timeout: testProbeTimeout}, func(string) string { return "" })
			if err == nil {
				t.Fatalf("the Console BFF probe accepted an unready surface: %+v", out)
			}
			if out.Status != "failed" {
				t.Fatalf("the Console BFF probe did not report a failure: %+v", out)
			}
		})
	}
}

func TestProbeConsoleBFFRefusesAnUnreachableSurface(t *testing.T) {
	out, err := probeConsoleBFF(config{Address: "http://127.0.0.1:1", Timeout: testProbeTimeout}, func(string) string { return "" })
	if err == nil {
		t.Fatalf("the Console BFF probe accepted an unreachable surface: %+v", out)
	}
	if !strings.HasPrefix(err.Error(), "console_bff_health") {
		t.Fatalf("the Console BFF probe did not name its own criterion: %v", err)
	}
}

// The Ledger owner registers no gRPC health service, so the probe reads the
// Ledger's own HTTP surface: GET /healthz answers liveness and GET /readyz
// answers readiness.
func TestProbeLedgerAnswersOverItsOwnSurface(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/readyz":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ready"}`))
		default:
			t.Errorf("the Ledger probe requested %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	out, err := probeLedger(config{Address: server.URL, Timeout: testProbeTimeout}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("the Ledger probe failed on a healthy surface: %v", err)
	}
	if out.Status != "ok" || out.Health == nil || out.Health.Status != "ready" {
		t.Fatalf("the Ledger probe did not report a ready Ledger: %+v", out)
	}
	if out.TransportPrincipal != "ledger" {
		t.Fatalf("the Ledger probe did not report its own transport principal: %+v", out)
	}
}

func TestProbeLedgerRefusesAnUnreadySurface(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"unavailable"}`))
	}))
	defer server.Close()

	out, err := probeLedger(config{Address: server.URL, Timeout: testProbeTimeout}, func(string) string { return "" })
	if err == nil {
		t.Fatalf("the Ledger probe accepted an unready surface: %+v", out)
	}
	if errorCode(err) != "LEDGER_READY" {
		t.Fatalf("the Ledger probe did not name its own criterion: %v", err)
	}
	if out.Status != "failed" {
		t.Fatalf("the Ledger probe did not report a failure: %+v", out)
	}
}

// The Ledger's HTTP listener address comes from the installation's own
// environment, so an installer that does not pass an address still reaches the
// Ledger owner.
func TestProbeLedgerReadsItsAddressFromTheInstallation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}))
	defer server.Close()

	environment := func(name string) string {
		if name == "LEDGER_ADDR" {
			return server.URL
		}
		return ""
	}
	out, err := probeFromArgs([]string{"--owner", "ledger", "--timeout", "5s"}, environment)
	if err != nil {
		t.Fatalf("the Ledger identity was not routed to its own criterion: %v", err)
	}
	if out.Status != "ok" {
		t.Fatalf("the Ledger probe did not report a ready Ledger: %+v", out)
	}
	if _, err := probeFromArgs([]string{"--owner", "ledger", "--timeout", "5s"}, func(string) string { return "" }); err == nil || errorCode(err) != "ADDRESS_MISSING" {
		t.Fatalf("a Ledger probe without an address was not refused: %v", err)
	}
}

// The deployment units keep their own criterion, and an identity that is
// neither an owner nor a deployment unit is still refused before any request
// is made.
func TestProbeFromArgsRoutesEachIdentityToItsOwnCriterion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	if _, err := probeFromArgs([]string{"--owner", "console_bff", "--addr", server.URL, "--timeout", "5s"}, func(string) string { return "" }); err != nil {
		t.Fatalf("the Console BFF identity was not routed to its own criterion: %v", err)
	}
	out, err := probeFromArgs([]string{"--owner", "not_an_owner", "--addr", server.URL, "--timeout", "5s"}, func(string) string { return "" })
	if err == nil || errorCode(err) != "OWNER_INVALID" {
		t.Fatalf("an identity that is neither an owner nor a deployment unit was not refused: %v", err)
	}
	if out.Status != "failed" {
		t.Fatalf("a refused identity did not report a failure: %+v", out)
	}
}


func TestProbeGRPCResolvesMaterialFromSharedCertDir(t *testing.T) {
	dir := t.TempDir()
	env := func(mapValues map[string]string) func(string) string {
		return func(key string) string { return mapValues[key] }
	}
	resolved := owneridentity.OwnerTLSFromEnv(env(map[string]string{
		"NODE_ENV":                "production",
		"OPL_GRPC_CA_FILE":        filepath.Join(dir, "ca.crt"),
		owneridentity.CertDirEnv:  dir,
	}), owneridentity.Tenant.Service())
	if resolved.CertFile != filepath.Join(dir, "tenant.crt") || resolved.KeyFile != filepath.Join(dir, "tenant.key") {
		t.Fatalf("resolved shared cert dir paths = (%q, %q)", resolved.CertFile, resolved.KeyFile)
	}
	explicitCert := filepath.Join(dir, "explicit.crt")
	explicitKey := filepath.Join(dir, "explicit.key")
	explicit := owneridentity.OwnerTLSFromEnv(env(map[string]string{
		"NODE_ENV":               "production",
		"OPL_GRPC_CA_FILE":       filepath.Join(dir, "ca.crt"),
		owneridentity.CertDirEnv: dir,
		"OPL_GRPC_CERT_FILE":     explicitCert,
		"OPL_GRPC_KEY_FILE":      explicitKey,
	}), owneridentity.Tenant.Service())
	if explicit.CertFile != explicitCert || explicit.KeyFile != explicitKey {
		t.Fatalf("explicit cert/key override lost: (%q, %q)", explicit.CertFile, explicit.KeyFile)
	}
	// Exercising probeGRPC confirms missing cert files under OPL_GRPC_CERT_DIR fail with tls_config rather than dialing unauthenticated.
	out, err := probeGRPC(config{Owner: owneridentity.Gateway, Address: "127.0.0.1:1", Timeout: testProbeTimeout}, env(map[string]string{
		"NODE_ENV":               "production",
		"OPL_GRPC_CA_FILE":       filepath.Join(dir, "ca.crt"),
		owneridentity.CertDirEnv: dir,
	}))
	if err == nil || !strings.HasPrefix(err.Error(), "tls_config:") || out.TransportPrincipal != "tenant" {
		t.Fatalf("probeGRPC with shared cert dir = (%+v, %v), want tls_config error for tenant principal", out, err)
	}
}
