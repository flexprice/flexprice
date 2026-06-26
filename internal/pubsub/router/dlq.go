package router

import (
	"context"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/tracing"
)

// routingDLQPublisher routes each poisoned message to a per-handler DLQ Kafka
// topic. It is registered as the global PoisonQueue publisher so the existing
// Retry middleware (and its position in the chain) stays untouched.
//
// PoisonQueue stamps middleware.PoisonedHandlerKey onto the message metadata
// before calling Publish, so we can look up the originating handler and route to
// the DLQ topic registered for it.
//
// When a handler has no per-consumer DLQ topic configured — either it never
// opted in (no Register call) or it registered with an empty dlqTopic — the
// message falls back to the legacy shared DLQ (the single cfg.Kafka.TopicDLQ
// topic, or an ephemeral in-memory queue when that is unset). This preserves
// the pre-routing behavior for every consumer that hasn't been migrated.
type routingDLQPublisher struct {
	inner          message.Publisher
	fallback       message.Publisher // legacy shared DLQ (kafka TopicDLQ or in-memory)
	fallbackTopic  string            // topic used with fallback publisher
	routes         map[string]string // handlerName -> dlqTopic
	consumerGroups map[string]string // handlerName -> consumerGroup (log labels)
	logger         *logger.Logger
	tracing        *tracing.Service
}

// newRoutingDLQPublisher builds a routing publisher with empty route tables.
// Routes are populated via Register at handler-registration time (startup),
// before router.Run, so the maps are never written concurrently. fallback +
// fallbackTopic implement the legacy shared-DLQ behavior for handlers without a
// per-consumer DLQ topic.
func newRoutingDLQPublisher(inner, fallback message.Publisher, fallbackTopic string, log *logger.Logger, tracingSvc *tracing.Service) *routingDLQPublisher {
	return &routingDLQPublisher{
		inner:          inner,
		fallback:       fallback,
		fallbackTopic:  fallbackTopic,
		routes:         make(map[string]string),
		consumerGroups: make(map[string]string),
		logger:         log,
		tracing:        tracingSvc,
	}
}

// Register opts a handler into per-consumer-group DLQ routing. An empty dlqTopic
// records the handler but disables DLQ publishing for it (per-env opt-out).
func (p *routingDLQPublisher) Register(handlerName, consumerGroup, dlqTopic string) {
	p.routes[handlerName] = dlqTopic
	p.consumerGroups[handlerName] = consumerGroup
}

// Publish is invoked by the PoisonQueue middleware once retries are exhausted.
// The topic argument is the placeholder passed to middleware.PoisonQueue and is
// intentionally ignored — routing is driven entirely by the handler metadata.
func (p *routingDLQPublisher) Publish(_ string, msgs ...*message.Message) error {
	ctx := context.Background()

	for _, msg := range msgs {
		handlerName := msg.Metadata.Get(middleware.PoisonedHandlerKey)

		dlqTopic := p.routes[handlerName]
		if dlqTopic == "" {
			// No per-consumer DLQ topic for this handler (never opted in, or
			// opted in with an empty topic). Fall back to the legacy shared DLQ
			// so the message is not lost.
			if err := p.publishFallback(ctx, handlerName, msg); err != nil {
				return err
			}
			continue
		}

		consumerGroup := p.consumerGroups[handlerName]
		reason := msg.Metadata.Get(middleware.ReasonForPoisonedKey)
		tenantID := msg.Metadata.Get("tenant_id")
		environmentID := msg.Metadata.Get("environment_id")

		p.logger.Error(ctx, "message_sent_to_dlq",
			"dlq_topic", dlqTopic,
			"handler_name", handlerName,
			"consumer_group", consumerGroup,
			"message_uuid", msg.UUID,
			"tenant_id", tenantID,
			"environment_id", environmentID,
			"error", reason,
		)

		p.tracing.CaptureException(ctx, newDLQError(handlerName, consumerGroup, dlqTopic, reason))

		if err := p.inner.Publish(dlqTopic, msg); err != nil {
			p.logger.Error(ctx, "failed to publish message to DLQ topic",
				"dlq_topic", dlqTopic,
				"handler_name", handlerName,
				"message_uuid", msg.UUID,
				"error", err,
			)
			return err
		}
	}

	return nil
}

// publishFallback routes a message to the legacy shared DLQ (cfg.Kafka.TopicDLQ
// or the in-memory queue). Mirrors the pre-routing behavior; intentionally no
// OTel exception capture so the old, quieter semantics are preserved.
func (p *routingDLQPublisher) publishFallback(ctx context.Context, handlerName string, msg *message.Message) error {
	p.logger.Warn(ctx, "no per-consumer DLQ topic configured, using fallback shared DLQ",
		"handler_name", handlerName,
		"fallback_topic", p.fallbackTopic,
		"message_uuid", msg.UUID,
	)

	if err := p.fallback.Publish(p.fallbackTopic, msg); err != nil {
		p.logger.Error(ctx, "failed to publish message to fallback DLQ",
			"fallback_topic", p.fallbackTopic,
			"handler_name", handlerName,
			"message_uuid", msg.UUID,
			"error", err,
		)
		return err
	}
	return nil
}

// Close shuts down the underlying publishers. The fallback may be the same
// publisher as inner (when the legacy DLQ is the same Kafka cluster), so it is
// only closed separately when it is a distinct instance (e.g. the in-memory queue).
func (p *routingDLQPublisher) Close() error {
	err := p.inner.Close()
	if p.fallback != nil && p.fallback != p.inner {
		if ferr := p.fallback.Close(); ferr != nil && err == nil {
			err = ferr
		}
	}
	return err
}

// dlqError carries DLQ context into the captured OTel exception so the SigNoz
// exception event is self-describing.
type dlqError struct {
	handlerName   string
	consumerGroup string
	dlqTopic      string
	reason        string
}

func newDLQError(handlerName, consumerGroup, dlqTopic, reason string) error {
	return &dlqError{
		handlerName:   handlerName,
		consumerGroup: consumerGroup,
		dlqTopic:      dlqTopic,
		reason:        reason,
	}
}

func (e *dlqError) Error() string {
	return "message_sent_to_dlq handler=" + e.handlerName +
		" consumer_group=" + e.consumerGroup +
		" dlq_topic=" + e.dlqTopic +
		" reason=" + e.reason
}
