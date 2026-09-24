package owneridentity

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Service is a transport principal. Console BFF is not a data owner.
type Service string

const ConsoleBFF Service = "console_bff"

func (s Service) String() string { return string(s) }
func (s Service) Valid() bool    { return s == ConsoleBFF || Owner(s).Valid() }
func (o Owner) Service() Service { return Service(o) }

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
