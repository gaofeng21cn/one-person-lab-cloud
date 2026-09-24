// Package transporttls provides the shared mTLS transport boundary for Cloud
// owner processes. It contains no owner policy; callers supply their own
// certificate files and service identity policy.
package transporttls

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Config struct {
	CAFile     string
	CertFile   string
	KeyFile    string
	ServerName string
}

func FromEnv(getenv func(string) string, prefix string) Config {
	return Config{
		CAFile:     strings.TrimSpace(getenv(prefix + "_CA_FILE")),
		CertFile:   strings.TrimSpace(getenv(prefix + "_CERT_FILE")),
		KeyFile:    strings.TrimSpace(getenv(prefix + "_KEY_FILE")),
		ServerName: strings.TrimSpace(getenv(prefix + "_SERVER_NAME")),
	}
}

func (c Config) Empty() bool {
	return c.CAFile == "" && c.CertFile == "" && c.KeyFile == "" && c.ServerName == ""
}

func (c Config) Validate() error {
	if c.Empty() {
		return nil
	}
	if c.CAFile == "" || c.CertFile == "" || c.KeyFile == "" {
		return errors.New("mTLS requires CA, certificate, and private key files")
	}
	if c.ServerName == "" {
		return errors.New("mTLS server name is required")
	}
	return nil
}

func (c Config) ClientCredentials() (credentials.TransportCredentials, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.Empty() {
		return nil, errors.New("mTLS is not configured")
	}
	pool, err := loadPool(c.CAFile)
	if err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load mTLS client certificate: %w", err)
	}
	return credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS13,
		RootCAs:      pool,
		Certificates: []tls.Certificate{cert},
		ServerName:   c.ServerName,
	}), nil
}

func (c Config) ServerOption() (grpc.ServerOption, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.Empty() {
		return nil, errors.New("mTLS is not configured")
	}
	pool, err := loadPool(c.CAFile)
	if err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load mTLS server certificate: %w", err)
	}
	return grpc.Creds(credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
	})), nil
}

func loadPool(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mTLS CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, errors.New("mTLS CA file contains no certificate")
	}
	return pool, nil
}
