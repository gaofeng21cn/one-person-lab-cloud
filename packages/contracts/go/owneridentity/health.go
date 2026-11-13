package owneridentity

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// HealthDialOptions returns transport-only options for the read-only gRPC health
// service. Health.Check is intentionally outside the owner metadata allowlist,
// but its mTLS connection still proves both the caller certificate and the
// target deployment-unit certificate.
func (c TLSConfig) HealthDialOptions(caller, target Service) ([]grpc.DialOption, error) {
	if !caller.Valid() || !target.Valid() {
		return nil, fmt.Errorf("known caller and target service identities required")
	}
	if c.AllowInsecureLocal {
		return []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, nil
	}
	config, err := c.load(caller)
	if err != nil {
		return nil, err
	}
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
	return []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(config))}, nil
}
