package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/expression"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/pubsub/kafka"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/types"
	goCache "github.com/patrickmn/go-cache"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// meterCacheTTL is how long we cache meter lists per (tenant, environment, eventName).
//
// Why 10 minutes:
//   - Meters are append-only in practice; existing meters never change after creation.
//   - The TTL only matters for newly-created meters: a consumer process will start
//     seeing a new meter within at most meterCacheTTL of it being created.
//   - Using NoExpiration would be marginally faster but would require a process
//     restart to pick up new meters. 10 minutes is a safe middle ground.
//
// Why in-process and not Redis:
//   - The global cache.Type may be "redis", which adds a network hop on every
//     lookup and would not improve latency over a fresh Postgres query.
//   - Meter data per (tenant, environment, eventName) key is tiny (~KB), so a
//     per-process copy across N consumer pods is perfectly fine.
//
// Memory footprint estimate (worst case):
//   - 200 tenants × 20 event names × 10 meters × ~500 B/meter ≈ 20 MB
//
// Singleton guarantee:
//   - This service is registered via fx.Provide() (main.go) and is therefore
//     instantiated exactly once per process. The goCache.Cache inside it, and
//     its single background cleanup goroutine, are also allocated exactly once.
const meterCacheTTL = 10 * time.Minute

// MeterUsageTrackingService handles meter-level usage tracking.
// Unlike FeatureUsageTrackingService, this skips subscription/feature/price resolution.
// It matches events to meters, extracts quantity, and writes to the meter_usage table.
type MeterUsageTrackingService interface {
	// PublishEvent publishes an event for meter usage tracking
	PublishEvent(ctx context.Context, event *events.Event) error

	// RegisterHandler registers the consumer handler with the router
	RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration)
}

type meterUsageTrackingService struct {
	ServiceParams
	pubSub              pubsub.PubSub
	meterUsageRepo      events.MeterUsageRepository
	expressionEvaluator expression.Evaluator
	// meterListCache is a dedicated in-memory cache for meter lists keyed by
	// "tenantID:environmentID:eventName". It is intentionally separate from the
	// global cache so it is always in-memory (fast) and unaffected by the
	// global cache.Type config (which may be Redis). Meters are immutable after
	// creation so no active invalidation is required.
	meterListCache *goCache.Cache
}

// NewMeterUsageTrackingService creates a new meter usage tracking service
func NewMeterUsageTrackingService(
	params ServiceParams,
	meterUsageRepo events.MeterUsageRepository,
) MeterUsageTrackingService {
	svc := &meterUsageTrackingService{
		ServiceParams:       params,
		meterUsageRepo:      meterUsageRepo,
		expressionEvaluator: expression.NewCELEvaluator(),
		meterListCache:      goCache.New(meterCacheTTL, 2*meterCacheTTL),
	}

	ps, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.MeterUsageTracking.ConsumerGroup,
	)
	if err != nil {
		params.Logger.Fatalw("failed to create pubsub for meter usage tracking", "error", err)
		return nil
	}
	svc.pubSub = ps

	return svc
}

// PublishEvent publishes an event to the meter usage tracking topic
func (s *meterUsageTrackingService) PublishEvent(ctx context.Context, event *events.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event for meter usage tracking: %w", err)
	}

	// Deterministic partition key: tenant + customer
	partitionKey := event.TenantID
	if event.ExternalCustomerID != "" {
		partitionKey = fmt.Sprintf("%s:%s", event.TenantID, event.ExternalCustomerID)
	}

	uniqueID := fmt.Sprintf("%s-%d-%d", event.ID, time.Now().UnixNano(), rand.Int63())
	msg := message.NewMessage(uniqueID, payload)
	msg.Metadata.Set("tenant_id", event.TenantID)
	msg.Metadata.Set("environment_id", event.EnvironmentID)
	msg.Metadata.Set("partition_key", partitionKey)

	topic := s.Config.MeterUsageTracking.Topic
	if err := s.pubSub.Publish(ctx, topic, msg); err != nil {
		return fmt.Errorf("failed to publish event for meter usage tracking: %w", err)
	}

	return nil
}

// RegisterHandler registers the consumer with throttle middleware
func (s *meterUsageTrackingService) RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration) {
	if !cfg.MeterUsageTracking.Enabled {
		s.Logger.Infow("meter usage tracking handler disabled by configuration")
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

	s.Logger.Infow("registered meter usage tracking handler",
		"topic", cfg.MeterUsageTracking.Topic,
		"rate_limit", cfg.MeterUsageTracking.RateLimit,
	)
}

// processMessage unmarshals the Kafka message and delegates to processEvent
func (s *meterUsageTrackingService) processMessage(msg *message.Message) error {
	tenantID := msg.Metadata.Get("tenant_id")
	environmentID := msg.Metadata.Get("environment_id")

	var event events.Event
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		s.Logger.Errorw("failed to unmarshal event for meter usage tracking",
			"error", err,
			"message_uuid", msg.UUID,
		)
		return nil // non-retriable
	}

	if tenantID == "" && event.TenantID != "" {
		tenantID = event.TenantID
	}
	if environmentID == "" && event.EnvironmentID != "" {
		environmentID = event.EnvironmentID
	}

	if tenantID == "" || environmentID == "" {
		s.Logger.Errorw("tenant_id and environment_id are required for meter usage tracking",
			"event_id", event.ID,
			"tenant_id", tenantID,
			"environment_id", environmentID,
		)
		return nil // non-retriable
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)

	if err := s.processEvent(ctx, &event); err != nil {
		s.Logger.Errorw("failed to process event for meter usage tracking",
			"error", err,
			"event_id", event.ID,
		)
		return err // retriable
	}

	return nil
}

// getMetersForEvent returns meters matching the given event name.
// Results are cached in-process for meterCacheTTL to avoid a Postgres round-trip
// on every Kafka message. The cache is nil-safe: if the service was constructed
// without a cache (e.g. directly in unit tests) it falls through to the repo.
func (s *meterUsageTrackingService) getMetersForEvent(ctx context.Context, eventName string) ([]*meter.Meter, error) {
	if s.meterListCache != nil {
		tenantID := types.GetTenantID(ctx)
		environmentID := types.GetEnvironmentID(ctx)
		cacheKey := tenantID + ":" + environmentID + ":" + eventName

		if cached, ok := s.meterListCache.Get(cacheKey); ok {
			return cached.([]*meter.Meter), nil
		}

		meterFilter := types.NewNoLimitMeterFilter()
		meterFilter.EventName = eventName

		meters, err := s.MeterRepo.List(ctx, meterFilter)
		if err != nil {
			return nil, err
		}

		// Only cache non-empty results. Caching empty slices for event names that
		// have no matching meters would cause unbounded cache growth when the
		// consumer receives high-cardinality event names. Unknown event names are
		// cheap to query (indexed, returns zero rows quickly).
		//
		// goCache.DefaultExpiration (0) means "use the TTL set at New() time",
		// i.e. meterCacheTTL. The stored slice is never mutated after insertion so
		// concurrent reads do not need additional synchronisation.
		if len(meters) > 0 {
			s.meterListCache.Set(cacheKey, meters, goCache.DefaultExpiration)
		}
		return meters, nil
	}

	meterFilter := types.NewNoLimitMeterFilter()
	meterFilter.EventName = eventName
	return s.MeterRepo.List(ctx, meterFilter)
}

// processEvent matches an event to meters and writes meter_usage records.
// No subscription/feature/price resolution needed.
func (s *meterUsageTrackingService) processEvent(ctx context.Context, event *events.Event) error {
	// Step 1: Lookup meters by event name (cache-first)
	meters, err := s.getMetersForEvent(ctx, event.EventName)
	if err != nil {
		return fmt.Errorf("failed to list meters for event %s: %w", event.EventName, err)
	}

	if len(meters) == 0 {
		s.Logger.Debugw("no meters found for event name, skipping",
			"event_id", event.ID,
			"event_name", event.EventName,
		)
		return nil
	}

	// Step 2: Match meters by filters, dedup check, and build usage records
	records := make([]*events.MeterUsage, 0, len(meters))
	for _, m := range meters {
		if !s.checkMeterFilters(event, m.Filters) {
			continue
		}

		qty, err := s.extractQuantity(event, m)
		if err != nil {
			s.Logger.Errorw("failed to extract quantity, skipping meter",
				"event_id", event.ID,
				"meter_id", m.ID,
				"error", err,
			)
			continue
		}

		if qty.IsNegative() {
			s.Logger.Warnw("negative quantity, setting to zero",
				"event_id", event.ID,
				"meter_id", m.ID,
			)
			qty = decimal.Zero
		}

		uniqueHash := s.generateUniqueHash(event, m)

		records = append(records, &events.MeterUsage{
			Event:      *event,
			MeterID:    m.ID,
			QtyTotal:   qty,
			UniqueHash: uniqueHash,
		})
	}

	if len(records) == 0 {
		return nil
	}

	// Clear properties for tenants not in the allowlist
	if !s.isPropertiesEnabled(ctx) {
		for _, rec := range records {
			rec.Properties = nil
		}
	}

	// Step 3: Bulk insert
	if err := s.meterUsageRepo.BulkInsertMeterUsage(ctx, records); err != nil {
		return fmt.Errorf("failed to bulk insert meter usage: %w", err)
	}

	s.Logger.Debugw("meter usage records inserted",
		"event_id", event.ID,
		"count", len(records),
	)

	return nil
}

// isPropertiesEnabled checks if the current tenant is in the properties allowlist
func (s *meterUsageTrackingService) isPropertiesEnabled(ctx context.Context) bool {
	tenantID, _ := ctx.Value(types.CtxTenantID).(string)
	for _, t := range s.Config.MeterUsageTracking.PropertiesEnabledTenants {
		if t == tenantID {
			return true
		}
	}
	return false
}

// checkMeterFilters validates that all meter filters match the event properties
func (s *meterUsageTrackingService) checkMeterFilters(event *events.Event, filters []meter.Filter) bool {
	if len(filters) == 0 {
		return true
	}

	for _, filter := range filters {
		propertyValue, exists := event.Properties[filter.Key]
		if !exists {
			return false
		}

		propStr := fmt.Sprintf("%v", propertyValue)
		if !lo.Contains(filter.Values, propStr) {
			return false
		}
	}

	return true
}

// generateUniqueHash returns a SHA-256 hex string used for deduplication.
// Two cases:
//  1. COUNT_UNIQUE: hash(eventName + fieldName + fieldValue) — two events with
//     the same field value produce the same hash and are deduplicated.
//  2. All other types: the hash will be empty
func (s *meterUsageTrackingService) generateUniqueHash(event *events.Event, m *meter.Meter) string {
	if m.Aggregation.Type == types.AggregationCountUnique && m.Aggregation.Field != "" {
		if fieldValue, ok := event.Properties[m.Aggregation.Field]; ok {
			hashStr := fmt.Sprintf("%s:%s:%v", event.EventName, m.Aggregation.Field, fieldValue)
			hash := sha256.Sum256([]byte(hashStr))
			return hex.EncodeToString(hash[:])
		}
	}

	return ""
}

// extractQuantity extracts the quantity from event properties based on the meter's aggregation config.
// Simplified version: no subscription or period needed.
func (s *meterUsageTrackingService) extractQuantity(event *events.Event, m *meter.Meter) (decimal.Decimal, error) {
	// CEL expression evaluation
	if m.Aggregation.Expression != "" {
		if m.Aggregation.Type == types.AggregationCountUnique {
			return decimal.Zero, fmt.Errorf("expression not supported with COUNT_UNIQUE")
		}

		qty, err := s.expressionEvaluator.EvaluateQuantity(m.Aggregation.Expression, event.Properties)
		if err != nil {
			return decimal.Zero, fmt.Errorf("CEL evaluation failed for event %s meter %s: %w", event.ID, m.ID, err)
		}
		if m.Aggregation.Multiplier != nil {
			qty = qty.Mul(*m.Aggregation.Multiplier)
		}
		return qty, nil
	}

	switch m.Aggregation.Type {
	case types.AggregationCount:
		return decimal.NewFromInt(1), nil

	case types.AggregationSum, types.AggregationAvg, types.AggregationLatest, types.AggregationMax:
		if m.Aggregation.Field == "" {
			return decimal.Zero, nil
		}
		val, ok := event.Properties[m.Aggregation.Field]
		if !ok {
			return decimal.Zero, nil
		}
		return s.convertToDecimal(val), nil

	case types.AggregationSumWithMultiplier:
		if m.Aggregation.Field == "" || m.Aggregation.Multiplier == nil {
			return decimal.Zero, nil
		}
		val, ok := event.Properties[m.Aggregation.Field]
		if !ok {
			return decimal.Zero, nil
		}
		return s.convertToDecimal(val).Mul(*m.Aggregation.Multiplier), nil

	case types.AggregationCountUnique:
		if m.Aggregation.Field == "" {
			return decimal.Zero, nil
		}
		if _, ok := event.Properties[m.Aggregation.Field]; !ok {
			return decimal.Zero, nil
		}
		return decimal.NewFromInt(1), nil

	case types.AggregationWeightedSum:
		if m.Aggregation.Field == "" {
			return decimal.Zero, nil
		}
		val, ok := event.Properties[m.Aggregation.Field]
		if !ok {
			return decimal.Zero, nil
		}
		return s.convertToDecimal(val), nil

	default:
		s.Logger.Warnw("unsupported aggregation type for meter usage",
			"meter_id", m.ID,
			"aggregation_type", m.Aggregation.Type,
		)
		return decimal.Zero, nil
	}
}

// convertToDecimal converts a property value to decimal
func (s *meterUsageTrackingService) convertToDecimal(val interface{}) decimal.Decimal {
	switch v := val.(type) {
	case float64:
		return decimal.NewFromFloat(v)
	case float32:
		return decimal.NewFromFloat32(v)
	case int:
		return decimal.NewFromInt(int64(v))
	case int64:
		return decimal.NewFromInt(v)
	case int32:
		return decimal.NewFromInt(int64(v))
	case uint:
		return decimal.NewFromInt(int64(v))
	case uint64:
		d, err := decimal.NewFromString(fmt.Sprintf("%d", v))
		if err != nil {
			return decimal.Zero
		}
		return d
	case string:
		d, err := decimal.NewFromString(v)
		if err != nil {
			return decimal.Zero
		}
		return d
	case json.Number:
		d, err := decimal.NewFromString(string(v))
		if err != nil {
			return decimal.Zero
		}
		return d
	default:
		return decimal.Zero
	}
}
