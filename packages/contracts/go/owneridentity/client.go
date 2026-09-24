package owneridentity

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// OutboundInterceptor presents the calling owner's identity and token on every
// outbound call. The target verifies the token against its own allowlist.
func OutboundInterceptor(owner Owner, token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, request, reply any, conn *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		outgoing := metadata.AppendToOutgoingContext(ctx, PeerHeader, owner.String())
		outgoing = metadata.AppendToOutgoingContext(outgoing, TokenHeader, token)
		return invoker(outgoing, method, request, reply, conn, opts...)
	}
}

// OutboundServiceInterceptor presents a non-domain process identity, such as
// the Console BFF, on every outbound call.
func OutboundServiceInterceptor(service ServiceIdentity, token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, request, reply any, conn *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		outgoing := metadata.AppendToOutgoingContext(ctx, PeerHeader, service.String())
		outgoing = metadata.AppendToOutgoingContext(outgoing, TokenHeader, token)
		return invoker(outgoing, method, request, reply, conn, opts...)
	}
}
