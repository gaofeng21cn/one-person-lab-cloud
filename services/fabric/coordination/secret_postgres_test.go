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
