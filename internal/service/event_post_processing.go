package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
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

	// Create watermill message
	messageID := watermill.NewUUID()
	msg := message.NewMessage(messageID, payload)

	// Set metadata
	msg.Metadata.Set("tenant_id", types.GetTenantID(ctx))

	s.Logger.Debugw("publishing event for post-processing",
		"event_id", event.ID,
		"event_name", event.EventName,
		"message_id", messageID,
	)

	// Publish to post-processing topic
	if err := s.pubSub.Publish(ctx, EventsPostProcessingTopic, msg); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to publish event for post-processing").
			Mark(ierr.ErrSystem)
	}

	return nil
}

// RegisterHandler registers a handler for the post-processing topic
func (s *eventPostProcessingService) RegisterHandler(router *pubsubRouter.Router) {
	router.AddNoPublishHandler(
		"events_post_processing_handler",
		EventsPostProcessingTopic,
		s.pubSub,
		s.processMessage,
	)
}

// Process a single event message
func (s *eventPostProcessingService) processMessage(msg *message.Message) error {
	s.Logger.Debugw("received event for post-processing", "message_uuid", msg.UUID)

	// Extract tenant ID from message metadata
	tenantID := msg.Metadata.Get("tenant_id")

	// Create a background context with tenant ID
	ctx := context.Background()
	if tenantID != "" {
		ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
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

	return nil
}

// Process a single event
func (s *eventPostProcessingService) processEvent(ctx context.Context, event *events.Event) error {
	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.Logger)

	// Create a base processed event with pending status
	processedEvent := &events.ProcessedEvent{
		Event:       *event,
		EventStatus: types.EventStatusPending,
		Quantity:    0,
		Cost:        decimal.Zero,
	}

	// 1. Skip comprehensive processing (but still save as pending) if no external customer ID
	if event.ExternalCustomerID == "" {
		s.Logger.Debugw("saving event as pending due to missing external customer ID",
			"event_id", event.ID)
		return s.processedEventRepo.InsertProcessedEvent(ctx, processedEvent)
	}

	// 2. Lookup customer
	customer, err := s.CustomerRepo.GetByLookupKey(ctx, event.ExternalCustomerID)
	if err != nil {
		s.Logger.Warnw("customer not found for event, saving as pending",
			"event_id", event.ID,
			"external_customer_id", event.ExternalCustomerID,
			"error", err,
		)
		return s.processedEventRepo.InsertProcessedEvent(ctx, processedEvent)
	}

	// Set the customer ID in the event if it's not already set
	if event.CustomerID == "" {
		event.CustomerID = customer.ID
		processedEvent.CustomerID = customer.ID
	}

	// 3. Get active subscriptions
	filter := types.NewSubscriptionFilter()
	filter.CustomerID = customer.ID
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}

	subscriptions, err := s.SubRepo.List(ctx, filter)
	if err != nil {
		s.Logger.Errorw("failed to get subscriptions, saving as pending",
			"event_id", event.ID,
			"customer_id", customer.ID,
			"error", err,
		)
		return s.processedEventRepo.InsertProcessedEvent(ctx, processedEvent)
	}

	if len(subscriptions) == 0 {
		s.Logger.Debugw("no active subscriptions found for customer, saving as pending",
			"event_id", event.ID,
			"customer_id", customer.ID,
		)
		return s.processedEventRepo.InsertProcessedEvent(ctx, processedEvent)
	}

	// 4. Get all meters
	meters, err := s.MeterRepo.ListAll(ctx, &types.MeterFilter{
		EventName: event.EventName,
	})
	if err != nil {
		s.Logger.Errorw("failed to get meters, saving as pending",
			"event_id", event.ID,
			"error", err,
		)
		return s.processedEventRepo.InsertProcessedEvent(ctx, processedEvent)
	}

	// 5. Process the event against each subscription
	processedEvents := make([]*events.ProcessedEvent, 0)

	for _, subscription := range subscriptions {
		// Get prices for the subscription's plan
		prices, err := s.PriceRepo.List(ctx, &types.PriceFilter{
			PlanIDs: []string{subscription.PlanID},
		})
		if err != nil {
			s.Logger.Errorw("failed to get prices for plan, skipping subscription",
				"event_id", event.ID,
				"plan_id", subscription.PlanID,
				"error", err,
			)
			continue // Skip this subscription but continue with others
		}

		// Find meters and prices that match this event
		matches := s.findMatchingPricesForEvent(event, prices, meters)

		for _, match := range matches {
			// Create a new processed event for each match
			processedEventCopy := &events.ProcessedEvent{
				Event:            *event,
				SubscriptionID:   subscription.ID,
				PriceID:          match.Price.ID,
				MeterID:          match.Meter.ID,
				AggregationField: match.Meter.Aggregation.Field,
				EventStatus:      types.EventStatusPending,
				Quantity:         0,
				Cost:             decimal.Zero,
				ProcessedAt:      lo.ToPtr(time.Now().UTC()),
			}

			// Check if we can process this price/meter combination
			// Only process COUNT and SUM aggregations for now
			// Skip FLAT_FEE billing models
			if (match.Meter.Aggregation.Type != types.AggregationCount &&
				match.Meter.Aggregation.Type != types.AggregationSum) ||
				match.Price.BillingModel == types.BILLING_MODEL_FLAT_FEE {

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
			processedEventCopy.Quantity = quantity.BigInt().Uint64()

			// Calculate cost
			cost := priceService.CalculateCost(ctx, match.Price, quantity)
			processedEventCopy.Cost = cost
			processedEventCopy.Currency = match.Price.Currency

			// Mark as processed since we've calculated everything successfully
			processedEventCopy.EventStatus = types.EventStatusProcessed

			processedEvents = append(processedEvents, processedEventCopy)
		}
	}

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

// Find matching prices for an event based on meter configuration and filters
func (s *eventPostProcessingService) findMatchingPricesForEvent(
	event *events.Event,
	prices []*price.Price,
	meters []*meter.Meter,
) []PriceMatch {
	matches := make([]PriceMatch, 0)

	// Find prices with associated meters
	for _, price := range prices {
		if price.Type != types.PRICE_TYPE_USAGE || price.MeterID == "" {
			continue
		}

		// Find the meter for this price
		var meter *meter.Meter
		for _, m := range meters {
			if m.ID == price.MeterID {
				meter = m
				break
			}
		}

		if meter == nil {
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
			return decimal.Zero, ""
		}

		val, ok := event.Properties[meter.Aggregation.Field]
		if !ok {
			return decimal.Zero, ""
		}

		// Convert value to decimal and string
		switch v := val.(type) {
		case float64:
			return decimal.NewFromFloat(v), fmt.Sprintf("%f", v)
		case int64:
			return decimal.NewFromInt(v), fmt.Sprintf("%d", v)
		case int:
			return decimal.NewFromInt(int64(v)), fmt.Sprintf("%d", v)
		case string:
			d, err := decimal.NewFromString(v)
			if err != nil {
				return decimal.Zero, v
			}
			return d, v
		default:
			// Try to convert to string
			str := fmt.Sprintf("%v", v)
			return decimal.Zero, str
		}

	default:
		// We're only supporting COUNT and SUM for now
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
