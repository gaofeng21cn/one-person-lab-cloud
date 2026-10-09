package coordination_test

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/internal/ownerservice"
)

// secretDispatcher is a coordination.ResourceDispatcher fixture that confirms one
// approved-store Secret per workspace and counts confirmations, so a retry or a
// second bind can be told apart.
type secretDispatcher struct {
	calls    int
	bySpace  map[string]coordination.SecretBindResult
	conflict bool
}

func (d *secretDispatcher) Provider() string { return "local-docker" }

func (d *secretDispatcher) EnsureResources(context.Context, coordination.ResourceIntent) (*coordination.ResourceResult, error) {
	return nil, fmt.Errorf("resource dispatch not used")
}

func (d *secretDispatcher) DeleteResource(context.Context, coordination.ResourceDeletionIntent, string) (coordination.ResourceDeletionFact, error) {
	return coordination.ResourceDeletionFact{}, fmt.Errorf("fixture_provider_delete_unavailable")
}

func (d *secretDispatcher) BindSecret(_ context.Context, in coordination.SecretBindIntent) (coordination.SecretBindResult, error) {
	d.calls++
	if d.conflict {
		return coordination.SecretBindResult{}, fmt.Errorf("provider_conflict")
	}
	if d.bySpace == nil {
		d.bySpace = map[string]coordination.SecretBindResult{}
	}
	if result, ok := d.bySpace[in.WorkspaceID]; ok {
		if result.Fingerprint != in.Fingerprint {
			return coordination.SecretBindResult{}, fmt.Errorf("provider_conflict")
		}
		return result, nil
	}
	version := strings.TrimPrefix(in.Fingerprint, "sha256:")
	if len(version) < 16 {
		return coordination.SecretBindResult{}, fmt.Errorf("provider_conflict")
	}
	result := coordination.SecretBindResult{SecretRef: in.SecretRef, Version: version[:16], Fingerprint: in.Fingerprint}
	d.bySpace[in.WorkspaceID] = result
	return result, nil
}

// rebindDispatcher is a ResourceDispatcher fixture that confirms any requested
// approved-store Secret identity, so a replacement with a new fingerprint is a
// legitimate provider confirmation rather than a conflict.
type rebindDispatcher struct{ calls int }

func (d *rebindDispatcher) Provider() string { return "local-docker" }

func (d *rebindDispatcher) EnsureResources(context.Context, coordination.ResourceIntent) (*coordination.ResourceResult, error) {
	return nil, fmt.Errorf("resource dispatch not used")
}

func (d *rebindDispatcher) DeleteResource(context.Context, coordination.ResourceDeletionIntent, string) (coordination.ResourceDeletionFact, error) {
	return coordination.ResourceDeletionFact{}, fmt.Errorf("fixture_provider_delete_unavailable")
}

func (d *rebindDispatcher) BindSecret(_ context.Context, in coordination.SecretBindIntent) (coordination.SecretBindResult, error) {
	d.calls++
	version := strings.TrimPrefix(in.Fingerprint, "sha256:")
	if len(version) < 16 {
		return coordination.SecretBindResult{}, fmt.Errorf("provider_conflict")
	}
	return coordination.SecretBindResult{SecretRef: in.SecretRef, Version: version[:16], Fingerprint: in.Fingerprint}, nil
}

func TestBindSecretRecordsProviderConfirmedBindingOnce(t *testing.T) {
	db := database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	catalogOwner := &catalog{acceptances: map[string]*api.QuoteAcceptance{}}
	catalogServer := grpc.NewServer()
	api.RegisterCatalogCoordinationServer(catalogServer, catalogOwner)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go catalogServer.Serve(listener)
	t.Cleanup(catalogServer.Stop)
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	authority := &identity{}
	service, err := coordination.New(db, ownerservice.NewAuthorizer(ownerservice.OwnerFabric, authority).Authorize, api.NewCatalogCoordinationClient(connection))
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := &secretDispatcher{}
	service.Dispatcher = dispatcher
	token := "isolated-fabric-serve-token-000000000001"
	config := ownerservice.Config{Owner: ownerservice.OwnerFabric, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.Workspace.Service(): token, owneridentity.Serve.Service(): token, owneridentity.Build.Service(): token}}
	server, err := ownerservice.NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Register(server); err != nil {
		t.Fatal(err)
	}
	fabricListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.ServeOn(fabricListener)
	t.Cleanup(server.Stop)
	opts, err := config.TLS.DialOptions(owneridentity.Serve.Service(), owneridentity.Fabric.Service(), token)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(fabricListener.Addr().String(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	client := api.NewFabricCoordinationClient(conn)

	// Seed a confirmed resource set with an execution resource for one workspace.
	const workspace, tenant = "workspace-secret", "tenant"
	const fingerprint = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	var setID string
	if err := db.QueryRowContext(ctx, `INSERT INTO fabric.resource_sets (id,tenant_id,workspace_id,provider,provider_profile_ref,region,compute_plan_id,storage_plan_id,accepted_quote_id,approved_specification,observation_result,observed_at) VALUES ('rset_secret',$1,$2,'local-docker','profile','local','cp','sp','quote', '{"billingMode":"LOCAL_NO_CHARGE"}'::jsonb,'confirmed',now()) RETURNING id`, tenant, workspace).Scan(&setID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fabric.resources (id,resource_set_id,kind,provider_purchase_key,billing_mode,requested_specification,observation_result) VALUES ('res_exec_seed',$1,'execution','execution:seed','LOCAL_NO_CHARGE','{}'::jsonb,'confirmed')`, setID); err != nil {
		t.Fatal(err)
	}
	command := func(key string) *api.SecretBindingCommand {
		return &api.SecretBindingCommand{Context: &api.CallContext{ActorId: "actor", RequestId: "request-" + key, IdempotencyKey: key, SessionId: proto.String("session"), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}}}, WorkspaceId: workspace, RuntimeInstanceId: "rt_secret", KeyBindingId: "key-1", SecretDeliveryReference: "opl-gateway-" + workspace, TargetSlot: "gateway", Fingerprint: fingerprint}
	}
	first, err := client.BindSecret(ctx, command("bind-key-1"))
	if err != nil {
		t.Fatal(err)
	}
	if first.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || first.GetVersion() == "" || first.GetFingerprint() != fingerprint || first.GetSecretBindingId() == "" {
		t.Fatalf("binding=%+v", first)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("provider confirmations=%d want 1", dispatcher.calls)
	}
	second, err := client.BindSecret(ctx, command("bind-key-1"))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(first, second) || dispatcher.calls != 1 {
		t.Fatalf("replay=%+v calls=%d", second, dispatcher.calls)
	}
	conflict := command("bind-key-2")
	conflict.SecretDeliveryReference = "opl-gateway-other"
	if _, err := client.BindSecret(ctx, conflict); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflict err=%v", err)
	}
	authority.deny = true
	if _, err := client.BindSecret(ctx, command("bind-key-3")); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("deny err=%v", err)
	}
}

// TestRebindSecretReplacesOnlyTheExpectedPredecessor proves Fabric's replacement
// contract: an initial Serve BindSecret creates the one active binding,
// RebindSecret with the exact predecessor retires it and writes the replacement as
// the one active binding (never two), a replayed RebindSecret returns the same
// readback without a second retirement, a wrong predecessor is refused, and the
// retired Workspace caller is refused.
func TestRebindSecretReplacesOnlyTheExpectedPredecessor(t *testing.T) {
	db := database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	catalogOwner := &catalog{acceptances: map[string]*api.QuoteAcceptance{}}
	catalogServer := grpc.NewServer()
	api.RegisterCatalogCoordinationServer(catalogServer, catalogOwner)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go catalogServer.Serve(listener)
	t.Cleanup(catalogServer.Stop)
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	authority := &identity{}
	service, err := coordination.New(db, ownerservice.NewAuthorizer(ownerservice.OwnerFabric, authority).Authorize, api.NewCatalogCoordinationClient(connection))
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := &rebindDispatcher{}
	service.Dispatcher = dispatcher
	token := "isolated-fabric-workspace-token-0000000001"
	config := ownerservice.Config{Owner: ownerservice.OwnerFabric, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.Workspace.Service(): token, owneridentity.Serve.Service(): token, owneridentity.Build.Service(): token}}
	server, err := ownerservice.NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Register(server); err != nil {
		t.Fatal(err)
	}
	fabricListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.ServeOn(fabricListener)
	t.Cleanup(server.Stop)

	const workspace, tenant = "workspace-rebind", "tenant"
	const firstFingerprint = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	const secondFingerprint = "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	var setID string
	if err := db.QueryRowContext(ctx, `INSERT INTO fabric.resource_sets (id,tenant_id,workspace_id,provider,provider_profile_ref,region,compute_plan_id,storage_plan_id,accepted_quote_id,approved_specification,observation_result,observed_at) VALUES ('rset_rebind',$1,$2,'local-docker','profile','local','cp','sp','quote', '{"billingMode":"LOCAL_NO_CHARGE"}'::jsonb,'confirmed',now()) RETURNING id`, tenant, workspace).Scan(&setID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fabric.resources (id,resource_set_id,kind,provider_purchase_key,billing_mode,requested_specification,observation_result) VALUES ('res_exec_rebind',$1,'execution','execution:rebind','LOCAL_NO_CHARGE','{}'::jsonb,'confirmed')`, setID); err != nil {
		t.Fatal(err)
	}
	call := func(key string) *api.CallContext {
		return &api.CallContext{ActorId: "actor", RequestId: "request-" + key, IdempotencyKey: key, SessionId: proto.String("session"), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}}}
	}
	serveOpts, err := config.TLS.DialOptions(owneridentity.Serve.Service(), owneridentity.Fabric.Service(), token)
	if err != nil {
		t.Fatal(err)
	}
	serveConn, err := grpc.NewClient(fabricListener.Addr().String(), serveOpts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { serveConn.Close() })
	client := api.NewFabricCoordinationClient(serveConn)

	first, err := client.BindSecret(ctx, &api.SecretBindingCommand{Context: call("bind-first"), WorkspaceId: workspace, RuntimeInstanceId: "rt_rebind", KeyBindingId: "key-1", SecretDeliveryReference: "opl-gateway-" + workspace, TargetSlot: "gateway", Fingerprint: firstFingerprint})
	if err != nil || first.GetSecretBindingId() == "" || dispatcher.calls != 1 {
		t.Fatalf("initial bind=%+v err=%v calls=%d", first, err, dispatcher.calls)
	}
	predecessor := first.GetSecretBindingId()

	rebind := func(key, predecessorID, fingerprint string) (*api.SecretBindingRebindReadback, error) {
		return client.RebindSecret(ctx, &api.SecretBindingRebindCommand{Context: call(key), WorkspaceId: workspace, RuntimeInstanceId: "rt_rebind", ExpectedCurrentSecretBindingId: predecessorID, KeyBindingId: "key-2", SecretDeliveryReference: "opl-gateway-" + workspace, TargetSlot: "gateway", Fingerprint: fingerprint})
	}
	replacement, err := rebind("rebind-replace", predecessor, secondFingerprint)
	if err != nil {
		t.Fatalf("rebind: %v", err)
	}
	if replacement.GetPreviousSecretBindingId() != predecessor || replacement.GetSecretBindingId() == "" || replacement.GetSecretBindingId() == predecessor || replacement.GetFingerprint() != secondFingerprint || replacement.GetVersion() == "" || replacement.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED {
		t.Fatalf("replacement=%+v", replacement)
	}
	// One active binding remains, the replacement, and the predecessor is retired.
	var active, retired string
	var activeCount int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM fabric.secret_bindings WHERE execution_resource_id='res_exec_rebind' AND purpose='gateway' AND revoked_at IS NULL`).Scan(&activeCount); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT id FROM fabric.secret_bindings WHERE execution_resource_id='res_exec_rebind' AND purpose='gateway' AND revoked_at IS NULL`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT id FROM fabric.secret_bindings WHERE id=$1 AND revoked_at IS NOT NULL`, predecessor).Scan(&retired); err != nil {
		t.Fatal(err)
	}
	if activeCount != 1 || active != replacement.GetSecretBindingId() || retired != predecessor {
		t.Fatalf("active=%q count=%d retired=%q want replacement=%q predecessor retired", active, activeCount, retired, replacement.GetSecretBindingId())
	}
	// A replay of the same replacement returns the same readback without a second
	// retirement or a second provider confirmation.
	calls := dispatcher.calls
	replayed, err := rebind("rebind-replace", predecessor, secondFingerprint)
	if err != nil {
		t.Fatalf("rebind replay: %v", err)
	}
	if !proto.Equal(replacement, replayed) || dispatcher.calls != calls {
		t.Fatalf("replay=%+v calls=%d (want %d)", replayed, dispatcher.calls, calls)
	}
	// A wrong predecessor is refused and never retires the active binding.
	if _, err = rebind("rebind-wrong", "sbx_not-the-predecessor", secondFingerprint); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("wrong predecessor err=%v want failed precondition", err)
	}
	// A different key under the replayed idempotency key is a conflict.
	conflict := &api.SecretBindingRebindCommand{Context: call("rebind-replace"), WorkspaceId: workspace, RuntimeInstanceId: "rt_rebind", ExpectedCurrentSecretBindingId: predecessor, KeyBindingId: "key-9", SecretDeliveryReference: "opl-gateway-" + workspace, TargetSlot: "gateway", Fingerprint: secondFingerprint}
	if _, err = client.RebindSecret(ctx, conflict); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("idempotency conflict err=%v want already exists", err)
	}
	// Workspace is no longer admitted: Serve is the only Secret-binding caller.
	workspaceOpts, err := config.TLS.DialOptions(owneridentity.Workspace.Service(), owneridentity.Fabric.Service(), token)
	if err != nil {
		t.Fatal(err)
	}
	workspaceConn, err := grpc.NewClient(fabricListener.Addr().String(), workspaceOpts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { workspaceConn.Close() })
	if _, err = api.NewFabricCoordinationClient(workspaceConn).RebindSecret(ctx, conflict); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("retired Workspace peer err=%v want permission denied", err)
	}
	// A denied authorization (wrong tenant/owner) is refused before any retirement.
	authority.deny = true
	defer func() { authority.deny = false }()
	if _, err = client.RebindSecret(ctx, &api.SecretBindingRebindCommand{Context: call("rebind-denied"), WorkspaceId: workspace, RuntimeInstanceId: "rt_rebind", ExpectedCurrentSecretBindingId: replacement.GetSecretBindingId(), KeyBindingId: "key-3", SecretDeliveryReference: "opl-gateway-" + workspace, TargetSlot: "gateway", Fingerprint: firstFingerprint}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("denied rebind err=%v want permission denied", err)
	}
}
