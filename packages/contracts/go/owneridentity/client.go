package owneridentity

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// OutboundInterceptor presents the calling owner's identity and token on every
// outbound call. The target verifies the token against its own allowlist.
func OutboundInterceptor(owner interface{ String() string }, token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, request, reply any, conn *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		md, _ := metadata.FromOutgoingContext(ctx)
		md = md.Copy()
		md.Set(PeerHeader, owner.String())
		md.Set(TokenHeader, token)
		outgoing := metadata.NewOutgoingContext(ctx, md)
		return invoker(outgoing, method, request, reply, conn, opts...)
	}
}
