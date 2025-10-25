package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
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
	"github.com/sourcegraph/conc/pool"
)

// PriceMatch represents a matching price and meter for an event
type PriceMatch struct {
	Price *price.Price
	Meter *meter.Meter
}

// EventPostProcessingService handles post-processing operations for metered events
type EventPostProcessingService interface {
	// Publish an event for post-processing
	PublishEvent(ctx context.Context, event *events.Event, isBackfill bool) error

	// Register message handler with the router
	RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration)

	// Get usage cost for a specific period
	GetPeriodCost(ctx context.Context, tenantID, environmentID, customerID, subscriptionID string, periodID uint64) (decimal.Decimal, error)

	// Get usage totals per feature for a period
	GetPeriodFeatureTotals(ctx context.Context, tenantID, environmentID, customerID, subscriptionID string, periodID uint64) ([]*events.PeriodFeatureTotal, error)

	// Get usage analytics for a customer
	GetUsageAnalytics(ctx context.Context, tenantID, environmentID, customerID string, lookbackHours int) ([]*events.UsageAnalytic, error)

	// Get detailed usage analytics with filtering, grouping, and time-series data
	GetDetailedUsageAnalytics(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error)

	// Reprocess events for a specific customer or with other filters
	ReprocessEvents(ctx context.Context, params *events.ReprocessEventsParams) error
}

type eventPostProcessingService struct {
	ServiceParams
	pubSub             pubsub.PubSub // Regular PubSub for normal processing
	backfillPubSub     pubsub.PubSub // Dedicated Kafka PubSub for backfill processing
	eventRepo          events.Repository
	processedEventRepo events.ProcessedEventRepository
}

// NewEventPostProcessingService creates a new event post-processing service
func NewEventPostProcessingService(
	params ServiceParams,
	eventRepo events.Repository,
	processedEventRepo events.ProcessedEventRepository,
) EventPostProcessingService {
	ev := &eventPostProcessingService{
		ServiceParams:      params,
		eventRepo:          eventRepo,
		processedEventRepo: processedEventRepo,
	}

	pubSub, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.EventPostProcessing.ConsumerGroup,
	)

	if err != nil {
		params.Logger.Fatalw("failed to create pubsub", "error", err)
		return nil
	}
	ev.pubSub = pubSub

	backfillPubSub, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.EventPostProcessing.ConsumerGroupBackfill,
	)
	if err != nil {
		params.Logger.Fatalw("failed to create backfill pubsub", "error", err)
		return nil
	}
	ev.backfillPubSub = backfillPubSub
	return ev
}

// PublishEvent publishes an event to the post-processing topic
func (s *eventPostProcessingService) PublishEvent(ctx context.Context, event *events.Event, isBackfill bool) error {
	// Basic validation guards
	if event == nil {
		return ierr.NewError("nil event").WithHint("PublishEvent received nil event").Mark(ierr.ErrValidation)
	}
	if event.TenantID == "" || event.EnvironmentID == "" || event.EventName == "" || event.ID == "" {
		return ierr.NewError("missing required event fields").
			WithHint("tenant_id, environment_id, event_name, and id must be set").
			WithReportableDetails(map[string]interface{}{
				"event_id":   event.ID,
				"tenant_id":  event.TenantID,
				"environment_id": event.EnvironmentID,
				"event_name": event.EventName,
			}).
			Mark(ierr.ErrValidation)
	}

	// Create message payload
	payload, err := json.Marshal(event)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to marshal event for post-processing").
			WithReportableDetails(map[string]interface{}{
				"event_id": event.ID,
				"event_name": event.EventName,
			}).
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
	msg.Metadata.Set("event_id", event.ID)
	msg.Metadata.Set("event_name", event.EventName)

	// TODO: use partition key in the producer

	pubSub := s.pubSub
	topic := s.Config.EventPostProcessing.Topic
	if isBackfill {
		pubSub = s.backfillPubSub
		topic = s.Config.EventPostProcessing.TopicBackfill
	}

	if pubSub == nil {
		return ierr.NewError("pubsub not initialized").
			WithHint("Please check the config").
			Mark(ierr.ErrSystem)
	}
	if strings.TrimSpace(topic) == "" {
		return ierr.NewError("post-processing topic not configured").
			WithHint("EventPostProcessing.Topic or TopicBackfill must be set").
			WithReportableDetails(map[string]interface{}{
				"is_backfill": isBackfill,
			}).
			Mark(ierr.ErrSystem)
	}

	s.Logger.Debugw("publishing event for post-processing",
		"event_id", event.ID,
		"event_name", event.EventName,
		"partition_key", partitionKey,
		"topic", topic,
	)

	// Publish to post-processing topic using the backfill PubSub (Kafka)
	if err := pubSub.Publish(ctx, topic, msg); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to publish event for post-processing").
			WithReportableDetails(map[string]interface{}{
				"event_id": event.ID,
				"event_name": event.EventName,
				"topic": topic,
			}).
			Mark(ierr.ErrSystem)
	}
	return nil
}

// RegisterHandler registers a handler for the post-processing topic with rate limiting
func (s *eventPostProcessingService) RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration) {
	// Add throttle middleware to this specific handler
	throttle := middleware.NewThrottle(cfg.EventPostProcessing.RateLimit, time.Second)

	// Add the handler
	router.AddNoPublishHandler(
		"events_post_processing_handler",
		cfg.EventPostProcessing.Topic,
		s.pubSub,
		s.processMessage,
		throttle.Middleware,
	)

	s.Logger.Infow("registered event post-processing handler",
		"topic", cfg.EventPostProcessing.Topic,
		"rate_limit", cfg.EventPostProcessing.RateLimit,
	)

	// Add backfill handler
	if cfg.EventPostProcessing.TopicBackfill == "" {
		s.Logger.Warnw("backfill topic not set, skipping backfill handler")
		return
	}

	backfillThrottle := middleware.NewThrottle(cfg.EventPostProcessing.RateLimitBackfill, time.Second)
	router.AddNoPublishHandler(
		"events_post_processing_backfill_handler",
		cfg.EventPostProcessing.TopicBackfill,
		s.backfillPubSub, // Use the dedicated Kafka backfill PubSub
		s.processMessage,
		backfillThrottle.Middleware,
	)

	s.Logger.Infow("registered event post-processing backfill handler",
		"topic", cfg.EventPostProcessing.TopicBackfill,
		"rate_limit", cfg.EventPostProcessing.RateLimitBackfill,
		"pubsub_type", "kafka",
	)
}

// Process a single event message
func (s *eventPostProcessingService) processMessage(msg *message.Message) error {
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
		structured := ierr.WithError(err).
			WithHint("Failed to unmarshal event for post-processing").
			WithReportableDetails(map[string]interface{}{
				"message_uuid": msg.UUID,
				"partition_key": partitionKey,
			}).
			Mark(ierr.ErrValidation)
		s.Logger.Errorw("failed to unmarshal event for post-processing",
			"error", structured,
			"message_uuid", msg.UUID,
		)
		return nil // Don't retry on unmarshal errors
	}

	// TODO: this is a hack to get the ingested_at for events that were ingested in the
	// post processing pipeline directly from the event ingestion service and hence
	// don't have an ingested_at value as it gets defaulted to now in the CH DB
	// However this is not required in case of backfill events as they are ingested
	// from the event ingestion service and hence have an ingested_at value
	var foundEvent *events.Event
	if event.IngestedAt.IsZero() {
		events, _, err := s.eventRepo.GetEvents(ctx, &events.GetEventsParams{
			EventID:            event.ID,
			ExternalCustomerID: event.ExternalCustomerID,
		})
		if err != nil {
			structured := ierr.WithError(err).
				WithHint("Failed to update event with ingested_at").
				WithReportableDetails(map[string]interface{}{
					"event_id":             event.ID,
					"external_customer_id": event.ExternalCustomerID,
				}).
				Mark(ierr.ErrDatabase)
			s.Logger.Errorw("failed to update event with ingested_at",
				"error", structured,
			)
			return structured
		}

		if len(events) == 0 {
			structured := ierr.NewError("event not found").
				WithHint("Event not found when updating ingested_at").
				WithReportableDetails(map[string]interface{}{
					"event_id":             event.ID,
					"external_customer_id": event.ExternalCustomerID,
				}).
				Mark(ierr.ErrNotFound)
			s.Logger.Errorw("event not found",
				"error", structured,
				"event_id", event.ID,
				"external_customer_id", event.ExternalCustomerID,
			)
			return nil // Don't retry on event not found
		}

		foundEvent = events[0]
	} else {
		foundEvent = &event
	}

	// Fallback to event metadata if message metadata is missing
	if tenantID == "" && foundEvent.TenantID != "" {
		tenantID = foundEvent.TenantID
		ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	}
	if environmentID == "" && foundEvent.EnvironmentID != "" {
		environmentID = foundEvent.EnvironmentID
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
	}

	// validate tenant id
	if foundEvent.TenantID != tenantID {
		structured := ierr.NewError("invalid tenant id").
			WithHint("tenant_id mismatch between message and event").
			WithReportableDetails(map[string]interface{}{
				"expected":      tenantID,
				"actual":        foundEvent.TenantID,
				"message_uuid":  msg.UUID,
				"partition_key": partitionKey,
			}).
			Mark(ierr.ErrValidation)
		s.Logger.Errorw("invalid tenant id",
			"error", structured,
			"expected", tenantID,
			"actual", foundEvent.TenantID,
			"message_uuid", msg.UUID,
		)
		return nil // Don't retry on invalid tenant id
	}

	// validate environment id
	if foundEvent.EnvironmentID != environmentID {
		structured := ierr.NewError("invalid environment id").
			WithHint("environment_id mismatch between message and event").
			WithReportableDetails(map[string]interface{}{
				"expected":      environmentID,
				"actual":        foundEvent.EnvironmentID,
				"message_uuid":  msg.UUID,
				"partition_key": partitionKey,
			}).
			Mark(ierr.ErrValidation)
		s.Logger.Errorw("invalid environment id",
			"error", structured,
			"expected", environmentID,
			"actual", foundEvent.EnvironmentID,
			"message_uuid", msg.UUID,
		)
		return nil // Don't retry on invalid environment id
	}

	// Process the event
	if err := s.processEvent(ctx, foundEvent); err != nil {
		structured := ierr.WithError(err).
			WithHint("Failed to process event").
			WithReportableDetails(map[string]interface{}{
				"event_id":   foundEvent.ID,
				"event_name": foundEvent.EventName,
			}).
			Mark(ierr.ErrSystem)
		s.Logger.Errorw("failed to process event",
			"error", structured,
			"event_id", foundEvent.ID,
			"event_name", foundEvent.EventName,
		)
		return err // Return original error for retry
	}

	s.Logger.Infow("event processed successfully",
		"event_id", foundEvent.ID,
		"event_name", foundEvent.EventName,
	)

	return nil
}

// Process a single event
func (s *eventPostProcessingService) processEvent(ctx context.Context, event *events.Event) error {
	s.Logger.Debugw("processing event",
		"event_id", event.ID,
		"event_name", event.EventName,
		"external_customer_id", event.ExternalCustomerID,
		"ingested_at", event.IngestedAt,
	)

	processedEvents, err := s.prepareProcessedEvents(ctx, event)
	if err != nil {
		s.Logger.Errorw("failed to prepare processed events",
			"error", err,
			"event_id", event.ID,
		)
		return err
	}

	if len(processedEvents) > 0 {
		if err := s.processedEventRepo.BulkInsertProcessedEvents(ctx, processedEvents); err != nil {
			return err
		}
	}

	return nil
}

// Generate a unique hash for deduplication
// there are 2 cases:
// 1. event_name + event_id // for non COUNT_UNIQUE aggregation types
// 2. event_name + event_field_name + event_field_value // for COUNT_UNIQUE aggregation types
func (s *eventPostProcessingService) generateUniqueHash(event *events.Event, meter *meter.Meter) string {
	var b strings.Builder
	b.Grow(len(event.EventName) + len(event.ID) + 64)
	b.WriteString(event.EventName)
	b.WriteString(":")
	b.WriteString(event.ID)

	if meter.Aggregation.Type == types.AggregationCountUnique && meter.Aggregation.Field != "" {
		if fieldValue, ok := event.Properties[meter.Aggregation.Field]; ok {
			b.WriteString(":")
			b.WriteString(meter.Aggregation.Field)
			b.WriteString(":")
			b.WriteString(stableString(fieldValue))
		}
	}

	hash := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(hash[:])
}

// stableString converts a value into a consistent string representation
func stableString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case json.Number:
		return string(val)
	case float64:
		return decimal.NewFromFloat(val).String()
	case float32:
		return decimal.NewFromFloat32(val).String()
	case int:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	case int32:
		return fmt.Sprintf("%d", val)
	case uint:
		return fmt.Sprintf("%d", val)
	case uint64:
		return fmt.Sprintf("%d", val)
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	}
}

func (s *eventPostProcessingService) prepareProcessedEvents(ctx context.Context, event *events.Event) ([]*events.ProcessedEvent, error) {
	subscriptionService := NewSubscriptionService(s.ServiceParams)

	// Create a base processed event
	baseProcessedEvent := event.ToProcessedEvent()

	// Results slice - will contain either a single skipped event or multiple processed events
	results := make([]*events.ProcessedEvent, 0)

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

	// Build efficient maps for lookups
	// Collect all price IDs and meter IDs from subscription line items
	priceIDs := make([]string, 0)
	meterIDs := make([]string, 0)
	subLineItemMap := make(map[string]*subscription.SubscriptionLineItem) // Map price_id -> line item

	// Extract meters, prices and line items from subscriptions
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
	processedEventsPerSub := make([]*events.ProcessedEvent, 0)

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
			processedEventCopy := &events.ProcessedEvent{
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
				processedEventCopy.FeatureID = feature.ID
			} else {
				s.Logger.Warnw("feature not found for meter",
					"event_id", event.ID,
					"meter_id", match.Meter.ID,
				)
				continue
			}

			// Check if we can process this price/meter combination
			canProcess := s.isSupportedAggregationForPostProcessing(match.Meter.Aggregation.Type, match.Price.BillingModel)

			if !canProcess {
				s.Logger.Debugw("unsupported aggregation type or billing model, skipping",
					"event_id", event.ID,
					"meter_id", match.Meter.ID,
					"aggregation_type", match.Meter.Aggregation.Type,
					"billing_model", match.Price.BillingModel,
				)
				continue
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

			// Derive sign from quantity: 0 for zero qty, 1 otherwise
			if quantity.IsZero() {
				processedEventCopy.Sign = 0
			} else {
				processedEventCopy.Sign = 1
			}

			// Store original quantity
			processedEventCopy.QtyTotal = quantity

			// Apply free units logic
			freeUnitsApplied := decimal.Zero
			billableQty := quantity

			// Store free units applied and billable quantity
			processedEventCopy.QtyFreeApplied = freeUnitsApplied
			processedEventCopy.QtyBillable = billableQty

			// Apply tiered pricing logic
			tierSnapshot := decimal.Zero
			processedEventCopy.TierSnapshot = tierSnapshot

			// Calculate cost details using the price service
			// since per event price can be very small, we don't round the cost
			priceService := NewPriceService(s.ServiceParams)
			costDetails := priceService.CalculateCostWithBreakup(ctx, match.Price, billableQty, false)

			// Set cost details on the processed event
			processedEventCopy.UnitCost = costDetails.EffectiveUnitCost
			processedEventCopy.Cost = costDetails.FinalCost
			processedEventCopy.Currency = match.Price.Currency

			processedEventsPerSub = append(processedEventsPerSub, processedEventCopy)
		}
	}

	// Return all processed events
	if len(processedEventsPerSub) > 0 {
		s.Logger.Debugw("event processing request prepared",
			"event_id", event.ID,
			"processed_events_count", len(processedEventsPerSub),
		)
		return processedEventsPerSub, nil
	}

	// If we got here, no events were processed
	return results, nil
}

// isSupportedAggregationType checks if the aggregation type is supported for post-processing
func (s *eventPostProcessingService) isSupportedAggregationType(agg types.AggregationType) bool {
	return agg == types.AggregationCount || agg == types.AggregationSum
}

// isSupportedBillingModel checks if the billing model is supported for post-processing
func (s *eventPostProcessingService) isSupportedBillingModel(billingModel types.BillingModel) bool {
	// We support usage-based billing models
	// FLAT_FEE is not appropriate for usage-based billing as it doesn't depend on consumption
	return billingModel == types.BILLING_MODEL_FLAT_FEE
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
	filterCache := make(map[string]bool)

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

		// Check meter filters with per-call cache keyed by meter ID
		allowed, cached := filterCache[meter.ID]
		if !cached {
			allowed = s.checkMeterFilters(event, meter.Filters)
			filterCache[meter.ID] = allowed
		}
		if !allowed {
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
			s.Logger.Debugw("sum aggregation with empty field name",
				"event_id", event.ID,
				"meter_id", meter.ID,
			)
			return decimal.Zero, ""
		}

		val, ok := event.Properties[meter.Aggregation.Field]
		if !ok {
			s.Logger.Debugw("property not found for sum aggregation",
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
				s.Logger.Debugw("failed to parse uint64 as decimal",
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
				s.Logger.Debugw("failed to parse string as decimal",
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
				s.Logger.Debugw("failed to parse json.Number as decimal",
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
			s.Logger.Debugw("unknown type for sum aggregation - cannot convert to decimal",
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
		s.Logger.Debugw("unsupported aggregation type",
			"event_id", event.ID,
			"meter_id", meter.ID,
			"aggregation_type", meter.Aggregation.Type,
		)
		return decimal.Zero, ""
	}
}

// GetPeriodCost returns the total cost for a subscription in a billing period
func (s *eventPostProcessingService) GetPeriodCost(ctx context.Context, tenantID, environmentID, customerID, subscriptionID string, periodID uint64) (decimal.Decimal, error) {
	return s.processedEventRepo.GetPeriodCost(ctx, tenantID, environmentID, customerID, subscriptionID, periodID)
}

// GetPeriodFeatureTotals returns usage totals by feature for a subscription in a period
func (s *eventPostProcessingService) GetPeriodFeatureTotals(ctx context.Context, tenantID, environmentID, customerID, subscriptionID string, periodID uint64) ([]*events.PeriodFeatureTotal, error) {
	return s.processedEventRepo.GetPeriodFeatureTotals(ctx, tenantID, environmentID, customerID, subscriptionID, periodID)
}

// GetUsageAnalytics returns recent usage analytics for a customer
func (s *eventPostProcessingService) GetUsageAnalytics(ctx context.Context, tenantID, environmentID, customerID string, lookbackHours int) ([]*events.UsageAnalytic, error) {
	return s.processedEventRepo.GetUsageAnalytics(ctx, tenantID, environmentID, customerID, lookbackHours)
}

// GetDetailedUsageAnalytics provides detailed usage analytics with filtering, grouping, and time-series data
func (s *eventPostProcessingService) GetDetailedUsageAnalytics(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error) {
	// Validate the request
	if req.ExternalCustomerID == "" {
		return nil, ierr.NewError("external_customer_id is required").
			WithHint("External customer ID is required").
			Mark(ierr.ErrValidation)
	}

	// Set default window size if needed
	windowSize := req.WindowSize
	if windowSize != "" {
		if err := windowSize.Validate(); err != nil {
			return nil, err
		}
	}

	// Step 1: Resolve customer
	customer, err := s.CustomerRepo.GetByLookupKey(ctx, req.ExternalCustomerID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Customer not found").
			WithReportableDetails(map[string]interface{}{
				"external_customer_id": req.ExternalCustomerID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Step 2: Get active subscriptions for the customer
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	filter := types.NewSubscriptionFilter()
	filter.CustomerID = customer.ID
	filter.WithLineItems = false
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}

	subscriptionsList, err := subscriptionService.ListSubscriptions(ctx, filter)
	if err != nil {
		s.Logger.Errorw("failed to get subscriptions for currency validation",
			"error", err,
			"customer_id", customer.ID,
		)
		return nil, err
	}

	// Step 3: Validate currency (all subscriptions should have the same currency)
	// This ensures analytics only works when customer has a single currency across all subscriptions
	finalCurrency := ""
	subscriptions := subscriptionsList.Items
	if len(subscriptions) > 0 {
		currency := subscriptions[0].Currency
		for _, sub := range subscriptions {
			if sub.Currency != currency {
				return nil, ierr.NewError("multiple currencies detected").
					WithHint("Analytics is only supported for customers with a single currency across all subscriptions").
					WithReportableDetails(map[string]interface{}{
						"customer_id": customer.ID,
					}).
					Mark(ierr.ErrValidation)
			}
		}
		finalCurrency = currency
	}

	// Step 4: Create the parameters object for repository call
	params := &events.UsageAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		CustomerID:         customer.ID,
		ExternalCustomerID: req.ExternalCustomerID,
		FeatureIDs:         req.FeatureIDs,
		Sources:            req.Sources,
		StartTime:          req.StartTime,
		EndTime:            req.EndTime,
		GroupBy:            req.GroupBy,
		WindowSize:         windowSize,
		PropertyFilters:    req.PropertyFilters,
	}

	// Step 5: Call the repository to get base analytics data
	analytics, err := s.processedEventRepo.GetDetailedUsageAnalytics(ctx, params)
	if err != nil {
		s.Logger.Errorw("failed to get detailed usage analytics",
			"error", err,
			"external_customer_id", req.ExternalCustomerID,
		)
		return nil, err
	}

	// Step 6: If no results, return early
	if len(analytics) == 0 {
		return s.ToGetUsageAnalyticsResponseDTO(ctx, analytics)
	}

	// Step 7: Enrich with feature and meter data from their respective repositories
	err = s.enrichAnalyticsWithFeatureAndMeterData(ctx, analytics)
	if err != nil {
		s.Logger.Warnw("failed to fully enrich analytics with feature and meter data",
			"error", err,
			"analytics_count", len(analytics),
		)
		// Continue with partial data rather than failing completely
	}

	// Step 8: Set currency on all analytics items if we have subscription data
	if len(subscriptions) > 0 && finalCurrency != "" {
		for _, item := range analytics {
			item.Currency = finalCurrency
		}
	}

	return s.ToGetUsageAnalyticsResponseDTO(ctx, analytics)
}

// enrichAnalyticsWithFeatureAndMeterData fetches and adds feature and meter data to analytics results
func (s *eventPostProcessingService) enrichAnalyticsWithFeatureAndMeterData(ctx context.Context, analytics []*events.DetailedUsageAnalytic) error {
	// Extract all unique feature IDs
	featureIDs := make([]string, 0)
	featureIDSet := make(map[string]bool)

	for _, item := range analytics {
		if item.FeatureID != "" && !featureIDSet[item.FeatureID] {
			featureIDs = append(featureIDs, item.FeatureID)
			featureIDSet[item.FeatureID] = true
		}
	}

	// If no feature IDs, nothing to enrich
	if len(featureIDs) == 0 {
		return nil
	}

	// Fetch all features in one call
	featureFilter := types.NewNoLimitFeatureFilter()
	featureFilter.FeatureIDs = featureIDs
	features, err := s.FeatureRepo.List(ctx, featureFilter)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to fetch features for enrichment").
			Mark(ierr.ErrDatabase)
	}

	// Extract all meter IDs from features
	meterIDs := make([]string, 0)
	meterIDSet := make(map[string]bool)
	featureMap := make(map[string]*feature.Feature)

	for _, f := range features {
		featureMap[f.ID] = f
		if f.MeterID != "" && !meterIDSet[f.MeterID] {
			meterIDs = append(meterIDs, f.MeterID)
			meterIDSet[f.MeterID] = true
		}
	}

	// Fetch all meters in one call
	meterFilter := types.NewNoLimitMeterFilter()
	meterFilter.MeterIDs = meterIDs
	meters, err := s.MeterRepo.List(ctx, meterFilter)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to fetch meters for enrichment").
			Mark(ierr.ErrDatabase)
	}

	// Create meter map for quick lookup
	meterMap := make(map[string]*meter.Meter)
	for _, m := range meters {
		meterMap[m.ID] = m
	}

	// Enrich analytics with feature and meter data
	for _, item := range analytics {
		if feature, ok := featureMap[item.FeatureID]; ok {
			item.FeatureName = feature.Name
			item.Unit = feature.UnitSingular
			item.UnitPlural = feature.UnitPlural

			if meter, ok := meterMap[feature.MeterID]; ok {
				item.MeterID = meter.ID
				item.EventName = meter.EventName
				item.AggregationType = meter.Aggregation.Type
			}
		}
	}

	return nil
}

// ReprocessEvents triggers reprocessing of events for a customer or with other filters
func (s *eventPostProcessingService) ReprocessEvents(ctx context.Context, params *events.ReprocessEventsParams) error {
	s.Logger.Infow("starting event reprocessing",
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

		// Publish events with bounded concurrency
		workers64 := s.Config.EventPostProcessing.RateLimitBackfill
		workers := int(workers64)
		if workers <= 0 {
			workers = 10
		}
		p := pool.New().WithMaxGoroutines(workers)
		publishedCount := 0
		var mu sync.Mutex
		for _, event := range unprocessedEvents {
			ev := event
			p.Go(func() {
				if err := s.PublishEvent(ctx, ev, true); err != nil {
					s.Logger.Errorw("failed to publish event for reprocessing",
						"event_id", ev.ID,
						"error", err,
					)
					return
				}
				mu.Lock()
				publishedCount++
				mu.Unlock()
			})
		}
		p.Wait()

		// Update totals and keyset for next batch using the last event in the batch
		totalEventsPublished += publishedCount
		lastEvent := unprocessedEvents[len(unprocessedEvents)-1]
		lastID = lastEvent.ID
		lastTimestamp = lastEvent.Timestamp

		s.Logger.Infow("published events for reprocessing",
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

	s.Logger.Infow("completed event reprocessing",
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
func (s *eventPostProcessingService) isSubscriptionValidForEvent(
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

func (s *eventPostProcessingService) ToGetUsageAnalyticsResponseDTO(ctx context.Context, analytics []*events.DetailedUsageAnalytic) (*dto.GetUsageAnalyticsResponse, error) {
	response := &dto.GetUsageAnalyticsResponse{
		TotalCost: decimal.Zero,
		Currency:  "",
		Items:     make([]dto.UsageAnalyticItem, 0, len(analytics)),
	}

	// Convert analytics to response items
	for _, analytic := range analytics {
		item := dto.UsageAnalyticItem{
			FeatureID:       analytic.FeatureID,
			FeatureName:     analytic.FeatureName,
			EventName:       analytic.EventName,
			Source:          analytic.Source,
			Unit:            analytic.Unit,
			UnitPlural:      analytic.UnitPlural,
			AggregationType: analytic.AggregationType,
			TotalUsage:      analytic.TotalUsage,
			TotalCost:       analytic.TotalCost,
			Currency:        analytic.Currency,
			EventCount:      analytic.EventCount,
			Properties:      analytic.Properties,
			Points:          make([]dto.UsageAnalyticPoint, 0, len(analytic.Points)),
		}

		// Map time-series points if available
		for _, point := range analytic.Points {
			item.Points = append(item.Points, dto.UsageAnalyticPoint{
				Timestamp:  point.Timestamp,
				Usage:      point.Usage,
				Cost:       point.Cost,
				EventCount: point.EventCount,
			})
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
