package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

// messagePublisher is the subset of the watermill publisher that EventPublisher depends
// on. *Producer satisfies it; tests substitute a fake to exercise the fan-out without a broker.
type messagePublisher interface {
	Publish(topic string, messages ...*message.Message) error
}

// EventPublisher publishes source events to Kafka. It always writes to the deployment's
// local cluster (cfg.Kafka); during the AWS→GCP migration, if a second cluster is
// configured (cfg.KafkaSecondary), it also writes there (presence-based dual-write). Writes
// are independent: one cluster failing does not block the other. Ingest is fire-and-forget
// at the service layer (CreateEvent logs and returns 202), so a failure here is logged
// per-cluster and returned, not customer-facing. See infrastructure/docs/GCP-CUTOVER-STEPWISE.md.
type EventPublisher struct {
	primary      messagePublisher
	primaryCfg   *config.KafkaConfig
	secondary    messagePublisher
	secondaryCfg *config.KafkaConfig
	logger       *logger.Logger
}

func NewEventPublisher(primaryProducer *Producer, secondaryProducer *SecondaryProducer, cfg *config.Configuration, logger *logger.Logger) (*EventPublisher, error) {
	if primaryProducer == nil {
		return nil, fmt.Errorf("kafka producer is not initialized")
	}
	ep := &EventPublisher{
		logger:     logger,
		primary:    primaryProducer,   // local cluster is always written
		primaryCfg: &cfg.Kafka,
	}
	// Presence-based dual-write: a configured second cluster ⇒ also write there.
	if cfg.KafkaSecondary != nil {
		if secondaryProducer == nil || secondaryProducer.Producer == nil {
			return nil, fmt.Errorf("kafka_secondary is configured but its producer is not initialized")
		}
		ep.secondary = secondaryProducer.Producer
		ep.secondaryCfg = cfg.KafkaSecondary
	}
	return ep, nil
}

func (p *EventPublisher) Publish(ctx context.Context, event *events.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to marshal event").
			Mark(ierr.ErrValidation)
	}

	if event.ID == "" {
		event.ID = watermill.NewUUID()
	}

	// Deterministic partition key (tenant_id[:external_customer_id]) so all events for a
	// customer land on the same partition, identically on every cluster.
	partitionKey := event.TenantID
	if event.ExternalCustomerID != "" {
		partitionKey = fmt.Sprintf("%s:%s", event.TenantID, event.ExternalCustomerID)
	}

	// Write to each cluster independently — one failing must not block the other.
	// A fresh message per cluster (the watermill publisher consumes the message it is handed);
	// same event.ID + payload everywhere keeps the streams dedup-identical.
	// The local cluster is always present (NewEventPublisher guarantees it).
	var firstErr error
	if err := p.primary.Publish(determineTopic(p.primaryCfg, event), p.buildMessage(event, payload, partitionKey)); err != nil {
		p.logFailure("primary", event, err)
		firstErr = err
	}
	if p.secondary != nil {
		if err := p.secondary.Publish(determineTopic(p.secondaryCfg, event), p.buildMessage(event, payload, partitionKey)); err != nil {
			p.logFailure("secondary", event, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (p *EventPublisher) logFailure(cluster string, event *events.Event, err error) {
	p.logger.With(
		"cluster", cluster,
		"event_id", event.ID,
		"tenant_id", event.TenantID,
		"error", err,
	).Error("kafka publish failed")
}

// buildMessage assembles a watermill message for a single publish. event.ID is the message
// UUID (and the ClickHouse ReplacingMergeTree dedup key), so every cluster gets the same
// ID and payload.
func (p *EventPublisher) buildMessage(event *events.Event, payload []byte, partitionKey string) *message.Message {
	msg := message.NewMessage(event.ID, payload)
	msg.Metadata.Set("tenant_id", event.TenantID)
	msg.Metadata.Set("environment_id", event.EnvironmentID)
	msg.Metadata.Set("partition_key", partitionKey)
	return msg
}

// determineTopic routes to a cluster's lazy or main topic using that cluster's own config.
func determineTopic(kc *config.KafkaConfig, event *events.Event) string {
	if slices.Contains(kc.RouteTenantsOnLazyMode, event.TenantID) {
		return kc.TopicLazy
	}
	return kc.Topic
}
