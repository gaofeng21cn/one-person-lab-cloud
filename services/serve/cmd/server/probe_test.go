package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

const probeTestToken = "probe-serve-token-0123456789abcdef0123456789"

// Exercise the actual command branch, including its exit status and stdout/stderr.
func TestWorkspaceProbeProcess(t *testing.T) {
	if os.Getenv("OPL_PROBE_TEST_CHILD") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	os.Args = append([]string{"opl-serve"}, os.Args[separator+1:]...)
	main()
	os.Exit(0)
}

type probeWireHandler struct {
	api.UnimplementedWorkspaceAccessReadbackServer
	seen  chan string
	reply func(context.Context, *api.ReadWorkspaceAccessRequest) (*api.WorkspaceAccessDecision, error)
}

func (h *probeWireHandler) ReadWorkspaceAccess(ctx context.Context, request *api.ReadWorkspaceAccessRequest) (*api.WorkspaceAccessDecision, error) {
	caller, ok := ownerservice.PeerOwner(ctx)
	if !ok || caller != owneridentity.Serve.Service() {
		return nil, status.Error(codes.PermissionDenied, "handler requires authenticated Serve identity")
	}
	h.seen <- request.GetWorkspaceId()
	if h.reply != nil {
		return h.reply(ctx, request)
	}
	return &api.WorkspaceAccessDecision{WorkspaceId: request.GetWorkspaceId(), Verdict: api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_CLOSED}, nil
}

func startProbeWorkspace(t *testing.T, tlsConfig owneridentity.TLSConfig, h *probeWireHandler, serverOwner ...owneridentity.Owner) string {
	t.Helper()
	owner := owneridentity.Workspace
	if len(serverOwner) > 0 {
		owner = serverOwner[0]
	}
	server, err := ownerservice.NewServer(ownerservice.Config{
		Owner: owner, TLS: tlsConfig,
		Peers: map[owneridentity.Service]string{owneridentity.Serve.Service(): probeTestToken},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Register(func(g *grpc.Server) { api.RegisterWorkspaceAccessReadbackServer(g, h) }); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.ServeOn(listener) }()
	t.Cleanup(server.Stop)
	return listener.Addr().String()
}

// The existing ownerservice testCertificates helper is package-private. Use its
// ephemeral ECDSA CA + SPIFFE service-certificate pattern, with real TLS on TCP.
func probeCertificates(t *testing.T) func(owneridentity.Service) owneridentity.TLSConfig {
	t.Helper()
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, kind string, data []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: data}), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	caFile := write("ca.pem", "CERTIFICATE", der)
	cache := map[owneridentity.Service]owneridentity.TLSConfig{}
	return func(service owneridentity.Service) owneridentity.TLSConfig {
		if config, ok := cache[service]; ok {
			return config
		}
		leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		uri, err := url.Parse("spiffe://opl.cloud/service/" + service.String())
		if err != nil {
			t.Fatal(err)
		}
		leaf := &x509.Certificate{SerialNumber: big.NewInt(int64(len(cache) + 2)), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), URIs: []*url.URL{uri}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
		der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := x509.MarshalPKCS8PrivateKey(leafKey)
		if err != nil {
			t.Fatal(err)
		}
		config := owneridentity.TLSConfig{CAFile: caFile, CertFile: write(service.String()+".pem", "CERTIFICATE", der), KeyFile: write(service.String()+".key", "PRIVATE KEY", encoded)}
		cache[service] = config
		return config
	}
}

func runProbeCommand(t *testing.T, address string, config owneridentity.TLSConfig, env map[string]string, args ...string) (map[string]any, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestWorkspaceProbeProcess$", "--", "probe-workspace-access"}, args...)...)
	values := map[string]string{
		"OPL_PROBE_TEST_CHILD": "1", "NODE_ENV": "production",
		"OPL_WORKSPACE_ADDR": address, "OPL_WORKSPACE_TOKEN": probeTestToken,
		"OPL_GRPC_CA_FILE": config.CAFile, "OPL_GRPC_CERT_FILE": config.CertFile, "OPL_GRPC_KEY_FILE": config.KeyFile,
		// A probe must bypass even configuration that would refuse normal startup.
		"OPL_SERVE_PEER_TOKENS": "invalid-json", "OPL_SERVE_ADDR": "invalid-listener", "OPL_SERVE_ACCESS_ADDR": "invalid-access-listener",
	}
	for key, value := range env {
		values[key] = value
	}
	for key, value := range values {
		command.Env = append(command.Env, key+"="+value)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatal("probe exceeded process deadline")
	}
	exit := 0
	if err != nil {
		if failure, ok := err.(*exec.ExitError); ok {
			exit = failure.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("probe wrote stderr instead of restricted JSON: %q", stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("invalid restricted JSON: %v (%q)", err, stdout.String())
	}
	// Only bounded booleans, a stable code and the zero write counter are allowed.
	if len(result) != 7 {
		t.Fatalf("unexpected JSON fields: %#v", result)
	}
	if result["schemaVersion"] != float64(1) || result["operationMode"] != "serve_workspace_access_probe" {
		t.Fatalf("unexpected native JSON contract: %#v", result)
	}
	for _, name := range []string{"authenticated", "workspaceClosed"} {
		if result[name] != (exit == 0) {
			t.Fatalf("%s must reflect probe success: %#v", name, result)
		}
	}
	counts, ok := result["mutationCounts"].(map[string]any)
	if !ok || len(counts) != 2 || counts["business"] != float64(0) || counts["provider"] != float64(0) {
		t.Fatalf("probe mutation scope changed: %#v", result)
	}
	if exit == 0 {
		if result["status"] != "observed" || result["errorCode"] != "none" {
			t.Fatalf("unexpected success output: %#v", result)
		}
	} else if result["status"] != "failed" || !regexp.MustCompile(`^[A-Z][A-Z_]+$`).MatchString(fmt.Sprint(result["errorCode"])) {
		t.Fatalf("failure must have bounded status and stable code: %#v", result)
	}
	return result, stdout.String(), exit
}

func TestWorkspaceProbeClosedWithServeMTLSBeforeDatabaseStartup(t *testing.T) {
	certificates := probeCertificates(t)
	handler := &probeWireHandler{seen: make(chan string, 2)}
	address := startProbeWorkspace(t, certificates(owneridentity.Workspace.Service()), handler)
	var previous string
	for range 2 {
		result, output, exit := runProbeCommand(t, address, certificates(owneridentity.Serve.Service()), nil)
		if exit != 0 || result["status"] != "observed" || result["errorCode"] != "none" {
			t.Fatalf("CLOSED probe: exit=%d result=%#v", exit, result)
		}
		select {
		case id := <-handler.seen:
			if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) || id == previous {
				t.Fatal("expected fresh synthetic UUID v4")
			}
			if strings.Contains(output, id) || strings.Contains(output, address) || strings.Contains(output, probeTestToken) {
				t.Fatal("probe leaked private input")
			}
			previous = id
		default:
			t.Fatal("Workspace did not receive authenticated Serve RPC")
		}
	}
}

func TestWorkspaceProbeRefusesUnexpectedPermission(t *testing.T) {
	certificates := probeCertificates(t)
	handler := &probeWireHandler{seen: make(chan string, 1), reply: func(_ context.Context, request *api.ReadWorkspaceAccessRequest) (*api.WorkspaceAccessDecision, error) {
		return &api.WorkspaceAccessDecision{WorkspaceId: request.GetWorkspaceId(), Verdict: api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_PERMITTED}, nil
	}}
	address := startProbeWorkspace(t, certificates(owneridentity.Workspace.Service()), handler)
	result, _, exit := runProbeCommand(t, address, certificates(owneridentity.Serve.Service()), nil)
	if exit != 1 || result["errorCode"] != "UNEXPECTED_PERMITTED" {
		t.Fatalf("PERMITTED must fail probe: exit=%d result=%#v", exit, result)
	}
}

func TestWorkspaceProbeRedactsRPCFailure(t *testing.T) {
	certificates := probeCertificates(t)
	handler := &probeWireHandler{seen: make(chan string, 1), reply: func(_ context.Context, request *api.ReadWorkspaceAccessRequest) (*api.WorkspaceAccessDecision, error) {
		return nil, status.Error(codes.PermissionDenied, "upstream-secret "+probeTestToken+" dns:///private-workspace:8181 "+request.GetWorkspaceId())
	}}
	address := startProbeWorkspace(t, certificates(owneridentity.Workspace.Service()), handler)
	result, output, exit := runProbeCommand(t, address, certificates(owneridentity.Serve.Service()), nil)
	if exit != 1 || result["errorCode"] != "RPC_PERMISSION_DENIED" {
		t.Fatalf("RPC failure: exit=%d result=%#v", exit, result)
	}
	if strings.Contains(output, "upstream-secret") || strings.Contains(output, probeTestToken) || strings.Contains(output, address) || strings.Contains(output, <-handler.seen) {
		t.Fatal("RPC error leaked private input")
	}
}

func TestWorkspaceProbeFiveSecondDeadline(t *testing.T) {
	certificates := probeCertificates(t)
	handler := &probeWireHandler{seen: make(chan string, 1), reply: func(ctx context.Context, _ *api.ReadWorkspaceAccessRequest) (*api.WorkspaceAccessDecision, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	address := startProbeWorkspace(t, certificates(owneridentity.Workspace.Service()), handler)
	started := time.Now()
	result, _, exit := runProbeCommand(t, address, certificates(owneridentity.Serve.Service()), nil)
	elapsed := time.Since(started)
	if exit != 1 || result["errorCode"] != "RPC_DEADLINE_EXCEEDED" || elapsed < 4800*time.Millisecond || elapsed > 7*time.Second {
		t.Fatalf("five-second RPC deadline: exit=%d elapsed=%s result=%#v", exit, elapsed, result)
	}
}

func TestWorkspaceProbeRejectsUnverifiedReadback(t *testing.T) {
	for _, tc := range []struct {
		name       string
		verdict    api.WorkspaceAccessVerdict
		mismatched bool
		code       string
	}{
		{name: "unspecified", verdict: api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_UNSPECIFIED, code: "UNEXPECTED_VERDICT"},
		{name: "unverified", verdict: api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_UNVERIFIED, code: "UNEXPECTED_VERDICT"},
		{name: "unknown", verdict: api.WorkspaceAccessVerdict(99), code: "UNEXPECTED_VERDICT"},
		{name: "mismatched workspace", verdict: api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_CLOSED, mismatched: true, code: "RESPONSE_MISMATCH"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			certificates := probeCertificates(t)
			handler := &probeWireHandler{seen: make(chan string, 1), reply: func(_ context.Context, request *api.ReadWorkspaceAccessRequest) (*api.WorkspaceAccessDecision, error) {
				id := request.GetWorkspaceId()
				if tc.mismatched {
					id = "different-workspace"
				}
				return &api.WorkspaceAccessDecision{WorkspaceId: id, Verdict: tc.verdict}, nil
			}}
			address := startProbeWorkspace(t, certificates(owneridentity.Workspace.Service()), handler)
			result, output, exit := runProbeCommand(t, address, certificates(owneridentity.Serve.Service()), nil)
			if exit != 1 || result["errorCode"] != tc.code || strings.Contains(output, "different-workspace") {
				t.Fatalf("unexpected readback: exit=%d result=%#v", exit, result)
			}
		})
	}
}

func TestWorkspaceProbeRefusesInvalidIdentityAndConfiguration(t *testing.T) {
	certificates := probeCertificates(t)
	handler := &probeWireHandler{seen: make(chan string, 4)}
	address := startProbeWorkspace(t, certificates(owneridentity.Workspace.Service()), handler)
	for _, tc := range []struct {
		name     string
		identity owneridentity.Service
		env      map[string]string
		args     []string
		code     string
	}{
		{name: "no address", identity: owneridentity.Serve.Service(), env: map[string]string{"OPL_WORKSPACE_ADDR": ""}, code: "CONFIG_INVALID"},
		{name: "short token", identity: owneridentity.Serve.Service(), env: map[string]string{"OPL_WORKSPACE_TOKEN": "short"}, code: "CONFIG_INVALID"},
		{name: "wrong token", identity: owneridentity.Serve.Service(), env: map[string]string{"OPL_WORKSPACE_TOKEN": strings.Repeat("x", 40)}, code: "RPC_UNAUTHENTICATED"},
		{name: "missing certificate", identity: owneridentity.Serve.Service(), env: map[string]string{"OPL_GRPC_CERT_FILE": "/missing/private-cert"}, code: "TLS_CONFIG_INVALID"},
		{name: "wrong client identity", identity: owneridentity.Build.Service(), code: "TLS_CONFIG_INVALID"},
		{name: "insecure local refused", identity: owneridentity.Serve.Service(), env: map[string]string{"NODE_ENV": "development", "OPL_GRPC_INSECURE_LOCAL": "1"}, code: "TLS_CONFIG_INVALID"},
		{name: "production insecure flag cannot bypass TLS", identity: owneridentity.Serve.Service(), env: map[string]string{"OPL_GRPC_INSECURE_LOCAL": "1", "OPL_GRPC_CERT_FILE": ""}, code: "TLS_CONFIG_INVALID"},
		{name: "customer workspace forbidden", identity: owneridentity.Serve.Service(), args: []string{"--workspace-id", "customer-workspace"}, code: "USAGE_INVALID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, output, exit := runProbeCommand(t, address, certificates(tc.identity), tc.env, tc.args...)
			if exit != 1 || result["errorCode"] != tc.code {
				t.Fatalf("configuration/identity must be refused: exit=%d result=%#v", exit, result)
			}
			if strings.Contains(output, address) || strings.Contains(output, probeTestToken) || strings.Contains(output, "private-cert") || strings.Contains(output, "customer-workspace") {
				t.Fatal("configuration failure leaked private input")
			}
			select {
			case <-handler.seen:
				t.Fatal("invalid identity or configuration reached Workspace handler")
			default:
			}
		})
	}
}

func TestWorkspaceProbeRefusesWrongWorkspaceTLSIdentity(t *testing.T) {
	certificates := probeCertificates(t)
	// A CA-trusted peer presenting another owner identity must not receive the RPC.
	handler := &probeWireHandler{seen: make(chan string, 1)}
	address := startProbeWorkspace(t, certificates(owneridentity.Build.Service()), handler, owneridentity.Build)
	result, _, exit := runProbeCommand(t, address, certificates(owneridentity.Serve.Service()), nil)
	if exit != 1 || result["errorCode"] != "RPC_UNAVAILABLE" {
		t.Fatalf("wrong server identity: exit=%d result=%#v", exit, result)
	}
	select {
	case <-handler.seen:
		t.Fatal("wrong TLS server identity received Workspace request")
	default:
	}
}

func TestWorkspaceProbeKeepsGRPCDiagnosticsPrivate(t *testing.T) {
	certificates := probeCertificates(t)
	handler := &probeWireHandler{seen: make(chan string, 1)}
	address := startProbeWorkspace(t, certificates(owneridentity.Workspace.Service()), handler)
	result, _, exit := runProbeCommand(t, address, certificates(owneridentity.Serve.Service()), map[string]string{
		"GRPC_GO_LOG_SEVERITY_LEVEL": "info", "GRPC_GO_LOG_VERBOSITY_LEVEL": "99",
	})
	if exit != 0 || result["status"] != "observed" {
		t.Fatalf("verbose gRPC configuration changed restricted output: exit=%d result=%#v", exit, result)
	}
}
