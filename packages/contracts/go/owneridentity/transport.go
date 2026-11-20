package owneridentity

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Service is a transport principal. Console BFF is not a data owner.
type Service string

const ConsoleBFF Service = "console_bff"

// ControlPlane is the retained process, not an additional target data owner.
// It revalidates CloudIdentity before serving its original customer records.
const ControlPlane Service = "control_plane"

func (s Service) String() string { return string(s) }
func (s Service) Valid() bool    { return s == ConsoleBFF || s == ControlPlane || Owner(s).Valid() }
func (o Owner) Service() Service { return Service(o) }

// DeploymentUnitTransportPrincipal resolves the transport principal of the
// deployment unit that serves one owner's typed boundary. CloudIdentity (tenant)
// and Gateway Integration (gateway) are two data owners inside one process and
// one deployment unit, and that process carries exactly one transport identity:
// tenant. A caller that reaches the Gateway owner therefore verifies the
// deployment unit's principal; the service name `gateway` is the owner identity
// on the data boundary (it names the audience an authorization decision is
// issued for), never a certificate a running process can present. Every other
// owner is its own transport principal.
func DeploymentUnitTransportPrincipal(owner Owner) Service {
	if owner == Gateway {
		return Tenant.Service()
	}
	return owner.Service()
}

// TLSConfig uses the instance's CA and certificates; this package issues none.
// Certificates carry URI SAN spiffe://opl.cloud/service/<service>.
type TLSConfig struct {
	CAFile             string
	CertFile           string
	KeyFile            string
	AllowInsecureLocal bool
}

func TLSFromEnv(getenv func(string) string) TLSConfig {
	return TLSConfig{CAFile: strings.TrimSpace(getenv("OPL_GRPC_CA_FILE")), CertFile: strings.TrimSpace(getenv("OPL_GRPC_CERT_FILE")), KeyFile: strings.TrimSpace(getenv("OPL_GRPC_KEY_FILE")), AllowInsecureLocal: getenv("NODE_ENV") != "production" && getenv("OPL_GRPC_INSECURE_LOCAL") == "1"}
}

// CertDirEnv names the directory that holds one certificate and key per owner.
// An installation that runs every owner inside one container cannot give each
// owner its own file variables, because one container carries one environment.
// Such an installation declares this directory instead, and every owner
// resolves its own pair from it.
const CertDirEnv = "OPL_GRPC_CERT_DIR"

// OwnerMaterialPaths names the certificate and the key of one owner inside a
// directory that holds one pair per owner. This is the one spelling of that
// layout: the composition that starts the owners and every owner-scoped
// command inside the shared container resolve their material the same way.
func OwnerMaterialPaths(directory string, principal Service) (string, string) {
	return filepath.Join(directory, principal.String()+".crt"), filepath.Join(directory, principal.String()+".key")
}

// OwnerTLSFromEnv resolves the transport material of one owner.
//
// An owner that runs in its own container is told its three files directly. An
// owner-scoped command that runs inside the shared container is told the
// directory that holds one pair per owner, and resolves its own pair from it.
// The declared files win when both are present, so an installation that
// declares them keeps the form it always had.
func OwnerTLSFromEnv(getenv func(string) string, principal Service) TLSConfig {
	config := TLSFromEnv(getenv)
	if config.CertFile != "" && config.KeyFile != "" {
		return config
	}
	directory := strings.TrimSpace(getenv(CertDirEnv))
	if directory == "" || !principal.Valid() {
		return config
	}
	certFile, keyFile := OwnerMaterialPaths(directory, principal)
	return TLSConfig{CAFile: config.CAFile, CertFile: certFile, KeyFile: keyFile, AllowInsecureLocal: config.AllowInsecureLocal}
}

func CertificateService(cert *x509.Certificate) (Service, error) {
	if cert == nil {
		return "", fmt.Errorf("service certificate required")
	}
	var service Service
	for _, uri := range cert.URIs {
		if uri.Scheme == "spiffe" && uri.Host == "opl.cloud" && strings.HasPrefix(uri.Path, "/service/") && uri.RawQuery == "" && uri.Fragment == "" {
			candidate := Service(strings.TrimPrefix(uri.Path, "/service/"))
			if !candidate.Valid() || service != "" {
				return "", fmt.Errorf("invalid or ambiguous service certificate identity")
			}
			service = candidate
		}
	}
	if service == "" {
		return "", fmt.Errorf("service URI SAN required")
	}
	return service, nil
}
func (c TLSConfig) load(identity Service) (*tls.Config, error) {
	if !identity.Valid() {
		return nil, fmt.Errorf("unknown service identity %q", identity)
	}
	if c.CAFile == "" || c.CertFile == "" || c.KeyFile == "" {
		return nil, fmt.Errorf("mTLS requires OPL_GRPC_CA_FILE, OPL_GRPC_CERT_FILE and OPL_GRPC_KEY_FILE")
	}
	pem, err := os.ReadFile(c.CAFile)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("mTLS CA file has no certificates")
	}
	cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, err
	}
	actual, err := CertificateService(leaf)
	if err != nil || actual != identity {
		return nil, fmt.Errorf("certificate does not identify service %s", identity)
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, RootCAs: roots, ClientCAs: roots}, nil
}
func (c TLSConfig) ServerCredentials(identity Service) (credentials.TransportCredentials, error) {
	if c.AllowInsecureLocal {
		return nil, nil
	}
	config, err := c.load(identity)
	if err != nil {
		return nil, err
	}
	config.ClientAuth = tls.RequireAndVerifyClientCert
	return credentials.NewTLS(config), nil
}
func (c TLSConfig) DialOptions(caller, target Service, token string) ([]grpc.DialOption, error) {
	if !caller.Valid() || !target.Valid() {
		return nil, fmt.Errorf("known caller and target service identities required")
	}
	if len(strings.TrimSpace(token)) < MinimumTokenLength {
		return nil, fmt.Errorf("calling service token must contain at least %d characters", MinimumTokenLength)
	}
	var creds credentials.TransportCredentials
	if c.AllowInsecureLocal {
		creds = insecure.NewCredentials()
	} else {
		config, err := c.load(caller)
		if err != nil {
			return nil, err
		}
		// CA, lifetime, server EKU and exact URI identity are checked below. DNS names
		// are not service identities and deployment addresses can change independently.
		config.InsecureSkipVerify = true
		config.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return fmt.Errorf("server certificate required")
			}
			intermediates := x509.NewCertPool()
			for _, cert := range state.PeerCertificates[1:] {
				intermediates.AddCert(cert)
			}
			if _, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: config.RootCAs, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
				return err
			}
			actual, err := CertificateService(state.PeerCertificates[0])
			if err != nil || actual != target {
				return fmt.Errorf("server certificate does not identify %s", target)
			}
			return nil
		}
		creds = credentials.NewTLS(config)
	}
	return []grpc.DialOption{grpc.WithTransportCredentials(creds), grpc.WithChainUnaryInterceptor(OutboundInterceptor(caller, token))}, nil
}
