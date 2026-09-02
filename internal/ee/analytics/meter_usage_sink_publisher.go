package analytics

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/logger"
)

// messagePublisher is the minimal publish surface this package needs. *kafka.Producer
// satisfies it. Declared locally so the analytics package does not depend on kafka's
// internal interface — it only needs "publish a message to a topic".
type messagePublisher interface {
	Publish(topic string, messages ...*message.Message) error
}

// MeterUsageSinkPublisher is the exported dependency the service depends on. The concrete impl
// (*meterUsageSinkPublisher) stays private per repo convention; consumers use this interface.
type MeterUsageSinkPublisher interface {
	PublishMeterUsage(ctx context.Context, records []*events.MeterUsage)
}

// meterUsageSinkPublisher is fire-and-forget: ClickHouse stays authoritative, so a publish
// failure here is logged and swallowed, never propagated to the caller.
type meterUsageSinkPublisher struct {
	publisher messagePublisher
	topic     string
	logger    *logger.Logger
}

// NewMeterUsageSinkPublisher is the fx provider. Returns untyped nil (not a typed-nil
// *meterUsageSinkPublisher) when disabled, so the caller's `!= nil` guard is FALSE.
func NewMeterUsageSinkPublisher(primaryProducer *kafka.Producer, cfg *config.Configuration, logger *logger.Logger) MeterUsageSinkPublisher {
	if !cfg.Analytics.Enabled || cfg.Analytics.MeterUsageSinkTopic == "" || primaryProducer == nil {
		return nil
	}
	return &meterUsageSinkPublisher{
		publisher: primaryProducer,
		topic:     cfg.Analytics.MeterUsageSinkTopic,
		logger:    logger,
	}
}

// PublishMeterUsage publishes each record as one JSON message keyed by event id (the lake's dedup key).
func (p *meterUsageSinkPublisher) PublishMeterUsage(ctx context.Context, records []*events.MeterUsage) {
	// nil publisher (feed disabled) ⇒ no-op.
	if p == nil || p.publisher == nil {
		return
	}

	for _, record := range records {
		// Lake dedups on (id, ingested_at); stamp it since BulkInsert leaves it zero.
		if record.IngestedAt.IsZero() {
			record.IngestedAt = time.Now().UTC()
		}

		payload, err := json.Marshal(record)
		if err != nil {
			p.logger.Ctx(ctx).With("event_id", record.ID, "error", err).
				Error("analytics meter_usage marshal failed")
			continue
		}

		msg := message.NewMessage(record.ID, payload)
		msg.Metadata.Set("tenant_id", record.TenantID)
		msg.Metadata.Set("environment_id", record.EnvironmentID)

		if err := p.publisher.Publish(p.topic, msg); err != nil {
			p.logger.Ctx(ctx).With(
				"event_id", record.ID,
				"tenant_id", record.TenantID,
				"topic", p.topic,
				"error", err,
			).Error("analytics meter_usage publish failed")
		}
	}
}
