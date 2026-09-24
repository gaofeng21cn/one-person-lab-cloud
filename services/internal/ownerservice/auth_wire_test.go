package ownerservice

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"math/big"
	"net"
	"net/url"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const wireToken = "0123456789abcdef0123456789abcdef"

type wireIdentity struct {
	api.UnimplementedCloudIdentityAuthorizationServer
}

func (*wireIdentity) AuthorizeAction(_ context.Context, r *api.AuthorizationRequest) (*api.AuthorizationDecision, error) {
	return &api.AuthorizationDecision{Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED, ActorId: r.ActorId, Scope: r.Scope, SessionId: r.SessionId, AcceptedOperationGrantId: r.AcceptedOperationGrantId, AudienceOwner: r.AudienceOwner, Action: r.Action, Resource: r.Resource, PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Second)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}, nil
}

type guardedOperations struct {
	api.UnimplementedOwnerOperationsServer
	auth *Authorizer
}

func (s *guardedOperations) Read(ctx context.Context, r *api.OwnerOperationRequest) (*api.Operation, error) {
	err := s.auth.Authorize(ctx, r.Context, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATION, &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_OPERATION, Id: &r.OperationId}, ResourceScope{TenantID: "tenant-a", ActorID: "actor-a"})
	if err != nil {
		return nil, err
	}
	return &api.Operation{OperationId: r.OperationId}, nil
}
func startWireServer(t *testing.T, s *Server) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = s.ServeOn(l) }()
	t.Cleanup(s.Stop)
	return l.Addr().String()
}
func TestMTLSAndUserAuthorizationOverWire(t *testing.T) {
	certs := testCertificates(t)
	identity, err := NewServer(Config{Owner: OwnerTenant, TLS: certs(OwnerTenant.Service()), Peers: map[Service]string{OwnerServe.Service(): wireToken}})
	if err != nil {
		t.Fatal(err)
	}
	_ = identity.Register(func(s *grpc.Server) { api.RegisterCloudIdentityAuthorizationServer(s, &wireIdentity{}) })
	auth, conn, err := AuthorizerFromConfig(Config{Owner: OwnerServe, TLS: certs(OwnerServe.Service()), CloudIdentityAddr: startWireServer(t, identity), CloudIdentityToken: wireToken})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	owner, err := NewServer(Config{Owner: OwnerServe, TLS: certs(OwnerServe.Service()), Peers: map[Service]string{owneridentity.ConsoleBFF: wireToken, OwnerBuild.Service(): wireToken}})
	if err != nil {
		t.Fatal(err)
	}
	_ = owner.Register(func(s *grpc.Server) { api.RegisterOwnerOperationsServer(s, &guardedOperations{auth: auth}) })
	addr := startWireServer(t, owner)
	request := func(tenant string) *api.OwnerOperationRequest {
		session := "session-a"
		return &api.OwnerOperationRequest{OperationId: "op-a", Context: &api.CallContext{ActorId: "actor-a", SessionId: &session, RequestId: "request-a", Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}}}}
	}
	dial := func(tls owneridentity.TLSConfig, target Service, extra ...grpc.DialOption) *grpc.ClientConn {
		t.Helper()
		opts, err := tls.DialOptions(owneridentity.ConsoleBFF, target, wireToken)
		if err != nil {
			t.Fatal(err)
		}
		opts = append(opts, extra...)
		c, err := grpc.NewClient(addr, opts...)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	client := api.NewOwnerOperationsClient(dial(certs(owneridentity.ConsoleBFF), OwnerServe.Service()))
	for _, tc := range []struct {
		name string
		r    *api.OwnerOperationRequest
		code codes.Code
	}{{"correct BFF identity", request("tenant-a"), codes.OK}, {"missing context", &api.OwnerOperationRequest{OperationId: "op-a"}, codes.Unauthenticated}, {"cross tenant", request("tenant-b"), codes.PermissionDenied}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err := client.Read(ctx, tc.r)
			if status.Code(err) != tc.code {
				t.Fatalf("code=%s want=%s err=%v", status.Code(err), tc.code, err)
			}
		})
	}
	untrusted := testCertificates(t)(owneridentity.ConsoleBFF)
	untrusted.CAFile = certs(owneridentity.ConsoleBFF).CAFile
	for _, tc := range []struct {
		name   string
		tls    owneridentity.TLSConfig
		target Service
		extra  []grpc.DialOption
		code   codes.Code
	}{{"wrong target", certs(owneridentity.ConsoleBFF), OwnerBuild.Service(), nil, codes.Unavailable}, {"untrusted certificate", untrusted, OwnerServe.Service(), nil, codes.Unavailable}, {"header impersonation", certs(owneridentity.ConsoleBFF), OwnerServe.Service(), []grpc.DialOption{grpc.WithChainUnaryInterceptor(owneridentity.OutboundInterceptor(OwnerBuild.Service(), wireToken))}, codes.Unauthenticated}} {
		t.Run(tc.name, func(t *testing.T) {
			c := api.NewOwnerOperationsClient(dial(tc.tls, tc.target, tc.extra...))
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := c.Read(ctx, request("tenant-a"))
			if status.Code(err) != tc.code {
				t.Fatalf("code=%s want=%s err=%v", status.Code(err), tc.code, err)
			}
		})
	}
}
func testCertificates(t *testing.T) func(Service) owneridentity.TLSConfig {
	t.Helper()
	dir := t.TempDir()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(dir, "ca.pem")
	_ = os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	cache := map[Service]owneridentity.TLSConfig{}
	return func(service Service) owneridentity.TLSConfig {
		if c, ok := cache[service]; ok {
			return c
		}
		leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		uri, _ := url.Parse("spiffe://opl.cloud/service/" + service.String())
		cert := &x509.Certificate{SerialNumber: big.NewInt(int64(len(cache) + 2)), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), URIs: []*url.URL{uri}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
		der, err := x509.CreateCertificate(rand.Reader, cert, ca, &leafKey.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		certFile := filepath.Join(dir, service.String()+".pem")
		keyFile := filepath.Join(dir, service.String()+".key")
		_ = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
		encoded, _ := x509.MarshalPKCS8PrivateKey(leafKey)
		_ = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0600)
		c := owneridentity.TLSConfig{CAFile: caFile, CertFile: certFile, KeyFile: keyFile}
		cache[service] = c
		return c
	}
}
