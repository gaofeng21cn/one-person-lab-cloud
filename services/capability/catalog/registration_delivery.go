package catalog

import (
	"context"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"time"
)

// DeliverBuildResults retries the original registration events. Ledger deliveries
// remain independently durable; a Build acknowledgement never acknowledges Ledger.
func (s *Service) DeliverBuildResults(ctx context.Context) error {
	if s.BuildInbox == nil {
		return nil
	}
	pending, err := s.Store.PendingDeliveries(ctx, "build", 20)
	if err != nil {
		return err
	}
	for _, delivery := range pending {
		e := delivery.Event
		payload := &api.CapabilityVersionRegisteredEvent{}
		if e.EventType != "capability.version_registered.v1" {
			continue
		}
		if err = protojson.Unmarshal(e.Payload, payload); err != nil {
			return err
		}
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		ack, err := s.BuildInbox.Deliver(callCtx, &api.DeliverEventRequest{AuthenticatedProducer: "capability", Event: &api.EventEnvelope{EventId: e.ID, EventType: e.EventType, SchemaVersion: e.SchemaVersion, Owner: "capability", TenantId: e.TenantID, Scope: "tenant", AggregateId: e.AggregateID, AggregateVersion: e.AggregateRevision, RequestId: e.CorrelationID, OccurredAt: timestamppb.New(e.OccurredAt), Payload: &api.EventEnvelope_CapabilityVersionRegistered{CapabilityVersionRegistered: payload}}})
		cancel()
		if err != nil || ack.GetEventId() != e.ID || ack.GetConsumer() != "build" || !ack.GetCommitted() {
			if err = s.Store.RecordDeliveryFailure(ctx, delivery.DeliveryID, "build_unavailable", time.Now().Add(5*time.Second)); err != nil {
				return err
			}
			continue
		}
		if err = s.Store.AcknowledgeDelivery(ctx, "build", e.ID); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) RunRegistrationDelivery(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		_ = s.DeliverBuildResults(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
