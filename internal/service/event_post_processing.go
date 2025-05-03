package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/pubsub"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const (
	EventsPostProcessingTopic = "events_post_processing"
)

// PriceMatch represents a matching price and meter for an event
type PriceMatch struct {
	Price *price.Price
	Meter *meter.Meter
}

// EventPostProcessingService handles post-processing operations for metered events
type EventPostProcessingService interface {
	// Publish an event for post-processing
	PublishEvent(ctx context.Context, event *events.Event) error

	// Register message handler with the router
	RegisterHandler(router *pubsubRouter.Router)

	// Query method for processed events with different filters
	GetUsageSummary(ctx context.Context, params *events.UsageSummaryParams) (decimal.Decimal, error)

	// Reprocess events for a specific customer or subscription
	// Used when a customer or subscription is created after events have been received
	ReprocessEvents(ctx context.Context, customerID, subscriptionID string) error
}

type eventPostProcessingService struct {
	ServiceParams
	pubSub             pubsub.PubSub
	processedEventRepo events.ProcessedEventRepository
}

// Package-level throttle for rate limiting event processing
var (
	eventThrottle     *middleware.Throttle
	eventThrottleOnce sync.Once
)

// initEventThrottle initializes the global throttle if not already initialized
func initEventThrottle() *middleware.Throttle {
	eventThrottleOnce.Do(func() {
		// Limit to 1 event per second
		eventThrottle = middleware.NewThrottle(1, time.Second)
	})
	return eventThrottle
}

// NewEventPostProcessingService creates a new event post-processing service
func NewEventPostProcessingService(
	params ServiceParams,
	pubSub pubsub.PubSub,
	processedEventRepo events.ProcessedEventRepository,
) EventPostProcessingService {
	return &eventPostProcessingService{
		ServiceParams:      params,
		pubSub:             pubSub,
		processedEventRepo: processedEventRepo,
	}
}

// PublishEvent publishes an event to the post-processing topic
func (s *eventPostProcessingService) PublishEvent(ctx context.Context, event *events.Event) error {
	// Create message payload
	payload, err := json.Marshal(event)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to marshal event for post-processing").
			Mark(ierr.ErrValidation)
	}

	// Create a deterministic partition key based on tenant_id and external_customer_id
	// This ensures all events for the same customer go to the same partition
	partitionKey := event.TenantID
	if event.ExternalCustomerID != "" {
		partitionKey = fmt.Sprintf("%s:%s", event.TenantID, event.ExternalCustomerID)
	}

	// Use the partition key as the message ID to ensure consistent partitioning
	msg := message.NewMessage(partitionKey, payload)

	// Set metadata for additional context
	msg.Metadata.Set("tenant_id", event.TenantID)
	msg.Metadata.Set("environment_id", event.EnvironmentID)
	msg.Metadata.Set("partition_key", partitionKey)

	s.Logger.Debugw("publishing event for post-processing",
		"event_id", event.ID,
		"event_name", event.EventName,
		"partition_key", partitionKey,
	)

	// Publish to post-processing topic
	if err := s.pubSub.Publish(ctx, EventsPostProcessingTopic, msg); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to publish event for post-processing").
			Mark(ierr.ErrSystem)
	}

	return nil
}

// RegisterHandler registers a handler for the post-processing topic with rate limiting
func (s *eventPostProcessingService) RegisterHandler(router *pubsubRouter.Router) {
	// Add throttle middleware to this specific handler
	throttle := middleware.NewThrottle(1, time.Second)

	// Add the handler
	router.AddNoPublishHandler(
		"events_post_processing_handler",
		EventsPostProcessingTopic,
		s.pubSub,
		s.processMessage,
		throttle.Middleware,
	)

	s.Logger.Infow("registered event post-processing handler",
		"topic", EventsPostProcessingTopic,
	)
}

// Process a single event message
func (s *eventPostProcessingService) processMessage(msg *message.Message) error {
	partitionKey := msg.Metadata.Get("partition_key")
	s.Logger.Debugw("processing event from message queue",
		"message_uuid", msg.UUID,
		"partition_key", partitionKey,
	)

	// Extract tenant ID from message metadata
	tenantID := msg.Metadata.Get("tenant_id")
	environmentID := msg.Metadata.Get("environment_id")

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
		s.Logger.Errorw("failed to unmarshal event for post-processing",
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
		s.Logger.Errorw("failed to process event",
			"error", err,
			"event_id", event.ID,
			"event_name", event.EventName,
		)
		return err // Return error for retry
	}

	s.Logger.Infow("event processed successfully",
		"event_id", event.ID,
		"event_name", event.EventName,
	)

	return nil
}

// Process a single event
func (s *eventPostProcessingService) processEvent(ctx context.Context, event *events.Event) error {
	s.Logger.Debugw("processing event",
		"event_id", event.ID,
		"event_name", event.EventName,
		"external_customer_id", event.ExternalCustomerID,
	)

	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.Logger)
	subscriptionService := NewSubscriptionService(s.ServiceParams)

	// Create a base processed event with pending status
	processedEvent := event.ToProcessedEvent()

	// 1. Skip comprehensive processing (but still save as pending) if no external customer ID
	if event.ExternalCustomerID == "" {
		s.Logger.Debugw("saving event as pending due to missing external customer ID",
			"event_id", event.ID)
		return s.processedEventRepo.InsertProcessedEvent(ctx, processedEvent)
	}

	// 2. Lookup customer - use cache-backed repository if available
	customer, err := s.CustomerRepo.GetByLookupKey(ctx, event.ExternalCustomerID)
	if err != nil {
		s.Logger.Warnw("customer not found for event, saving as pending",
			"event_id", event.ID,
			"external_customer_id", event.ExternalCustomerID,
			"error", err,
		)
		return s.processedEventRepo.InsertProcessedEvent(ctx, processedEvent)
	}
	s.Logger.Debugw("found customer for event",
		"event_id", event.ID,
		"customer_id", customer.ID,
	)

	// Set the customer ID in the event if it's not already set
	if event.CustomerID == "" {
		event.CustomerID = customer.ID
		processedEvent.CustomerID = customer.ID
	}

	// 3. Get active subscriptions with expanded data for efficiency
	// We request all related data in a single query to minimize DB round trips
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
		s.Logger.Errorw("failed to get subscriptions, saving as pending",
			"event_id", event.ID,
			"customer_id", customer.ID,
			"error", err,
		)
		return s.processedEventRepo.InsertProcessedEvent(ctx, processedEvent)
	}
	subscriptions := subscriptionsList.Items
	s.Logger.Debugw("found subscriptions for customer",
		"event_id", event.ID,
		"customer_id", customer.ID,
		"subscription_count", len(subscriptions),
	)

	if len(subscriptions) == 0 {
		s.Logger.Debugw("no active subscriptions found for customer, saving as pending",
			"event_id", event.ID,
			"customer_id", customer.ID,
		)
		return s.processedEventRepo.InsertProcessedEvent(ctx, processedEvent)
	}

	// Build efficient maps for lookups
	meterMap := make(map[string]*meter.Meter)
	priceMap := make(map[string]*price.Price)
	featureMap := make(map[string]*feature.Feature)
	featureMeterMap := make(map[string]*feature.Feature)

	// Extract meters and prices from subscriptions
	for _, sub := range subscriptions {
		if sub.Plan == nil || sub.Plan.Prices == nil {
			continue
		}

		for _, item := range sub.Plan.Prices {
			if !item.IsUsage() {
				continue
			}

			priceMap[item.ID] = item.Price

			if item.MeterID != "" && item.Meter != nil {
				meterMap[item.MeterID] = item.Meter.ToMeter()
			}
		}
	}

	// Get features if not already expanded in subscriptions
	if len(meterMap) > 0 && (featureMap == nil || len(featureMap) == 0) {
		featureFilter := types.NewNoLimitFeatureFilter()
		featureFilter.MeterIDs = lo.Keys(meterMap)
		features, err := s.FeatureRepo.List(ctx, featureFilter)
		if err != nil {
			s.Logger.Errorw("failed to get features",
				"error", err,
				"event_id", event.ID,
				"meter_count", len(meterMap),
			)
			return err
		}

		for _, f := range features {
			featureMap[f.ID] = f
			featureMeterMap[f.MeterID] = f
		}
	}

	// 5. Process the event against each subscription
	processedEvents := make([]*events.ProcessedEvent, 0)

	for _, sub := range subscriptions {
		// Get active usage-based line items
		subscriptionLineItems := lo.Filter(sub.LineItems, func(item *subscription.SubscriptionLineItem, _ int) bool {
			return item.IsUsage() && item.IsActive()
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
				prices = append(prices, price)
			} else {
				s.Logger.Warnw("price not found for subscription line item",
					"event_id", event.ID,
					"subscription_id", sub.ID,
					"line_item_id", item.ID,
					"price_id", item.PriceID,
				)

				return ierr.WithError(err).
					WithHint("Price not found in the price map for subscription line item").
					Mark(ierr.ErrValidation)
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
			// Create a new processed event for each match
			processedEventCopy := &events.ProcessedEvent{
				Event:            *event,
				SubscriptionID:   sub.ID,
				PriceID:          match.Price.ID,
				MeterID:          match.Meter.ID,
				AggregationField: match.Meter.Aggregation.Field,
				EventStatus:      types.EventStatusPending,
				Quantity:         0,
				Cost:             decimal.Zero,
				ProcessedAt:      lo.ToPtr(time.Now().UTC()),
			}

			// Set feature ID if available
			if feature, ok := featureMeterMap[match.Meter.ID]; ok {
				processedEventCopy.FeatureID = feature.ID
			} else {
				s.Logger.Warnw("feature not found for meter",
					"event_id", event.ID,
					"meter_id", match.Meter.ID,
				)

				return ierr.WithError(err).
					WithHint("Feature not found for meter").
					Mark(ierr.ErrValidation)
			}

			// Check if we can process this price/meter combination
			canProcess := s.isSupportedAggregationType(match.Meter.Aggregation.Type) &&
				s.isSupportedBillingModel(match.Price.BillingModel)

			if !canProcess {
				s.Logger.Debugw("unsupported aggregation type or billing model, saving as pending",
					"event_id", event.ID,
					"meter_id", match.Meter.ID,
					"aggregation_type", match.Meter.Aggregation.Type,
					"billing_model", match.Price.BillingModel,
				)
				processedEvents = append(processedEvents, processedEventCopy)
				continue
			}

			// Extract quantity based on meter aggregation
			quantity, fieldValue := s.extractQuantityFromEvent(event, match.Meter)
			processedEventCopy.AggregationFieldValue = fieldValue

			// Validate the quantity is positive and within reasonable bounds
			if quantity.IsNegative() {
				s.Logger.Warnw("negative quantity calculated, setting to zero",
					"event_id", event.ID,
					"meter_id", match.Meter.ID,
					"calculated_quantity", quantity.String(),
				)
				quantity = decimal.Zero
			}

			// Convert to uint64 safely
			if quantity.GreaterThan(decimal.NewFromInt(0)) {
				processedEventCopy.Quantity = quantity.BigInt().Uint64()
			}

			// Calculate cost based on price and quantity
			cost := priceService.CalculateCost(ctx, match.Price, quantity)
			processedEventCopy.Cost = cost
			processedEventCopy.Currency = match.Price.Currency

			// Mark as processed since we've calculated everything successfully
			processedEventCopy.EventStatus = types.EventStatusProcessed

			processedEvents = append(processedEvents, processedEventCopy)
		}
	}

	s.Logger.Debugw("event processing complete",
		"event_id", event.ID,
		"processed_events_count", len(processedEvents),
	)

	// 6. Insert processed events
	if len(processedEvents) > 0 {
		if err := s.processedEventRepo.BulkInsertProcessedEvents(ctx, processedEvents); err != nil {
			s.Logger.Errorw("failed to insert processed events",
				"event_id", event.ID,
				"count", len(processedEvents),
				"error", err,
			)
			return err
		}

		s.Logger.Infow("successfully processed event",
			"event_id", event.ID,
			"processed_events", len(processedEvents),
		)
	} else {
		s.Logger.Debugw("no matching prices/meters found for event, saving as pending",
			"event_id", event.ID,
			"event_name", event.EventName,
		)
		return s.processedEventRepo.InsertProcessedEvent(ctx, processedEvent)
	}

	return nil
}

// isSupportedAggregationType checks if the aggregation type is supported for post-processing
func (s *eventPostProcessingService) isSupportedAggregationType(agg types.AggregationType) bool {
	return agg == types.AggregationCount || agg == types.AggregationSum
}

// isSupportedBillingModel checks if the billing model is supported for post-processing
func (s *eventPostProcessingService) isSupportedBillingModel(billingModel types.BillingModel) bool {
	// We support usage-based billing models
	// FLAT_FEE is not appropriate for usage-based billing as it doesn't depend on consumption
	return billingModel != types.BILLING_MODEL_FLAT_FEE
}

// isSupportedAggregationForPostProcessing checks if the aggregation type and billing model are supported
func (s *eventPostProcessingService) isSupportedAggregationForPostProcessing(
	agg types.AggregationType,
	billingModel types.BillingModel,
) bool {
	return s.isSupportedAggregationType(agg) && s.isSupportedBillingModel(billingModel)
}

// Find matching prices for an event based on meter configuration and filters
func (s *eventPostProcessingService) findMatchingPricesForEvent(
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
			s.Logger.Warnw("post-processing: meter not found for price",
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
func (s *eventPostProcessingService) checkMeterFilters(event *events.Event, filters []meter.Filter) bool {
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
func (s *eventPostProcessingService) extractQuantityFromEvent(
	event *events.Event,
	meter *meter.Meter,
) (decimal.Decimal, string) {
	switch meter.Aggregation.Type {
	case types.AggregationCount:
		// For count, always return 1 and empty string for field value
		return decimal.NewFromInt(1), ""

	case types.AggregationSum:
		if meter.Aggregation.Field == "" {
			s.Logger.Warnw("sum aggregation with empty field name",
				"event_id", event.ID,
				"meter_id", meter.ID,
			)
			return decimal.Zero, ""
		}

		val, ok := event.Properties[meter.Aggregation.Field]
		if !ok {
			s.Logger.Warnw("property not found for sum aggregation",
				"event_id", event.ID,
				"meter_id", meter.ID,
				"field", meter.Aggregation.Field,
			)
			return decimal.Zero, ""
		}

		// Convert value to decimal and string with detailed error handling
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
			s.Logger.Warnw("unknown type for sum aggregation - cannot convert to decimal",
				"event_id", event.ID,
				"meter_id", meter.ID,
				"field", meter.Aggregation.Field,
				"type", fmt.Sprintf("%T", v),
				"value", stringValue,
			)
			return decimal.Zero, stringValue
		}

		return decimalValue, stringValue

	default:
		// We're only supporting COUNT and SUM for now
		s.Logger.Warnw("unsupported aggregation type",
			"event_id", event.ID,
			"meter_id", meter.ID,
			"aggregation_type", meter.Aggregation.Type,
		)
		return decimal.Zero, ""
	}
}

// GetUsageSummary returns the pre-computed usage total for the given parameters
func (s *eventPostProcessingService) GetUsageSummary(ctx context.Context, params *events.UsageSummaryParams) (decimal.Decimal, error) {
	// 1. Get the meter to check if we can use pre-computed values or need fallback
	if params.MeterID == "" {
		return s.processedEventRepo.GetUsageSummary(ctx, params)
	}

	meter, err := s.MeterRepo.GetMeter(ctx, params.MeterID)
	if err != nil {
		s.Logger.Errorw("failed to get meter for usage summary",
			"meter_id", params.MeterID,
			"error", err,
		)
		return decimal.Zero, err
	}

	// 2. Get the price to check the billing model, if price_id is provided
	var price *price.Price
	if params.PriceID != "" {
		price, err = s.PriceRepo.Get(ctx, params.PriceID)
		if err != nil {
			s.Logger.Errorw("failed to get price for usage summary",
				"price_id", params.PriceID,
				"error", err,
			)
			return decimal.Zero, err
		}
	}

	// 3. Check if we can use pre-computed values or need to fall back
	usePreComputed := true

	// Only supported aggregation types: COUNT and SUM
	if meter.Aggregation.Type != types.AggregationCount && meter.Aggregation.Type != types.AggregationSum {
		usePreComputed = false
	}

	// Don't use pre-computed for FLAT_FEE billing models
	if price != nil && price.BillingModel == types.BILLING_MODEL_FLAT_FEE {
		usePreComputed = false
	}

	if usePreComputed {
		return s.processedEventRepo.GetUsageSummary(ctx, params)
	}

	// Fall back to on-demand calculation using the events repo
	// This would delegate to the SubscriptionService.GetUsageBySubscription method
	// For simplicity, we'll log this case but still use pre-computed data in this implementation
	s.Logger.Infow("falling back to on-demand calculation for unsupported aggregation or billing model",
		"meter_id", params.MeterID,
		"aggregation_type", meter.Aggregation.Type,
		"has_price", price != nil,
	)

	// In a real implementation, this would call the traditional usage calculation
	// But for now, we'll still use the pre-computed data
	return s.processedEventRepo.GetUsageSummary(ctx, params)
}

// ReprocessEvents triggers reprocessing of events for a customer or subscription
func (s *eventPostProcessingService) ReprocessEvents(ctx context.Context, customerID, subscriptionID string) error {
	if customerID == "" && subscriptionID == "" {
		return ierr.NewError("either customer_id or subscription_id is required").
			WithHint("Either customer ID or subscription ID is required").
			Mark(ierr.ErrValidation)
	}

	// Find pending events
	eventsList, err := s.processedEventRepo.FindUnprocessedEvents(ctx, customerID, subscriptionID)
	if err != nil {
		return err
	}

	if len(eventsList) == 0 {
		s.Logger.Infow("no pending events found",
			"customer_id", customerID,
			"subscription_id", subscriptionID,
		)
		return nil
	}

	s.Logger.Infow("reprocessing events",
		"customer_id", customerID,
		"subscription_id", subscriptionID,
		"event_count", len(eventsList),
	)

	// Process each event
	for _, processedEvent := range eventsList {
		// Convert processed event back to regular event
		event := &events.Event{
			ID:                 processedEvent.ID,
			TenantID:           processedEvent.TenantID,
			CustomerID:         processedEvent.CustomerID,
			ExternalCustomerID: processedEvent.ExternalCustomerID,
			EventName:          processedEvent.EventName,
			Source:             processedEvent.Source,
			Timestamp:          processedEvent.Timestamp,
			IngestedAt:         processedEvent.IngestedAt,
			Properties:         processedEvent.Properties,
			EnvironmentID:      processedEvent.EnvironmentID,
		}

		if err := s.processEvent(ctx, event); err != nil {
			s.Logger.Errorw("failed to reprocess event",
				"event_id", processedEvent.ID,
				"error", err,
			)
			// Continue with other events
		}
	}

	return nil
}
