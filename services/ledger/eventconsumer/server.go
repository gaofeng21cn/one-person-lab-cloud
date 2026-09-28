// Package eventconsumer adapts authenticated typed domain events to Ledger's
// existing append-only receipt store. It owns no producer state or workflow.
package eventconsumer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
	"opl-cloud/services/ledger/internal/ledger"
)

type Server struct {
	api.UnimplementedDomainInboxServer
	api.UnimplementedLedgerCoordinationServer
	store          *ledger.PostgresStore
	Authorizer     *ownerservice.Authorizer
	Catalog        api.CatalogCoordinationClient
	Workspace      api.OwnerCommitReadbackClient
	WorkspaceInbox api.DomainInboxClient
	inbox          *ownerstore.Store
}

func New(db *sql.DB) (*Server, error) {
	if db == nil {
		return nil, errors.New("Ledger PostgreSQL store required")
	}
	store := ledger.NewPostgresStore(db)
	inbox, err := ownerstore.New(db, "ledger")
	if err != nil {
		return nil, err
	}
	return &Server{store: store, inbox: inbox}, nil
}
func (s *Server) Install(ctx context.Context) error { return s.store.Install(ctx) }

func (s *Server) Deliver(ctx context.Context, r *api.DeliverEventRequest) (*api.InboxAck, error) {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || (peer != owneridentity.Build.Service() && peer != owneridentity.Capability.Service() && peer != owneridentity.Serve.Service()) {
		return nil, status.Error(codes.Unauthenticated, "verified Build, Capability or Serve peer required")
	}
	event := r.GetEvent()
	if event == nil || r.GetAuthenticatedProducer() != string(peer) || event.GetOwner() != string(peer) {
		return nil, status.Error(codes.PermissionDenied, "event producer differs from authenticated peer")
	}
	payload, err := eventPayload(event)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "unsupported domain evidence payload")
	}
	aggregateType, err := aggregateType(event)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Ledger evidence transaction unavailable")
	}
	defer tx.Rollback()
	inboxResult, err := s.inbox.DeliverInbox(ctx, tx, ownerstore.InboundEvent{
		ID: "in_ledger_" + string(peer) + "_" + event.EventId, SourceOwner: string(peer), SourceEventID: event.EventId,
		EventType: event.EventType, SchemaVersion: event.SchemaVersion, AggregateType: aggregateType,
		AggregateID: event.AggregateId, AggregateRevision: event.AggregateVersion, Payload: payload,
	}, time.Now().UTC())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid domain evidence identity")
	}
	if inboxResult.Decision == ownerstore.InboxConflict {
		return nil, status.Error(codes.AlreadyExists, "event identity conflicts with persisted Inbox evidence")
	}
	if inboxResult.Decision == ownerstore.InboxDuplicate {
		if err = tx.Commit(); err != nil {
			return nil, status.Error(codes.Unavailable, "Ledger Inbox acknowledgement unavailable")
		}
		return &api.InboxAck{EventId: event.EventId, Consumer: "ledger", Committed: true, Duplicate: true, AppliedAggregateVersion: event.AggregateVersion}, nil
	}
	receipt, err := s.store.RecordDomainEventTx(ctx, tx, event)
	if errors.Is(err, ledger.ErrInvalidReceiptInput) {
		return nil, status.Error(codes.InvalidArgument, "invalid domain evidence")
	}
	if errors.Is(err, ledger.ErrIdempotencyConflict) {
		return nil, status.Error(codes.AlreadyExists, "event identity conflicts with persisted evidence")
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Ledger evidence persistence unavailable")
	}
	if err = s.inbox.MarkInboxProcessed(ctx, tx, string(peer), event.EventId, receipt.ReceiptID, "", time.Now().UTC()); err != nil {
		return nil, status.Error(codes.Unavailable, "Ledger Inbox processing state unavailable")
	}
	if err = tx.Commit(); err != nil {
		return nil, status.Error(codes.Unavailable, "Ledger evidence commit unavailable")
	}
	return &api.InboxAck{EventId: event.EventId, Consumer: "ledger", Committed: true, Duplicate: receipt.Replayed, AppliedAggregateVersion: event.AggregateVersion}, nil
}

func aggregateType(event *api.EventEnvelope) (string, error) {
	switch event.EventType {
	case "package.uploaded.v1":
		return "package_version", nil
	case "build.artifact_confirmed.v1", "build.failed.v1":
		return "build", nil
	case "capability.version_registered.v1":
		return "capability_version", nil
	case "serve.agent_readiness_observed.v1":
		return "runtime_instance", nil
	default:
		return "", fmt.Errorf("unsupported domain event %s", event.EventType)
	}
}

func eventPayload(event *api.EventEnvelope) ([]byte, error) {
	var payload any
	switch event.EventType {
	case "package.uploaded.v1":
		payload = event.GetPackageUploaded()
	case "build.artifact_confirmed.v1":
		payload = event.GetBuildArtifactConfirmed()
	case "build.failed.v1":
		payload = event.GetBuildFailed()
	case "capability.version_registered.v1":
		payload = event.GetCapabilityVersionRegistered()
	case "serve.agent_readiness_observed.v1":
		payload = event.GetRuntimeReadinessObserved()
	default:
		return nil, fmt.Errorf("unsupported domain event %s", event.EventType)
	}
	if payload == nil {
		return nil, errors.New("domain event payload is required")
	}
	message, ok := payload.(interface{ ProtoReflect() protoreflect.Message })
	if !ok {
		return nil, errors.New("domain event payload is not a protobuf message")
	}
	return protojson.Marshal(message)
}

// DeliverReceiptEvents retries Ledger's own immutable receipt-recorded Outbox
// events to Workspace. Workspace ACK is independent from producer ACK; a lost
// response leaves the same receipt event pending for replay.
func (s *Server) DeliverReceiptEvents(ctx context.Context) error {
	if s.WorkspaceInbox == nil || s.inbox == nil {
		return nil
	}
	pending, err := s.inbox.PendingDeliveries(ctx, "workspace", 20)
	if err != nil {
		return err
	}
	for _, delivery := range pending {
		e := delivery.Event
		if e.EventType != "ledger.receipt_recorded.v1" {
			continue
		}
		payload := &api.ReceiptRecordedEvent{}
		if err := protojson.Unmarshal(e.Payload, payload); err != nil {
			return err
		}
		envelope := &api.EventEnvelope{EventId: e.ID, EventType: e.EventType, SchemaVersion: e.SchemaVersion, Owner: "ledger", TenantId: e.TenantID, AggregateId: e.AggregateID, AggregateVersion: e.AggregateRevision, OccurredAt: timestamppb.New(e.OccurredAt), RequestId: e.CorrelationID, Scope: "tenant", Payload: &api.EventEnvelope_ReceiptRecorded{ReceiptRecorded: payload}}
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		ack, callErr := s.WorkspaceInbox.Deliver(callCtx, &api.DeliverEventRequest{Event: envelope, AuthenticatedProducer: "ledger"})
		cancel()
		if callErr != nil || ack.GetEventId() != e.ID || ack.GetConsumer() != "workspace" || !ack.GetCommitted() {
			if err := s.inbox.RecordDeliveryFailure(ctx, delivery.DeliveryID, "consumer_unavailable", time.Now().UTC().Add(5*time.Second)); err != nil {
				return err
			}
			continue
		}
		if err := s.inbox.AcknowledgeDelivery(ctx, "workspace", e.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) RunReceiptDelivery(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		_ = s.DeliverReceiptEvents(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
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
