package clients

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"net"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"testing"
)

func TestServeUpdateTransportPreservesTypedSourceAndAuthority(t *testing.T) {
	call := &api.CallContext{ActorId: "actor-original", SessionId: proto.String("session-original"), IdempotencyKey: "upgrade-original", AuthorizationContextId: "serve-update-authority", Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-original"}}}}
	body := &api.UpdateWorkspaceVersionRequest{ExpectedCurrentAgentDeploymentId: "dep-original", ApplicationSelection: &api.WorkspaceApplicationSelection{Kind: api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP, RuntimeVersionId: proto.String("runtime-next")}}
	called := false
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		called = true
		input, ok := req.(*api.UpdateWorkspaceVersionRpcRequest)
		if !ok || info.FullMethod != api.ServeProductService_UpdateWorkspaceVersion_FullMethodName || input.GetWorkspaceId() != "ws_1042" || !proto.Equal(input.GetContext(), call) || !proto.Equal(input.GetBody(), body) {
			t.Errorf("forwarded %s %v", info.FullMethod, req)
		}
		md, _ := metadata.FromIncomingContext(ctx)
		if values := md.Get(owneridentity.PeerHeader); len(values) != 1 || values[0] != owneridentity.ConsoleBFF.String() {
			t.Errorf("peer=%v", values)
		}
		return &api.Operation{OperationId: "op-original"}, nil
	}))
	api.RegisterServeProductServiceServer(server, &api.UnimplementedServeProductServiceServer{})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	client, err := Dial(Config{Addresses: map[owneridentity.Owner]string{owneridentity.Serve: listener.Addr().String()}, Tokens: map[owneridentity.Owner]string{owneridentity.Serve: "0123456789abcdef0123456789abcdef"}, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	result, err := client.UpdateWorkspaceVersion(WithCallContext(context.Background(), call), "ws_1042", body)
	if err != nil || !called || result.GetOperationId() != "op-original" {
		t.Fatalf("result=%v err=%v called=%v", result, err, called)
	}
}
