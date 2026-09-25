// Package eventconsumer adapts authenticated typed domain events to Ledger's
// existing append-only receipt store. It owns no producer state or workflow.
package eventconsumer

import (
	"context"
	"database/sql"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/ledger/internal/ledger"
)

type Server struct {
	api.UnimplementedDomainInboxServer
	api.UnimplementedLedgerCoordinationServer
	store      *ledger.PostgresStore
	Authorizer *ownerservice.Authorizer
	Catalog    api.CatalogCoordinationClient
	Workspace  api.OwnerCommitReadbackClient
}

func New(db *sql.DB) (*Server, error) {
	if db == nil {
		return nil, errors.New("Ledger PostgreSQL store required")
	}
	return &Server{store: ledger.NewPostgresStore(db)}, nil
}
func (s *Server) Install(ctx context.Context) error { return s.store.Install(ctx) }

func (s *Server) Deliver(ctx context.Context, r *api.DeliverEventRequest) (*api.InboxAck, error) {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || (peer != owneridentity.Build.Service() && peer != owneridentity.Capability.Service()) {
		return nil, status.Error(codes.Unauthenticated, "verified Build or Capability peer required")
	}
	event := r.GetEvent()
	if r.GetAuthenticatedProducer() != string(peer) || event.GetOwner() != string(peer) {
		return nil, status.Error(codes.PermissionDenied, "event producer differs from authenticated peer")
	}
	receipt, err := s.store.RecordDomainEvent(ctx, event)
	if errors.Is(err, ledger.ErrInvalidReceiptInput) {
		return nil, status.Error(codes.InvalidArgument, "invalid domain evidence")
	}
	if errors.Is(err, ledger.ErrIdempotencyConflict) {
		return nil, status.Error(codes.AlreadyExists, "event identity conflicts with persisted evidence")
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Ledger evidence persistence unavailable")
	}
	return &api.InboxAck{EventId: event.EventId, Consumer: "ledger", Committed: true, Duplicate: receipt.Replayed, AppliedAggregateVersion: event.AggregateVersion}, nil
}

// NewGRPC uses the same certificate identity and configured peer-token validation
// as the other owner processes. Production cannot enable insecure-local mode.
func (s *Server) NewGRPC(config ownerservice.Config) (*grpc.Server, error) {
	if config.Owner != owneridentity.Ledger {
		return nil, errors.New("Ledger service identity required")
	}
	creds, err := config.TLS.ServerCredentials(owneridentity.Ledger.Service())
	if err != nil {
		return nil, err
	}
	options := []grpc.ServerOption{grpc.UnaryInterceptor(func(ctx context.Context, request any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		peer, err := ownerservice.Authenticate(ctx, config)
		if err != nil {
			return nil, err
		}
		return next(ownerservice.WithPeerOwner(ctx, peer), request)
	})}
	if creds != nil {
		options = append(options, grpc.Creds(creds))
	}
	server := grpc.NewServer(options...)
	api.RegisterDomainInboxServer(server, s)
	api.RegisterLedgerCoordinationServer(server, s)
	return server, nil
}
