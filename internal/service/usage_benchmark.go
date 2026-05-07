package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/pubsub/kafka"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// UsageBenchmarkService publishes benchmark trigger events and consumes them
// to compare GetFeatureUsageBySubscription against the meter_usage pipeline.
type UsageBenchmarkService interface {
	// PublishEvent sends a thin benchmark trigger to Kafka. Non-blocking best-effort.
	PublishEvent(ctx context.Context, event *events.UsageBenchmarkEvent) error

	// RegisterHandler wires the consumer into the router.
	RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration)
}

type usageBenchmarkService struct {
	ServiceParams
	pubSub    pubsub.PubSub
	benchRepo events.UsageBenchmarkRepository
}

// NewUsageBenchmarkService is the production constructor wired by FX.
func NewUsageBenchmarkService(
	params ServiceParams,
	benchRepo events.UsageBenchmarkRepository,
) UsageBenchmarkService {
	svc := &usageBenchmarkService{
		ServiceParams: params,
		benchRepo:     benchRepo,
	}

	ps, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.UsageBenchmark.ConsumerGroup,
	)
	if err != nil {
		params.Logger.Warnw("usage benchmark: kafka unavailable, benchmark event publishing disabled", "error", err)
		return svc
	}
	svc.pubSub = ps
	return svc
}

// NewUsageBenchmarkServiceForTest builds a minimal service using injected deps (test only).
func NewUsageBenchmarkServiceForTest(
	benchRepo events.UsageBenchmarkRepository,
	ps pubsub.PubSub,
) *usageBenchmarkService {
	return &usageBenchmarkService{
		pubSub:    ps,
		benchRepo: benchRepo,
	}
}

// PublishEvent marshals and publishes a UsageBenchmarkEvent.
func (s *usageBenchmarkService) PublishEvent(ctx context.Context, event *events.UsageBenchmarkEvent) error {
	if s.pubSub == nil {
		return nil
	}

	if s.Config == nil {
		return nil
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("usage benchmark: failed to marshal event: %w", err)
	}

	msg := message.NewMessage(fmt.Sprintf("bench-%s-%d", event.SubscriptionID, time.Now().UnixNano()), payload)
	msg.Metadata.Set("tenant_id", event.TenantID)
	msg.Metadata.Set("environment_id", event.EnvironmentID)

	topic := s.Config.UsageBenchmark.Topic
	if err := s.pubSub.Publish(ctx, topic, msg); err != nil {
		return fmt.Errorf("usage benchmark: failed to publish to %s: %w", topic, err)
	}
	return nil
}

// RegisterHandler wires the benchmark consumer into the watermill router.
func (s *usageBenchmarkService) RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration) {
	if !cfg.UsageBenchmark.Enabled {
		s.Logger.Infow("usage benchmark consumer disabled by configuration")
		return
	}

	throttle := middleware.NewThrottle(cfg.UsageBenchmark.RateLimit, time.Second)

	router.AddNoPublishHandler(
		"usage_benchmark_handler",
		cfg.UsageBenchmark.Topic,
		s.pubSub,
		s.processMessage,
		throttle.Middleware,
	)

	s.Logger.Infow("registered usage benchmark handler",
		"topic", cfg.UsageBenchmark.Topic,
		"rate_limit", cfg.UsageBenchmark.RateLimit,
	)
}

// processMessage is the internal watermill handler delegate.
func (s *usageBenchmarkService) processMessage(msg *message.Message) error {
	return s.ProcessMessageForTest(msg)
}

// ProcessMessageForTest is exported so unit tests can call it directly.
func (s *usageBenchmarkService) ProcessMessageForTest(msg *message.Message) error {
	tenantID := msg.Metadata.Get("tenant_id")
	environmentID := msg.Metadata.Get("environment_id")

	var evt events.UsageBenchmarkEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		if s.Logger != nil {
			s.Logger.Errorw("usage benchmark: failed to unmarshal event", "error", err)
		}
		return nil
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)

	featureAmt, currency := s.callFeatureUsagePipeline(ctx, &evt)
	// TODO: replace with s.callMeterUsagePipeline(ctx, &evt) when GetMeterUsageBySubscription is ready.
	meterAmt := featureAmt

	record := &events.UsageBenchmarkRecord{
		TenantID:           tenantID,
		EnvironmentID:      environmentID,
		SubscriptionID:     evt.SubscriptionID,
		StartTime:          evt.StartTime,
		EndTime:            evt.EndTime,
		FeatureUsageAmount: featureAmt,
		MeterUsageAmount:   meterAmt,
		Diff:               featureAmt.Sub(meterAmt),
		Currency:           currency,
		CreatedAt:          time.Now().UTC(),
	}

	if err := s.benchRepo.Insert(ctx, record); err != nil {
		if s.Logger != nil {
			s.Logger.Errorw("usage benchmark: failed to insert record",
				"subscription_id", evt.SubscriptionID,
				"error", err,
			)
		}
		// Ack anyway — benchmark data is non-critical.
	}
	return nil
}

// callFeatureUsagePipeline calls GetFeatureUsageBySubscription (source of truth).
func (s *usageBenchmarkService) callFeatureUsagePipeline(ctx context.Context, evt *events.UsageBenchmarkEvent) (decimal.Decimal, string) {
	if s.FeatureUsageRepo == nil {
		return decimal.Zero, ""
	}
	subSvc := NewSubscriptionService(s.ServiceParams)
	resp, err := subSvc.GetFeatureUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: evt.SubscriptionID,
		StartTime:      evt.StartTime,
		EndTime:        evt.EndTime,
		Source:         string(types.UsageSourceAnalytics),
	})
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warnw("usage benchmark: feature pipeline call failed",
				"subscription_id", evt.SubscriptionID,
				"error", err,
			)
		}
		return decimal.Zero, ""
	}
	return decimal.NewFromFloat(resp.Amount), resp.Currency
}
