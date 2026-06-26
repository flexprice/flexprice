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
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// UsageBenchmarkService publishes benchmark trigger events and consumes them
// to compare two analytics pipelines. Two event kinds share the topic:
//   - "subscription": GetFeatureUsageBySubscription vs GetMeterUsageBySubscription
//   - "analytics":    meterUsage GetDetailedAnalytics with FINAL vs without FINAL (perf + result diff)
type UsageBenchmarkService interface {
	// PublishEvent sends a thin benchmark trigger to Kafka. Non-blocking best-effort.
	PublishEvent(ctx context.Context, event *events.UsageBenchmarkEvent) error

	// PublishAnalyticsEvent sends the analytics-kind benchmark trigger carrying the raw request.
	// Fire-and-forget: callers should not propagate errors to the live response.
	PublishAnalyticsEvent(ctx context.Context, req *dto.GetUsageAnalyticsRequest) error

	// RegisterHandler wires the consumer into the router.
	RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration)
}

type usageBenchmarkService struct {
	ServiceParams
	pubSub             pubsub.PubSub
	benchRepo          events.UsageBenchmarkRepository
	meterUsageBenchRepo events.MeterUsageBenchmarkRepository

	// Injected analytics services for the analytics-kind benchmark path.
	// Optional — if nil, the analytics dispatch silently no-ops. NewUsageBenchmarkService
	// wires them; the wallet path uses a partial constructor that leaves them nil.
	featureUsageTrackingService FeatureUsageTrackingService
	meterUsageService           MeterUsageService
}

// NewUsageBenchmarkService is the production constructor wired by FX.
func NewUsageBenchmarkService(
	params ServiceParams,
	benchRepo events.UsageBenchmarkRepository,
	meterUsageBenchRepo events.MeterUsageBenchmarkRepository,
	featureUsageTrackingService FeatureUsageTrackingService,
	meterUsageService MeterUsageService,
) UsageBenchmarkService {
	return &usageBenchmarkService{
		ServiceParams:               params,
		benchRepo:                   benchRepo,
		meterUsageBenchRepo:         meterUsageBenchRepo,
		pubSub:                      params.UsageBenchmarkPubSub.PubSub,
		featureUsageTrackingService: featureUsageTrackingService,
		meterUsageService:           meterUsageService,
	}
}

// NewUsageBenchmarkServiceForTest builds a minimal service using injected deps (test only).
func NewUsageBenchmarkServiceForTest(
	benchRepo events.UsageBenchmarkRepository,
	meterUsageBenchRepo events.MeterUsageBenchmarkRepository,
	ps pubsub.PubSub,
) *usageBenchmarkService {
	return &usageBenchmarkService{
		pubSub:             ps,
		benchRepo:          benchRepo,
		meterUsageBenchRepo: meterUsageBenchRepo,
	}
}

// PublishEvent marshals and publishes a UsageBenchmarkEvent (any kind).
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

	// Use SubscriptionID in the watermill UUID when present so subscription-kind
	// events remain debuggable; analytics-kind events fall back to a timestamp suffix.
	msgID := fmt.Sprintf("bench-%s-%d", event.SubscriptionID, time.Now().UnixNano())
	if event.Kind == events.UsageBenchmarkKindAnalytics {
		msgID = fmt.Sprintf("bench-analytics-%s", types.GenerateUUID())
	}
	msg := message.NewMessage(msgID, payload)
	msg.Metadata.Set("tenant_id", event.TenantID)
	msg.Metadata.Set("environment_id", event.EnvironmentID)

	topic := s.Config.UsageBenchmark.Topic
	if err := s.pubSub.Publish(ctx, topic, msg); err != nil {
		return fmt.Errorf("usage benchmark: failed to publish to %s: %w", topic, err)
	}
	return nil
}

// PublishAnalyticsEvent wraps a request into an analytics-kind event and publishes it.
// Fire-and-forget contract: callers (HTTP handlers) must not block or fail on errors.
func (s *usageBenchmarkService) PublishAnalyticsEvent(ctx context.Context, req *dto.GetUsageAnalyticsRequest) error {
	if req == nil {
		return nil
	}
	if s.pubSub == nil || s.Config == nil {
		return nil
	}

	raw, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("usage benchmark: failed to marshal analytics request: %w", err)
	}

	evt := &events.UsageBenchmarkEvent{
		Kind:             events.UsageBenchmarkKindAnalytics,
		TenantID:         types.GetTenantID(ctx),
		EnvironmentID:    types.GetEnvironmentID(ctx),
		StartTime:        req.StartTime,
		EndTime:          req.EndTime,
		AnalyticsRequest: raw,
	}
	return s.PublishEvent(ctx, evt)
}

// RegisterHandler wires the benchmark consumer into the watermill router.
func (s *usageBenchmarkService) RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration) {
	if !cfg.UsageBenchmark.Enabled {
		s.Logger.Info(context.Background(), "usage benchmark consumer disabled by configuration")
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

	s.Logger.Info(context.Background(), "registered usage benchmark handler",
		"topic", cfg.UsageBenchmark.Topic,
		"rate_limit", cfg.UsageBenchmark.RateLimit,
	)
}

// processMessage is the internal watermill handler delegate.
func (s *usageBenchmarkService) processMessage(msg *message.Message) error {
	return s.ProcessMessageForTest(msg)
}

// ProcessMessageForTest is exported so unit tests can call it directly.
// Dispatches by event Kind; empty Kind is treated as subscription for back-compat
// with events already in flight when this code rolled out.
func (s *usageBenchmarkService) ProcessMessageForTest(msg *message.Message) error {
	tenantID := msg.Metadata.Get("tenant_id")
	environmentID := msg.Metadata.Get("environment_id")

	var evt events.UsageBenchmarkEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		if s.Logger != nil {
			s.Logger.Error(context.Background(), "usage benchmark: failed to unmarshal event", "error", err)
		}
		return nil
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)

	switch evt.Kind {
	case events.UsageBenchmarkKindAnalytics:
		return s.processAnalyticsMessage(ctx, msg, &evt)
	default:
		// Empty Kind == subscription (back-compat).
		return s.processSubscriptionMessage(ctx, msg, &evt)
	}
}

// processSubscriptionMessage holds the original subscription-pipeline benchmark logic.
func (s *usageBenchmarkService) processSubscriptionMessage(ctx context.Context, _ *message.Message, evt *events.UsageBenchmarkEvent) error {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	featureAmt, featureCurrency := s.callFeatureUsagePipeline(ctx, evt)
	meterAmt, meterCurrency := s.callMeterUsagePipeline(ctx, evt)

	// Prefer feature pipeline's currency (source of truth); fall back to meter
	// pipeline's currency if the feature call returned no result.
	currency := featureCurrency
	if currency == "" {
		currency = meterCurrency
	}

	diff := featureAmt.Sub(meterAmt)
	if !diff.IsZero() && s.Logger != nil {
		s.Logger.Info(context.Background(), "usage benchmark: feature/meter pipelines disagree",
			"subscription_id", evt.SubscriptionID,
			"tenant_id", tenantID,
			"environment_id", environmentID,
			"start_time", evt.StartTime,
			"end_time", evt.EndTime,
			"feature_amount", featureAmt,
			"meter_amount", meterAmt,
			"diff", diff,
			"currency", currency,
		)
	}

	record := &events.UsageBenchmarkRecord{
		TenantID:           tenantID,
		EnvironmentID:      environmentID,
		SubscriptionID:     evt.SubscriptionID,
		StartTime:          evt.StartTime,
		EndTime:            evt.EndTime,
		FeatureUsageAmount: featureAmt,
		MeterUsageAmount:   meterAmt,
		Diff:               diff,
		Currency:           currency,
		CreatedAt:          time.Now().UTC(),
	}

	if err := s.benchRepo.Insert(ctx, record); err != nil {
		if s.Logger != nil {
			s.Logger.Error(context.Background(), "usage benchmark: failed to insert record",
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
			s.Logger.Info(ctx, "usage benchmark: feature pipeline call failed",
				"subscription_id", evt.SubscriptionID,
				"error", err,
			)
		}
		return decimal.Zero, ""
	}
	return decimal.NewFromFloat(resp.Amount), resp.Currency
}

// callMeterUsagePipeline calls GetMeterUsageBySubscription. Returns (0, "") on
// error so the benchmark insert still succeeds with a recorded diff.
func (s *usageBenchmarkService) callMeterUsagePipeline(ctx context.Context, evt *events.UsageBenchmarkEvent) (decimal.Decimal, string) {
	if s.MeterUsageRepo == nil {
		return decimal.Zero, ""
	}
	subSvc := NewSubscriptionService(s.ServiceParams)
	resp, err := subSvc.GetMeterUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: evt.SubscriptionID,
		StartTime:      evt.StartTime,
		EndTime:        evt.EndTime,
		Source:         string(types.UsageSourceAnalytics),
	})
	if err != nil {
		if s.Logger != nil {
			s.Logger.Info(ctx, "usage benchmark: meter pipeline call failed",
				"subscription_id", evt.SubscriptionID,
				"error", err,
			)
		}
		return decimal.Zero, ""
	}
	return decimal.NewFromFloat(resp.Amount), resp.Currency
}
