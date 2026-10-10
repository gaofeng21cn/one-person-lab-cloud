package identity_test

import (
	"context"
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
	"opl-cloud/services/gateway-integration/identity"
)

// fabricSecretStub records the exact approved-store command the Gateway client
// sends and answers with a provider-confirmed readback.
type fabricSecretStub struct {
	api.UnimplementedFabricCoordinationServer
	calls    int
	last     *api.ManagedSecretCommand
	readback *api.ManagedSecretReadback
	err      error
}

func (s *fabricSecretStub) PutManagedSecret(_ context.Context, r *api.ManagedSecretCommand) (*api.ManagedSecretReadback, error) {
	s.calls++
	s.last = proto.Clone(r).(*api.ManagedSecretCommand)
	if s.err != nil {
		return nil, s.err
	}
	if s.readback != nil {
		return proto.Clone(s.readback).(*api.ManagedSecretReadback), nil
	}
	return &api.ManagedSecretReadback{SecretRef: r.SecretDeliveryReference, Version: "v1", Fingerprint: r.Fingerprint}, nil
}

func fabricSecretStore(t *testing.T) (*identity.FabricManagedSecretStore, *fabricSecretStub) {
	t.Helper()
	stub := &fabricSecretStub{}
	server := grpc.NewServer()
	api.RegisterFabricCoordinationServer(server, stub)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return identity.NewFabricManagedSecretStore(api.NewFabricCoordinationClient(conn)), stub
}

func managedSecretDelivery() identity.SecretDelivery {
	fingerprint := "sha256:" + strings.Repeat("ab", 32)
	return identity.SecretDelivery{
		TenantID: "tenant", WorkspaceID: "workspace-secret", Purpose: "workspace_managed", Fingerprint: fingerprint, Raw: "sk-raw-key",
		ExternalKeyID: "4242",
		Call: &api.CallContext{RequestId: "req-create", IdempotencyKey: "op-1:create_managed_key", ActorId: "actor", AuthorizationContextId: "caller-decision",
			AcceptedOperationGrantId: proto.String("grant-workspace"), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant"}}}},
	}
}

// TestFabricManagedSecretStoreSendsTheApprovedContinuation proves the Gateway
// writes the raw value through Fabric with its own derived continuation: the
// caller's authorization context is never forwarded, the original idempotency
// identity is preserved, the external key id becomes the provider key binding and
// the returned handle is opaque with the raw value cleared.
func TestFabricManagedSecretStoreSendsTheApprovedContinuation(t *testing.T) {
	store, stub := fabricSecretStore(t)
	stored, err := store.PutSecret(t.Context(), managedSecretDelivery())
	if err != nil {
		t.Fatal(err)
	}
	if stub.calls != 1 || stub.last == nil {
		t.Fatalf("fabric calls=%d", stub.calls)
	}
	command := stub.last
	if command.GetContext().GetAuthorizationContextId() != "" || command.GetContext().GetIdempotencyKey() != "op-1:create_managed_key" ||
		command.GetContext().GetActorId() != "actor" || command.GetContext().GetAcceptedOperationGrantId() != "grant-workspace" {
		t.Fatalf("continuation context=%+v", command.GetContext())
	}
	if command.GetWorkspaceId() != "workspace-secret" || command.GetWorkspaceApiKeyId() != 4242 ||
		command.GetFingerprint() != managedSecretDelivery().Fingerprint || command.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef("workspace-secret") ||
		command.GetGatewayApiKey() != "sk-raw-key" {
		t.Fatalf("command=%+v", command)
	}
	if stored.Reference != contracts.WorkspaceGatewaySecretRef("workspace-secret") || stored.Raw != "" || stored.Fingerprint != managedSecretDelivery().Fingerprint || stored.ExternalKeyID != "4242" {
		t.Fatalf("stored=%+v", stored)
	}
}

// TestFabricManagedSecretStoreFailsClosed proves a missing original identity, an
// unusable external key id or a diverging Fabric readback never becomes a
// confirmed delivery.
func TestFabricManagedSecretStoreFailsClosed(t *testing.T) {
	store, stub := fabricSecretStore(t)
	missingCall := managedSecretDelivery()
	missingCall.Call = nil
	if _, err := store.PutSecret(t.Context(), missingCall); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("missing call err=%v", err)
	}
	badKey := managedSecretDelivery()
	badKey.ExternalKeyID = "not-a-key-id"
	if _, err := store.PutSecret(t.Context(), badKey); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("bad key id err=%v", err)
	}
	if stub.calls != 0 {
		t.Fatalf("an invalid delivery reached Fabric: calls=%d", stub.calls)
	}
	stub.readback = &api.ManagedSecretReadback{SecretRef: contracts.WorkspaceGatewaySecretRef("workspace-secret"), Version: "v1", Fingerprint: "sha256:" + strings.Repeat("cd", 32)}
	if _, err := store.PutSecret(t.Context(), managedSecretDelivery()); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("diverging readback err=%v", err)
	}
	stub.readback = nil
	stub.err = status.Error(codes.Unavailable, "store pending")
	if _, err := store.PutSecret(t.Context(), managedSecretDelivery()); status.Code(err) != codes.Unavailable {
		t.Fatalf("store failure err=%v", err)
	}
}
