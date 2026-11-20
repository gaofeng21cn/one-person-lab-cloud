package owneridentity

import (
	"path/filepath"
	"testing"
)

// An installation that runs every owner in its own container declares the
// owner's three files. The composed installation runs every owner in one
// container, so that container can carry only one environment and declares the
// directory that holds one certificate and key per owner instead. Both forms
// must resolve the same material, and only the declared form may change.
func TestOwnerTLSFromEnvResolvesTheOwnersPairFromTheSharedDirectory(t *testing.T) {
	directory := "/var/run/opl-owner-tls"
	environment := map[string]string{
		"OPL_GRPC_CA_FILE":  filepath.Join(directory, "ca.crt"),
		"OPL_GRPC_CERT_DIR": directory,
	}
	getenv := func(name string) string { return environment[name] }
	config := OwnerTLSFromEnv(getenv, DeploymentUnitTransportPrincipal(Serve))
	if config.CAFile != filepath.Join(directory, "ca.crt") {
		t.Fatalf("CA file = %q, want the declared CA", config.CAFile)
	}
	if want := filepath.Join(directory, "serve.crt"); config.CertFile != want {
		t.Fatalf("certificate = %q, want %q", config.CertFile, want)
	}
	if want := filepath.Join(directory, "serve.key"); config.KeyFile != want {
		t.Fatalf("key = %q, want %q", config.KeyFile, want)
	}
	if config.AllowInsecureLocal {
		t.Fatal("a production installation must not resolve an insecure local transport")
	}
	// The composition that starts the owners resolves the material of the
	// owners it starts from the same directory with the same spelling.
	certFile, keyFile := OwnerMaterialPaths(directory, ControlPlane)
	if certFile != filepath.Join(directory, "control_plane.crt") || keyFile != filepath.Join(directory, "control_plane.key") {
		t.Fatalf("control plane material = %q, %q", certFile, keyFile)
	}
}

// The declared files win: an installation that states them keeps the form it
// always had, and the directory is only a fallback.
func TestOwnerTLSFromEnvKeepsTheDeclaredFiles(t *testing.T) {
	environment := map[string]string{
		"OPL_GRPC_CA_FILE":   "/tls/ca.crt",
		"OPL_GRPC_CERT_FILE": "/tls/tls.crt",
		"OPL_GRPC_KEY_FILE":  "/tls/tls.key",
		"OPL_GRPC_CERT_DIR":  "/var/run/opl-owner-tls",
	}
	getenv := func(name string) string { return environment[name] }
	config := OwnerTLSFromEnv(getenv, DeploymentUnitTransportPrincipal(Serve))
	if config.CertFile != "/tls/tls.crt" || config.KeyFile != "/tls/tls.key" {
		t.Fatalf("declared material = %q, %q", config.CertFile, config.KeyFile)
	}
}

// Without material there is nothing to resolve: the caller reports the
// missing configuration instead of guessing a path.
func TestOwnerTLSFromEnvWithoutMaterial(t *testing.T) {
	getenv := func(string) string { return "" }
	config := OwnerTLSFromEnv(getenv, DeploymentUnitTransportPrincipal(Serve))
	if config.CertFile != "" || config.KeyFile != "" || config.CAFile != "" {
		t.Fatalf("unexpected material: %#v", config)
	}
	// An unknown principal must not resolve a path either.
	environment := map[string]string{"OPL_GRPC_CERT_DIR": "/var/run/opl-owner-tls"}
	config = OwnerTLSFromEnv(func(name string) string { return environment[name] }, Service("unknown_owner"))
	if config.CertFile != "" || config.KeyFile != "" {
		t.Fatalf("unknown principal resolved material: %#v", config)
	}
}
