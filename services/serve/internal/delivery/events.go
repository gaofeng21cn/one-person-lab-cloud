package delivery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

// appendRuntimeReadinessEvent records the exact runtime observation in Serve's
// Outbox transaction. It is an observation fact, not a customer-success or
// Ledger receipt fact.
func appendRuntimeReadinessEvent(ctx context.Context, tx *sql.Tx, store *ownerstore.Store, command *api.RuntimeDeployCommand, observation RuntimeObservation) error {
	if command == nil || store == nil {
		return errors.New("Serve event store and deployment command are required")
	}
	outcome := "unknown"
	available := false
	if observation.State == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY {
		outcome, available = "confirmed", true
	} else if observation.State == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED {
		outcome = "rejected"
	}
	payload, err := protojson.Marshal(&api.RuntimeReadinessObservedEvent{
		RuntimeInstanceId:                command.RuntimeInstanceId,
		WorkspaceId:                      command.WorkspaceId,
		DeploymentId:                     command.DeploymentId,
		Outcome:                          outcome,
		ApplicationAvailable:             available,
		CredentialInjectionVerified:      command.RuntimeConfiguration != nil,
		AppliedModelConfigurationVersion: command.ModelConfigurationVersion,
		ReceiptId:                        optionalString(observation.ReadinessEvidenceRef),
	})
	if err != nil {
		return fmt.Errorf("marshal Serve readiness event: %w", err)
	}
	baseRevision := command.ExecutionEpoch*10 + 1
	if observation.State == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY {
		baseRevision = command.ExecutionEpoch*10 + 3
	} else if observation.State == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED {
		baseRevision = command.ExecutionEpoch*10 + 4
	}
	if baseRevision < 1 {
		baseRevision = 1
	}
	payloadHash := ownerstore.Event{Payload: payload}.PayloadSHA256()
	// A runtime can be observed more than once in one execution epoch. The
	// state-specific revision is the first revision for that fact, not a unique
	// revision forever: a later observation with a different evidence reference
	// must advance monotonically instead of colliding with the immutable Outbox
	// uniqueness fence. Replays of the same payload remain idempotent.
	var recordedRevision int64
	err = tx.QueryRowContext(ctx, `SELECT aggregate_revision FROM serve.outbox_events
		WHERE aggregate_type=$1 AND aggregate_id=$2 AND event_type=$3 AND payload_sha256=$4
		ORDER BY aggregate_revision DESC LIMIT 1`, "runtime_instance", command.RuntimeInstanceId, "serve.agent_readiness_observed.v1", payloadHash).Scan(&recordedRevision)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	revision := baseRevision
	var existingHash string
	err = tx.QueryRowContext(ctx, `SELECT payload_sha256 FROM serve.outbox_events
		WHERE aggregate_type=$1 AND aggregate_id=$2 AND aggregate_revision=$3 AND event_type=$4`,
		"runtime_instance", command.RuntimeInstanceId, revision, "serve.agent_readiness_observed.v1").Scan(&existingHash)
	if err == nil {
		if existingHash == payloadHash {
			return nil
		}
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(aggregate_revision),0)+1 FROM serve.outbox_events
			WHERE aggregate_type=$1 AND aggregate_id=$2`, "runtime_instance", command.RuntimeInstanceId).Scan(&revision); err != nil {
			return err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	event := ownerstore.Event{
		ID:                "evt_" + stableID("readiness_", command.DeploymentId, fmt.Sprint(revision), outcome, observation.ReadinessEvidenceRef),
		EventType:         "serve.agent_readiness_observed.v1",
		SchemaVersion:     1,
		AggregateType:     "runtime_instance",
		AggregateID:       command.RuntimeInstanceId,
		AggregateRevision: revision,
		TenantID:          command.Context.GetScope().GetTenant().GetTenantId(),
		CorrelationID:     command.Context.GetRequestId(),
		Payload:           payload,
		OccurredAt:        observation.ObservedAt.UTC(),
	}
	var recordedHash string
	var recordedPayload []byte
	err = tx.QueryRowContext(ctx, "SELECT payload_sha256, payload FROM serve.outbox_events WHERE id=$1", event.ID).Scan(&recordedHash, &recordedPayload)
	if err == nil {
		if recordedHash != event.PayloadSHA256() {
			return errors.New("Serve readiness event identity conflicts with persisted Outbox event")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return store.AppendEvent(ctx, tx, event)
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// DeliverEvents retries Serve Outbox deliveries independently for every
// contract subscriber. Each ACK is recorded only after that consumer commits;
// a lost response leaves the same event pending for replay.
func (s *Service) DeliverEvents(ctx context.Context) error {
	for _, target := range []struct {
		name   string
		client api.DomainInboxClient
	}{
		{name: "ledger", client: s.Ledger},
		{name: "workspace", client: s.Workspace},
	} {
		if target.client == nil {
			continue
		}
		pending, err := s.Store.PendingDeliveries(ctx, target.name, 20)
		if err != nil {
			return err
		}
		for _, delivery := range pending {
			e := delivery.Event
			if e.EventType != "serve.agent_readiness_observed.v1" {
				continue
			}
			payload := &api.RuntimeReadinessObservedEvent{}
			if err := protojson.Unmarshal(e.Payload, payload); err != nil {
				return err
			}
			scope := "platform"
			if e.TenantID != "" {
				scope = "tenant"
			}
			envelope := &api.EventEnvelope{
				EventId: e.ID, EventType: e.EventType, SchemaVersion: e.SchemaVersion,
				Owner: "serve", TenantId: e.TenantID, AggregateId: e.AggregateID,
				AggregateVersion: e.AggregateRevision, OccurredAt: timestamppb.New(e.OccurredAt),
				RequestId: e.CorrelationID, Scope: scope,
				Payload: &api.EventEnvelope_RuntimeReadinessObserved{RuntimeReadinessObserved: payload},
			}
			callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			ack, callErr := target.client.Deliver(callCtx, &api.DeliverEventRequest{Event: envelope, AuthenticatedProducer: "serve"})
			cancel()
			if callErr != nil || ack.GetEventId() != e.ID || ack.GetConsumer() != target.name || !ack.GetCommitted() {
				if err := s.Store.RecordDeliveryFailure(ctx, delivery.DeliveryID, "consumer_unavailable", time.Now().UTC().Add(5*time.Second)); err != nil {
					return err
				}
				continue
			}
			if err := s.Store.AcknowledgeDelivery(ctx, target.name, e.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) RunEventDelivery(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		_ = s.DeliverEvents(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
