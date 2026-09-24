package ownerservice

import (
	"context"
	"fmt"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
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

// ReadyCheck reports whether one dependency of this owner is usable. A dependency
// that cannot be reached is not ready; it is never reported as healthy.
type ReadyCheck func(context.Context) error

// Server is one owner process's gRPC server.
type Server struct {
	config   Config
	server   *grpc.Server
	health   *health.Server
	listener net.Listener
	products map[string]bool
	required []string
	checks   map[string]ReadyCheck
	mu       sync.Mutex
	serving  bool
	closers  []interface{ Close() error }
}

// TrackCloser registers a dependency opened while wiring product handlers. The
// owner bootstrap closes tracked connections together with its database.
func (s *Server) TrackCloser(c interface{ Close() error }) error {
	if c == nil {
		return fmt.Errorf("%s: closer is required", s.config.Owner)
	}
	s.closers = append(s.closers, c)
	return nil
}

func (s *Server) closeTracked() error {
	var first error
	for i := len(s.closers) - 1; i >= 0; i-- {
		if err := s.closers[i].Close(); err != nil && first == nil {
			first = err
		}
	}
	s.closers = nil
	return first
}

// ErrNotReady reports that this owner cannot truthfully claim SERVING. A process in
// this state keeps the health service NOT_SERVING so a probe never sees an owner
// that registered no product service or cannot reach its own dependencies.
type ErrNotReady struct{ Reason string }

// Error implements error.
func (e ErrNotReady) Error() string { return e.Reason }

// NewServer builds the owner's gRPC server with the shared identity interceptor
// and one health service. Callers register their own service groups afterwards.
func NewServer(config Config, options ...grpc.ServerOption) (*Server, error) {
	if !config.Owner.Valid() {
		return nil, fmt.Errorf("%q is not a Cloud owner", config.Owner)
	}
	creds, err := config.TLS.ServerCredentials(config.Owner.Service())
	if err != nil {
		return nil, err
	}
	if creds != nil {
		options = append(options, grpc.Creds(creds))
	}
	options = append(options, grpc.ChainUnaryInterceptor(identityInterceptor(config)))
	server := grpc.NewServer(options...)
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(server, healthServer)
	// An owner is only serving once it has registered every group it owns and its
	// dependencies answer; the initial state is NOT_SERVING so a probe never sees a
	// half-registered or dependency-less owner.
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	return &Server{
		config:   config,
		server:   server,
		health:   healthServer,
		products: map[string]bool{},
		checks:   map[string]ReadyCheck{},
	}, nil
}

// Register installs one service group without naming it. It counts as infrastructure
// the owner serves, not as the owner's product surface; an owner with only
// unnamed groups and no declared product service stays NOT_SERVING.
func (s *Server) Register(register RegisterFunc) error {
	return s.RegisterGroup("", register)
}

// RegisterGroup installs one named service group and records it as the owner's
// product surface. Registering after MarkServing is refused: an owner may not
// widen its product API while it already reports SERVING.
func (s *Server) RegisterGroup(name string, register RegisterFunc) error {
	if register == nil {
		return fmt.Errorf("%s: register function is required", s.config.Owner)
	}
	if s.healthServing() {
		return fmt.Errorf("%s: cannot register a service group after reporting SERVING", s.config.Owner)
	}
	register(s.server)
	if strings.TrimSpace(name) != "" {
		s.products[strings.TrimSpace(name)] = true
	}
	return nil
}

// RequireProductGroups declares the contract service groups this owner is
// configured to serve. Readiness fails until every declared group is registered,
// so a process that has not wired its domain handlers reports NOT_SERVING with the
// exact missing group instead of claiming health from whichever group happens to
// be registered.
func (s *Server) RequireProductGroups(names ...string) error {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("%s: required product group name is required", s.config.Owner)
		}
		s.required = append(s.required, name)
	}
	return nil
}

// ProductGroups lists the named service groups this owner registered, so startup
// and readiness logs name the surface the process actually exposes.
func (s *Server) ProductGroups() []string {
	names := make([]string, 0, len(s.products))
	for name := range s.products {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// AddReadinessCheck records one named dependency readiness check. MarkServing and
// Ready run every recorded check, so an owner with an unreachable database stays
// NOT_SERVING instead of claiming readiness from registration alone.
func (s *Server) AddReadinessCheck(name string, check ReadyCheck) error {
	name = strings.TrimSpace(name)
	if name == "" || check == nil {
		return fmt.Errorf("%s: readiness check name and function are required", s.config.Owner)
	}
	s.checks[name] = check
	return nil
}

// Ready reports why this owner is not ready, or nil when it is. It requires at
// least one registered product service group, because a process that exposes only
// health is not a serving domain owner.
func (s *Server) Ready(ctx context.Context) error {
	missing := make([]string, 0, len(s.required))
	for _, name := range s.required {
		if !s.products[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return ErrNotReady{Reason: fmt.Sprintf("%s: product service group not registered: %s", s.config.Owner, strings.Join(missing, ", "))}
	}
	if len(s.products) == 0 {
		return ErrNotReady{Reason: fmt.Sprintf("%s: no product service group is registered", s.config.Owner)}
	}
	names := make([]string, 0, len(s.checks))
	for name := range s.checks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := s.checks[name](ctx); err != nil {
			return ErrNotReady{Reason: fmt.Sprintf("%s: dependency %s is not ready: %v", s.config.Owner, name, err)}
		}
	}
	return nil
}

// MarkServing flips the health status to SERVING only when every registered group
// and dependency is ready. It returns the reason instead of reporting SERVING when
// readiness cannot be proven.
func (s *Server) MarkServing(ctx context.Context) error {
	if err := s.Ready(ctx); err != nil {
		s.setServing(false)
		s.health.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
		log.Printf("%s not serving: %v", s.config.Owner, err)
		return err
	}
	s.setServing(true)
	s.health.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	return nil
}

// MarkNotServing reports a dependency that is not ready.
func (s *Server) MarkNotServing(reason string) {
	if reason != "" {
		log.Printf("%s not serving: %s", s.config.Owner, reason)
	}
	s.setServing(false)
	s.health.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
}

// healthServing reports whether this process already told the health service it
// is SERVING. The gRPC health server exposes no read accessor, so the transition
// is recorded here; the health service remains the single reported status.
func (s *Server) healthServing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serving
}

func (s *Server) setServing(serving bool) {
	s.mu.Lock()
	s.serving = serving
	s.mu.Unlock()
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
	return owneridentity.OutboundInterceptor(owner.Service(), token)
}

// DialOptions returns the client-side credentials for calling one owner from
// another.
func DialOptions(owner Owner, token string) []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithUnaryInterceptor(OutboundIdentityInterceptor(owner, token)),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 30 * time.Second, Timeout: 10 * time.Second}),
	}
}
