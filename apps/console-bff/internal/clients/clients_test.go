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

func TestEveryBFFOwnerReadPreservesUserContextAndBFFIdentity(t *testing.T) {
	session := "session-a"
	call := &api.CallContext{ActorId: "actor-a", SessionId: &session, RequestId: "request-a", Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-a"}}}}
	methods := map[string]bool{}
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, request any, info *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		if values := md.Get(owneridentity.PeerHeader); len(values) != 1 || values[0] != owneridentity.ConsoleBFF.String() {
			t.Errorf("caller identity=%v, want console_bff", values)
		}
		typed, ok := request.(interface{ GetContext() *api.CallContext })
		if !ok || !proto.Equal(typed.GetContext(), call) {
			t.Errorf("%s lost CallContext: %+v", info.FullMethod, request)
		}
		methods[info.FullMethod] = true
		switch info.FullMethod {
		case api.WorkspaceProductService_GetWorkspace_FullMethodName:
			return &api.Workspace{}, nil
		case api.ServeProductService_ListDeployments_FullMethodName:
			return &api.DeploymentPage{}, nil
		case api.ServeProductService_GetWorkspaceAccess_FullMethodName:
			return &api.WorkspaceAccess{}, nil
		case api.BuildProductService_GetBuild_FullMethodName:
			return &api.BuildJob{}, nil
		case api.CapabilityProductService_GetCapabilityVersion_FullMethodName:
			return &api.CapabilityVersion{}, nil
		default:
			return &api.Operation{}, nil
		}
	}))
	api.RegisterWorkspaceProductServiceServer(server, &api.UnimplementedWorkspaceProductServiceServer{})
	api.RegisterServeProductServiceServer(server, &api.UnimplementedServeProductServiceServer{})
	api.RegisterBuildProductServiceServer(server, &api.UnimplementedBuildProductServiceServer{})
	api.RegisterCapabilityProductServiceServer(server, &api.UnimplementedCapabilityProductServiceServer{})
	api.RegisterOwnerOperationsServer(server, &api.UnimplementedOwnerOperationsServer{})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	config := Config{Addresses: map[owneridentity.Owner]string{}, Tokens: map[owneridentity.Owner]string{}, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}}
	for _, owner := range ReachableOwners() {
		config.Addresses[owner] = listener.Addr().String()
		config.Tokens[owner] = "0123456789abcdef0123456789abcdef"
	}
	client, err := Dial(config)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx := WithCallContext(context.Background(), call)
	if _, err := client.Workspace(ctx, "workspace-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Deployments(ctx, "workspace-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.WorkspaceAccess(ctx, "workspace-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Build(ctx, "build-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CapabilityVersion(ctx, "version-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Operation(ctx, owneridentity.Build, "operation-a"); err != nil {
		t.Fatal(err)
	}
	if len(methods) != 6 {
		t.Fatalf("exercised %d methods", len(methods))
	}
}
