package service

import (
	"context"
	"encoding/json" // used in meterUsageValueToDecimal for json.Number
	"fmt"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	meterusage "github.com/flexprice/flexprice/internal/domain/meter_usage"
	"github.com/flexprice/flexprice/internal/expression"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/pubsub/kafka"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// MeterUsageTrackingService enriches raw events with meter IDs and writes rows
// to the meter_usage ClickHouse table.
//
// Unlike FeatureUsageTrackingService it does NOT perform customer, subscription,
// or period lookups — meter_usage is purely a meter-scoped aggregation layer.
type MeterUsageTrackingService interface {
	RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration)
}

type meterUsageTrackingService struct {
	ServiceParams
	pubSub              pubsub.PubSub
	meterUsageRepo      meterusage.Repository
	expressionEvaluator expression.Evaluator
}

// NewMeterUsageTrackingService constructs the service and wires up its Kafka PubSub.
func NewMeterUsageTrackingService(
	params ServiceParams,
	meterUsageRepo meterusage.Repository,
) MeterUsageTrackingService {
	svc := &meterUsageTrackingService{
		ServiceParams:       params,
		meterUsageRepo:      meterUsageRepo,
		expressionEvaluator: expression.NewCELEvaluator(),
	}

	pubSub, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.MeterUsageTracking.ConsumerGroup,
	)
	if err != nil {
		params.Logger.Fatalw("failed to create meter_usage pubsub", "error", err)
		return nil
	}
	svc.pubSub = pubSub
	return svc
}

// RegisterHandler wires the consumer to the Watermill router.
func (s *meterUsageTrackingService) RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration) {
	if !cfg.MeterUsageTracking.Enabled {
		s.Logger.Infow("meter_usage tracking handler disabled by configuration")
		return
	}

	throttle := middleware.NewThrottle(cfg.MeterUsageTracking.RateLimit, time.Second)

	router.AddNoPublishHandler(
		"meter_usage_tracking_handler",
		cfg.MeterUsageTracking.Topic,
		s.pubSub,
		s.processMessage,
		throttle.Middleware,
	)

	s.Logger.Infow("registered meter_usage tracking handler",
		"topic", cfg.MeterUsageTracking.Topic,
		"consumer_group", cfg.MeterUsageTracking.ConsumerGroup,
		"rate_limit", cfg.MeterUsageTracking.RateLimit,
	)
}

// processMessage is the Watermill handler. It deserialises the Kafka message and
// dispatches to processEvent.
func (s *meterUsageTrackingService) processMessage(msg *message.Message) error {
	tenantID := msg.Metadata.Get("tenant_id")
	environmentID := msg.Metadata.Get("environment_id")

	ctx := context.Background()
	if tenantID != "" {
		ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	}
	if environmentID != "" {
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
	}

	var event events.Event
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		s.Logger.Errorw("meter_usage: failed to unmarshal event",
			"error", err,
			"message_uuid", msg.UUID,
		)
		return nil // non-retryable: bad payload
	}

	// Fallback: populate ctx values from event body if metadata was empty
	if tenantID == "" && event.TenantID != "" {
		ctx = context.WithValue(ctx, types.CtxTenantID, event.TenantID)
	}
	if environmentID == "" && event.EnvironmentID != "" {
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, event.EnvironmentID)
	}

	if event.TenantID == "" || event.EnvironmentID == "" {
		s.Logger.Warnw("meter_usage: skipping event with missing tenant/environment",
			"event_id", event.ID,
		)
		return nil
	}

	if err := s.processEvent(ctx, &event); err != nil {
		s.Logger.Errorw("meter_usage: failed to process event",
			"error", err,
			"event_id", event.ID,
			"event_name", event.EventName,
		)
		return err // retryable
	}

	return nil
}

// processEvent prepares MeterUsage rows and bulk-inserts them.
func (s *meterUsageTrackingService) processEvent(ctx context.Context, event *events.Event) error {
	records, err := s.prepareInserts(ctx, event)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}
	return s.meterUsageRepo.BulkInsert(ctx, records)
}

// prepareInserts resolves matching meters for the event and builds MeterUsage rows.
func (s *meterUsageTrackingService) prepareInserts(ctx context.Context, event *events.Event) ([]*meterusage.MeterUsage, error) {
	// STEP 1: Look up all meters that match this event name within the tenant/environment.
	meterFilter := types.NewNoLimitMeterFilter()
	meterFilter.EventName = event.EventName

	meters, err := s.MeterRepo.List(ctx, meterFilter)
	if err != nil {
		s.Logger.ErrorwCtx(ctx, "meter_usage: failed to list meters",
			"error", err,
			"event_id", event.ID,
			"event_name", event.EventName,
		)
		return nil, err
	}

	if len(meters) == 0 {
		return nil, nil
	}

	// Determine whether to store properties for this tenant.
	storeProperties := lo.Contains(
		s.Config.MeterUsageTracking.AllowedPropertiesInsertForTenant,
		event.TenantID,
	)

	now := time.Now().UTC()
	records := make([]*meterusage.MeterUsage, 0, len(meters))

	// STEP 2: For each matching meter, apply filters and compute qty_total / unique_hash.
	for _, m := range meters {
		if !s.checkMeterUsageFilters(event, m.Filters) {
			continue
		}

		qtyTotal, uniqueHash := s.extractMeterUsageQty(ctx, event, m)

		// Copy the full event via embedding; override ID to composite key and
		// clear properties when the tenant is not in the allow-list.
		eventCopy := *event
		eventCopy.ID = fmt.Sprintf("%s_%s", event.ID, m.ID)
		eventCopy.IngestedAt = now
		if !storeProperties {
			eventCopy.Properties = nil
		}

		records = append(records, &meterusage.MeterUsage{
			Event:      eventCopy,
			MeterID:    m.ID,
			QtyTotal:   qtyTotal,
			UniqueHash: uniqueHash,
		})
	}

	return records, nil
}

// checkMeterUsageFilters returns true when all meter filters are satisfied by the event.
func (s *meterUsageTrackingService) checkMeterUsageFilters(event *events.Event, filters []meter.Filter) bool {
	if len(filters) == 0 {
		return true
	}
	for _, f := range filters {
		propVal, exists := event.Properties[f.Key]
		if !exists {
			return false
		}
		if !lo.Contains(f.Values, fmt.Sprintf("%v", propVal)) {
			return false
		}
	}
	return true
}

// extractMeterUsageQty computes qty_total and unique_hash for a single event-meter pair.
//
// qty_total semantics by aggregation type:
//   - COUNT:               1.0  (count of events; sum at query time)
//   - SUM:                 property value
//   - SUM_WITH_MULTIPLIER: property value × multiplier (baked in at write time)
//   - AVG:                 property value  (avg computed at query time)
//   - MAX:                 property value  (max computed at query time)
//   - LATEST:              property value  (argMax at query time)
//   - WEIGHTED_SUM:        property value  (weight applied at query time)
//   - COUNT_UNIQUE:        1.0  (uniqueness tracked via unique_hash)
//   - Expression:          CEL result
func (s *meterUsageTrackingService) extractMeterUsageQty(
	ctx context.Context,
	event *events.Event,
	m *meter.Meter,
) (qtyTotal decimal.Decimal, uniqueHash string) {
	// CEL expression takes priority over aggregation type.
	if m.Aggregation.Expression != "" {
		qty, err := s.expressionEvaluator.EvaluateQuantity(m.Aggregation.Expression, event.Properties)
		if err != nil {
			s.Logger.WarnwCtx(ctx, "meter_usage: CEL evaluation failed, using 0",
				"error", err,
				"event_id", event.ID,
				"meter_id", m.ID,
			)
			return decimal.Zero, ""
		}
		if m.Aggregation.Multiplier != nil {
			qty = qty.Mul(*m.Aggregation.Multiplier)
		}
		return qty, ""
	}

	switch m.Aggregation.Type {
	case types.AggregationCount:
		return decimal.NewFromInt(1), ""

	case types.AggregationCountUnique:
		if m.Aggregation.Field == "" {
			return decimal.Zero, ""
		}
		val, ok := event.Properties[m.Aggregation.Field]
		if !ok {
			return decimal.Zero, ""
		}
		return decimal.NewFromInt(1), s.meterUsageValueToString(val)

	case types.AggregationSum,
		types.AggregationAvg,
		types.AggregationMax,
		types.AggregationLatest,
		types.AggregationWeightedSum:
		if m.Aggregation.Field == "" {
			return decimal.Zero, ""
		}
		val, ok := event.Properties[m.Aggregation.Field]
		if !ok {
			return decimal.Zero, ""
		}
		qty := s.meterUsageValueToDecimal(ctx, event.ID, m.ID, val)
		return qty, ""

	case types.AggregationSumWithMultiplier:
		if m.Aggregation.Field == "" || m.Aggregation.Multiplier == nil {
			return decimal.Zero, ""
		}
		val, ok := event.Properties[m.Aggregation.Field]
		if !ok {
			return decimal.Zero, ""
		}
		qty := s.meterUsageValueToDecimal(ctx, event.ID, m.ID, val)
		if qty.IsZero() {
			return decimal.Zero, ""
		}
		return qty.Mul(*m.Aggregation.Multiplier), ""

	default:
		s.Logger.WarnwCtx(ctx, "meter_usage: unsupported aggregation type",
			"event_id", event.ID,
			"meter_id", m.ID,
			"aggregation_type", m.Aggregation.Type,
		)
		return decimal.Zero, ""
	}
}

// meterUsageValueToDecimal converts a raw event property value to decimal.Decimal.
func (s *meterUsageTrackingService) meterUsageValueToDecimal(
	ctx context.Context, eventID, meterID string, val interface{},
) decimal.Decimal {
	switch v := val.(type) {
	case float64:
		return decimal.NewFromFloat(v)
	case float32:
		return decimal.NewFromFloat32(v)
	case int:
		return decimal.NewFromInt(int64(v))
	case int32:
		return decimal.NewFromInt(int64(v))
	case int64:
		return decimal.NewFromInt(v)
	case uint:
		return decimal.NewFromInt(int64(v))
	case uint64:
		d, err := decimal.NewFromString(fmt.Sprintf("%d", v))
		if err != nil {
			s.Logger.WarnwCtx(ctx, "meter_usage: failed to parse uint64",
				"event_id", eventID, "meter_id", meterID, "value", v, "error", err)
			return decimal.Zero
		}
		return d
	case string:
		d, err := decimal.NewFromString(v)
		if err != nil {
			s.Logger.WarnwCtx(ctx, "meter_usage: failed to parse string as decimal",
				"event_id", eventID, "meter_id", meterID, "value", v, "error", err)
			return decimal.Zero
		}
		return d
	case json.Number:
		d, err := decimal.NewFromString(string(v))
		if err != nil {
			s.Logger.WarnwCtx(ctx, "meter_usage: failed to parse json.Number as decimal",
				"event_id", eventID, "meter_id", meterID, "value", v, "error", err)
			return decimal.Zero
		}
		return d
	default:
		s.Logger.WarnwCtx(ctx, "meter_usage: unknown property type, cannot convert to decimal",
			"event_id", eventID, "meter_id", meterID,
			"type", fmt.Sprintf("%T", v), "value", fmt.Sprintf("%v", v))
		return decimal.Zero
	}
}

// meterUsageValueToString converts a raw event property value to string.
// Used only for COUNT_UNIQUE unique_hash extraction.
func (s *meterUsageTrackingService) meterUsageValueToString(val interface{}) string {
	switch v := val.(type) {
	case string:
		return v
	case json.Number:
		return string(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}
