package composition

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// CertDirEnv names the directory that holds one certificate and key per owner.
//
// The installation's own PKI already writes that layout: the published local
// PKI script writes ca.crt plus one <principal>.crt and <principal>.key per
// declared process, and the published Compose file maps each owner to
// /pki/<principal>.crt. Inside one shared container every owner still needs
// its own mTLS identity, and the entrypoint has to reach an owner with that
// owner's own identity, the way the installer reaches an owner today by
// running the owner probe inside the owner's own container.
const CertDirEnv = "OPL_GRPC_CERT_DIR"

// OwnerCertificatePaths returns the certificate and key one owner process uses
// inside the shared container. It returns empty paths when the installation
// runs without transport security, and an error when the installation asks
// for transport security without providing the material.
func OwnerCertificatePaths(process Process, getenv func(string) string) (string, string, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	base := owneridentity.TLSFromEnv(getenv)
	if base.AllowInsecureLocal {
		return "", "", nil
	}
	directory := strings.TrimSpace(getenv(CertDirEnv))
	if directory == "" {
		return "", "", fmt.Errorf("owner %s: set %s to the directory that holds one certificate and key per owner", process.Service, CertDirEnv)
	}
	certFile := filepath.Join(directory, process.TransportPrincipal+".crt")
	keyFile := filepath.Join(directory, process.TransportPrincipal+".key")
	for _, path := range []string{certFile, keyFile} {
		if _, err := os.Stat(path); err != nil {
			return "", "", fmt.Errorf("owner %s: certificate material %s is not readable: %w", process.Service, path, err)
		}
	}
	return certFile, keyFile, nil
}

// ChildEnvironment returns the environment one owner process runs with. The
// contract supplies an allowlist for each process; only those names (plus the
// explicitly shared names) are projected from the composition environment.
// This is required because the parent container carries every owner's Secret
// reference while a child must retain the per-owner isolation of the former
// one-process-per-container deployment.
func ChildEnvironment(process Process, environ []string, getenv func(string) string) ([]string, error) {
	values := make(map[string]string, len(environ))
	for _, entry := range environ {
		name, value, ok := strings.Cut(entry, "=")
		if ok && name != "" {
			values[name] = value
		}
	}
	allowed := make(map[string]bool, len(process.AllowedEnv))
	for _, name := range process.AllowedEnv {
		allowed[name] = true
	}
	// Process literals used by callers that construct a Process directly (for
	// example focused tests) still receive their declared fields. A production
	// plan always sets AllowedEnv from the topology's sharedEnv declaration.
	for _, name := range append(append(append([]string{}, process.Env...), process.OptionalEnv...), process.ExternalEnv...) {
		allowed[name] = true
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("owner %s declares no environment allowlist", process.Service)
	}
	projected := make([]string, 0, len(allowed)+2)
	seen := make(map[string]bool, len(allowed)+2)
	for _, entry := range environ {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || !allowed[name] || seen[name] || name == "OPL_GRPC_CERT_FILE" || name == "OPL_GRPC_KEY_FILE" {
			continue
		}
		value := values[name]
		if listener, declared := process.ListenerEnvironment[name]; declared {
			value = listener
		}
		if peer, configured := process.PeerEnvironment[name]; configured {
			value = peer
		}
		if name == process.DatabaseEnv {
			if alias := ownerDatabaseAlias(process); values[alias] != "" {
				value = values[alias]
			}
		}
		if name == "OPL_INTERNAL_SERVICE_TOKEN" {
			if alias := ownerServiceTokenAlias(process); values[alias] != "" {
				value = values[alias]
			}
		}
		projected = append(projected, name+"="+value)
		seen[name] = true
	}
	// Aliased values may be the only source of a required child variable when
	// the parent intentionally omits its legacy name. Add the mapped variable
	// once, preserving the child contract's canonical name.
	if process.DatabaseEnv != "" && allowed[process.DatabaseEnv] && !seen[process.DatabaseEnv] {
		if value := values[ownerDatabaseAlias(process)]; value != "" {
			projected = append(projected, process.DatabaseEnv+"="+value)
			seen[process.DatabaseEnv] = true
		}
	}
	if allowed["OPL_INTERNAL_SERVICE_TOKEN"] && !seen["OPL_INTERNAL_SERVICE_TOKEN"] {
		if value := values[ownerServiceTokenAlias(process)]; value != "" {
			projected = append(projected, "OPL_INTERNAL_SERVICE_TOKEN="+value)
			seen["OPL_INTERNAL_SERVICE_TOKEN"] = true
		}
	}
	certFile, keyFile, err := OwnerCertificatePaths(process, getenv)
	if err != nil {
		return nil, err
	}
	if certFile == "" {
		return projected, nil
	}
	return append(projected,
		"OPL_GRPC_CERT_FILE="+certFile,
		"OPL_GRPC_KEY_FILE="+keyFile,
	), nil
}

func ownerDatabaseAlias(process Process) string {
	if process.DatabaseEnv == "" {
		return ""
	}
	principal := strings.ToUpper(strings.ReplaceAll(process.TransportPrincipal, "-", "_"))
	if principal == "" {
		return ""
	}
	return "OPL_" + principal + "_DATABASE_URL"
}

func ownerServiceTokenAlias(process Process) string {
	principal := strings.ToUpper(strings.ReplaceAll(process.TransportPrincipal, "-", "_"))
	if principal == "" {
		return ""
	}
	return "OPL_" + principal + "_SERVICE_TOKEN"
}

// Checker verifies one process against the readiness criterion the contract
// declares for it.
type Checker struct {
	getenv       func(string) string
	httpClient   *http.Client
	checkTimeout time.Duration
}

// NewChecker returns a checker that reads the installation's environment.
func NewChecker(getenv func(string) string) *Checker {
	if getenv == nil {
		getenv = os.Getenv
	}
	return &Checker{
		getenv:       getenv,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		checkTimeout: 5 * time.Second,
	}
}

// Check evaluates one criterion. It returns an error that names the owner and
// the check that did not pass, so the composition can report the owner that
// never became ready instead of a generic timeout.
func (c *Checker) Check(ctx context.Context, step Step) error {
	for _, check := range step.Criterion.Checks {
		address, err := checkAddress(step.Process, check.AddressEnv, c.getenv)
		if err != nil {
			return err
		}
		switch check.Transport {
		case "grpc":
			err = c.checkGRPC(ctx, step.Process, address)
		case "http":
			err = c.checkHTTP(ctx, step.Process, check, address)
		default:
			err = fmt.Errorf("unknown transport %q", check.Transport)
		}
		if err != nil {
			return fmt.Errorf("owner %s is not ready: %s check %s on %s failed: %w",
				step.Process.Service, check.Role, describeCheck(check), address, err)
		}
	}
	return nil
}

// checkAddress resolves the address one owner's declared listener is reached at
// in the shared container. The composition resolves the owner's own listener
// from the contract, so the probe reads the address the owner actually bound.
func checkAddress(process Process, addressEnv string, getenv func(string) string) (string, error) {
	if address, declared := process.ListenerEnvironment[addressEnv]; declared {
		return address, nil
	}
	return ResolveAddress(process, addressEnv, getenv)
}

// describeCheck names one check the way the contract declares it.
func describeCheck(check Check) string {
	if check.Transport == "http" {
		return fmt.Sprintf("%s %s", check.Method, check.Path)
	}
	return fmt.Sprintf("%s/%s", check.Service, check.Method)
}

// checkGRPC reads the owner's own gRPC health service. The health service is
// read-only and carries no owner identity, but its mTLS connection still
// proves the caller's certificate and the target deployment unit's
// certificate.
func (c *Checker) checkGRPC(ctx context.Context, process Process, address string) error {
	principal := owneridentity.Service(process.TransportPrincipal)
	if !principal.Valid() {
		return fmt.Errorf("owner %s declares the unknown transport principal %q", process.Service, process.TransportPrincipal)
	}
	certFile, keyFile, err := OwnerCertificatePaths(process, c.getenv)
	if err != nil {
		return err
	}
	base := owneridentity.TLSFromEnv(c.getenv)
	transport := owneridentity.TLSConfig{CAFile: base.CAFile, CertFile: certFile, KeyFile: keyFile, AllowInsecureLocal: base.AllowInsecureLocal}
	options, err := transport.HealthDialOptions(principal, principal)
	if err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, c.checkTimeout)
	defer cancel()
	connection, err := grpc.NewClient(grpcTarget(address), options...)
	if err != nil {
		return err
	}
	defer connection.Close()
	response, err := healthpb.NewHealthClient(connection).Check(callCtx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if response.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("the health service reported %s", response.GetStatus())
	}
	return nil
}

// checkHTTP reads one HTTP route the owning source registers. A readiness
// check that carries a projection also has to report that the projected owner
// is ready.
func (c *Checker) checkHTTP(ctx context.Context, process Process, check Check, address string) error {
	target := httpBaseURL(address) + check.Path
	request, err := http.NewRequestWithContext(ctx, check.Method, target, nil)
	if err != nil {
		return err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("the route answered %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	if check.Projection == "" {
		return nil
	}
	if check.Projection != "contracts.FabricReadiness" {
		return fmt.Errorf("the contract declares the unknown projection %q", check.Projection)
	}
	var readiness contracts.FabricReadiness
	if err := json.Unmarshal(body, &readiness); err != nil {
		return fmt.Errorf("the projection body does not carry %s: %w", check.Projection, err)
	}
	if !readiness.ServiceReady {
		return fmt.Errorf("the projection does not report a ready service")
	}
	return nil
}

// grpcTarget turns a listen address into a gRPC dial target. A bare port
// listens on every interface of the shared container, so it is reached
// through the loopback address.
func grpcTarget(address string) string {
	address = strings.TrimSpace(address)
	if index := strings.Index(address, "://"); index >= 0 {
		return strings.TrimSuffix(address[index+3:], "/")
	}
	if strings.HasPrefix(address, ":") {
		return "127.0.0.1" + address
	}
	return address
}

// httpBaseURL turns a listen address into the base URL of its HTTP surface.
func httpBaseURL(address string) string {
	address = strings.TrimSpace(address)
	if strings.Contains(address, "://") {
		return strings.TrimSuffix(address, "/")
	}
	if strings.HasPrefix(address, ":") {
		return "http://127.0.0.1" + address
	}
	return "http://" + address
}
