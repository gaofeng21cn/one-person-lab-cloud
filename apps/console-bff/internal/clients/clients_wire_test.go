package clients

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/requestcontext"
	"opl-cloud/packages/contracts/go/transporttls"
)

type workspaceWireServer struct {
	api.UnimplementedWorkspaceProductServiceServer
	metadata metadata.MD
	request  *api.GetWorkspaceRpcRequest
}

func (s *workspaceWireServer) GetWorkspace(ctx context.Context, request *api.GetWorkspaceRpcRequest) (*api.Workspace, error) {
	s.metadata, _ = metadata.FromIncomingContext(ctx)
	s.request = request
	return &api.Workspace{Id: request.GetWorkspaceId()}, nil
}

func TestClientsWorkspaceUsesServiceIdentityAndTypedCallContext(t *testing.T) {
	serverTLS, clientTLS := testClientTLSConfigs(t)
	serverCredentials, err := serverTLS.ServerOption()
	if err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer(serverCredentials)
	wire := &workspaceWireServer{}
	api.RegisterWorkspaceProductServiceServer(grpcServer, wire)
	serveDone := make(chan error, 1)
	go func() { serveDone <- grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		listener.Close()
		select {
		case err := <-serveDone:
			if err != nil && err != grpc.ErrServerStopped {
				t.Errorf("serve: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("workspace wire server did not stop")
		}
	})

	const token = "bff-service-token-012345678901234567890123"
	upstreams, err := Dial(Config{
		Addresses:    map[owneridentity.Owner]string{owneridentity.Workspace: listener.Addr().String()},
		ServiceToken: token,
		TLS:          clientTLS,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer upstreams.Close()

	call := &api.CallContext{
		RequestId: "request-1",
		ActorId:   "actor-1",
		Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{
			Tenant: &api.TenantScope{TenantId: "tenant-1"},
		}},
	}
	workspace, err := upstreams.Workspace(requestcontext.WithCallContext(context.Background(), call), "workspace-1")
	if err != nil {
		t.Fatal(err)
	}
	if workspace.GetId() != "workspace-1" {
		t.Fatalf("workspace id = %q, want workspace-1", workspace.GetId())
	}
	if got := wire.metadata.Get(owneridentity.PeerHeader); len(got) != 1 || got[0] != owneridentity.ConsoleBFF.String() {
		t.Fatalf("peer identity = %v, want %q", got, owneridentity.ConsoleBFF)
	}
	if got := wire.metadata.Get(owneridentity.TokenHeader); len(got) != 1 || got[0] != token {
		t.Fatalf("peer token = %v, want configured service token", got)
	}
	if wire.request == nil || wire.request.GetContext().GetRequestId() != call.GetRequestId() || wire.request.GetContext().GetActorId() != call.GetActorId() || wire.request.GetContext().GetScope().GetTenant().GetTenantId() != "tenant-1" {
		t.Fatalf("typed CallContext was not propagated: %v", wire.request.GetContext())
	}
}

func testClientTLSConfigs(t *testing.T) (transporttls.Config, transporttls.Config) {
	t.Helper()
	dir := t.TempDir()
	caKey, caCert := makeClientCertificate(t, nil, nil, true)
	serverKey, serverCert := makeClientCertificate(t, caCert, caKey, false, "localhost")
	clientKey, clientCert := makeClientCertificate(t, caCert, caKey, false)
	write := func(name string, value []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, value, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	encodeCert := func(cert *x509.Certificate) []byte {
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	}
	encodeKey := func(key *rsa.PrivateKey) []byte {
		return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	}
	caPath := write("ca.pem", encodeCert(caCert))
	return transporttls.Config{
			CAFile: caPath, CertFile: write("server.pem", encodeCert(serverCert)),
			KeyFile: write("server-key.pem", encodeKey(serverKey)), ServerName: "localhost",
		}, transporttls.Config{
			CAFile: caPath, CertFile: write("client.pem", encodeCert(clientCert)),
			KeyFile: write("client-key.pem", encodeKey(clientKey)), ServerName: "localhost",
		}
}

func makeClientCertificate(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey, isCA bool, names ...string) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	commonName := "test-ca"
	if len(names) > 0 {
		commonName = names[0]
	}
	template := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: commonName},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		DNSNames: names,
	}
	issuer := template
	parentKey := key
	if isCA {
		template.IsCA = true
		template.BasicConstraintsValid = true
		template.KeyUsage |= x509.KeyUsageCertSign
	}
	if ca != nil {
		issuer = ca
		parentKey = caKey
	}
	der, err := x509.CreateCertificate(rand.Reader, template, issuer, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return key, cert
}

func TestClientsRejectUntrustedServerCertificate(t *testing.T) {
	_, trustedClientTLS := testClientTLSConfigs(t)
	rogueServerTLS, _ := testClientTLSConfigs(t)
	serverCredentials, err := rogueServerTLS.ServerOption()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer(serverCredentials)
	api.RegisterWorkspaceProductServiceServer(grpcServer, &workspaceWireServer{})
	go grpcServer.Serve(listener)
	t.Cleanup(func() {
		grpcServer.Stop()
		listener.Close()
	})

	upstreams, err := Dial(Config{
		Addresses:    map[owneridentity.Owner]string{owneridentity.Workspace: listener.Addr().String()},
		ServiceToken: "bff-service-token-012345678901234567890123",
		TLS:          trustedClientTLS,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer upstreams.Close()
	call := &api.CallContext{
		RequestId: "request-untrusted-server",
		ActorId:   "actor-1",
		Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{
			Tenant: &api.TenantScope{TenantId: "tenant-1"},
		}},
	}
	if _, err := upstreams.Workspace(requestcontext.WithCallContext(context.Background(), call), "workspace-1"); err == nil {
		t.Fatal("client accepted a server certificate signed by an untrusted CA")
	}
}
