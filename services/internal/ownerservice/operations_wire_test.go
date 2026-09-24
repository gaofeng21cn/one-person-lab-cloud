package ownerservice

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
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/transporttls"
	"opl-cloud/services/internal/ownerstore"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// TestOwnerProcessOverTheWire proves one owner process's real boundary behaviour
// against a real PostgreSQL server and a real gRPC listener: the health status it
// reports, the typed Operation it reads back for its own row, its refusal of an
// unknown operation, and its refusal of an unauthenticated caller.
func TestOwnerProcessOverTheWire(t *testing.T) {
	t.Setenv("OPL_POSTGRES_TESTS", "1")
	adminDSN := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	ctx := context.Background()

	harness, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{
		AdminDSN:        adminDSN,
		Owner:           "serve",
		Database:        "opl_serve",
		SchemaOwnerRole: "opl_serve_owner",
		WriterRole:      "opl_serve_writer",
		RuntimeRole:     "opl_serve_runtime",
	})
	if err != nil {
		t.Fatalf("provision isolated database: %v", err)
	}
	t.Cleanup(func() { _ = harness.Close(context.Background()) })

	// This package is shared by every owner, so its test installs a representative
	// owner schema in the shape the contract fixes: the schema owner creates the schema
	// and grants its writes to the owner's writer role, which is the only reach the
	// runtime login has.
	if err := harness.Install(ctx, harness.OwnerDSN, harness.DatabaseName(), ownerSchemaMigrations{}); err != nil {
		t.Fatalf("install owner schema: %v", err)
	}

	database, err := OpenDatabase(ctx, OwnerServe, harness.RuntimeDSN)
	if err != nil {
		t.Fatalf("open owner database: %v", err)
	}
	defer database.Close()
	operations, err := NewOperations(OwnerServe, database.Store())
	if err != nil {
		t.Fatal(err)
	}
	token := "0123456789abcdef0123456789abcdef"
	if _, err := database.DB().ExecContext(ctx, `INSERT INTO serve.operations
		(id, tenant_id, actor_id, kind, resource_id, status, stage, observation_result, request_id, accepted_input)
		VALUES ('op-wire-proof','tenant-wire','actor-wire','runtime_deploy','resource-wire','running','queued','unknown','request-wire','{}'::jsonb)`); err != nil {
		t.Fatalf("record operation: %v", err)
	}

	serverTLS, clientTLS := testTLSConfigs(t)
	config := Config{Owner: OwnerServe, Services: map[owneridentity.ServiceIdentity]string{owneridentity.ConsoleBFF: token}, TLS: serverTLS}
	server, err := NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := operations.Register(server); err != nil {
		t.Fatal(err)
	}
	if err := server.RequireProductGroups("ServeProductService"); err != nil {
		t.Fatal(err)
	}
	// The Serve domain handlers are not implemented, so the process reports
	// NOT_SERVING; the Operation readback group it does serve is still reachable and
	// is what this test reads.
	if err := server.AddReadinessCheck("product_handlers", func(context.Context) error {
		return ErrHandlersNotImplemented
	}); err != nil {
		t.Fatal(err)
	}
	if err := server.MarkServing(ctx); err == nil {
		t.Fatal("an owner with unimplemented handlers reported SERVING")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.ServeOn(listener) }()
	t.Cleanup(func() {
		server.Stop()
		select {
		case err := <-served:
			if err != nil && err != grpc.ErrServerStopped {
				t.Errorf("serve: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("owner server did not stop")
		}
	})

	addr := listener.Addr().String()
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	authenticated := dialOwner(t, addr, token, &clientTLS)
	defer authenticated.Close()

	health, err := healthpb.NewHealthClient(authenticated).Check(callCtx, &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if health.GetStatus() != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("health = %s, want NOT_SERVING for an owner without its product handlers", health.GetStatus())
	}

	call := &api.CallContext{
		RequestId: "request-wire-read",
		ActorId:   "actor-wire",
		Scope:     &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-wire"}}},
	}
	read, err := api.NewOwnerOperationsClient(authenticated).Read(callCtx, &api.OwnerOperationRequest{Context: call, OperationId: "op-wire-proof"})
	if err != nil {
		t.Fatalf("read stored operation: %v", err)
	}
	switch {
	case read.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE:
		t.Errorf("owner = %s, want serve", read.GetOwner())
	case read.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_RUNTIME_DEPLOY:
		t.Errorf("kind = %s, want runtime_deploy", read.GetKind())
	case read.GetStage() != api.OperationStageEnum_OPERATION_STAGE_ENUM_QUEUED:
		t.Errorf("stage = %s, want queued", read.GetStage())
	case read.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_RUNNING:
		t.Errorf("status = %s, want running", read.GetStatus())
	case read.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN:
		t.Errorf("observation = %s, want unknown", read.GetObservationResult())
	case read.GetPollAfterSeconds() != int32(checkCadence/time.Second):
		t.Errorf("pollAfterSeconds = %d, want the owner's own cadence", read.GetPollAfterSeconds())
	}

	if _, err := api.NewOwnerOperationsClient(authenticated).Read(callCtx, &api.OwnerOperationRequest{Context: call, OperationId: "op-missing"}); status.Code(err) != codes.NotFound {
		t.Errorf("unknown operation code = %v, want NotFound", status.Code(err))
	}

	anonymous := dialOwner(t, addr, "", &clientTLS)
	defer anonymous.Close()
	_, err = api.NewOwnerOperationsClient(anonymous).Read(callCtx, &api.OwnerOperationRequest{Context: call, OperationId: "op-wire-proof"})
	switch status.Code(err) {
	case codes.Unauthenticated, codes.PermissionDenied:
	default:
		t.Errorf("unauthenticated caller failed with %v, want an identity refusal", err)
	}
}

// ownerSchemaMigrations is a representative owner schema for this package's own
// boundary test. Each owner's real schema is owned and tested by that owner.
type ownerSchemaMigrations struct{}

func (ownerSchemaMigrations) ReadDir(string) ([]ownerstore.DirEntry, error) {
	return []ownerstore.DirEntry{ownerSchemaEntry{}}, nil
}

func (ownerSchemaMigrations) ReadFile(string) ([]byte, error) {
	return []byte(`BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_serve' THEN RAISE EXCEPTION 'Wrong database: expected opl_serve, got %', current_database(); END IF;
END $$;
CREATE SCHEMA serve AUTHORIZATION opl_serve_owner;
REVOKE ALL ON SCHEMA serve FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT CONNECT ON DATABASE opl_serve TO opl_serve_writer;
SET LOCAL ROLE opl_serve_owner;
CREATE TABLE serve.operations (
  id text NOT NULL,
  tenant_id text,
  actor_id text NOT NULL,
  kind text NOT NULL,
  resource_id text NOT NULL,
  status text NOT NULL DEFAULT 'accepted',
  stage text NOT NULL,
  error_code text,
  observation_result text,
  request_id text NOT NULL,
  accepted_input jsonb NOT NULL,
  result jsonb,
  worker_lease_token text,
  worker_lease_until timestamptz,
  started_at timestamptz,
  completed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id)
);
GRANT USAGE ON SCHEMA serve TO opl_serve_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA serve TO opl_serve_writer;
COMMIT;
`), nil
}

type ownerSchemaEntry struct{}

func (ownerSchemaEntry) Name() string { return "0001_owner_schema.sql" }
func (ownerSchemaEntry) IsDir() bool  { return false }

func dialOwner(t *testing.T, addr, token string, tlsConfig *transporttls.Config) *grpc.ClientConn {
	t.Helper()
	credentials, err := tlsConfig.ClientCredentials()
	if err != nil {
		t.Fatal(err)
	}
	options := []grpc.DialOption{grpc.WithTransportCredentials(credentials)}
	if token != "" {
		options = append(options, grpc.WithChainUnaryInterceptor(owneridentity.OutboundServiceInterceptor(owneridentity.ConsoleBFF, token)))
	}
	conn, err := grpc.NewClient(addr, options...)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func testTLSConfigs(t *testing.T) (transporttls.Config, transporttls.Config) {
	t.Helper()
	dir := t.TempDir()
	caKey, caCert := makeCertificate(t, nil, nil, true)
	serverKey, serverCert := makeCertificate(t, caCert, caKey, false, "localhost")
	clientKey, clientCert := makeCertificate(t, caCert, caKey, false)
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

func makeCertificate(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey, isCA bool, names ...string) (*rsa.PrivateKey, *x509.Certificate) {
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
