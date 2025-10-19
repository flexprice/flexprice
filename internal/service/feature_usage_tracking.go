package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/pubsub/kafka"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// FeatureUsageTrackingService handles feature usage tracking operations for metered events
type FeatureUsageTrackingService interface {
	// Publish an event for feature usage tracking
	PublishEvent(ctx context.Context, event *events.Event, isBackfill bool) error

	// Register message handler with the router
	RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration)

	// Register message handler with the router
	RegisterHandlerLazy(router *pubsubRouter.Router, cfg *config.Configuration)

	// Get detailed usage analytics with filtering, grouping, and time-series data
	GetDetailedUsageAnalytics(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error)

	// Reprocess events for a specific customer or with other filters
	ReprocessEvents(ctx context.Context, params *events.ReprocessEventsParams) error
}

type featureUsageTrackingService struct {
	ServiceParams
	pubSub           pubsub.PubSub // Regular PubSub for normal processing
	backfillPubSub   pubsub.PubSub // Dedicated Kafka PubSub for backfill processing
	lazyPubSub       pubsub.PubSub // Dedicated Kafka PubSub for lazy processing
	eventRepo        events.Repository
	featureUsageRepo events.FeatureUsageRepository
}

// NewFeatureUsageTrackingService creates a new feature usage tracking service
func NewFeatureUsageTrackingService(
	params ServiceParams,
	eventRepo events.Repository,
	featureUsageRepo events.FeatureUsageRepository,
) FeatureUsageTrackingService {
	ev := &featureUsageTrackingService{
		ServiceParams:    params,
		eventRepo:        eventRepo,
		featureUsageRepo: featureUsageRepo,
	}

	pubSub, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.FeatureUsageTracking.ConsumerGroup,
	)

	if err != nil {
		params.Logger.Fatalw("failed to create pubsub", "error", err)
		return nil
	}
	ev.pubSub = pubSub

	backfillPubSub, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.FeatureUsageTracking.ConsumerGroupBackfill,
	)
	if err != nil {
		params.Logger.Fatalw("failed to create backfill pubsub", "error", err)
		return nil
	}
	ev.backfillPubSub = backfillPubSub

	lazyPubSub, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.FeatureUsageTrackingLazy.ConsumerGroup,
	)

	if err != nil {
		params.Logger.Fatalw("failed to create lazy pubsub", "error", err)
		return nil
	}
	ev.lazyPubSub = lazyPubSub

	return ev
}

// PublishEvent publishes an event to the feature usage tracking topic
func (s *featureUsageTrackingService) PublishEvent(ctx context.Context, event *events.Event, isBackfill bool) error {
	// Create message payload
	payload, err := json.Marshal(event)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to marshal event for feature usage tracking").
			Mark(ierr.ErrValidation)
	}

	// Create a deterministic partition key based on tenant_id and external_customer_id
	// This ensures all events for the same customer go to the same partition
	partitionKey := event.TenantID
	if event.ExternalCustomerID != "" {
		partitionKey = fmt.Sprintf("%s:%s", event.TenantID, event.ExternalCustomerID)
	}

	// Make UUID truly unique by adding nanosecond precision timestamp and random bytes
	uniqueID := fmt.Sprintf("%s-%d-%d", event.ID, time.Now().UnixNano(), rand.Int63())

	// Use the partition key as the message ID to ensure consistent partitioning
	msg := message.NewMessage(uniqueID, payload)

	// Set metadata for additional context
	msg.Metadata.Set("tenant_id", event.TenantID)
	msg.Metadata.Set("environment_id", event.EnvironmentID)
	msg.Metadata.Set("partition_key", partitionKey)

	if isBackfill {
		pubSub := s.backfillPubSub
		topic := s.Config.FeatureUsageTracking.TopicBackfill

		if pubSub == nil {
			return ierr.NewError("pubsub not initialized").
				WithHint("Please check the config").
				Mark(ierr.ErrSystem)
		}

		s.Logger.Debugw("publishing event for feature usage tracking",
			"event_id", event.ID,
			"event_name", event.EventName,
			"partition_key", partitionKey,
			"topic", topic,
		)

		// Publish to feature usage tracking topic using the backfill PubSub (Kafka)
		if err := pubSub.Publish(ctx, topic, msg); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to publish event for feature usage tracking").
				Mark(ierr.ErrSystem)
		}
	}
	return nil
}

// RegisterHandler registers a handler for the feature usage tracking topic with rate limiting
func (s *featureUsageTrackingService) RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration) {
	// Add throttle middleware to this specific handler
	throttle := middleware.NewThrottle(cfg.FeatureUsageTracking.RateLimit, time.Second)

	// Add the handler
	router.AddNoPublishHandler(
		"feature_usage_tracking_handler",
		cfg.FeatureUsageTracking.Topic,
		s.pubSub,
		s.processMessage,
		throttle.Middleware,
	)

	s.Logger.Infow("registered event feature usage tracking handler",
		"topic", cfg.FeatureUsageTracking.Topic,
		"rate_limit", cfg.FeatureUsageTracking.RateLimit,
	)

	// Add backfill handler
	if cfg.FeatureUsageTracking.TopicBackfill == "" {
		s.Logger.Warnw("backfill topic not set, skipping backfill handler")
		return
	}

	backfillThrottle := middleware.NewThrottle(cfg.FeatureUsageTracking.RateLimitBackfill, time.Second)
	router.AddNoPublishHandler(
		"feature_usage_tracking_backfill_handler",
		cfg.FeatureUsageTracking.TopicBackfill,
		s.backfillPubSub, // Use the dedicated Kafka backfill PubSub
		s.processMessage,
		backfillThrottle.Middleware,
	)

	s.Logger.Infow("registered event feature usage tracking backfill handler",
		"topic", cfg.FeatureUsageTracking.TopicBackfill,
		"rate_limit", cfg.FeatureUsageTracking.RateLimitBackfill,
		"pubsub_type", "kafka",
	)
}

// RegisterHandler registers a handler for the feature usage tracking topic with rate limiting
func (s *featureUsageTrackingService) RegisterHandlerLazy(router *pubsubRouter.Router, cfg *config.Configuration) {
	// Add throttle middleware to this specific handler
	throttle := middleware.NewThrottle(cfg.FeatureUsageTrackingLazy.RateLimit, time.Second)

	// Add the handler
	router.AddNoPublishHandler(
		"feature_usage_tracking_lazy_handler",
		cfg.FeatureUsageTrackingLazy.Topic,
		s.lazyPubSub,
		s.processMessage,
		throttle.Middleware,
	)

	s.Logger.Infow("registered event feature usage tracking lazy handler",
		"topic", cfg.FeatureUsageTrackingLazy.Topic,
		"rate_limit", cfg.FeatureUsageTrackingLazy.RateLimit,
	)
}

// Process a single event message for feature usage tracking
func (s *featureUsageTrackingService) processMessage(msg *message.Message) error {
	// Extract tenant ID from message metadata
	partitionKey := msg.Metadata.Get("partition_key")
	tenantID := msg.Metadata.Get("tenant_id")
	environmentID := msg.Metadata.Get("environment_id")

	s.Logger.Debugw("processing event from message queue",
		"message_uuid", msg.UUID,
		"partition_key", partitionKey,
		"tenant_id", tenantID,
		"environment_id", environmentID,
	)

	// Create a background context with tenant ID
	ctx := context.Background()
	if tenantID != "" {
		ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	}

	if environmentID != "" {
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
	}

	// Unmarshal the event
	var event events.Event
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		s.Logger.Errorw("failed to unmarshal event for feature usage tracking",
			"error", err,
			"message_uuid", msg.UUID,
		)
		return nil // Don't retry on unmarshal errors
	}

	// validate tenant id
	if event.TenantID != tenantID {
		s.Logger.Errorw("invalid tenant id",
			"expected", tenantID,
			"actual", event.TenantID,
			"message_uuid", msg.UUID,
		)
		return nil // Don't retry on invalid tenant id
	}

	// Process the event
	if err := s.processEvent(ctx, &event); err != nil {
		s.Logger.Errorw("failed to process event for feature usage tracking",
			"error", err,
			"event_id", event.ID,
			"event_name", event.EventName,
		)
		return err // Return error for retry
	}

	s.Logger.Infow("event for feature usage tracking processed successfully",
		"event_id", event.ID,
		"event_name", event.EventName,
	)

	return nil
}

// Process a single event for feature usage tracking
func (s *featureUsageTrackingService) processEvent(ctx context.Context, event *events.Event) error {
	s.Logger.Debugw("processing event",
		"event_id", event.ID,
		"event_name", event.EventName,
		"external_customer_id", event.ExternalCustomerID,
		"ingested_at", event.IngestedAt,
	)

	featureUsage, err := s.prepareProcessedEvents(ctx, event)
	if err != nil {
		s.Logger.Errorw("failed to prepare feature usage",
			"error", err,
			"event_id", event.ID,
		)
		return err
	}

	if len(featureUsage) > 0 {
		if err := s.featureUsageRepo.BulkInsertProcessedEvents(ctx, featureUsage); err != nil {
			return err
		}
	}

	return nil
}

// Generate a unique hash for deduplication
// there are 2 cases:
// 1. event_name + event_id // for non COUNT_UNIQUE aggregation types
// 2. event_name + event_field_name + event_field_value // for COUNT_UNIQUE aggregation types
func (s *featureUsageTrackingService) generateUniqueHash(event *events.Event, meter *meter.Meter) string {
	hashStr := fmt.Sprintf("%s:%s", event.EventName, event.ID)

	// For meters with field-based aggregation, include the field value in the hash
	if meter.Aggregation.Type == types.AggregationCountUnique && meter.Aggregation.Field != "" {
		if fieldValue, ok := event.Properties[meter.Aggregation.Field]; ok {
			hashStr = fmt.Sprintf("%s:%s:%v", event.EventName, meter.Aggregation.Field, fieldValue)
		}
	}

	hash := sha256.Sum256([]byte(hashStr))
	return hex.EncodeToString(hash[:])
}

func (s *featureUsageTrackingService) prepareProcessedEvents(ctx context.Context, event *events.Event) ([]*events.FeatureUsage, error) {
	subscriptionService := NewSubscriptionService(s.ServiceParams)

	// Create a base processed event
	baseProcessedEvent := event.ToProcessedEvent()

	// Results slice - will contain either a single skipped event or multiple processed events
	results := make([]*events.FeatureUsage, 0)

	// CASE 1: Lookup customer
	customer, err := s.CustomerRepo.GetByLookupKey(ctx, event.ExternalCustomerID)
	if err != nil {
		s.Logger.Warnw("customer not found for event, skipping",
			"event_id", event.ID,
			"external_customer_id", event.ExternalCustomerID,
			"error", err,
		)
		// Simply skip the event if customer not found
		// TODO: add sentry span for customer not found
		return results, nil
	}

	// Set the customer ID in the event if it's not already set
	if event.CustomerID == "" {
		event.CustomerID = customer.ID
		baseProcessedEvent.CustomerID = customer.ID
	}

	// CASE 2: Get active subscriptions
	filter := types.NewSubscriptionFilter()
	filter.CustomerID = customer.ID
	filter.WithLineItems = true
	filter.Expand = lo.ToPtr(string(types.ExpandPrices) + "," + string(types.ExpandMeters) + "," + string(types.ExpandFeatures))
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}

	subscriptionsList, err := subscriptionService.ListSubscriptions(ctx, filter)
	if err != nil {
		s.Logger.Errorw("failed to get subscriptions",
			"event_id", event.ID,
			"customer_id", customer.ID,
			"error", err,
		)
		// TODO: add sentry span for failed to get subscriptions
		return results, err
	}

	subscriptions := subscriptionsList.Items
	if len(subscriptions) == 0 {
		s.Logger.Debugw("no active subscriptions found for customer, skipping",
			"event_id", event.ID,
			"customer_id", customer.ID,
		)
		// TODO: add sentry span for no active subscriptions found
		return results, nil
	}

	// Filter subscriptions to only include those that are active for the event timestamp
	validSubscriptions := make([]*dto.SubscriptionResponse, 0)
	for _, sub := range subscriptions {
		if s.isSubscriptionValidForEvent(sub, event) {
			validSubscriptions = append(validSubscriptions, sub)
		}
	}

	subscriptions = validSubscriptions
	if len(subscriptions) == 0 {
		s.Logger.Debugw("no subscriptions valid for event timestamp, skipping",
			"event_id", event.ID,
			"customer_id", customer.ID,
			"event_timestamp", event.Timestamp,
		)
		return results, nil
	}

	// Collect all price IDs and meter IDs from subscription line items
	priceIDs := make([]string, 0)
	meterIDs := make([]string, 0)
	subLineItemMap := make(map[string]*subscription.SubscriptionLineItem) // Map price_id -> line item

	// Extract price IDs and meter IDs from all subscription line items in a single pass
	for _, sub := range subscriptions {
		for _, item := range sub.LineItems {
			if !item.IsUsage() || !item.IsActive(event.Timestamp) {
				continue
			}

			subLineItemMap[item.PriceID] = item
			priceIDs = append(priceIDs, item.PriceID)
		}
	}

	// Remove duplicates
	priceIDs = lo.Uniq(priceIDs)

	// Fetch all prices in bulk
	priceFilter := types.NewNoLimitPriceFilter().
		WithPriceIDs(priceIDs).
		WithStatus(types.StatusPublished).
		WithExpand(string(types.ExpandMeters))

	prices, err := s.PriceRepo.List(ctx, priceFilter)
	if err != nil {
		s.Logger.Errorw("failed to get prices",
			"error", err,
			"event_id", event.ID,
			"price_count", len(priceIDs),
		)
		return results, err
	}

	// Build price map and collect meter IDs
	priceMap := make(map[string]*price.Price)
	for _, p := range prices {
		if !p.IsUsage() {
			continue
		}
		priceMap[p.ID] = p
		if p.MeterID != "" {
			meterIDs = append(meterIDs, p.MeterID)
		}
	}

	// Remove duplicate meter IDs
	meterIDs = lo.Uniq(meterIDs)

	// Fetch all meters in bulk
	meterFilter := types.NewNoLimitMeterFilter()
	meterFilter.MeterIDs = meterIDs

	meters, err := s.MeterRepo.List(ctx, meterFilter)
	if err != nil {
		s.Logger.Errorw("failed to get meters",
			"error", err,
			"event_id", event.ID,
			"meter_count", len(meterIDs),
		)
		return results, err
	}

	// Build meter map
	meterMap := make(map[string]*meter.Meter)
	for _, m := range meters {
		meterMap[m.ID] = m
	}

	// Build feature maps
	featureMap := make(map[string]*feature.Feature)      // Map feature_id -> feature
	featureMeterMap := make(map[string]*feature.Feature) // Map meter_id -> feature

	if len(meterMap) > 0 {
		featureFilter := types.NewNoLimitFeatureFilter()
		featureFilter.MeterIDs = lo.Keys(meterMap)
		features, err := s.FeatureRepo.List(ctx, featureFilter)
		if err != nil {
			s.Logger.Errorw("failed to get features",
				"error", err,
				"event_id", event.ID,
				"meter_count", len(meterMap),
			)
			// TODO: add sentry span for failed to get features
			return results, err
		}

		for _, f := range features {
			featureMap[f.ID] = f
			featureMeterMap[f.MeterID] = f
		}
	}

	// Process the event against each subscription
	featureUsagePerSub := make([]*events.FeatureUsage, 0)

	for _, sub := range subscriptions {
		// Calculate the period ID for this subscription (epoch-ms of period start)
		periodID, err := types.CalculatePeriodID(
			event.Timestamp,
			sub.StartDate,
			sub.CurrentPeriodStart,
			sub.CurrentPeriodEnd,
			sub.BillingAnchor,
			sub.BillingPeriodCount,
			sub.BillingPeriod,
		)
		if err != nil {
			s.Logger.Errorw("failed to calculate period id",
				"event_id", event.ID,
				"subscription_id", sub.ID,
				"error", err,
			)
			// TODO: add sentry span for failed to calculate period id
			continue
		}

		// Get active usage-based line items
		subscriptionLineItems := lo.Filter(sub.LineItems, func(item *subscription.SubscriptionLineItem, _ int) bool {
			return item.IsUsage() && item.IsActive(event.Timestamp)
		})

		if len(subscriptionLineItems) == 0 {
			s.Logger.Debugw("no active usage-based line items found for subscription",
				"event_id", event.ID,
				"subscription_id", sub.ID,
			)
			continue
		}

		// Collect relevant prices for matching
		prices := make([]*price.Price, 0, len(subscriptionLineItems))
		for _, item := range subscriptionLineItems {
			if price, ok := priceMap[item.PriceID]; ok {
				if event.Timestamp.Before(item.StartDate) || (!item.EndDate.IsZero() && event.Timestamp.After(item.EndDate)) {
					continue
				}
				prices = append(prices, price)
			} else {
				s.Logger.Warnw("price not found for subscription line item",
					"event_id", event.ID,
					"subscription_id", sub.ID,
					"line_item_id", item.ID,
					"price_id", item.PriceID,
				)
				// Skip this item but continue with others - don't fail the whole batch
				continue
			}
		}

		// Find meters and prices that match this event
		matches := s.findMatchingPricesForEvent(event, prices, meterMap)

		if len(matches) == 0 {
			s.Logger.Debugw("no matching prices/meters found for subscription",
				"event_id", event.ID,
				"subscription_id", sub.ID,
				"event_name", event.EventName,
			)
			continue
		}

		for _, match := range matches {
			// Find the corresponding line item
			lineItem, ok := subLineItemMap[match.Price.ID]
			if !ok {
				s.Logger.Warnw("line item not found for price",
					"event_id", event.ID,
					"subscription_id", sub.ID,
					"price_id", match.Price.ID,
				)
				continue
			}

			// Create a unique hash for deduplication
			uniqueHash := s.generateUniqueHash(event, match.Meter)

			// TODO: Check for duplicate events also maybe just call for COUNT_UNIQUE and not all cases

			// Create a new processed event for each match
			featureUsageCopy := &events.FeatureUsage{
				Event:          *event,
				SubscriptionID: sub.ID,
				SubLineItemID:  lineItem.ID,
				PriceID:        match.Price.ID,
				MeterID:        match.Meter.ID,
				PeriodID:       periodID,
				UniqueHash:     uniqueHash,
				Sign:           1, // Default to positive sign
			}

			// Set feature ID if available
			if feature, ok := featureMeterMap[match.Meter.ID]; ok {
				featureUsageCopy.FeatureID = feature.ID
			} else {
				s.Logger.Warnw("feature not found for meter",
					"event_id", event.ID,
					"meter_id", match.Meter.ID,
				)
				continue
			}

			// Extract quantity based on meter aggregation
			quantity, _ := s.extractQuantityFromEvent(event, match.Meter, sub.Subscription, periodID)

			// Validate the quantity is positive and within reasonable bounds
			if quantity.IsNegative() {
				s.Logger.Warnw("negative quantity calculated, setting to zero",
					"event_id", event.ID,
					"meter_id", match.Meter.ID,
					"calculated_quantity", quantity.String(),
				)
				quantity = decimal.Zero
			}

			// Store original quantity
			featureUsageCopy.QtyTotal = quantity

			featureUsagePerSub = append(featureUsagePerSub, featureUsageCopy)
		}
	}

	// Return all processed events
	if len(featureUsagePerSub) > 0 {
		s.Logger.Debugw("event processing request prepared",
			"event_id", event.ID,
			"feature_usage_count", len(featureUsagePerSub),
		)
		return featureUsagePerSub, nil
	}

	// If we got here, no events were processed
	return results, nil
}

// Find matching prices for an event based on meter configuration and filters
func (s *featureUsageTrackingService) findMatchingPricesForEvent(
	event *events.Event,
	prices []*price.Price,
	meterMap map[string]*meter.Meter,
) []PriceMatch {
	matches := make([]PriceMatch, 0)

	// Find prices with associated meters
	for _, price := range prices {
		if !price.IsUsage() {
			continue
		}

		meter, ok := meterMap[price.MeterID]
		if !ok || meter == nil {
			s.Logger.Warnw("feature usage tracking: meter not found for price",
				"event_id", event.ID,
				"price_id", price.ID,
				"meter_id", price.MeterID,
			)
			continue
		}

		// Skip if meter doesn't match the event name
		if meter.EventName != event.EventName {
			continue
		}

		// Check meter filters
		if !s.checkMeterFilters(event, meter.Filters) {
			continue
		}

		// Add to matches
		matches = append(matches, PriceMatch{
			Price: price,
			Meter: meter,
		})
	}

	// Sort matches by filter specificity (most specific first)
	sort.Slice(matches, func(i, j int) bool {
		// Calculate priority based on filter count
		priorityI := len(matches[i].Meter.Filters)
		priorityJ := len(matches[j].Meter.Filters)

		if priorityI != priorityJ {
			return priorityI > priorityJ
		}

		// Tie-break using price ID for deterministic ordering
		return matches[i].Price.ID < matches[j].Price.ID
	})

	return matches
}

// Check if an event matches the meter filters
func (s *featureUsageTrackingService) checkMeterFilters(event *events.Event, filters []meter.Filter) bool {
	if len(filters) == 0 {
		return true // No filters means everything matches
	}

	for _, filter := range filters {
		propertyValue, exists := event.Properties[filter.Key]
		if !exists {
			return false
		}

		// Convert property value to string for comparison
		propStr := fmt.Sprintf("%v", propertyValue)

		// Check if the value is in the filter values
		if !lo.Contains(filter.Values, propStr) {
			return false
		}
	}

	return true
}

// Extract quantity from event based on meter aggregation
// Returns the quantity and the string representation of the field value
func (s *featureUsageTrackingService) extractQuantityFromEvent(
	event *events.Event,
	meter *meter.Meter,
	subscription *subscription.Subscription,
	periodID uint64,
) (decimal.Decimal, string) {
	switch meter.Aggregation.Type {
	case types.AggregationCount:
		// For count, always return 1 and empty string for field value
		return decimal.NewFromInt(1), ""

	case types.AggregationSum, types.AggregationAvg, types.AggregationLatest, types.AggregationMax:
		if meter.Aggregation.Field == "" {
			s.Logger.Warnw("aggregation with empty field name",
				"event_id", event.ID,
				"meter_id", meter.ID,
				"aggregation_type", meter.Aggregation.Type,
			)
			return decimal.Zero, ""
		}

		val, ok := event.Properties[meter.Aggregation.Field]
		if !ok {
			s.Logger.Warnw("property not found for aggregation",
				"event_id", event.ID,
				"meter_id", meter.ID,
				"field", meter.Aggregation.Field,
				"aggregation_type", meter.Aggregation.Type,
			)
			return decimal.Zero, ""
		}

		// Convert value to decimal and string with detailed error handling
		decimalValue, stringValue := s.convertValueToDecimal(val, event, meter)
		return decimalValue, stringValue

	case types.AggregationSumWithMultiplier:
		if meter.Aggregation.Field == "" {
			s.Logger.Warnw("sum_with_multiplier aggregation with empty field name",
				"event_id", event.ID,
				"meter_id", meter.ID,
			)
			return decimal.Zero, ""
		}

		if meter.Aggregation.Multiplier == nil {
			s.Logger.Warnw("sum_with_multiplier aggregation without multiplier",
				"event_id", event.ID,
				"meter_id", meter.ID,
			)
			return decimal.Zero, ""
		}

		val, ok := event.Properties[meter.Aggregation.Field]
		if !ok {
			s.Logger.Warnw("property not found for sum_with_multiplier aggregation",
				"event_id", event.ID,
				"meter_id", meter.ID,
				"field", meter.Aggregation.Field,
			)
			return decimal.Zero, ""
		}

		// Convert value to decimal and apply multiplier
		decimalValue, stringValue := s.convertValueToDecimal(val, event, meter)
		if decimalValue.IsZero() {
			return decimal.Zero, stringValue
		}

		// Apply multiplier
		result := decimalValue.Mul(*meter.Aggregation.Multiplier)
		return result, stringValue

	case types.AggregationCountUnique:
		if meter.Aggregation.Field == "" {
			s.Logger.Warnw("count_unique aggregation with empty field name",
				"event_id", event.ID,
				"meter_id", meter.ID,
			)
			return decimal.Zero, ""
		}

		val, ok := event.Properties[meter.Aggregation.Field]
		if !ok {
			s.Logger.Warnw("property not found for count_unique aggregation",
				"event_id", event.ID,
				"meter_id", meter.ID,
				"field", meter.Aggregation.Field,
			)
			return decimal.Zero, ""
		}

		// For count_unique, we return 1 if the value exists (uniqueness is handled at aggregation level)
		// and convert the value to string for tracking
		stringValue := s.convertValueToString(val)
		return decimal.NewFromInt(1), stringValue
	case types.AggregationWeightedSum:
		if meter.Aggregation.Field == "" {
			s.Logger.Warnw("weighted_sum aggregation with empty field name",
				"event_id", event.ID,
				"meter_id", meter.ID,
			)
			return decimal.Zero, ""
		}

		val, ok := event.Properties[meter.Aggregation.Field]
		if !ok {
			s.Logger.Warnw("property not found for weighted_sum aggregation",
				"event_id", event.ID,
				"meter_id", meter.ID,
				"field", meter.Aggregation.Field,
			)
			return decimal.Zero, ""
		}

		// Convert value to decimal and apply multiplier
		decimalValue, stringValue := s.convertValueToDecimal(val, event, meter)
		if decimalValue.IsZero() {
			return decimal.Zero, stringValue
		}

		// Apply multiplier
		result, err := s.getTotalUsageForWeightedSumAggregation(subscription, event, decimalValue, periodID)
		if err != nil {
			return decimal.Zero, stringValue
		}
		return result, stringValue
	default:
		s.Logger.Warnw("unsupported aggregation type",
			"event_id", event.ID,
			"meter_id", meter.ID,
			"aggregation_type", meter.Aggregation.Type,
		)
		return decimal.Zero, ""
	}
}

// convertValueToDecimal converts a property value to decimal and string representation
func (s *featureUsageTrackingService) convertValueToDecimal(val interface{}, event *events.Event, meter *meter.Meter) (decimal.Decimal, string) {
	var decimalValue decimal.Decimal
	var stringValue string

	switch v := val.(type) {
	case float64:
		decimalValue = decimal.NewFromFloat(v)
		stringValue = fmt.Sprintf("%f", v)

	case float32:
		decimalValue = decimal.NewFromFloat32(v)
		stringValue = fmt.Sprintf("%f", v)

	case int:
		decimalValue = decimal.NewFromInt(int64(v))
		stringValue = fmt.Sprintf("%d", v)

	case int64:
		decimalValue = decimal.NewFromInt(v)
		stringValue = fmt.Sprintf("%d", v)

	case int32:
		decimalValue = decimal.NewFromInt(int64(v))
		stringValue = fmt.Sprintf("%d", v)

	case uint:
		// Convert uint to int64 safely
		decimalValue = decimal.NewFromInt(int64(v))
		stringValue = fmt.Sprintf("%d", v)

	case uint64:
		// Convert uint64 to string then parse to ensure no overflow
		str := fmt.Sprintf("%d", v)
		var err error
		decimalValue, err = decimal.NewFromString(str)
		if err != nil {
			s.Logger.Warnw("failed to parse uint64 as decimal",
				"event_id", event.ID,
				"meter_id", meter.ID,
				"value", v,
				"error", err,
			)
			return decimal.Zero, str
		}
		stringValue = str

	case string:
		var err error
		decimalValue, err = decimal.NewFromString(v)
		if err != nil {
			s.Logger.Warnw("failed to parse string as decimal",
				"event_id", event.ID,
				"meter_id", meter.ID,
				"value", v,
				"error", err,
			)
			return decimal.Zero, v
		}
		stringValue = v

	case json.Number:
		var err error
		decimalValue, err = decimal.NewFromString(string(v))
		if err != nil {
			s.Logger.Warnw("failed to parse json.Number as decimal",
				"event_id", event.ID,
				"meter_id", meter.ID,
				"value", v,
				"error", err,
			)
			return decimal.Zero, string(v)
		}
		stringValue = string(v)

	default:
		// Try to convert to string representation
		stringValue = fmt.Sprintf("%v", v)
		s.Logger.Warnw("unknown type for aggregation - cannot convert to decimal",
			"event_id", event.ID,
			"meter_id", meter.ID,
			"field", meter.Aggregation.Field,
			"aggregation_type", meter.Aggregation.Type,
			"type", fmt.Sprintf("%T", v),
			"value", stringValue,
		)
		return decimal.Zero, stringValue
	}

	return decimalValue, stringValue
}

// convertValueToString converts a property value to string representation
func (s *featureUsageTrackingService) convertValueToString(val interface{}) string {
	switch v := val.(type) {
	case string:
		return v
	case float64, float32, int, int64, int32, uint, uint64:
		return fmt.Sprintf("%v", v)
	case json.Number:
		return string(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// AnalyticsData holds all data required for analytics processing
type AnalyticsData struct {
	Customer      *customer.Customer
	Subscriptions []*subscription.Subscription
	Analytics     []*events.DetailedUsageAnalytic
	Features      map[string]*feature.Feature
	Meters        map[string]*meter.Meter
	Prices        map[string]*price.Price
	Currency      string
	Params        *events.UsageAnalyticsParams
}

// GetDetailedUsageAnalytics provides detailed usage analytics with filtering, grouping, and time-series data
func (s *featureUsageTrackingService) GetDetailedUsageAnalytics(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error) {
	// 1. Validate request
	if err := s.validateAnalyticsRequest(req); err != nil {
		return nil, err
	}

	// 2. Fetch all required data in parallel
	data, err := s.fetchAnalyticsData(ctx, req)
	if err != nil {
		return nil, err
	}

	// 3. Process and return response
	return s.buildAnalyticsResponse(ctx, data, req)
}

// validateAnalyticsRequest validates the analytics request
func (s *featureUsageTrackingService) validateAnalyticsRequest(req *dto.GetUsageAnalyticsRequest) error {
	if req.ExternalCustomerID == "" {
		return ierr.NewError("external_customer_id is required").
			WithHint("External customer ID is required").
			Mark(ierr.ErrValidation)
	}

	if req.WindowSize != "" {
		return req.WindowSize.Validate()
	}

	return nil
}

// fetchAnalyticsData fetches all required data sequentially
func (s *featureUsageTrackingService) fetchAnalyticsData(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*AnalyticsData, error) {
	// 1. Fetch customer
	customer, err := s.fetchCustomer(ctx, req.ExternalCustomerID)
	if err != nil {
		return nil, err
	}

	// 2. Fetch subscriptions
	subscriptions, err := s.fetchSubscriptions(ctx, customer.ID)
	if err != nil {
		return nil, err
	}

	// 3. Validate currency consistency
	currency, err := s.validateCurrency(subscriptions)
	if err != nil {
		return nil, err
	}

	// 4. Create params and fetch analytics
	params := s.createAnalyticsParams(ctx, req)
	params.CustomerID = customer.ID
	analytics, err := s.fetchAnalytics(ctx, params)
	if err != nil {
		return nil, err
	}

	// 5. Build data structure
	data := &AnalyticsData{
		Customer:      customer,
		Subscriptions: subscriptions,
		Analytics:     analytics,
		Currency:      currency,
		Params:        params,
		Features:      make(map[string]*feature.Feature),
		Meters:        make(map[string]*meter.Meter),
		Prices:        make(map[string]*price.Price),
	}

	// 6. Enrich with metadata if we have analytics data
	if len(analytics) > 0 {
		if err := s.enrichWithMetadata(ctx, data); err != nil {
			s.Logger.Warnw("failed to enrich analytics with metadata",
				"error", err,
				"analytics_count", len(analytics),
			)
			// Continue with partial data rather than failing completely
		}
	}

	return data, nil
}

// buildAnalyticsResponse processes the data and builds the final response
func (s *featureUsageTrackingService) buildAnalyticsResponse(ctx context.Context, data *AnalyticsData, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error) {
	// If no results, return early
	if len(data.Analytics) == 0 {
		return s.ToGetUsageAnalyticsResponseDTO(ctx, data.Analytics, req)
	}

	// Calculate costs
	if err := s.calculateCosts(ctx, data); err != nil {
		s.Logger.Warnw("failed to calculate costs",
			"error", err,
			"analytics_count", len(data.Analytics),
		)
		// Continue with partial data rather than failing completely
	}

	// Set currency on all analytics items
	if data.Currency != "" {
		for _, item := range data.Analytics {
			item.Currency = data.Currency
		}
	}

	// Aggregate results by requested grouping dimensions
	analytics := s.aggregateAnalyticsByGrouping(data.Analytics, data.Params.GroupBy)

	return s.ToGetUsageAnalyticsResponseDTO(ctx, analytics, req)
}

// fetchCustomer fetches customer by external customer ID
func (s *featureUsageTrackingService) fetchCustomer(ctx context.Context, externalCustomerID string) (*customer.Customer, error) {
	customer, err := s.CustomerRepo.GetByLookupKey(ctx, externalCustomerID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Customer not found").
			WithReportableDetails(map[string]interface{}{
				"external_customer_id": externalCustomerID,
			}).
			Mark(ierr.ErrNotFound)
	}
	return customer, nil
}

// fetchSubscriptions fetches active subscriptions for a customer
func (s *featureUsageTrackingService) fetchSubscriptions(ctx context.Context, customerID string) ([]*subscription.Subscription, error) {
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	filter := types.NewSubscriptionFilter()
	filter.CustomerID = customerID
	filter.WithLineItems = true
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}

	subscriptionsList, err := subscriptionService.ListSubscriptions(ctx, filter)
	if err != nil {
		s.Logger.Errorw("failed to get subscriptions for analytics",
			"error", err,
			"customer_id", customerID,
		)
		return nil, err
	}

	// Convert to domain objects
	subscriptions := make([]*subscription.Subscription, len(subscriptionsList.Items))
	for i, subResp := range subscriptionsList.Items {
		subscriptions[i] = subResp.Subscription
	}

	return subscriptions, nil
}

// buildMaxBucketFeatures builds a map of max bucket features from the request parameters
func (s *featureUsageTrackingService) buildMaxBucketFeatures(ctx context.Context, params *events.UsageAnalyticsParams) (map[string]*events.MaxBucketFeatureInfo, error) {
	maxBucketFeatures := make(map[string]*events.MaxBucketFeatureInfo)

	// Check if FeatureIDs is empty and fetch all feature IDs from database if needed
	var features []*feature.Feature
	var err error

	if len(params.FeatureIDs) == 0 {
		s.Logger.Debugw("no feature IDs provided, fetching all features from database",
			"tenant_id", params.TenantID,
			"environment_id", params.EnvironmentID,
		)

		// Create filter to fetch all features for this tenant/environment
		featureFilter := types.NewNoLimitFeatureFilter()
		features, err = s.FeatureRepo.List(ctx, featureFilter)
		if err != nil {
			s.Logger.Errorw("failed to fetch features from database",
				"error", err,
				"tenant_id", params.TenantID,
				"environment_id", params.EnvironmentID,
			)
			return nil, ierr.WithError(err).
				WithHint("Failed to fetch features for max bucket analysis").
				Mark(ierr.ErrDatabase)
		}

		// Extract feature IDs and update params
		featureIDs := make([]string, len(features))
		for i, f := range features {
			featureIDs[i] = f.ID
		}
		params.FeatureIDs = featureIDs

		s.Logger.Debugw("fetched feature IDs from database",
			"count", len(featureIDs),
			"feature_ids", featureIDs,
		)
	} else {
		// Fetch features using provided feature IDs
		featureFilter := types.NewNoLimitFeatureFilter()
		featureFilter.FeatureIDs = params.FeatureIDs
		features, err = s.FeatureRepo.List(ctx, featureFilter)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to fetch features for max bucket analysis").
				Mark(ierr.ErrDatabase)
		}
	}

	// Extract meter IDs
	meterIDs := make([]string, 0)
	meterIDSet := make(map[string]bool)
	featureToMeterMap := make(map[string]string)

	for _, f := range features {
		if f.MeterID != "" && !meterIDSet[f.MeterID] {
			meterIDs = append(meterIDs, f.MeterID)
			meterIDSet[f.MeterID] = true
		}
		featureToMeterMap[f.ID] = f.MeterID
	}

	// Fetch meters if needed
	if len(meterIDs) > 0 {
		meterFilter := types.NewNoLimitMeterFilter()
		meterFilter.MeterIDs = meterIDs
		meters, err := s.MeterRepo.List(ctx, meterFilter)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to fetch meters for max bucket analysis").
				Mark(ierr.ErrDatabase)
		}

		// Build meter map
		meterMap := make(map[string]*meter.Meter)
		for _, m := range meters {
			meterMap[m.ID] = m
		}

		// Check features for bucketed max meters
		for _, f := range features {
			if meterID := featureToMeterMap[f.ID]; meterID != "" {
				if m, exists := meterMap[meterID]; exists && m.IsBucketedMaxMeter() {
					maxBucketFeatures[f.ID] = &events.MaxBucketFeatureInfo{
						FeatureID:    f.ID,
						MeterID:      meterID,
						BucketSize:   types.WindowSize(m.Aggregation.BucketSize),
						EventName:    m.EventName,
						PropertyName: m.Aggregation.Field,
					}
				}
			}
		}
	}

	return maxBucketFeatures, nil
}

// fetchAnalytics fetches analytics data from repository
func (s *featureUsageTrackingService) fetchAnalytics(ctx context.Context, params *events.UsageAnalyticsParams) ([]*events.DetailedUsageAnalytic, error) {
	// Build max bucket features map (this will handle fetching features if needed)
	maxBucketFeatures, err := s.buildMaxBucketFeatures(ctx, params)
	if err != nil {
		return nil, err
	}

	// Fetch analytics with max bucket features
	analytics, err := s.featureUsageRepo.GetDetailedUsageAnalytics(ctx, params, maxBucketFeatures)
	if err != nil {
		s.Logger.Errorw("failed to get detailed usage analytics",
			"error", err,
			"external_customer_id", params.ExternalCustomerID,
		)
		return nil, err
	}
	return analytics, nil
}

// createAnalyticsParams creates analytics parameters from request
func (s *featureUsageTrackingService) createAnalyticsParams(ctx context.Context, req *dto.GetUsageAnalyticsRequest) *events.UsageAnalyticsParams {
	return &events.UsageAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: req.ExternalCustomerID,
		FeatureIDs:         req.FeatureIDs,
		Sources:            req.Sources,
		StartTime:          req.StartTime,
		EndTime:            req.EndTime,
		GroupBy:            req.GroupBy,
		WindowSize:         req.WindowSize,
		PropertyFilters:    req.PropertyFilters,
	}
}

// validateCurrency validates currency consistency across subscriptions
func (s *featureUsageTrackingService) validateCurrency(subscriptions []*subscription.Subscription) (string, error) {
	if len(subscriptions) == 0 {
		return "", nil
	}

	currency := subscriptions[0].Currency
	for _, sub := range subscriptions {
		if sub.Currency != currency {
			return "", ierr.NewError("multiple currencies detected").
				WithHint("Analytics is only supported for customers with a single currency across all subscriptions").
				WithReportableDetails(map[string]interface{}{
					"customer_id": sub.CustomerID,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return currency, nil
}

// enrichWithMetadata enriches analytics data with feature, meter, and price information
func (s *featureUsageTrackingService) enrichWithMetadata(ctx context.Context, data *AnalyticsData) error {
	// Extract unique feature IDs
	featureIDs := s.extractUniqueFeatureIDs(data.Analytics)
	if len(featureIDs) == 0 {
		return nil
	}

	// Fetch features with meter expansion
	featureFilter := types.NewNoLimitFeatureFilter()
	featureFilter.FeatureIDs = featureIDs
	featureFilter.Expand = lo.ToPtr(string(types.ExpandMeters))
	features, err := s.FeatureRepo.List(ctx, featureFilter)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to fetch features for enrichment").
			Mark(ierr.ErrDatabase)
	}

	// Build feature map and extract meter IDs
	meterIDs := make([]string, 0)
	meterIDSet := make(map[string]bool)
	for _, f := range features {
		data.Features[f.ID] = f
		if f.MeterID != "" && !meterIDSet[f.MeterID] {
			meterIDs = append(meterIDs, f.MeterID)
			meterIDSet[f.MeterID] = true
		}
	}

	// Fetch meters
	if len(meterIDs) > 0 {
		meterFilter := types.NewNoLimitMeterFilter()
		meterFilter.MeterIDs = meterIDs
		meters, err := s.MeterRepo.List(ctx, meterFilter)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to fetch meters for enrichment").
				Mark(ierr.ErrDatabase)
		}

		for _, m := range meters {
			data.Meters[m.ID] = m
		}
	}

	// Fetch prices from subscription line items
	if err := s.fetchSubscriptionPrices(ctx, data); err != nil {
		return err
	}

	// Enrich analytics with metadata
	s.enrichAnalyticsWithMetadata(data)

	return nil
}

// extractUniqueFeatureIDs extracts unique feature IDs from analytics
func (s *featureUsageTrackingService) extractUniqueFeatureIDs(analytics []*events.DetailedUsageAnalytic) []string {
	featureIDSet := make(map[string]bool)
	featureIDs := make([]string, 0)

	for _, item := range analytics {
		if item.FeatureID != "" && !featureIDSet[item.FeatureID] {
			featureIDs = append(featureIDs, item.FeatureID)
			featureIDSet[item.FeatureID] = true
		}
	}

	return featureIDs
}

// fetchSubscriptionPrices fetches prices from subscription line items
func (s *featureUsageTrackingService) fetchSubscriptionPrices(ctx context.Context, data *AnalyticsData) error {
	// Collect price IDs from subscription line items
	priceIDs := make([]string, 0)
	priceIDSet := make(map[string]bool)

	for _, sub := range data.Subscriptions {
		for _, lineItem := range sub.LineItems {
			if lineItem.IsUsage() && lineItem.MeterID != "" && lineItem.PriceID != "" {
				if !priceIDSet[lineItem.PriceID] {
					priceIDs = append(priceIDs, lineItem.PriceID)
					priceIDSet[lineItem.PriceID] = true
				}
			}
		}
	}

	// Fetch prices
	if len(priceIDs) > 0 {
		priceFilter := types.NewNoLimitPriceFilter()
		priceFilter.PriceIDs = priceIDs
		priceFilter.WithStatus(types.StatusPublished)
		priceFilter.AllowExpiredPrices = false
		prices, err := s.PriceRepo.List(ctx, priceFilter)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to fetch subscription prices for cost calculation").
				Mark(ierr.ErrDatabase)
		}

		// Create price map by meter ID
		for _, p := range prices {
			if p.MeterID != "" {
				data.Prices[p.MeterID] = p
			}
		}
	}

	return nil
}

// enrichAnalyticsWithMetadata enriches analytics with feature and meter data
func (s *featureUsageTrackingService) enrichAnalyticsWithMetadata(data *AnalyticsData) {
	for _, item := range data.Analytics {
		if feature, ok := data.Features[item.FeatureID]; ok {
			item.FeatureName = feature.Name
			item.Unit = feature.UnitSingular
			item.UnitPlural = feature.UnitPlural

			if meter, ok := data.Meters[feature.MeterID]; ok {
				item.MeterID = meter.ID
				item.EventName = meter.EventName
				item.AggregationType = meter.Aggregation.Type
			}
		}
	}
}

// calculateCosts calculates costs for analytics items
func (s *featureUsageTrackingService) calculateCosts(ctx context.Context, data *AnalyticsData) error {
	priceService := NewPriceService(s.ServiceParams)

	for _, item := range data.Analytics {
		if feature, ok := data.Features[item.FeatureID]; ok {
			if meter, ok := data.Meters[feature.MeterID]; ok {
				if price, hasPricing := data.Prices[meter.ID]; hasPricing {
					// Calculate cost based on meter type
					if meter.IsBucketedMaxMeter() {
						s.calculateBucketedCost(ctx, priceService, item, price)
					} else {
						s.calculateRegularCost(ctx, priceService, item, meter, price)
					}
				}
			}
		}
	}

	return nil
}

// calculateBucketedCost calculates cost for bucketed max meters
func (s *featureUsageTrackingService) calculateBucketedCost(ctx context.Context, priceService PriceService, item *events.DetailedUsageAnalytic, price *price.Price) {
	var cost decimal.Decimal

	if len(item.Points) > 0 {
		// Use points as buckets
		bucketedValues := make([]decimal.Decimal, len(item.Points))
		for i, point := range item.Points {
			bucketedValues[i] = s.getCorrectUsageValueForPoint(point, types.AggregationMax)
		}
		cost = priceService.CalculateBucketedCost(ctx, price, bucketedValues)

		// Calculate cost for each point
		for i := range item.Points {
			pointCost := priceService.CalculateCost(ctx, price, s.getCorrectUsageValueForPoint(item.Points[i], types.AggregationMax))
			item.Points[i].Cost = pointCost
		}
	} else {
		// Treat total usage as single bucket
		if item.MaxUsage.IsPositive() {
			bucketedValues := []decimal.Decimal{item.MaxUsage}
			cost = priceService.CalculateBucketedCost(ctx, price, bucketedValues)
		}
	}

	item.TotalCost = cost
	item.Currency = price.Currency
}

// calculateRegularCost calculates cost for regular meters
func (s *featureUsageTrackingService) calculateRegularCost(ctx context.Context, priceService PriceService, item *events.DetailedUsageAnalytic, meter *meter.Meter, price *price.Price) {
	// Set correct usage value
	item.TotalUsage = s.getCorrectUsageValue(item, meter.Aggregation.Type)

	// Calculate total cost
	cost := priceService.CalculateCost(ctx, price, item.TotalUsage)
	item.TotalCost = cost
	item.Currency = price.Currency

	// Calculate cost for each point
	for i := range item.Points {
		pointUsage := s.getCorrectUsageValueForPoint(item.Points[i], meter.Aggregation.Type)
		pointCost := priceService.CalculateCost(ctx, price, pointUsage)
		item.Points[i].Cost = pointCost
	}
}

// aggregateAnalyticsByGrouping aggregates analytics results by the requested grouping dimensions
// This ensures that when grouping by source, we return source-level totals rather than source+feature combinations
func (s *featureUsageTrackingService) aggregateAnalyticsByGrouping(analytics []*events.DetailedUsageAnalytic, groupBy []string) []*events.DetailedUsageAnalytic {
	// If no grouping requested or only feature_id grouping, return as-is
	if len(groupBy) == 0 || (len(groupBy) == 1 && groupBy[0] == "feature_id") {
		return analytics
	}

	// Create a map to aggregate results by the requested grouping dimensions
	aggregatedMap := make(map[string]*events.DetailedUsageAnalytic)

	for _, item := range analytics {
		// Create a key based on the requested grouping dimensions
		key := s.createGroupingKey(item, groupBy)

		if existing, exists := aggregatedMap[key]; exists {
			// Aggregate with existing item
			existing.TotalUsage = existing.TotalUsage.Add(item.TotalUsage)
			existing.MaxUsage = lo.Ternary(existing.MaxUsage.GreaterThan(item.MaxUsage), existing.MaxUsage, item.MaxUsage)
			existing.LatestUsage = lo.Ternary(existing.LatestUsage.GreaterThan(item.LatestUsage), existing.LatestUsage, item.LatestUsage)
			existing.CountUniqueUsage += item.CountUniqueUsage
			existing.EventCount += item.EventCount
			existing.TotalCost = existing.TotalCost.Add(item.TotalCost)

			// For time series points, we need to merge them by timestamp
			existing.Points = s.mergeTimeSeriesPoints(existing.Points, item.Points)
		} else {
			// Create a new aggregated item
			aggregated := &events.DetailedUsageAnalytic{
				FeatureID:        "", // Will be set based on grouping
				FeatureName:      item.FeatureName,
				EventName:        item.EventName,
				Source:           item.Source,
				Unit:             item.Unit,
				UnitPlural:       item.UnitPlural,
				AggregationType:  item.AggregationType,
				TotalUsage:       item.TotalUsage,
				MaxUsage:         item.MaxUsage,
				LatestUsage:      item.LatestUsage,
				CountUniqueUsage: item.CountUniqueUsage,
				EventCount:       item.EventCount,
				TotalCost:        item.TotalCost,
				Currency:         item.Currency,
				Properties:       make(map[string]string),
				Points:           make([]events.UsageAnalyticPoint, len(item.Points)),
			}

			// Copy properties
			for k, v := range item.Properties {
				aggregated.Properties[k] = v
			}

			// Copy points
			copy(aggregated.Points, item.Points)

			// Set grouping-specific fields
			s.setGroupingFields(aggregated, item, groupBy)

			aggregatedMap[key] = aggregated
		}
	}

	// Convert map to slice
	result := make([]*events.DetailedUsageAnalytic, 0, len(aggregatedMap))
	for _, item := range aggregatedMap {
		result = append(result, item)
	}

	return result
}

// createGroupingKey creates a unique key for grouping based on the requested dimensions
func (s *featureUsageTrackingService) createGroupingKey(item *events.DetailedUsageAnalytic, groupBy []string) string {
	keyParts := make([]string, 0, len(groupBy))

	for _, group := range groupBy {
		switch group {
		case "feature_id":
			keyParts = append(keyParts, item.FeatureID)
		case "source":
			keyParts = append(keyParts, item.Source)
		default:
			if strings.HasPrefix(group, "properties.") {
				propertyName := strings.TrimPrefix(group, "properties.")
				if value, exists := item.Properties[propertyName]; exists {
					keyParts = append(keyParts, value)
				} else {
					keyParts = append(keyParts, "")
				}
			}
		}
	}

	return strings.Join(keyParts, "|")
}

// setGroupingFields sets the appropriate fields based on the grouping dimensions
func (s *featureUsageTrackingService) setGroupingFields(aggregated *events.DetailedUsageAnalytic, item *events.DetailedUsageAnalytic, groupBy []string) {
	for _, group := range groupBy {
		switch group {
		case "feature_id":
			// For feature_id grouping, keep the first feature_id encountered
			if aggregated.FeatureID == "" {
				aggregated.FeatureID = item.FeatureID
			}
		case "source":
			// For source grouping, keep the first source encountered
			if aggregated.Source == "" {
				aggregated.Source = item.Source
			}
		default:
			if strings.HasPrefix(group, "properties.") {
				propertyName := strings.TrimPrefix(group, "properties.")
				if _, exists := aggregated.Properties[propertyName]; !exists {
					if value, itemExists := item.Properties[propertyName]; itemExists {
						aggregated.Properties[propertyName] = value
					}
				}
			}
		}
	}
}

// mergeTimeSeriesPoints merges time series points from two analytics items
func (s *featureUsageTrackingService) mergeTimeSeriesPoints(existing []events.UsageAnalyticPoint, new []events.UsageAnalyticPoint) []events.UsageAnalyticPoint {
	// Create a map to track points by timestamp
	pointMap := make(map[time.Time]*events.UsageAnalyticPoint)

	// Add existing points
	for i := range existing {
		pointMap[existing[i].Timestamp] = &existing[i]
	}

	// Merge new points
	for i := range new {
		if existingPoint, exists := pointMap[new[i].Timestamp]; exists {
			// Aggregate with existing point
			existingPoint.Usage = existingPoint.Usage.Add(new[i].Usage)
			existingPoint.MaxUsage = lo.Ternary(existingPoint.MaxUsage.GreaterThan(new[i].MaxUsage), existingPoint.MaxUsage, new[i].MaxUsage)
			existingPoint.LatestUsage = lo.Ternary(existingPoint.LatestUsage.GreaterThan(new[i].LatestUsage), existingPoint.LatestUsage, new[i].LatestUsage)
			existingPoint.CountUniqueUsage += new[i].CountUniqueUsage
			existingPoint.EventCount += new[i].EventCount
			existingPoint.Cost = existingPoint.Cost.Add(new[i].Cost)
		} else {
			// Add new point
			pointMap[new[i].Timestamp] = &new[i]
		}
	}

	// Convert back to slice and sort by timestamp
	result := make([]events.UsageAnalyticPoint, 0, len(pointMap))
	for _, point := range pointMap {
		result = append(result, *point)
	}

	// Sort by timestamp
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})

	return result
}

// getCorrectUsageValue returns the correct usage value based on the meter's aggregation type
func (s *featureUsageTrackingService) getCorrectUsageValue(item *events.DetailedUsageAnalytic, aggregationType types.AggregationType) decimal.Decimal {
	switch aggregationType {
	case types.AggregationCountUnique:
		return decimal.NewFromInt(int64(item.CountUniqueUsage))
	case types.AggregationMax:
		return item.MaxUsage
	case types.AggregationLatest:
		return item.LatestUsage
	case types.AggregationSum, types.AggregationSumWithMultiplier, types.AggregationAvg, types.AggregationWeightedSum:
		return item.TotalUsage
	default:
		// Default to SUM for unknown types
		return item.TotalUsage
	}
}

// getCorrectUsageValueForPoint returns the correct usage value for a time series point based on aggregation type
func (s *featureUsageTrackingService) getCorrectUsageValueForPoint(point events.UsageAnalyticPoint, aggregationType types.AggregationType) decimal.Decimal {
	switch aggregationType {
	case types.AggregationCountUnique:
		return decimal.NewFromInt(int64(point.CountUniqueUsage))
	case types.AggregationMax:
		return point.MaxUsage
	case types.AggregationLatest:
		return point.LatestUsage
	case types.AggregationSum, types.AggregationSumWithMultiplier, types.AggregationAvg, types.AggregationWeightedSum:
		return point.Usage
	default:
		// Default to SUM for unknown types
		return point.Usage
	}
}

// ReprocessEvents triggers reprocessing of events for a customer or with other filters
func (s *featureUsageTrackingService) ReprocessEvents(ctx context.Context, params *events.ReprocessEventsParams) error {
	s.Logger.Infow("starting event reprocessing for feature usage tracking",
		"external_customer_id", params.ExternalCustomerID,
		"event_name", params.EventName,
		"start_time", params.StartTime,
		"end_time", params.EndTime,
	)

	// Set default batch size if not provided
	batchSize := params.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	// Create find params from reprocess params
	findParams := &events.FindUnprocessedEventsParams{
		ExternalCustomerID: params.ExternalCustomerID,
		EventName:          params.EventName,
		StartTime:          params.StartTime,
		EndTime:            params.EndTime,
		BatchSize:          batchSize,
	}

	// We'll process in batches to avoid memory issues with large datasets
	processedBatches := 0
	totalEventsFound := 0
	totalEventsPublished := 0
	var lastID string
	var lastTimestamp time.Time

	// Keep processing batches until we're done
	for {
		// Update keyset pagination parameters for next batch
		if lastID != "" && !lastTimestamp.IsZero() {
			findParams.LastID = lastID
			findParams.LastTimestamp = lastTimestamp
		}

		// Find unprocessed events
		unprocessedEvents, err := s.eventRepo.FindUnprocessedEvents(ctx, findParams)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to find unprocessed events").
				WithReportableDetails(map[string]interface{}{
					"external_customer_id": params.ExternalCustomerID,
					"event_name":           params.EventName,
					"batch":                processedBatches,
				}).
				Mark(ierr.ErrDatabase)
		}

		eventsCount := len(unprocessedEvents)
		totalEventsFound += eventsCount
		s.Logger.Infow("found unprocessed events",
			"batch", processedBatches,
			"count", eventsCount,
			"total_found", totalEventsFound,
		)

		// If no more events, we're done
		if eventsCount == 0 {
			break
		}

		// Publish each event to the feature usage tracking topic
		for _, event := range unprocessedEvents {
			// hardcoded delay to avoid rate limiting
			// TODO: remove this to make it configurable
			if err := s.PublishEvent(ctx, event, true); err != nil {
				s.Logger.Errorw("failed to publish event for reprocessing for feature usage tracking",
					"event_id", event.ID,
					"error", err,
				)
				// Continue with other events instead of failing the whole batch
				continue
			}
			totalEventsPublished++

			// Update the last seen ID and timestamp for next batch
			lastID = event.ID
			lastTimestamp = event.Timestamp
		}

		s.Logger.Infow("published events for reprocessing for feature usage tracking",
			"batch", processedBatches,
			"count", eventsCount,
			"total_published", totalEventsPublished,
		)

		// Update for next batch
		processedBatches++

		// If we didn't get a full batch, we're done
		if eventsCount < batchSize {
			break
		}
	}

	s.Logger.Infow("completed event reprocessing for feature usage tracking",
		"external_customer_id", params.ExternalCustomerID,
		"event_name", params.EventName,
		"batches_processed", processedBatches,
		"total_events_found", totalEventsFound,
		"total_events_published", totalEventsPublished,
	)

	return nil
}

// isSubscriptionValidForEvent checks if a subscription is valid for processing the given event
// It ensures the event timestamp falls within the subscription's active period
func (s *featureUsageTrackingService) isSubscriptionValidForEvent(
	sub *dto.SubscriptionResponse,
	event *events.Event,
) bool {
	// Event must be after subscription start date
	if event.Timestamp.Before(sub.StartDate) {
		s.Logger.Debugw("event timestamp before subscription start date",
			"event_id", event.ID,
			"subscription_id", sub.ID,
			"event_timestamp", event.Timestamp,
			"subscription_start_date", sub.StartDate,
		)
		return false
	}

	// If subscription has an end date, event must be before or equal to it
	if sub.EndDate != nil && event.Timestamp.After(*sub.EndDate) {
		s.Logger.Debugw("event timestamp after subscription end date",
			"event_id", event.ID,
			"subscription_id", sub.ID,
			"event_timestamp", event.Timestamp,
			"subscription_end_date", *sub.EndDate,
		)
		return false
	}

	// Additional check: if subscription is cancelled, make sure event is before cancellation
	if sub.SubscriptionStatus == types.SubscriptionStatusCancelled && sub.CancelledAt != nil {
		if event.Timestamp.After(*sub.CancelledAt) {
			s.Logger.Debugw("event timestamp after subscription cancellation date",
				"event_id", event.ID,
				"subscription_id", sub.ID,
				"event_timestamp", event.Timestamp,
				"subscription_cancelled_at", *sub.CancelledAt,
			)
			return false
		}
	}

	return true
}

func (s *featureUsageTrackingService) ToGetUsageAnalyticsResponseDTO(ctx context.Context, analytics []*events.DetailedUsageAnalytic, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error) {
	response := &dto.GetUsageAnalyticsResponse{
		TotalCost: decimal.Zero,
		Currency:  "",
		Items:     make([]dto.UsageAnalyticItem, 0, len(analytics)),
	}

	// Convert analytics to response items
	for _, analytic := range analytics {
		// For bucketed MAX, use TotalUsage (sum of bucket maxes) not MaxUsage
		// TotalUsage already contains the sum from getMaxBucketTotals
		totalUsage := analytic.TotalUsage
		if analytic.AggregationType != types.AggregationMax || analytic.TotalUsage.IsZero() {
			// For non-MAX or when TotalUsage is not set, use the aggregation-specific value
			totalUsage = s.getCorrectUsageValue(analytic, analytic.AggregationType)
		}

		item := dto.UsageAnalyticItem{
			FeatureID:       analytic.FeatureID,
			FeatureName:     analytic.FeatureName,
			EventName:       analytic.EventName,
			Source:          analytic.Source,
			Unit:            analytic.Unit,
			UnitPlural:      analytic.UnitPlural,
			AggregationType: analytic.AggregationType,
			TotalUsage:      totalUsage, // Now correctly uses sum of bucket maxes for bucketed MAX
			TotalCost:       analytic.TotalCost,
			Currency:        analytic.Currency,
			EventCount:      analytic.EventCount,
			Properties:      analytic.Properties,
			Points:          make([]dto.UsageAnalyticPoint, 0, len(analytic.Points)),
		}
		// Map time-series points if available
		if req.WindowSize != "" {
			for _, point := range analytic.Points {
				// Use the correct usage value based on aggregation type
				correctUsage := s.getCorrectUsageValueForPoint(point, analytic.AggregationType)
				item.Points = append(item.Points, dto.UsageAnalyticPoint{
					Timestamp:  point.Timestamp,
					Usage:      correctUsage,
					Cost:       point.Cost,
					EventCount: point.EventCount,
				})
			}
		}

		response.Items = append(response.Items, item)
		response.TotalCost = response.TotalCost.Add(analytic.TotalCost)
		response.Currency = analytic.Currency
	}

	// sort by feature name
	sort.Slice(response.Items, func(i, j int) bool {
		return response.Items[i].FeatureName < response.Items[j].FeatureName
	})

	return response, nil
}

func (s *featureUsageTrackingService) getTotalUsageForWeightedSumAggregation(
	subscription *subscription.Subscription,
	event *events.Event,
	propertyValue decimal.Decimal,
	periodID uint64,
) (decimal.Decimal, error) {
	// Convert periodID (epoch milliseconds) back to time for the period start
	periodStart := time.UnixMilli(int64(periodID))

	// Calculate the period end using the subscription's billing configuration
	periodEnd, err := types.NextBillingDate(periodStart, subscription.BillingAnchor, subscription.BillingPeriodCount, subscription.BillingPeriod, nil)
	if err != nil {
		return decimal.Zero, ierr.WithError(err).
			WithHint("Failed to calculate period end for weighted sum aggregation").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscription.ID,
				"period_id":       periodID,
				"period_start":    periodStart,
			}).
			Mark(ierr.ErrValidation)
	}

	// Calculate total billing period duration in seconds
	totalPeriodSeconds := periodEnd.Sub(periodStart).Seconds()
	if totalPeriodSeconds <= 0 {
		return decimal.Zero, ierr.NewError("invalid billing period duration").
			WithHint("Billing period duration must be positive").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscription.ID,
				"period_id":       periodID,
				"period_start":    periodStart,
				"period_end":      periodEnd,
				"total_seconds":   totalPeriodSeconds,
			}).
			Mark(ierr.ErrValidation)
	}

	// Calculate remaining seconds from event timestamp to period end
	remainingSeconds := math.Max(0, periodEnd.Sub(event.Timestamp).Seconds())

	// Apply weighted sum formula: (value / billing_period_seconds) * remaining_seconds
	// This gives us the proportion of the value that should be counted for the remaining period
	weightedUsage := propertyValue.Div(decimal.NewFromFloat(totalPeriodSeconds)).Mul(decimal.NewFromFloat(remainingSeconds))

	return weightedUsage, nil
}
