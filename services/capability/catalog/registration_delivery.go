package catalog

import (
	"context"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"time"
)

// DeliverRegistrations retries each consumer of the original registration event.
// A Build acknowledgement never acknowledges Ledger, or vice versa.
func (s *Service) DeliverRegistrations(ctx context.Context) error {
	for consumer, client := range map[string]api.DomainInboxClient{"build": s.BuildInbox, "ledger": s.LedgerInbox} {
		if client == nil {
			continue
		}
		pending, err := s.Store.PendingDeliveries(ctx, consumer, 20)
		if err != nil {
			return err
		}
		for _, delivery := range pending {
			e := delivery.Event
			envelope := &api.EventEnvelope{EventId: e.ID, EventType: e.EventType, SchemaVersion: e.SchemaVersion, Owner: "capability", TenantId: e.TenantID, Scope: "tenant", AggregateId: e.AggregateID, AggregateVersion: e.AggregateRevision, RequestId: e.CorrelationID, OccurredAt: timestamppb.New(e.OccurredAt)}
			switch e.EventType {
			case "capability.version_registered.v1":
				payload := &api.CapabilityVersionRegisteredEvent{}
				if err := protojson.Unmarshal(e.Payload, payload); err != nil {
					return err
				}
				envelope.Payload = &api.EventEnvelope_CapabilityVersionRegistered{CapabilityVersionRegistered: payload}
			case "package.uploaded.v1":
				payload := &api.PackageUploadedEvent{}
				if err := protojson.Unmarshal(e.Payload, payload); err != nil {
					return err
				}
				envelope.Payload = &api.EventEnvelope_PackageUploaded{PackageUploaded: payload}
			default:
				return fmt.Errorf("unsupported Capability event %s", e.EventType)
			}
			callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			ack, err := client.Deliver(callCtx, &api.DeliverEventRequest{AuthenticatedProducer: "capability", Event: envelope})
			cancel()
			if err != nil || ack.GetEventId() != e.ID || ack.GetConsumer() != consumer || !ack.GetCommitted() {
				if err = s.Store.RecordDeliveryFailure(ctx, delivery.DeliveryID, "consumer_unavailable", time.Now().Add(5*time.Second)); err != nil {
					return err
				}
				continue
			}
			if err = s.Store.AcknowledgeDelivery(ctx, consumer, e.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Service) RunRegistrationDelivery(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		_ = s.DeliverRegistrations(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
