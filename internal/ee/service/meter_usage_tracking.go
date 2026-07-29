package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/expression"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/pubsub/kafka"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const eventDeduplicationLockTTL = 24 * time.Hour

// MeterUsageTrackingService handles meter-level usage tracking.
// Unlike FeatureUsageTrackingService, this skips subscription/feature/price resolution.
// It matches events to meters, extracts quantity, and writes to the meter_usage table.
type MeterUsageTrackingService interface {
	// PublishEvent publishes an event for meter usage tracking
	PublishEvent(ctx context.Context, event *events.Event) error

	// RegisterHandler registers the consumer handler with the router
	RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration)

	// RegisterHandlerLazy registers a dedicated consumer for the events_lazy
	// topic (lazy-mode tenants — see kafka.RouteTenantsOnLazyMode).
	RegisterHandlerLazy(router *pubsubRouter.Router, cfg *config.Configuration)

	// RegisterBulkHandler registers a batch-mode consumer that reads
	// RawEventBatch messages published by the bulk-ingest API path and
	// bulk-inserts meter_usage records for every event in one call.
	RegisterBulkHandler(router *pubsubRouter.Router, cfg *config.Configuration)
}

type meterUsageTrackingService struct {
	ServiceParams
	pubSub              pubsub.PubSub
	lazyPubSub          pubsub.PubSub
	bulkPubSub          pubsub.PubSub
	meterUsageRepo      events.MeterUsageRepository
	expressionEvaluator expression.Evaluator
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
	}

	ps, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.MeterUsageTracking.ConsumerGroup,
	)
	if err != nil {
		params.Logger.Fatal(context.Background(), "failed to create pubsub for meter usage tracking", "error", err)
		return nil
	}
	svc.pubSub = ps

	lazyPS, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.MeterUsageTrackingLazy.ConsumerGroup,
	)
	if err != nil {
		params.Logger.Fatal(context.Background(), "failed to create lazy pubsub for meter usage tracking", "error", err)
		return nil
	}
	svc.lazyPubSub = lazyPS

	bulkPS, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.BulkMeterUsageTracking.ConsumerGroup,
	)
	if err != nil {
		params.Logger.Fatal(context.Background(), "failed to create bulk pubsub for meter usage tracking", "error", err)
		return nil
	}
	svc.bulkPubSub = bulkPS

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
		s.Logger.Info(context.Background(), "meter usage tracking handler disabled by configuration")
		return
	}

	throttle := middleware.NewThrottle(cfg.MeterUsageTracking.RateLimit, time.Second)

	router.AddNoPublishHandler(
		"meter_usage_tracking_handler",
		cfg.MeterUsageTracking.Topic,
		cfg.MeterUsageTracking.TopicDLQ,
		s.pubSub,
		s.processMessage,
		throttle.Middleware,
	)

	s.Logger.Info(context.Background(), "registered meter usage tracking handler",
		"topic", cfg.MeterUsageTracking.Topic,
		"rate_limit", cfg.MeterUsageTracking.RateLimit,
	)
}

// RegisterHandlerLazy registers a separate consumer for the events_lazy topic.
// Same processMessage logic as RegisterHandler; the split lets lazy-mode tenant
// traffic flow through its own topic + consumer group so it can't starve or
// be starved by the normal stream. Mirrors the pattern in
// FeatureUsageTrackingService and CostSheetUsageTrackingService.
func (s *meterUsageTrackingService) RegisterHandlerLazy(router *pubsubRouter.Router, cfg *config.Configuration) {
	if !cfg.MeterUsageTrackingLazy.Enabled {
		s.Logger.Info(context.Background(), "meter usage tracking lazy handler disabled by configuration")
		return
	}

	throttle := middleware.NewThrottle(cfg.MeterUsageTrackingLazy.RateLimit, time.Second)

	router.AddNoPublishHandler(
		"meter_usage_tracking_lazy_handler",
		cfg.MeterUsageTrackingLazy.Topic,
		cfg.MeterUsageTrackingLazy.TopicDLQ,
		s.lazyPubSub,
		s.processMessage,
		throttle.Middleware,
	)

	s.Logger.Info(context.Background(), "registered meter usage tracking lazy handler",
		"topic", cfg.MeterUsageTrackingLazy.Topic,
		"rate_limit", cfg.MeterUsageTrackingLazy.RateLimit,
	)
}

// RegisterBulkHandler subscribes the bulk consumer group to the bulk-events
// topic. One Kafka message = one RawEventBatch = one bulk-insert into
// meter_usage. Runs alongside RegisterHandler(Lazy); the two groups process
// disjoint topics so bulk traffic can't starve the single-event stream.
func (s *meterUsageTrackingService) RegisterBulkHandler(router *pubsubRouter.Router, cfg *config.Configuration) {
	if !cfg.BulkMeterUsageTracking.Enabled {
		s.Logger.Info(context.Background(), "bulk meter usage tracking handler disabled by configuration")
		return
	}

	throttle := middleware.NewThrottle(cfg.BulkMeterUsageTracking.RateLimit, time.Second)

	dlq := cfg.BulkMeterUsageTracking.TopicDLQ
	if dlq == "" {
		dlq = cfg.Kafka.TopicDLQ
	}

	router.AddNoPublishHandler(
		"bulk_meter_usage_tracking_handler",
		cfg.BulkMeterUsageTracking.Topic,
		dlq,
		s.bulkPubSub,
		s.processBulkMessage,
		throttle.Middleware,
	)

	s.Logger.Info(context.Background(), "registered bulk meter usage tracking handler",
		"topic", cfg.BulkMeterUsageTracking.Topic,
		"consumer_group", cfg.BulkMeterUsageTracking.ConsumerGroup,
		"rate_limit", cfg.BulkMeterUsageTracking.RateLimit,
	)
}

// processBulkMessage unmarshals a RawEventBatch, matches every event to its
// meters, and issues a single BulkInsertMeterUsage. Malformed rows inside the
// batch are skipped so healthy siblings still land in ClickHouse.
func (s *meterUsageTrackingService) processBulkMessage(ctx context.Context, msg *message.Message) (err error) {
	var batch events.EventBatch
	if err := json.Unmarshal(msg.Payload, &batch); err != nil {
		s.Logger.Error(ctx, "failed to unmarshal bulk meter usage batch",
			"error", err,
			"message_uuid", msg.UUID,
		)
		return fmt.Errorf("non-retriable unmarshal error: %w", err)
	}

	if len(batch.Events) == 0 {
		return nil
	}

	tenantID := batch.TenantID
	environmentID := batch.EnvironmentID
	if tenantID != "" {
		ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	}
	if environmentID != "" {
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
	}

	// Per-event Redis dedup mirrors the single-event path: acquire an event-ID
	// keyed lock (TTL = 24h) before doing any work for that event, skip events
	// whose lock is already held, and release every lock we acquired if the
	// batch fails downstream so redelivery isn't dedup-skipped until TTL.
	// Gated by BulkMeterUsageTracking.RedisDeduplicationEnabled so the bulk
	// consumer can toggle dedup independently of the single-event stream.
	dedupEnabled := s.Config.BulkMeterUsageTracking.RedisDeduplicationEnabled && s.ServiceParams.Locker != nil
	var dedupLocks []cache.Lock
	defer func() {
		if err == nil {
			return
		}
		for _, lock := range dedupLocks {
			if releaseErr := lock.Release(ctx); releaseErr != nil {
				s.Logger.Error(ctx, "failed to release lock on bulk meter usage tracking event", "error", releaseErr)
			}
		}
	}()

	records := make([]*events.MeterUsage, 0, len(batch.Events))
	eventsToInsert := make([]*events.Event, 0, len(batch.Events))
	for _, evt := range batch.Events {
		if evt == nil {
			continue
		}
		if evt.TenantID == "" {
			evt.TenantID = tenantID
		}
		if evt.EnvironmentID == "" {
			evt.EnvironmentID = environmentID
		}
		evt.EventName = strings.TrimSpace(evt.EventName)

		if dedupEnabled && evt.ID != "" && !evt.ForceReprocess {
			cacheKey := cache.GenerateKey(ctx, cache.PrefixEvent, evt.ID)
			lock, lockErr := s.ServiceParams.Locker.AcquireLock(ctx, cacheKey, eventDeduplicationLockTTL)
			if lockErr != nil {
				// Fail open: log and continue processing the event so a Redis
				// blip can't silently drop billing rows.
				s.Logger.Error(ctx, "failed to acquire lock on bulk meter usage tracking event", "error", lockErr, "event_id", evt.ID)
			} else if !lock.AcquiredSuccessfully() {
				s.Logger.Info(ctx, "event already processed, skipping in bulk batch", "event_id", evt.ID)
				continue
			} else {
				dedupLocks = append(dedupLocks, lock)
			}
		}

		meters, err := s.MeterRepo.GetMatchingMetersByEventName(ctx, evt.EventName)
		if err != nil {
			// Meter-lookup errors are retriable: return so the whole batch is
			// redelivered rather than silently dropping downstream billing rows.
			return fmt.Errorf("list meters for event %s: %w", evt.EventName, err)
		}
		if len(meters) == 0 {
			continue
		}

		eventProducedRecord := false
		for _, m := range meters {
			if !s.checkMeterFilters(evt, m.Filters) {
				continue
			}
			qty, err := s.extractQuantity(evt, m)
			if err != nil {
				s.Logger.Error(ctx, "failed to extract quantity, skipping meter",
					"event_id", evt.ID,
					"meter_id", m.ID,
					"error", err,
				)
				continue
			}
			if qty.IsNegative() {
				qty = decimal.Zero
			}
			records = append(records, &events.MeterUsage{
				Event:      *evt,
				MeterID:    m.ID,
				QtyTotal:   qty,
				UniqueHash: s.generateUniqueHash(evt, m),
			})
			eventProducedRecord = true
		}
		if eventProducedRecord {
			eventsToInsert = append(eventsToInsert, evt)
		}
	}

	if len(records) == 0 {
		s.Logger.Debug(ctx, "bulk meter usage batch produced zero records, skipping",
			"batch_size", len(batch.Events),
			"message_uuid", msg.UUID,
		)
		return nil
	}

	if err := s.meterUsageRepo.BulkInsertMeterUsage(ctx, records); err != nil {
		s.Logger.Error(ctx, "bulk meter usage insert failed",
			"error", err,
			"record_count", len(records),
			"message_uuid", msg.UUID,
		)
		return fmt.Errorf("bulk insert meter usage: %w", err)
	}

	s.Logger.Debug(ctx, "bulk meter usage batch inserted",
		"record_count", len(records),
		"batch_size", len(batch.Events),
		"message_uuid", msg.UUID,
	)

	if s.Config.BulkMeterUsageTracking.PostInsertSideEffectsEnabled {
		s.runBulkMeterUsagePostInsertSideEffects(ctx, eventsToInsert)
	}
	return nil
}

// processMessage unmarshals the Kafka message and delegates to processEvent
func (s *meterUsageTrackingService) processMessage(ctx context.Context, msg *message.Message) error {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	var event events.Event
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		s.Logger.Error(ctx, "failed to unmarshal event for meter usage tracking",
			"error", err,
			"message_uuid", msg.UUID,
		)
		return nil // non-retriable
	}

	if tenantID == "" && event.TenantID != "" {
		tenantID = event.TenantID
		ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	}
	if environmentID == "" && event.EnvironmentID != "" {
		environmentID = event.EnvironmentID
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
	}

	event.EventName = strings.TrimSpace(event.EventName)

	if tenantID == "" || environmentID == "" {
		s.Logger.Info(ctx, "tenant_id and environment_id are required for meter usage tracking",
			"event_id", event.ID,
			"tenant_id", tenantID,
			"environment_id", environmentID,
		)
		return nil // non-retriable
	}

	// if force reprocess is true, then we need to process the event even if it has already been processed
	if msg.Metadata.Get("force_reprocess") == "true" {
		event.ForceReprocess = true
	}

	if err := s.processEvent(ctx, &event); err != nil {
		s.Logger.Error(ctx, "failed to process event for meter usage tracking",
			"error", err,
			"event_id", event.ID,
		)
		return err // retriable
	}

	return nil
}

// processEvent matches an event to meters and writes meter_usage records.
// No subscription/feature/price resolution needed.
func (s *meterUsageTrackingService) processEvent(ctx context.Context, event *events.Event) (err error) {
	// Step 0: Check if the event is already processed
	if event.ID != "" &&
		!event.ForceReprocess &&
		s.Config.MeterUsageTracking.RedisDeduplicationEnabled &&
		s.ServiceParams.Locker != nil {
		eventId := event.ID
		cacheKey := cache.GenerateKey(ctx, cache.PrefixEvent, eventId)
		lock, lockErr := s.ServiceParams.Locker.AcquireLock(ctx, cacheKey, eventDeduplicationLockTTL)
		if lockErr != nil {
			s.Logger.Error(ctx, "failed to acquire lock on meter usage tracking event", "error", lockErr, "event_id", eventId)
		} else {
			if !lock.AcquiredSuccessfully() {
				s.Logger.Info(ctx, "event already processed, skipping", "event_id", eventId)
				return nil
			}

			// Release the dedup lock on any processing failure below so retries
			// aren't dedup-skipped until TTL. `err` here is the named return —
			// it captures failures from further processing, not just the AcquireLock result.
			defer func() {
				if err != nil {
					releaseErr := lock.Release(ctx)
					if releaseErr != nil {
						s.Logger.Error(ctx, "failed to release lock on meter usage tracking event", "error", releaseErr, "event_id", eventId)
					}
				}
			}()
		}
	}

	// Step 1: Lookup meters by event name (cache-first)
	meters, err := s.MeterRepo.GetMatchingMetersByEventName(ctx, event.EventName)
	if err != nil {
		return fmt.Errorf("failed to list meters for event %s: %w", event.EventName, err)
	}

	if len(meters) == 0 {
		s.Logger.Debug(ctx, "no meters found for event name, skipping",
			"event_id", event.ID,
			"event_name", event.EventName,
		)
		// fall through — the record loop is a no-op and the len(records)==0
		// check below fires the webhook (single call site).
	}

	// Step 2: Match meters by filters, dedup check, and build usage records
	records := make([]*events.MeterUsage, 0, len(meters))
	for _, m := range meters {
		if !s.checkMeterFilters(event, m.Filters) {
			continue
		}

		qty, err := s.extractQuantity(event, m)
		if err != nil {
			s.Logger.Error(ctx, "failed to extract quantity, skipping meter",
				"event_id", event.ID,
				"meter_id", m.ID,
				"error", err,
			)
			continue
		}

		if qty.IsNegative() {
			s.Logger.Info(ctx, "negative quantity, setting to zero",
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
		reason := types.RejectedEventReasonNoMatchingMeter
		if len(meters) == 0 {
			reason = types.RejectedEventReasonNoMeterForName
		}
		s.publishRejectedEventWebhook(ctx, event, reason)
		return nil
	}

	// Step 3: Bulk insert
	if err := s.meterUsageRepo.BulkInsertMeterUsage(ctx, records); err != nil {
		return fmt.Errorf("failed to bulk insert meter usage: %w", err)
	}

	s.Logger.Debug(ctx, "meter usage records inserted",
		"event_id", event.ID,
		"count", len(records),
	)

	// Step 4: Run post-insert side effects
	// This includes:
	// 1. Resolving the customer for the event
	// 2. Running the customer onboarding workflow
	// 3. Checking for spend breaches and logging alerts
	s.runMeterUsagePostInsertSideEffects(ctx, event, records)

	return nil
}

// publishRejectedEventWebhook fires the event.rejected webhook when an event
// produced no meter usage. Opt-in, best-effort (failures are logged, never
// returned), and throttled to once per window per (tenant, env, event_name).
func (s *meterUsageTrackingService) publishRejectedEventWebhook(ctx context.Context, event *events.Event, reason types.RejectedEventReason) {
	if !s.Config.MeterUsageTracking.RejectedEventWebhookEnabled || s.WebhookPublisher == nil {
		return
	}

	// Throttle via a name-keyed lock (distinct from the event-ID dedup lock). The
	// lock is never released — it expires by TTL, which is the window. No Locker =
	// fail open (publish every time).
	if window := s.Config.MeterUsageTracking.RejectedEventWebhookWindow; window > 0 && s.Locker != nil {
		throttleKey := cache.GenerateKey(ctx, cache.PrefixEvent, "rejected_webhook", event.EventName)
		lock, err := s.Locker.AcquireLock(ctx, throttleKey, window)
		if err != nil {
			s.Logger.Error(ctx, "failed to acquire throttle lock for rejected event webhook",
				"error", err, "event_name", event.EventName)
		} else if !lock.AcquiredSuccessfully() {
			return // already fired within the window
		}
	}

	internal := webhookDto.InternalRejectedEvent{
		Reason: reason,
		Event: webhookDto.RejectedEventData{
			ID:                 event.ID,
			EventName:          event.EventName,
			CustomerID:         event.CustomerID,
			ExternalCustomerID: event.ExternalCustomerID,
			Source:             event.Source,
			Properties:         event.Properties,
			Timestamp:          event.Timestamp,
		},
	}

	webhookPayload, err := json.Marshal(internal)
	if err != nil {
		s.Logger.Error(ctx, "failed to marshal rejected event webhook payload",
			"error", err, "event_id", event.ID)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SYSTEM_EVENT),
		EventName:     types.WebhookEventEventRejected,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
		EntityType:    types.SystemEntityTypeEvent,
		EntityID:      event.ID,
	}

	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.Error(ctx, "failed to publish rejected event webhook",
			"error", err, "event_id", event.ID, "event_name", event.EventName)
	}
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
//  2. All other types: hash(eventName + eventID) — every distinct event is unique.
func (s *meterUsageTrackingService) generateUniqueHash(event *events.Event, m *meter.Meter) string {
	var hashStr string

	if m.Aggregation.Type == types.AggregationCountUnique {
		if fieldValue, ok := event.Properties[m.Aggregation.Field]; ok {
			hashStr = fmt.Sprintf("%s:%s:%v", event.EventName, m.Aggregation.Field, fieldValue)
		}
	}

	if hashStr == "" {
		return ""
	}

	hash := sha256.Sum256([]byte(hashStr))
	return hex.EncodeToString(hash[:])
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
		s.Logger.Info(context.Background(), "unsupported aggregation type for meter usage",
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
