package ownerservice

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
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
	identity, err := NewServer(Config{Owner: OwnerTenant, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[Service]string{OwnerServe.Service(): wireToken}})
	if err != nil {
		t.Fatal(err)
	}
	_ = identity.Register(func(s *grpc.Server) { api.RegisterCloudIdentityAuthorizationServer(s, &wireIdentity{}) })
	auth, identityConn, err := AuthorizerFromConfig(Config{Owner: OwnerServe, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, CloudIdentityAddr: startWireServer(t, identity), CloudIdentityToken: wireToken})
	if err != nil {
		t.Fatal(err)
	}
	defer identityConn.Close()
	operations, err := NewOperations(OwnerServe, database.Store(), auth)
	if err != nil {
		t.Fatal(err)
	}
	token := "0123456789abcdef0123456789abcdef"
	if _, err := database.DB().ExecContext(ctx, `INSERT INTO serve.operations
		(id, actor_id, kind, resource_id, status, stage, observation_result, request_id, accepted_input)
		VALUES ('op-wire-proof','actor-wire','runtime_deploy','resource-wire','running','queued','unknown','request-wire','{}'::jsonb)`); err != nil {
		t.Fatalf("record operation: %v", err)
	}

	config := Config{TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Owner: OwnerServe, Peers: map[Service]string{Service(OwnerWorkspace): token}}
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

	authenticated := dialOwner(t, addr, token)
	defer authenticated.Close()

	health, err := healthpb.NewHealthClient(authenticated).Check(callCtx, &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if health.GetStatus() != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("health = %s, want NOT_SERVING for an owner without its product handlers", health.GetStatus())
	}

	session := "session-wire"
	userContext := &api.CallContext{ActorId: "actor-wire", SessionId: &session, RequestId: "request-wire", Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}}
	read, err := api.NewOwnerOperationsClient(authenticated).Read(callCtx, &api.OwnerOperationRequest{Context: userContext, OperationId: "op-wire-proof"})
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

	if _, err := api.NewOwnerOperationsClient(authenticated).Read(callCtx, &api.OwnerOperationRequest{Context: userContext, OperationId: "op-missing"}); status.Code(err) != codes.NotFound {
		t.Errorf("unknown operation code = %v, want NotFound", status.Code(err))
	}

	for _, test := range []struct {
		name string
		call *api.CallContext
	}{
		{"missing context", nil},
		{"other actor", &api.CallContext{ActorId: "another-actor", SessionId: &session, RequestId: "request-other", Scope: userContext.Scope}},
		{"tenant against platform record", &api.CallContext{ActorId: "actor-wire", SessionId: &session, RequestId: "request-other", Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "other-tenant"}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := api.NewOwnerOperationsClient(authenticated).Read(callCtx, &api.OwnerOperationRequest{Context: test.call, OperationId: "op-wire-proof"})
			if status.Code(err) != codes.Unauthenticated && status.Code(err) != codes.PermissionDenied {
				t.Fatalf("expected authorization refusal, got %v", err)
			}
		})
	}
	anonymous := dialOwner(t, addr, "")
	defer anonymous.Close()
	_, err = api.NewOwnerOperationsClient(anonymous).Read(callCtx, &api.OwnerOperationRequest{OperationId: "op-wire-proof"})
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

func dialOwner(t *testing.T, addr, token string) *grpc.ClientConn {
	t.Helper()
	options := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if token != "" {
		options = append(options, grpc.WithChainUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.Service(owneridentity.Workspace), token)))
	}
	conn, err := grpc.NewClient(addr, options...)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}
