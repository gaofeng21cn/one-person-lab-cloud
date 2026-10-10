package coordination_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/fabric/internal/fabric"
	"opl-cloud/services/internal/ownerservice"
)

// managedSecretDispatcher models the provider's existing idempotent Secret
// journal: a write is counted once per original operation key, a replay of the
// same key returns the stored identity without a second write, and the same key
// with a different value is a conflict.
type managedSecretDispatcher struct {
	writes, reads int
	fail          bool
	providerErr   error
	results       map[string]coordination.ManagedSecretResult
	fingerprints  map[string]string
}

func (d *managedSecretDispatcher) Provider() string { return "local-docker" }

func (d *managedSecretDispatcher) EnsureResources(context.Context, coordination.ResourceIntent) (*coordination.ResourceResult, error) {
	return nil, status.Error(codes.Unimplemented, "resource dispatch is not used")
}

func (d *managedSecretDispatcher) BindSecret(context.Context, coordination.SecretBindIntent) (coordination.SecretBindResult, error) {
	return coordination.SecretBindResult{}, status.Error(codes.Unimplemented, "Secret binding is not used")
}

func (d *managedSecretDispatcher) PutManagedSecret(_ context.Context, in coordination.PutManagedSecretIntent) (coordination.ManagedSecretResult, error) {
	d.reads++
	if d.results == nil {
		d.results, d.fingerprints = map[string]coordination.ManagedSecretResult{}, map[string]string{}
	}
	if stored, ok := d.results[in.IdempotencyKey]; ok {
		if d.fingerprints[in.IdempotencyKey] != in.Fingerprint {
			return coordination.ManagedSecretResult{}, fabric.ErrGatewaySecretIdempotencyConflict
		}
		return stored, nil
	}
	if d.providerErr != nil {
		return coordination.ManagedSecretResult{}, d.providerErr
	}
	if d.fail {
		return coordination.ManagedSecretResult{}, fabric.ErrWorkspaceLaunchPending
	}
	d.writes++
	result := coordination.ManagedSecretResult{SecretRef: in.SecretRef, Version: strings.TrimPrefix(in.Fingerprint, "sha256:")[:16], Fingerprint: in.Fingerprint}
	d.results[in.IdempotencyKey], d.fingerprints[in.IdempotencyKey] = result, in.Fingerprint
	return result, nil
}

func managedSecretFingerprintOf(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}

// managedSecretSystem serves Fabric coordination over a real gRPC server with a
// distinct tenant transport token, so the caller peer rule is exercised as the
// deployment enforces it.
func managedSecretSystem(t *testing.T, dispatcher coordination.ResourceDispatcher) (api.FabricCoordinationClient, api.FabricCoordinationClient, *identity) {
	t.Helper()
	db := database(t)
	catalogOwner := &catalog{acceptances: map[string]*api.QuoteAcceptance{}}
	catalogServer := grpc.NewServer()
	api.RegisterCatalogCoordinationServer(catalogServer, catalogOwner)
	catalogListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go catalogServer.Serve(catalogListener)
	t.Cleanup(catalogServer.Stop)
	catalogConn, err := grpc.NewClient(catalogListener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { catalogConn.Close() })
	authority := &identity{}
	service, err := coordination.New(db, ownerservice.NewAuthorizer(ownerservice.OwnerFabric, authority).Authorize, api.NewCatalogCoordinationClient(catalogConn))
	if err != nil {
		t.Fatal(err)
	}
	service.Dispatcher = dispatcher
	const tenantToken = "isolated-fabric-tenant-token-0000000001"
	const workspaceToken = "isolated-fabric-workspace-token-0000000001"
	config := ownerservice.Config{Owner: ownerservice.OwnerFabric, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.Tenant.Service(): tenantToken, owneridentity.Workspace.Service(): workspaceToken}}
	server, err := ownerservice.NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Register(server); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.ServeOn(listener)
	t.Cleanup(server.Stop)
	tenantOpts, err := config.TLS.DialOptions(owneridentity.Tenant.Service(), owneridentity.Fabric.Service(), tenantToken)
	if err != nil {
		t.Fatal(err)
	}
	tenantConn, err := grpc.NewClient(listener.Addr().String(), tenantOpts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tenantConn.Close() })
	workspaceOpts, err := config.TLS.DialOptions(owneridentity.Workspace.Service(), owneridentity.Fabric.Service(), workspaceToken)
	if err != nil {
		t.Fatal(err)
	}
	workspaceConn, err := grpc.NewClient(listener.Addr().String(), workspaceOpts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { workspaceConn.Close() })
	return api.NewFabricCoordinationClient(tenantConn), api.NewFabricCoordinationClient(workspaceConn), authority
}

func managedSecretCommand(workspace, key, idempotencyKey string) *api.ManagedSecretCommand {
	return &api.ManagedSecretCommand{
		Context: &api.CallContext{ActorId: "actor", RequestId: "request-" + idempotencyKey, IdempotencyKey: idempotencyKey, SessionId: proto.String("session"),
			Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-managed-secret"}}}},
		WorkspaceId: workspace, WorkspaceApiKeyId: 77, Fingerprint: managedSecretFingerprintOf(key),
		SecretDeliveryReference: contracts.WorkspaceGatewaySecretRef(workspace), GatewayApiKey: key,
	}
}

// TestPutManagedSecretWritesOnceAndReplaysTheStoredIdentity proves the tenant
// transport principal writes one approved-store Secret for a Workspace and a
// replay of the same original operation key returns the stored identity without a
// second provider write.
func TestPutManagedSecretWritesOnceAndReplaysTheStoredIdentity(t *testing.T) {
	const workspace, key = "workspace-managed-secret", "raw-managed-key-value"
	dispatcher := &managedSecretDispatcher{}
	tenant, _, _ := managedSecretSystem(t, dispatcher)
	first, err := tenant.PutManagedSecret(t.Context(), managedSecretCommand(workspace, key, "op-1:create_managed_key:put_managed_secret"))
	if err != nil {
		t.Fatal(err)
	}
	if first.GetSecretRef() != contracts.WorkspaceGatewaySecretRef(workspace) || first.GetVersion() == "" || first.GetFingerprint() != managedSecretFingerprintOf(key) {
		t.Fatalf("readback=%+v", first)
	}
	if dispatcher.writes != 1 || dispatcher.reads != 1 {
		t.Fatalf("provider writes=%d reads=%d want exactly one write", dispatcher.writes, dispatcher.reads)
	}
	replay, err := tenant.PutManagedSecret(t.Context(), managedSecretCommand(workspace, key, "op-1:create_managed_key:put_managed_secret"))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(first, replay) || dispatcher.writes != 1 || dispatcher.reads != 2 {
		t.Fatalf("replay=%+v writes=%d reads=%d", replay, dispatcher.writes, dispatcher.reads)
	}
}

// TestPutManagedSecretRefusesInvalidInputAndWrongCallers proves the owner
// validates the exact original identity, admits only the Gateway Integration
// transport principal, and keeps CloudIdentity's decision authoritative.
func TestPutManagedSecretRefusesInvalidInputAndWrongCallers(t *testing.T) {
	const workspace, key = "workspace-managed-secret-2", "raw-managed-key-value-2"
	dispatcher := &managedSecretDispatcher{}
	tenant, workspaceClient, authority := managedSecretSystem(t, dispatcher)
	valid := func() *api.ManagedSecretCommand {
		return managedSecretCommand(workspace, key, "op-2:create_managed_key:put_managed_secret")
	}
	for name, mutate := range map[string]func(*api.ManagedSecretCommand){
		"empty workspace":        func(r *api.ManagedSecretCommand) { r.WorkspaceId = "" },
		"zero external key id":   func(r *api.ManagedSecretCommand) { r.WorkspaceApiKeyId = 0 },
		"raw fingerprint":        func(r *api.ManagedSecretCommand) { r.Fingerprint = strings.TrimPrefix(r.Fingerprint, "sha256:") },
		"mismatched fingerprint": func(r *api.ManagedSecretCommand) { r.Fingerprint = managedSecretFingerprintOf("another-key") },
		"foreign delivery ref":   func(r *api.ManagedSecretCommand) { r.SecretDeliveryReference = "opl-gateway-someone-else" },
		"empty key value":        func(r *api.ManagedSecretCommand) { r.GatewayApiKey = "" },
		"missing idempotency":    func(r *api.ManagedSecretCommand) { r.Context.IdempotencyKey = "" },
	} {
		command := valid()
		mutate(command)
		if _, err := tenant.PutManagedSecret(t.Context(), command); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("%s err=%v want invalid argument", name, err)
		}
	}
	if dispatcher.reads != 0 {
		t.Fatalf("an invalid command reached the provider: reads=%d", dispatcher.reads)
	}
	if _, err := workspaceClient.PutManagedSecret(t.Context(), valid()); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("workspace caller err=%v want permission denied", err)
	}
	authority.deny = true
	if _, err := tenant.PutManagedSecret(t.Context(), valid()); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("denied authorization err=%v want permission denied", err)
	}
	if dispatcher.reads != 0 {
		t.Fatalf("a refused command reached the provider: reads=%d", dispatcher.reads)
	}
}

// TestPutManagedSecretMapsProviderOutcomes proves a pending provider result is
// retryable and a reused write identity with a different Secret is a definite
// conflict, while a successful write is never reported before provider
// confirmation.
func TestPutManagedSecretMapsProviderOutcomes(t *testing.T) {
	dispatcher := &managedSecretDispatcher{fail: true}
	tenant, _, _ := managedSecretSystem(t, dispatcher)
	const workspace, key = "workspace-managed-secret-3", "raw-managed-key-value-3"
	if _, err := tenant.PutManagedSecret(t.Context(), managedSecretCommand(workspace, key, "op-3:create_managed_key:put_managed_secret")); status.Code(err) != codes.Unavailable {
		t.Fatalf("pending err=%v want unavailable", err)
	}
	dispatcher.fail = false
	command := managedSecretCommand(workspace, key, "op-3:create_managed_key:put_managed_secret")
	if _, err := tenant.PutManagedSecret(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	conflict := managedSecretCommand(workspace, "a-different-raw-key", "op-3:create_managed_key:put_managed_secret")
	if _, err := tenant.PutManagedSecret(t.Context(), conflict); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflict err=%v want already exists", err)
	}
}
