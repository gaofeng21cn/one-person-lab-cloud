package ownerservice

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"

	"opl-cloud/packages/contracts/go/owneridentity"
)

// RegisterFunc registers one owner service group on the gRPC server. Registration
// happens after the identity interceptor is installed, so every group is behind
// the same allowlist.
type RegisterFunc func(*grpc.Server)

// Server is one owner process's gRPC server.
type Server struct {
	config   Config
	server   *grpc.Server
	health   *health.Server
	listener net.Listener
}

// NewServer builds the owner's gRPC server with the shared identity interceptor
// and one health service. Callers register their own service groups afterwards.
func NewServer(config Config, options ...grpc.ServerOption) (*Server, error) {
	if !config.Owner.Valid() {
		return nil, fmt.Errorf("%q is not a Cloud owner", config.Owner)
	}
	options = append(options, grpc.ChainUnaryInterceptor(identityInterceptor(config)))
	server := grpc.NewServer(options...)
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(server, healthServer)
	// An owner is only serving once it has registered every group it owns; the
	// initial state is NOT_SERVING so a probe never sees a half-registered owner.
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	return &Server{config: config, server: server, health: healthServer}, nil
}

// Register installs one owner service group.
func (s *Server) Register(register RegisterFunc) {
	register(s.server)
}

// MarkServing flips the health status once every owner group is registered and the
// backing store is reachable.
func (s *Server) MarkServing() {
	s.health.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
}

// MarkNotServing reports a dependency that is not ready.
func (s *Server) MarkNotServing(reason string) {
	if reason != "" {
		log.Printf("%s not serving: %s", s.config.Owner, reason)
	}
	s.health.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
}

// Serve blocks on the owner's listener.
func (s *Server) Serve() error {
	listener, err := Listen(s.config.Addr)
	if err != nil {
		return err
	}
	s.listener = listener
	log.Printf("%s listening on %s", s.config.Owner, s.config.Addr)
	return s.server.Serve(listener)
}

// ServeOn blocks on a caller-supplied listener, which tests use to take an
// ephemeral port without a fixed address.
func (s *Server) ServeOn(listener net.Listener) error {
	s.listener = listener
	return s.server.Serve(listener)
}

// Stop gracefully stops the server.
func (s *Server) Stop() { s.server.GracefulStop() }

// identityInterceptor refuses a call whose peer identity is not in the owner's
// allowlist, then records the verified peer on the request context.
func identityInterceptor(config Config) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// Health probes are served by the gRPC health service and carry no owner
		// identity; they are read-only and expose no domain state.
		if info != nil && info.FullMethod == "/grpc.health.v1.Health/Check" {
			return handler(ctx, request)
		}
		peer, err := Authenticate(ctx, config)
		if err != nil {
			return nil, err
		}
		return handler(WithPeerOwner(ctx, peer), request)
	}
}

// OutboundIdentityInterceptor presents the calling owner's identity and token on
// every outbound call. The token is the caller's own identity token; the target
// verifies it against its allowlist. It delegates to the shared wire convention so
// every in-repo caller presents the identical headers.
func OutboundIdentityInterceptor(owner Owner, token string) grpc.UnaryClientInterceptor {
	return owneridentity.OutboundInterceptor(owner, token)
}

// DialOptions returns the client-side credentials for calling one owner from
// another.
func DialOptions(owner Owner, token string) []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithUnaryInterceptor(OutboundIdentityInterceptor(owner, token)),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 30 * time.Second, Timeout: 10 * time.Second}),
	}
}
