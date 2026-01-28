package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/pubsub/kafka"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CostSheetUsageTrackingService handles cost sheet usage tracking operations for metered events
type CostSheetUsageTrackingService interface {
	// Publish an event for cost sheet usage tracking
	PublishEvent(ctx context.Context, event *events.Event, isBackfill bool) error

	// Register message handler with the router
	RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration)

	// Register message handler with the router
	RegisterHandlerLazy(router *pubsubRouter.Router, cfg *config.Configuration)

	// Get Analytics
	GetCostSheetUsageAnalytics(ctx context.Context, req *dto.GetCostAnalyticsRequest) (*dto.GetCostAnalyticsResponse, error)

	// Reprocess cost sheet usage
	ReprocessEvents(ctx context.Context, params *events.ReprocessEventsParams) error
}

type costsheetUsageTrackingService struct {
	ServiceParams
	pubSub        pubsub.PubSub
	lazyPubSub    pubsub.PubSub
	eventRepo     events.Repository
	costUsageRepo events.CostSheetUsageRepository
}

// NewCostSheetUsageTrackingService creates a new cost sheet usage tracking service
func NewCostSheetUsageTrackingService(
	params ServiceParams,
	eventRepo events.Repository,
	costUsageRepo events.CostSheetUsageRepository,
) CostSheetUsageTrackingService {
	ev := &costsheetUsageTrackingService{
		ServiceParams: params,
		eventRepo:     eventRepo,
		costUsageRepo: costUsageRepo,
	}

	pubSub, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.CostSheetUsageTracking.ConsumerGroup,
	)

	if err != nil {
		params.Logger.Fatalw("failed to create pubsub", "error", err)
		return nil
	}
	ev.pubSub = pubSub

	lazyPubSub, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.CostSheetUsageTrackingLazy.ConsumerGroup,
	)
	if err != nil {
		params.Logger.Fatalw("failed to create lazy pubsub", "error", err)
		return nil
	}
	ev.lazyPubSub = lazyPubSub
	return ev
}

// PublishEvent publishes an event to the cost sheet usage tracking topic
func (s *costsheetUsageTrackingService) PublishEvent(ctx context.Context, event *events.Event, isBackfill bool) error {
	// Create message payload
	payload, err := json.Marshal(event)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to marshal event for costsheet usage tracking").
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

	pubSub := s.pubSub
	topic := s.Config.CostSheetUsageTracking.Topic

	if pubSub == nil {
		return ierr.NewError("pubsub not initialized").
			WithHint("Please check the config").
			Mark(ierr.ErrSystem)
	}

	s.Logger.Debugw("publishing event for cost sheet usage tracking",
		"event_id", event.ID,
		"event_name", event.EventName,
		"partition_key", partitionKey,
		"topic", topic,
	)

	// Publish to costsheet usage tracking topic using the backfill PubSub (Kafka)
	if err := pubSub.Publish(ctx, topic, msg); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to publish event for costsheet usage tracking").
			Mark(ierr.ErrSystem)
	}
	return nil
}

// RegisterHandler registers a handler for the cost sheet usage tracking topic with rate limiting
func (s *costsheetUsageTrackingService) RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration) {
	if !cfg.CostSheetUsageTracking.Enabled {
		s.Logger.Infow("cost sheet usage tracking handler disabled by configuration")
		return
	}

	// Add throttle middleware to this specific handler
	throttle := middleware.NewThrottle(cfg.CostSheetUsageTracking.RateLimit, time.Second)

	// Add the handler
	router.AddNoPublishHandler(
		"costsheet_usage_tracking_handler",
		cfg.CostSheetUsageTracking.Topic,
		s.pubSub,
		s.processMessage,
		throttle.Middleware,
	)

	s.Logger.Infow("registered event costsheet usage tracking handler",
		"topic", cfg.CostSheetUsageTracking.Topic,
		"rate_limit", cfg.CostSheetUsageTracking.RateLimit,
	)
}

// RegisterHandlerLazy registers a handler for the costsheet usage tracking topic with rate limiting
func (s *costsheetUsageTrackingService) RegisterHandlerLazy(router *pubsubRouter.Router, cfg *config.Configuration) {
	if !cfg.CostSheetUsageTrackingLazy.Enabled {
		s.Logger.Infow("cost sheet usage tracking lazy handler disabled by configuration")
		return
	}

	// Add throttle middleware to this specific handler
	throttle := middleware.NewThrottle(cfg.CostSheetUsageTrackingLazy.RateLimit, time.Second)

	// Add the handler
	router.AddNoPublishHandler(
		"costsheet_usage_tracking_lazy_handler",
		cfg.CostSheetUsageTrackingLazy.Topic,
		s.lazyPubSub,
		s.processMessage,
		throttle.Middleware,
	)

	s.Logger.Infow("registered event costsheet usage tracking lazy handler",
		"topic", cfg.CostSheetUsageTrackingLazy.Topic,
		"rate_limit", cfg.CostSheetUsageTrackingLazy.RateLimit,
	)
}

// Process a single event message for cost sheet usage tracking
func (s *costsheetUsageTrackingService) processMessage(msg *message.Message) error {
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
		s.Logger.Errorw("failed to unmarshal event for cost sheet usage tracking",
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
		s.Logger.Errorw("failed to process event for cost sheet usage tracking",
			"error", err,
			"event_id", event.ID,
			"event_name", event.EventName,
		)
		return err // Return error for retry
	}

	s.Logger.Infow("event for cost sheet usage tracking processed successfully",
		"event_id", event.ID,
		"event_name", event.EventName,
	)

	return nil
}

// Process a single event for cost sheet usage tracking
func (s *costsheetUsageTrackingService) processEvent(ctx context.Context, event *events.Event) error {
	s.Logger.Debugw("processing event",
		"event_id", event.ID,
		"event_name", event.EventName,
		"external_customer_id", event.ExternalCustomerID,
		"ingested_at", event.IngestedAt,
	)

	costUsage, err := s.prepareProcessedEvents(ctx, event)
	if err != nil {
		s.Logger.Errorw("failed to prepare cost usage",
			"error", err,
			"event_id", event.ID,
		)
		return err
	}

	if len(costUsage) > 0 {
		if err := s.costUsageRepo.BulkInsertProcessedEvents(ctx, costUsage); err != nil {
			return err
		}
		return nil
	}

	return nil
}

// Generate a unique hash for deduplication
// there are 2 cases:
// 1. event_name + event_id // for non COUNT_UNIQUE aggregation types
// 2. event_name + event_field_name + event_field_value // for COUNT_UNIQUE aggregation types
func (s *costsheetUsageTrackingService) generateUniqueHash(event *events.Event, meter *meter.Meter) string {
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

func (s *costsheetUsageTrackingService) prepareProcessedEvents(ctx context.Context, event *events.Event) ([]*events.CostUsage, error) {
	results := make([]*events.CostUsage, 0)

	// CASE 1: Get active costsheet (with caching)
	costSheet, err := s.getActiveCostsheetWithCache(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			s.Logger.Debugw("no active costsheet found for event, skipping",
				"event_id", event.ID,
				"tenant_id", event.TenantID,
				"environment_id", event.EnvironmentID,
			)
			return results, nil
		}
		return results, err
	}

	// CASE 2: Lookup customer (similar to feature usage tracking)
	var cust *customer.Customer
	cust, err = s.CustomerRepo.GetByLookupKey(ctx, event.ExternalCustomerID)
	if err != nil {
		s.Logger.Warnw("customer not found for event, skipping",
			"event_id", event.ID,
			"external_customer_id", event.ExternalCustomerID,
			"error", err,
		)
		return results, err
	}

	// Set the customer ID in the event if it's not already set
	if event.CustomerID == "" {
		event.CustomerID = cust.ID
	}

	// CASE 3: Fetch prices associated with this costsheet
	priceFilter := types.NewNoLimitPriceFilter().
		WithEntityIDs([]string{costSheet.ID}).
		WithEntityType(types.PRICE_ENTITY_TYPE_COSTSHEET).
		WithStatus(types.StatusPublished).
		WithExpand(string(types.ExpandMeters))

	prices, err := s.PriceRepo.List(ctx, priceFilter)
	if err != nil {
		s.Logger.Errorw("failed to get prices for costsheet",
			"error", err,
			"event_id", event.ID,
			"costsheet_id", costSheet.ID,
		)
		return results, err
	}

	if len(prices) == 0 {
		s.Logger.Debugw("no prices found for costsheet, skipping",
			"event_id", event.ID,
			"costsheet_id", costSheet.ID,
		)
		return results, nil
	}

	// Build price map and collect meter IDs
	priceMap := make(map[string]*price.Price)
	meterIDs := make([]string, 0)
	for _, price := range prices {
		if !price.IsUsage() {
			continue
		}
		priceMap[price.ID] = price
		if price.MeterID != "" {
			meterIDs = append(meterIDs, price.MeterID)
		}
	}

	// Remove duplicate meter IDs
	meterIDs = lo.Uniq(meterIDs)

	if len(meterIDs) == 0 {
		s.Logger.Debugw("no usage-based prices found for costsheet, skipping",
			"event_id", event.ID,
			"costsheet_id", costSheet.ID,
		)
		return results, nil
	}

	// CASE 4: Fetch all meters in bulk
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

	// CASE 5: Build feature maps
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
			return results, err
		}

		for _, f := range features {
			featureMap[f.ID] = f
			featureMeterMap[f.MeterID] = f
		}
	}

	// CASE 6: Convert prices to slice for matching
	prices = make([]*price.Price, 0, len(priceMap))
	for _, p := range priceMap {
		prices = append(prices, p)
	}

	// CASE 7: Find matching prices/meters for this event
	matches := s.findMatchingPricesForEvent(event, prices, meterMap)

	if len(matches) == 0 {
		s.Logger.Debugw("no matching prices/meters found for event",
			"event_id", event.ID,
			"event_name", event.EventName,
			"costsheet_id", costSheet.ID,
		)
		return results, nil
	}

	// CASE 8: Process each match and create CostUsage records
	for _, match := range matches {
		// Create a unique hash for deduplication
		uniqueHash := s.generateUniqueHash(event, match.Meter)

		// Create a new cost usage record
		costUsage := &events.CostUsage{
			Event:       *event,
			CostSheetID: costSheet.ID,
			PriceID:     match.Price.ID,
			MeterID:     match.Meter.ID,
			UniqueHash:  uniqueHash,
			Sign:        1, // Default to positive sign
		}

		// Set feature ID if available
		if feature, ok := featureMeterMap[match.Meter.ID]; ok {
			costUsage.FeatureID = feature.ID
		} else {
			s.Logger.Warnw("feature not found for meter",
				"event_id", event.ID,
				"meter_id", match.Meter.ID,
			)
			// Continue without feature ID - it's optional for cost tracking
		}

		// Extract quantity based on meter aggregation
		quantity, _ := s.extractQuantityFromEvent(event, match.Meter)

		// Validate the quantity is positive and within reasonable bounds
		if quantity.IsNegative() {
			s.Logger.Warnw("negative quantity calculated, setting to zero",
				"event_id", event.ID,
				"meter_id", match.Meter.ID,
				"calculated_quantity", quantity.String(),
			)
			quantity = decimal.Zero
		}

		// Store quantity
		costUsage.QtyTotal = quantity

		results = append(results, costUsage)
	}

	if len(results) > 0 {
		s.Logger.Debugw("cost usage processing request prepared",
			"event_id", event.ID,
			"cost_usage_count", len(results),
		)
		return results, nil
	}

	return results, nil
}

// Find matching prices for an event based on meter configuration and filters
func (s *costsheetUsageTrackingService) findMatchingPricesForEvent(
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
			s.Logger.Warnw("cost sheet usage tracking: meter not found for price",
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
func (s *costsheetUsageTrackingService) checkMeterFilters(event *events.Event, filters []meter.Filter) bool {
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
func (s *costsheetUsageTrackingService) extractQuantityFromEvent(
	event *events.Event,
	meter *meter.Meter,
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
		// Weighted sum requires subscription and period context which we don't have for cost sheet usage tracking
		// For cost sheet usage tracking, we'll treat it as a regular sum
		s.Logger.Warnw("weighted_sum aggregation not fully supported for cost sheet usage tracking, treating as sum",
			"event_id", event.ID,
			"meter_id", meter.ID,
		)
		if meter.Aggregation.Field == "" {
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

		decimalValue, stringValue := s.convertValueToDecimal(val, event, meter)
		return decimalValue, stringValue

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
func (s *costsheetUsageTrackingService) convertValueToDecimal(val interface{}, event *events.Event, meter *meter.Meter) (decimal.Decimal, string) {
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
func (s *costsheetUsageTrackingService) convertValueToString(val interface{}) string {
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

// ReprocessEvents triggers reprocessing of events for a customer or with other filters
func (s *costsheetUsageTrackingService) ReprocessEvents(ctx context.Context, params *events.ReprocessEventsParams) error {
	s.Logger.Infow("starting event reprocessing for cost sheet usage tracking",
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
		unprocessedEvents, err := s.eventRepo.FindUnprocessedEventsFromFeatureUsage(ctx, findParams)
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

		// Publish each event to the cost sheet usage tracking topic
		for _, event := range unprocessedEvents {
			if err := s.PublishEvent(ctx, event, true); err != nil {
				s.Logger.Errorw("failed to publish event for reprocessing for cost sheet usage tracking",
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

		s.Logger.Infow("published events for reprocessing for cost sheet usage tracking",
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

	s.Logger.Infow("completed event reprocessing for cost sheet usage tracking",
		"external_customer_id", params.ExternalCustomerID,
		"event_name", params.EventName,
		"batches_processed", processedBatches,
		"total_events_found", totalEventsFound,
		"total_events_published", totalEventsPublished,
	)

	return nil
}

func (s *costsheetUsageTrackingService) GetCostSheetUsageAnalytics(ctx context.Context, req *dto.GetCostAnalyticsRequest) (*dto.GetCostAnalyticsResponse, error) {

	// STEP0: Validate request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// STEP1: Get active costsheet (with caching)
	costSheet, err := s.getActiveCostsheetWithCache(ctx)
	if err != nil {
		s.Logger.Errorw("failed to fetch active costsheet", "error", err)
		return nil, err
	}

	if costSheet == nil {
		s.Logger.Debugw("no active costsheet found for tenant")
		return nil, ierr.NewError("no active costsheet found").
			WithHint("Please activate a costsheet for this tenant").
			Mark(ierr.ErrNotFound)
	}

	// STEP3: Get customer if external_customer_id is provided
	var customer *customer.Customer
	if req.ExternalCustomerID != "" {
		customer, err = s.CustomerRepo.GetByLookupKey(ctx, req.ExternalCustomerID)
		if err != nil {
			s.Logger.Errorw("failed to get customer", "error", err, "external_customer_id", req.ExternalCustomerID)
			return nil, err
		}
		if customer == nil {
			return nil, ierr.NewError("customer not found").
				WithHint("No customer found with the provided external_customer_id").
				WithReportableDetails(map[string]interface{}{
					"external_customer_id": req.ExternalCustomerID,
				}).
				Mark(ierr.ErrNotFound)
		}
	}

	// STEP4: Fetch analytics from costsheet usage repo
	analytics, err := s.fetchAnalytics(ctx, costSheet, req, customer)
	if err != nil {
		s.Logger.Errorw("failed to fetch costsheet analytics", "error", err, "costsheet_id", costSheet.ID)
		return nil, err
	}

	// STEP5: Calculate costs for analytics items
	priceService := NewPriceService(s.ServiceParams)
	if err := s.calculateCosts(ctx, costSheet, analytics, priceService); err != nil {
		s.Logger.Errorw("failed to calculate costs", "error", err, "costsheet_id", costSheet.ID)
		return nil, err
	}

	// STEP6: Build response
	response := s.buildAnalyticsResponse(costSheet, req, analytics, customer)

	s.Logger.Debugw("successfully retrieved costsheet analytics",
		"costsheet_id", costSheet.ID,
		"analytics_count", len(analytics),
		"total_cost", response.TotalCost,
	)

	return response, nil
}

func (s *costsheetUsageTrackingService) fetchAnalytics(ctx context.Context, costsheet *dto.CostsheetResponse, req *dto.GetCostAnalyticsRequest, customer *customer.Customer) ([]*events.DetailedUsageAnalytic, error) {
	// STEP1: Extract features and build max bucket and sum bucket features maps
	maxBucketFeatures, sumBucketFeatures, featureMap, meterMap, err := s.buildBucketFeaturesFromCostsheet(ctx, costsheet)
	if err != nil {
		return nil, err
	}

	// STEP2: Create analytics parameters
	params := &events.UsageAnalyticsParams{
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		FeatureIDs:    req.FeatureIDs,
		StartTime:     req.StartTime,
		EndTime:       req.EndTime,
		GroupBy:       []string{"feature_id"}, // Default grouping for costsheet analytics
	}

	// Set customer info if provided
	if customer != nil {
		params.CustomerID = customer.ID
		params.ExternalCustomerID = customer.ExternalID
	}

	// STEP3: Fetch analytics using repository method
	analytics, err := s.costUsageRepo.GetDetailedUsageAnalytics(ctx, costsheet.ID, params.ExternalCustomerID, params, maxBucketFeatures, sumBucketFeatures)
	if err != nil {
		s.Logger.Errorw("failed to get detailed usage analytics from costsheet repo",
			"error", err,
			"costsheet_id", costsheet.ID,
			"external_customer_id", params.ExternalCustomerID,
		)
		return nil, err
	}

	// STEP4: Enrich analytics with feature and meter metadata
	s.enrichAnalyticsWithMetadata(analytics, featureMap, meterMap)

	s.Logger.Debugw("fetched analytics from costsheet usage repo",
		"costsheet_id", costsheet.ID,
		"analytics_count", len(analytics),
		"feature_count", len(featureMap),
		"meter_count", len(meterMap),
		"max_bucket_features_count", len(maxBucketFeatures),
		"sum_bucket_features_count", len(sumBucketFeatures),
	)

	return analytics, nil
}

// buildBucketFeaturesFromCostsheet extracts features from costsheet and builds max and sum bucket features maps
func (s *costsheetUsageTrackingService) buildBucketFeaturesFromCostsheet(ctx context.Context, costsheet *dto.CostsheetResponse) (map[string]*events.MaxBucketFeatureInfo, map[string]*events.SumBucketFeatureInfo, map[string]*feature.Feature, map[string]*meter.Meter, error) {
	// Collect meter IDs from prices
	meterIDs := make([]string, 0)
	meterIDSet := make(map[string]bool)

	for _, priceResp := range costsheet.Prices {
		if priceResp == nil || !priceResp.IsUsage() {
			continue
		}
		if priceResp.MeterID != "" && !meterIDSet[priceResp.MeterID] {
			meterIDs = append(meterIDs, priceResp.MeterID)
			meterIDSet[priceResp.MeterID] = true
		}
	}

	if len(meterIDs) == 0 {
		s.Logger.Debugw("no usage-based prices with meters found in costsheet",
			"costsheet_id", costsheet.ID,
		)
		return make(map[string]*events.MaxBucketFeatureInfo), make(map[string]*events.SumBucketFeatureInfo), make(map[string]*feature.Feature), make(map[string]*meter.Meter), nil
	}

	// Fetch all meters in bulk
	meterFilter := types.NewNoLimitMeterFilter()
	meterFilter.MeterIDs = meterIDs
	meters, err := s.MeterRepo.List(ctx, meterFilter)
	if err != nil {
		s.Logger.Errorw("failed to get meters for costsheet analytics",
			"error", err,
			"costsheet_id", costsheet.ID,
			"meter_count", len(meterIDs),
		)
		return nil, nil, nil, nil, err
	}

	// Build meter map
	meterMap := make(map[string]*meter.Meter)
	for _, m := range meters {
		meterMap[m.ID] = m
	}

	// Fetch features associated with these meters
	featureMap := make(map[string]*feature.Feature)
	if len(meterMap) > 0 {
		featureFilter := types.NewNoLimitFeatureFilter()
		featureFilter.MeterIDs = lo.Keys(meterMap)
		features, err := s.FeatureRepo.List(ctx, featureFilter)
		if err != nil {
			s.Logger.Errorw("failed to get features for costsheet analytics",
				"error", err,
				"costsheet_id", costsheet.ID,
				"meter_count", len(meterMap),
			)
			return nil, nil, nil, nil, err
		}

		for _, f := range features {
			featureMap[f.ID] = f
		}
	}

	// Build max bucket and sum bucket features maps
	maxBucketFeatures := make(map[string]*events.MaxBucketFeatureInfo)
	sumBucketFeatures := make(map[string]*events.SumBucketFeatureInfo)
	for _, f := range featureMap {
		if f.MeterID != "" {
			if m, exists := meterMap[f.MeterID]; exists {
				if m.IsBucketedMaxMeter() {
					maxBucketFeatures[f.ID] = &events.MaxBucketFeatureInfo{
						FeatureID:    f.ID,
						MeterID:      f.MeterID,
						BucketSize:   types.WindowSize(m.Aggregation.BucketSize),
						EventName:    m.EventName,
						PropertyName: m.Aggregation.Field,
					}
				} else if m.IsBucketedSumMeter() {
					sumBucketFeatures[f.ID] = &events.SumBucketFeatureInfo{
						FeatureID:    f.ID,
						MeterID:      f.MeterID,
						BucketSize:   types.WindowSize(m.Aggregation.BucketSize),
						EventName:    m.EventName,
						PropertyName: m.Aggregation.Field,
					}
				}
			}
		}
	}

	return maxBucketFeatures, sumBucketFeatures, featureMap, meterMap, nil
}

// enrichAnalyticsWithMetadata enriches analytics items with feature and meter metadata
func (s *costsheetUsageTrackingService) enrichAnalyticsWithMetadata(analytics []*events.DetailedUsageAnalytic, featureMap map[string]*feature.Feature, meterMap map[string]*meter.Meter) {
	for _, item := range analytics {
		if feature, exists := featureMap[item.FeatureID]; exists {
			item.FeatureName = feature.Name
			item.Unit = feature.UnitSingular
			item.UnitPlural = feature.UnitPlural
		}

		if meter, exists := meterMap[item.MeterID]; exists {
			item.EventName = meter.EventName
			item.AggregationType = meter.Aggregation.Type
		}
	}
}

// calculateCosts calculates costs for all analytics items
func (s *costsheetUsageTrackingService) calculateCosts(ctx context.Context, costsheet *dto.CostsheetResponse, analytics []*events.DetailedUsageAnalytic, priceService PriceService) error {
	// Build price map from costsheet
	priceMap := make(map[string]*price.Price)
	for _, priceResp := range costsheet.Prices {
		if priceResp != nil && priceResp.Price != nil {
			priceMap[priceResp.ID] = priceResp.Price
		}
	}

	// Fetch meters for determining bucketed max meters
	meterIDs := make([]string, 0)
	meterIDSet := make(map[string]bool)
	for _, item := range analytics {
		if item.MeterID != "" && !meterIDSet[item.MeterID] {
			meterIDs = append(meterIDs, item.MeterID)
			meterIDSet[item.MeterID] = true
		}
	}

	meterMap := make(map[string]*meter.Meter)
	if len(meterIDs) > 0 {
		meterFilter := types.NewNoLimitMeterFilter()
		meterFilter.MeterIDs = meterIDs
		meters, err := s.MeterRepo.List(ctx, meterFilter)
		if err != nil {
			s.Logger.Errorw("failed to get meters for cost calculation",
				"error", err,
				"costsheet_id", costsheet.ID,
			)
			return err
		}
		for _, m := range meters {
			meterMap[m.ID] = m
		}
	}

	// Calculate cost for each analytics item
	for _, item := range analytics {
		price, hasPrice := priceMap[item.PriceID]
		if !hasPrice {
			s.Logger.Debugw("price not found for analytics item",
				"price_id", item.PriceID,
				"feature_id", item.FeatureID,
			)
			continue
		}

		meter, hasMeter := meterMap[item.MeterID]
		if !hasMeter {
			s.Logger.Debugw("meter not found for analytics item",
				"meter_id", item.MeterID,
				"feature_id", item.FeatureID,
			)
			continue
		}

		// Calculate cost based on meter type
		if meter.IsBucketedMaxMeter() || meter.IsBucketedSumMeter() {
			s.calculateBucketedCost(ctx, priceService, item, price)
		} else {
			s.calculateRegularCost(ctx, priceService, item, meter, price)
		}
	}

	s.Logger.Debugw("calculated costs for costsheet analytics",
		"costsheet_id", costsheet.ID,
		"analytics_items_processed", len(analytics),
	)

	return nil
}

// buildAnalyticsResponse builds the analytics response from analytics items
func (s *costsheetUsageTrackingService) buildAnalyticsResponse(costsheet *dto.CostsheetResponse, req *dto.GetCostAnalyticsRequest, analytics []*events.DetailedUsageAnalytic, customer *customer.Customer) *dto.GetCostAnalyticsResponse {
	response := &dto.GetCostAnalyticsResponse{
		CostsheetID:   costsheet.ID,
		StartTime:     req.StartTime,
		EndTime:       req.EndTime,
		TotalCost:     decimal.Zero,
		TotalQuantity: decimal.Zero,
		TotalEvents:   0,
		CostAnalytics: make([]dto.CostAnalyticItem, 0, len(analytics)),
	}

	if customer != nil {
		response.CustomerID = customer.ID
		response.ExternalCustomerID = customer.ExternalID
	}

	// Convert analytics to cost analytic items and calculate totals
	for _, item := range analytics {
		costItem := dto.CostAnalyticItem{
			MeterID:       item.MeterID,
			MeterName:     item.EventName,
			Properties:    item.Properties,
			TotalCost:     item.TotalCost,
			TotalQuantity: item.TotalUsage,
			TotalEvents:   int64(item.EventCount),
			Currency:      item.Currency,
			PriceID:       item.PriceID,
			CostsheetID:   costsheet.ID,
			CostByPeriod:  make([]dto.CostPoint, 0, len(item.Points)),
		}

		if customer != nil {
			costItem.CustomerID = customer.ID
			costItem.ExternalCustomerID = customer.ExternalID
		}

		// Add time-series points
		for _, point := range item.Points {
			costItem.CostByPeriod = append(costItem.CostByPeriod, dto.CostPoint{
				Timestamp:  point.Timestamp,
				Cost:       point.Cost,
				Quantity:   point.Usage,
				EventCount: int64(point.EventCount),
			})
		}

		response.CostAnalytics = append(response.CostAnalytics, costItem)

		// Accumulate totals
		response.TotalCost = response.TotalCost.Add(item.TotalCost)
		response.TotalQuantity = response.TotalQuantity.Add(item.TotalUsage)
		response.TotalEvents += int64(item.EventCount)

		// Set currency from first item
		if response.Currency == "" && item.Currency != "" {
			response.Currency = item.Currency
		}
	}

	return response
}

// calculateBucketedCost calculates cost for bucketed max meters
func (s *costsheetUsageTrackingService) calculateBucketedCost(ctx context.Context, priceService PriceService, item *events.DetailedUsageAnalytic, price *price.Price) {
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
		// Treat total usage (sum of bucket maxes) as single bucket
		// For MAX with buckets, TotalUsage contains the sum of bucket maxes
		if item.TotalUsage.IsPositive() {
			bucketedValues := []decimal.Decimal{item.TotalUsage}
			cost = priceService.CalculateBucketedCost(ctx, price, bucketedValues)
		}
	}

	item.TotalCost = cost
	item.Currency = price.Currency
}

// calculateRegularCost calculates cost for regular meters
func (s *costsheetUsageTrackingService) calculateRegularCost(ctx context.Context, priceService PriceService, item *events.DetailedUsageAnalytic, meter *meter.Meter, price *price.Price) {
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

// getCorrectUsageValue returns the correct usage value based on the meter's aggregation type
func (s *costsheetUsageTrackingService) getCorrectUsageValue(item *events.DetailedUsageAnalytic, aggregationType types.AggregationType) decimal.Decimal {
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
func (s *costsheetUsageTrackingService) getCorrectUsageValueForPoint(point events.UsageAnalyticPoint, aggregationType types.AggregationType) decimal.Decimal {
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

// getActiveCostsheetWithCache fetches the active costsheet for the tenant with caching
// It first checks the cache, and if not found, fetches from the service and caches the result
func (s *costsheetUsageTrackingService) getActiveCostsheetWithCache(ctx context.Context) (*dto.CostsheetResponse, error) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixCostsheet, tenantID, environmentID)

	// Try to get from cache first
	cacheClient := cache.GetInMemoryCache()
	var costSheet *dto.CostsheetResponse
	if cached, found := cacheClient.ForceCacheGet(ctx, cacheKey); found {
		if cachedCostSheet, ok := cached.(*dto.CostsheetResponse); ok {
			s.Logger.Debugw("costsheet cache hit", "tenant_id", tenantID, "environment_id", environmentID)
			costSheet = cachedCostSheet
		}
	}

	// If not in cache, fetch from service
	if costSheet == nil {
		costSheetService := NewCostsheetService(s.ServiceParams)
		var err error
		costSheet, err = costSheetService.GetActiveCostsheetForTenant(ctx)
		if err != nil {
			return nil, err
		}

		// Cache the result for 5 minutes
		cacheClient.ForceCacheSet(ctx, cacheKey, costSheet, 5*time.Minute)
		s.Logger.Debugw("costsheet cached", "tenant_id", tenantID, "environment_id", environmentID)
	}

	return costSheet, nil
}
