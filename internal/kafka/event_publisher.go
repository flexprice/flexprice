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
		primary:    primaryProducer, // local cluster is always written
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

// determineBulkTopic resolves the batch topic for one cluster, like determineTopic.
func determineBulkTopic(kc *config.KafkaConfig) string {
	if kc == nil {
		return ""
	}
	return kc.TopicBulk
}

// determineTopic routes to a cluster's lazy or main topic using that cluster's own config.
func determineTopic(kc *config.KafkaConfig, event *events.Event) string {
	if slices.Contains(kc.RouteTenantsOnLazyMode, event.TenantID) {
		return kc.TopicLazy
	}
	return kc.Topic
}

// PublishBatch writes events as batched messages so one message becomes one ClickHouse
// INSERT downstream, paying the per-INSERT cost once per batch instead of once per event.
// Events are grouped by tenant and environment only — the envelope carries one of each — and
// placement is left to sarama's default partitioner, since the producer sets no Kafka key.
func (p *EventPublisher) PublishBatch(ctx context.Context, evts []*events.Event) error {
	if len(evts) == 0 {
		return nil
	}

	if determineBulkTopic(p.primaryCfg) == "" {
		return ierr.NewError("batch topic is not configured").
			WithHint("Set kafka.topic_bulk (FLEXPRICE_KAFKA_TOPIC_BULK)").
			Mark(ierr.ErrValidation)
	}

	// Bounds come from the local cluster so both clusters get identical payloads.
	groups, err := groupForBatching(evts, p.primaryCfg)
	if err != nil {
		return err
	}

	var firstErr error
	for _, g := range groups {
		payload, err := json.Marshal(events.EventBatch{
			Events:        g.events,
			TenantID:      g.tenantID,
			EnvironmentID: g.environmentID,
		})
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to marshal event batch").
				Mark(ierr.ErrValidation)
		}

		// No single event id to use as the message UUID; dedup still holds because
		// ClickHouse dedupes on the ids inside the payload.
		if err := p.publishBatchMessage(ctx, payload, g, len(g.events)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

type eventGroup struct {
	tenantID      string
	environmentID string
	events        []*events.Event
}

// groupForBatching buckets events by tenant and environment, then cuts each bucket into size-
// and byte-bounded batches, measuring bytes as each event marshals. Scope must not be mixed:
// the envelope carries one tenant/environment for the whole batch.
func groupForBatching(evts []*events.Event, kc *config.KafkaConfig) ([]eventGroup, error) {
	maxSize := kc.BulkMaxBatchSize
	if maxSize <= 0 {
		maxSize = 1
	}
	maxBytes := kc.BulkMaxBatchBytes

	open := make(map[string]*eventGroup)
	sizes := make(map[string]int)
	out := make([]eventGroup, 0, len(evts)/maxSize+1)

	for _, e := range evts {
		if e.ID == "" {
			e.ID = watermill.NewUUID()
		}
		groupKey := e.TenantID + "|" + e.EnvironmentID

		size := 0
		if maxBytes > 0 {
			b, err := json.Marshal(e)
			if err != nil {
				return nil, ierr.WithError(err).
					WithHint("Failed to marshal event for batching").
					WithReportableDetails(map[string]interface{}{"event_id": e.ID}).
					Mark(ierr.ErrValidation)
			}
			size = len(b)
		}

		g, ok := open[groupKey]
		if !ok {
			g = &eventGroup{tenantID: e.TenantID, environmentID: e.EnvironmentID}
			open[groupKey] = g
		}

		// Close before breaching either bound. An event larger than maxBytes still goes out
		// alone, so an over-limit event surfaces as a broker rejection rather than silence.
		overSize := len(g.events)+1 > maxSize
		overBytes := maxBytes > 0 && len(g.events) > 0 && sizes[groupKey]+size > maxBytes
		if overSize || overBytes {
			out = append(out, *g)
			g = &eventGroup{tenantID: e.TenantID, environmentID: e.EnvironmentID}
			open[groupKey] = g
			sizes[groupKey] = 0
		}

		g.events = append(g.events, e)
		sizes[groupKey] += size
	}

	for _, g := range open {
		if len(g.events) > 0 {
			out = append(out, *g)
		}
	}
	return out, nil
}

// publishBatchMessage writes one batch to every cluster, resolving the topic per cluster as
// Publish does — topic names need not match across clusters.
func (p *EventPublisher) publishBatchMessage(ctx context.Context, payload []byte, g eventGroup, count int) error {
	msg := message.NewMessage(watermill.NewUUID(), payload)
	msg.Metadata.Set("tenant_id", g.tenantID)
	msg.Metadata.Set("environment_id", g.environmentID)
	msg.Metadata.Set("batch_size", fmt.Sprintf("%d", count))

	var firstErr error
	if err := p.primary.Publish(determineBulkTopic(p.primaryCfg), msg); err != nil {
		p.logBatchFailure(ctx, "primary", g, count, err)
		firstErr = err
	}
	if p.secondary != nil {
		if err := p.secondary.Publish(determineBulkTopic(p.secondaryCfg), msg); err != nil {
			p.logBatchFailure(ctx, "secondary", g, count, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (p *EventPublisher) logBatchFailure(ctx context.Context, cluster string, g eventGroup, count int, err error) {
	p.logger.Ctx(ctx).With(
		"cluster", cluster,
		"tenant_id", g.tenantID,
		"environment_id", g.environmentID,
		"batch_size", count,
		"error", err,
	).Error("kafka batch publish failed")
}
