package catalog

import (
	"context"
	"database/sql"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

// emitPolicyChanged records catalog.policy_changed.v1 in the same transaction as
// the price policy version it announces. The event owner, schema version and
// consumer set are fixed by the shared contract, so the producer cannot skip a
// consumer; delivery to each consumer is acknowledged independently.
func (s *Service) emitPolicyChanged(ctx context.Context, tx *sql.Tx, call *api.CallContext, version *api.PricePolicyVersion) error {
	payload, err := protojson.Marshal(&api.CatalogPolicyChangedEvent{
		PolicyVersionId: version.Id,
		PolicyKind:      "price",
		ValidFrom:       version.ValidFrom,
	})
	if err != nil {
		return status.Error(codes.Internal, "encode catalog policy event")
	}
	tenantID := call.GetScope().GetTenant().GetTenantId()
	event := ownerstore.Event{
		ID:                id("evt"),
		EventType:         "catalog.policy_changed.v1",
		SchemaVersion:     1,
		AggregateType:     "price_policy_version",
		AggregateID:       version.Id,
		AggregateRevision: 1,
		TenantID:          tenantID,
		CorrelationID:     call.GetRequestId(),
		Payload:           payload,
		OccurredAt:        time.Now().UTC(),
	}
	if err := s.Store.AppendEvent(ctx, tx, event); err != nil {
		return status.Error(codes.Internal, "record catalog policy event")
	}
	return nil
}

// DeliverPolicyEvents retries each configured consumer of the policy-change
// event. A consumer that is not wired in this deployment is skipped, which
// leaves its delivery pending rather than dropping it.
func (s *Service) DeliverPolicyEvents(ctx context.Context) error {
	for consumer, client := range map[string]api.DomainInboxClient{"ledger": s.Ledger} {
		if client == nil {
			continue
		}
		pending, err := s.Store.PendingDeliveries(ctx, consumer, 20)
		if err != nil {
			return err
		}
		for _, delivery := range pending {
			event := delivery.Event
			if event.EventType != "catalog.policy_changed.v1" {
				continue
			}
			payload := &api.CatalogPolicyChangedEvent{}
			if err := protojson.Unmarshal(event.Payload, payload); err != nil {
				return err
			}
			scope := "platform"
			if event.TenantID != "" {
				scope = "tenant"
			}
			envelope := &api.EventEnvelope{
				EventId:          event.ID,
				EventType:        event.EventType,
				SchemaVersion:    event.SchemaVersion,
				Owner:            "resource_catalog",
				TenantId:         event.TenantID,
				Scope:            scope,
				AggregateId:      event.AggregateID,
				AggregateVersion: event.AggregateRevision,
				RequestId:        event.CorrelationID,
				OccurredAt:       timestamppb.New(event.OccurredAt),
				Payload:          &api.EventEnvelope_CatalogPolicyChanged{CatalogPolicyChanged: payload},
			}
			callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			ack, err := client.Deliver(callCtx, &api.DeliverEventRequest{AuthenticatedProducer: "resource_catalog", Event: envelope})
			cancel()
			if err != nil || ack.GetEventId() != event.ID || ack.GetConsumer() != consumer || !ack.GetCommitted() {
				if err := s.Store.RecordDeliveryFailure(ctx, delivery.DeliveryID, "consumer_unavailable", time.Now().Add(5*time.Second)); err != nil {
					return err
				}
				continue
			}
			if err := s.Store.AcknowledgeDelivery(ctx, consumer, event.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// RunPolicyDelivery polls the owner's own Outbox until the context is cancelled.
func (s *Service) RunPolicyDelivery(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		_ = s.DeliverPolicyEvents(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
