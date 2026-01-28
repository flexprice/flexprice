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
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/pubsub/kafka"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	workflowModels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
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

	// Register replay handler with the router
	RegisterHandlerReplay(router *pubsubRouter.Router, cfg *config.Configuration)

	// GetDetailedUsageAnalytics provides comprehensive usage analytics with filtering, grouping, and time-series data
	GetDetailedUsageAnalytics(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error)

	// Get detailed usage analytics version 2 with filtering, grouping, and time-series data
	GetDetailedUsageAnalyticsV2(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error)

	// Reprocess events for a specific customer or with other filters
	ReprocessEvents(ctx context.Context, params *events.ReprocessEventsParams) error

	// TriggerReprocessEventsWorkflow triggers a Temporal workflow to reprocess events asynchronously
	TriggerReprocessEventsWorkflow(ctx context.Context, req *dto.ReprocessEventsRequest) (*workflowModels.TemporalWorkflowResult, error)

	// Get HuggingFace Inference
	GetHuggingFaceBillingData(ctx context.Context, req *dto.GetHuggingFaceBillingDataRequest) (*dto.GetHuggingFaceBillingDataResponse, error)

	// BenchmarkPrepareV1 runs the original prepareProcessedEvents function and returns timing info
	// This is for benchmarking purposes only - does not persist to ClickHouse
	BenchmarkPrepareV1(ctx context.Context, event *events.Event) (*dto.BenchmarkResult, error)

	// BenchmarkPrepareV2 runs the optimized prepareProcessedEventsV2 function and returns timing info
	// This is for benchmarking purposes only - does not persist to ClickHouse
	BenchmarkPrepareV2(ctx context.Context, event *events.Event) (*dto.BenchmarkResult, error)

	// DebugEvent provides debugging information for an event by ID
	DebugEvent(ctx context.Context, eventID string) (*dto.GetEventByIDResponse, error)
}

type featureUsageTrackingService struct {
	ServiceParams
	pubSub           pubsub.PubSub // Regular PubSub for normal processing
	backfillPubSub   pubsub.PubSub // Dedicated Kafka PubSub for backfill processing
	lazyPubSub       pubsub.PubSub // Dedicated Kafka PubSub for lazy processing
	replayPubSub     pubsub.PubSub // Dedicated Kafka PubSub for replay processing
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

	replayPubSub, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.FeatureUsageTrackingReplay.ConsumerGroup,
	)
	if err != nil {
		params.Logger.Fatalw("failed to create replay pubsub", "error", err)
		return nil
	}
	ev.replayPubSub = replayPubSub

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

	pubSub := s.pubSub
	topic := s.Config.FeatureUsageTracking.Topic
	if isBackfill {
		pubSub = s.backfillPubSub
		topic = s.Config.FeatureUsageTracking.TopicBackfill
	}

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
	return nil
}

// RegisterHandler registers a handler for the feature usage tracking topic with rate limiting
func (s *featureUsageTrackingService) RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration) {
	if !cfg.FeatureUsageTracking.Enabled {
		s.Logger.Infow("feature usage tracking handler disabled by configuration")
		return
	}

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

	if !cfg.FeatureUsageTracking.BackfillEnabled {
		s.Logger.Infow("feature usage tracking backfill handler disabled by configuration")
		return
	}

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
	if !cfg.FeatureUsageTrackingLazy.Enabled {
		s.Logger.Infow("feature usage tracking lazy handler disabled by configuration")
		return
	}

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

// RegisterHandlerReplay registers a handler for the feature usage tracking replay topic with rate limiting
func (s *featureUsageTrackingService) RegisterHandlerReplay(router *pubsubRouter.Router, cfg *config.Configuration) {
	if !cfg.FeatureUsageTrackingReplay.Enabled {
		s.Logger.Infow("feature usage tracking replay handler disabled by configuration")
		return
	}

	// Check if replay topic is configured
	if cfg.FeatureUsageTrackingReplay.Topic == "" {
		s.Logger.Warnw("replay topic not set, skipping replay handler")
		return
	}

	// Add throttle middleware to this specific handler
	replayThrottle := middleware.NewThrottle(cfg.FeatureUsageTrackingReplay.RateLimit, time.Second)

	// Add the handler
	router.AddNoPublishHandler(
		"feature_usage_tracking_replay_handler",
		cfg.FeatureUsageTrackingReplay.Topic,
		s.replayPubSub, // Use the dedicated Kafka replay PubSub
		s.processMessage,
		replayThrottle.Middleware,
	)

	s.Logger.Infow("registered event feature usage tracking replay handler",
		"topic", cfg.FeatureUsageTrackingReplay.Topic,
		"rate_limit", cfg.FeatureUsageTrackingReplay.RateLimit,
		"pubsub_type", "kafka",
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

	// Unmarshal the event
	var event events.Event
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		s.Logger.Errorw("failed to unmarshal event for feature usage tracking",
			"error", err,
			"message_uuid", msg.UUID,
		)
		return nil // Don't retry on unmarshal errors
	}

	// validate tenant id (todo commenting for now)
	// if event.TenantID != tenantID {
	// 	s.Logger.Errorw("invalid tenant id",
	// 		"expected", tenantID,
	// 		"actual", event.TenantID,
	// 		"message_uuid", msg.UUID,
	// 	)
	// 	return nil // Don't retry on invalid tenant id
	// }

	if tenantID == "" && event.TenantID != "" {
		tenantID = event.TenantID
	}

	if environmentID == "" && event.EnvironmentID != "" {
		environmentID = event.EnvironmentID
	}

	// Create a background context with tenant ID
	ctx := context.Background()
	if tenantID != "" {
		ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	}

	if environmentID != "" {
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
	}

	if tenantID == "" {
		s.Logger.Errorw("tenant id is required for feature usage tracking: event_id", event.ID,
			"event_name", event.EventName,
			"message_uuid", msg.UUID,
		)
		return nil // Don't retry on invalid tenant id
	}

	if environmentID == "" {
		s.Logger.Errorw("environment id is required for feature usage tracking: event_id", event.ID,
			"event_name", event.EventName,
			"message_uuid", msg.UUID,
		)
		return nil // Don't retry on invalid environment id
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
		"tenant_id", tenantID,
		"environment_id", environmentID,
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

	featureUsage, err := s.prepareProcessedEventsV2(ctx, event)
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

		// Only publish wallet balance alerts if enabled in configuration
		if s.Config.FeatureUsageTracking.WalletAlertPushEnabled {
			walletBalanceAlertService := NewWalletBalanceAlertService(s.ServiceParams)
			for _, fu := range featureUsage {
				event := &wallet.WalletBalanceAlertEvent{
					ID:                    types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_ALERT),
					Timestamp:             time.Now().UTC(),
					Source:                EventSourceFeatureUsage,
					CustomerID:            fu.CustomerID,
					ForceCalculateBalance: false,
					TenantID:              fu.TenantID,
					EnvironmentID:         fu.EnvironmentID,
				}
				if err := walletBalanceAlertService.PublishEvent(ctx, event); err != nil {
					s.Logger.Errorw("failed to publish wallet balance alert event",
						"error", err,
						"event_id", event.ID,
						"customer_id", event.CustomerID,
					)
					continue
				}

				s.Logger.Infow("wallet balance alert event published successfully",
					"event_id", event.ID,
					"customer_id", event.CustomerID,
				)
			}
		} else {
			s.Logger.Debugw("wallet balance alert push disabled by configuration",
				"feature_usage_count", len(featureUsage),
			)
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
		s.Logger.Warnw("customer not found for event",
			"event_id", event.ID,
			"external_customer_id", event.ExternalCustomerID,
			"error", err,
		)

		// Try to auto-create customer via workflow if configured
		customer, err = s.handleMissingCustomer(ctx, event)
		if err != nil {
			s.Logger.Errorw("failed to handle missing customer",
				"event_id", event.ID,
				"external_customer_id", event.ExternalCustomerID,
				"error", err,
			)
			// Return error to retry event processing per user decision
			return results, err
		}

		if customer == nil {
			// No workflow config or workflow not configured for auto-creation, skip event
			s.Logger.Infow("skipping event - no customer and no auto-creation workflow configured",
				"event_id", event.ID,
				"external_customer_id", event.ExternalCustomerID,
			)
			return results, nil
		}

		s.Logger.Infow("customer auto-created via workflow",
			"event_id", event.ID,
			"external_customer_id", event.ExternalCustomerID,
			"customer_id", customer.ID,
		)
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
	filter.Expand = lo.ToPtr(string(types.ExpandPrices) + "," + string(types.ExpandMeters))
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

// prepareProcessedEventsV2 is an optimized version of prepareProcessedEvents that uses meter-based lookup.
// Instead of fetching all subscriptions with all line items, this approach:
// 1. Queries meters by event name (targeted)
// 2. Gets features by meter IDs
// 3. Gets subscription line items by meter IDs + customer ID (instead of all subscriptions)
// 4. Batch-fetches subscriptions only for matching line items (for period calculation)
// This significantly reduces database load for high-volume event processing.
func (s *featureUsageTrackingService) prepareProcessedEventsV2(ctx context.Context, event *events.Event) ([]*events.FeatureUsage, error) {
	results := make([]*events.FeatureUsage, 0)

	// STEP 1: Lookup customer
	customer, err := s.CustomerRepo.GetByLookupKey(ctx, event.ExternalCustomerID)
	if err != nil {
		s.Logger.Warnw("customer not found for event",
			"event_id", event.ID,
			"external_customer_id", event.ExternalCustomerID,
			"error", err,
		)

		// Try to auto-create customer via workflow if configured
		customer, err = s.handleMissingCustomer(ctx, event)
		if err != nil {
			s.Logger.Errorw("failed to handle missing customer",
				"event_id", event.ID,
				"external_customer_id", event.ExternalCustomerID,
				"error", err,
			)
			return results, err
		}

		if customer == nil {
			s.Logger.Infow("skipping event - no customer and no auto-creation workflow configured",
				"event_id", event.ID,
				"external_customer_id", event.ExternalCustomerID,
			)
			return results, nil
		}

		s.Logger.Infow("customer auto-created via workflow",
			"event_id", event.ID,
			"external_customer_id", event.ExternalCustomerID,
			"customer_id", customer.ID,
		)
	}

	// Set the customer ID in the event if it's not already set
	if event.CustomerID == "" {
		event.CustomerID = customer.ID
	}

	// STEP 2: Get meters by event name (targeted query - typically 1-2 meters per event name)
	meterFilter := types.NewNoLimitMeterFilter()
	meterFilter.EventName = event.EventName

	meters, err := s.MeterRepo.List(ctx, meterFilter)
	if err != nil {
		s.Logger.Errorw("failed to get meters by event name",
			"event_id", event.ID,
			"event_name", event.EventName,
			"error", err,
		)
		return results, err
	}

	if len(meters) == 0 {
		s.Logger.Debugw("no meters found for event name, skipping",
			"event_id", event.ID,
			"event_name", event.EventName,
		)
		return results, nil
	}

	// Build meter map and collect meter IDs
	meterMap := make(map[string]*meter.Meter)
	meterIDs := make([]string, 0, len(meters))
	for _, m := range meters {
		// Check meter filters before including
		if !s.checkMeterFilters(event, m.Filters) {
			continue
		}
		meterMap[m.ID] = m
		meterIDs = append(meterIDs, m.ID)
	}

	if len(meterIDs) == 0 {
		s.Logger.Debugw("no meters match event filters, skipping",
			"event_id", event.ID,
			"event_name", event.EventName,
		)
		return results, nil
	}

	// STEP 3: Get features by meter IDs
	featureFilter := types.NewNoLimitFeatureFilter()
	featureFilter.MeterIDs = meterIDs
	features, err := s.FeatureRepo.List(ctx, featureFilter)
	if err != nil {
		s.Logger.Errorw("failed to get features by meter IDs",
			"error", err,
			"event_id", event.ID,
			"meter_count", len(meterIDs),
		)
		return results, err
	}

	// Build feature maps
	featureMeterMap := make(map[string]*feature.Feature) // meter_id -> feature
	for _, f := range features {
		featureMeterMap[f.MeterID] = f
	}

	// STEP 4: Get subscription line items by meter IDs + customer ID (TARGETED QUERY)
	lineItemFilter := types.NewNoLimitSubscriptionLineItemFilter()
	lineItemFilter.MeterIDs = meterIDs
	lineItemFilter.CustomerIDs = []string{customer.ID}
	lineItemFilter.ActiveFilter = true
	lineItemFilter.CurrentPeriodStart = &event.Timestamp

	lineItems, err := s.SubscriptionLineItemRepo.List(ctx, lineItemFilter)
	if err != nil {
		s.Logger.Errorw("failed to get subscription line items",
			"error", err,
			"event_id", event.ID,
			"customer_id", customer.ID,
			"meter_ids", meterIDs,
		)
		return results, err
	}

	if len(lineItems) == 0 {
		s.Logger.Debugw("no active subscription line items found for meters and customer, skipping",
			"event_id", event.ID,
			"customer_id", customer.ID,
			"meter_ids", meterIDs,
		)
		return results, nil
	}

	// Filter line items that are active for the event timestamp
	activeLineItems := make([]*subscription.SubscriptionLineItem, 0, len(lineItems))
	for _, li := range lineItems {
		if li.IsActive(event.Timestamp) && li.IsUsage() {
			activeLineItems = append(activeLineItems, li)
		}
	}

	if len(activeLineItems) == 0 {
		s.Logger.Debugw("no line items active for event timestamp, skipping",
			"event_id", event.ID,
			"customer_id", customer.ID,
			"event_timestamp", event.Timestamp,
		)
		return results, nil
	}

	// STEP 5: Batch lookup subscriptions for period calculation
	subscriptionIDs := lo.Uniq(lo.Map(activeLineItems, func(li *subscription.SubscriptionLineItem, _ int) string {
		return li.SubscriptionID
	}))

	subFilter := types.NewNoLimitSubscriptionFilter()
	subFilter.SubscriptionIDs = subscriptionIDs
	subFilter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}

	subscriptions, err := s.SubRepo.List(ctx, subFilter)
	if err != nil {
		s.Logger.Errorw("failed to get subscriptions",
			"error", err,
			"event_id", event.ID,
			"subscription_ids", subscriptionIDs,
		)
		return results, err
	}

	// Build subscription map
	subscriptionMap := make(map[string]*subscription.Subscription)
	for _, sub := range subscriptions {
		// Validate subscription is valid for this event
		if !s.isSubscriptionValidForEventV2(sub, event) {
			continue
		}
		subscriptionMap[sub.ID] = sub
	}

	if len(subscriptionMap) == 0 {
		s.Logger.Debugw("no valid subscriptions for event, skipping",
			"event_id", event.ID,
			"customer_id", customer.ID,
		)
		return results, nil
	}

	// STEP 6: Build FeatureUsage records for each matching line item
	// Note: We don't need to query prices separately - the line item already has PriceID
	// and we've already filtered by IsUsage() when building activeLineItems
	featureUsagePerSub := make([]*events.FeatureUsage, 0)

	for _, lineItem := range activeLineItems {
		// Get subscription for this line item
		sub, ok := subscriptionMap[lineItem.SubscriptionID]
		if !ok {
			s.Logger.Debugw("subscription not found for line item",
				"event_id", event.ID,
				"line_item_id", lineItem.ID,
				"subscription_id", lineItem.SubscriptionID,
			)
			continue
		}

		// Get meter for this line item
		m, ok := meterMap[lineItem.MeterID]
		if !ok {
			s.Logger.Warnw("meter not found for line item",
				"event_id", event.ID,
				"line_item_id", lineItem.ID,
				"meter_id", lineItem.MeterID,
			)
			continue
		}

		// Get feature for this meter
		f, ok := featureMeterMap[lineItem.MeterID]
		if !ok {
			s.Logger.Warnw("feature not found for meter",
				"event_id", event.ID,
				"meter_id", lineItem.MeterID,
			)
			continue
		}

		// Calculate the period ID for this subscription
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
			continue
		}

		// Create a unique hash for deduplication
		uniqueHash := s.generateUniqueHash(event, m)

		// Create FeatureUsage record
		// Use lineItem.PriceID directly - no need to fetch price from DB
		featureUsageCopy := &events.FeatureUsage{
			Event:          *event,
			SubscriptionID: sub.ID,
			SubLineItemID:  lineItem.ID,
			PriceID:        lineItem.PriceID, // Use directly from line item
			MeterID:        m.ID,
			FeatureID:      f.ID,
			PeriodID:       periodID,
			UniqueHash:     uniqueHash,
			Sign:           1, // Default to positive sign
		}

		// Extract quantity based on meter aggregation
		quantity, _ := s.extractQuantityFromEvent(event, m, sub, periodID)

		// Validate the quantity is positive
		if quantity.IsNegative() {
			s.Logger.Warnw("negative quantity calculated, setting to zero",
				"event_id", event.ID,
				"meter_id", m.ID,
				"calculated_quantity", quantity.String(),
			)
			quantity = decimal.Zero
		}

		featureUsageCopy.QtyTotal = quantity
		featureUsagePerSub = append(featureUsagePerSub, featureUsageCopy)
	}

	if len(featureUsagePerSub) > 0 {
		s.Logger.Debugw("event processing request prepared (V2)",
			"event_id", event.ID,
			"feature_usage_count", len(featureUsagePerSub),
		)
		return featureUsagePerSub, nil
	}

	return results, nil
}

// isSubscriptionValidForEventV2 validates a subscription domain model for the given event.
// This is a variant of isSubscriptionValidForEvent that works with the domain model
// instead of the DTO response (needed for the optimized V2 flow).
func (s *featureUsageTrackingService) isSubscriptionValidForEventV2(
	sub *subscription.Subscription,
	event *events.Event,
) bool {
	// Event must be after subscription start date
	if event.Timestamp.Before(sub.StartDate) {
		return false
	}

	// If subscription has an end date, event must be before or equal to it
	if sub.EndDate != nil && event.Timestamp.After(*sub.EndDate) {
		return false
	}

	// Additional check: if subscription is cancelled, make sure event is before cancellation
	if sub.SubscriptionStatus == types.SubscriptionStatusCancelled && sub.CancelledAt != nil {
		if event.Timestamp.After(*sub.CancelledAt) {
			return false
		}
	}

	return true
}

func (s *featureUsageTrackingService) handleMissingCustomer(
	ctx context.Context,
	event *events.Event,
) (*customer.Customer, error) {
	// Get workflow config from settings
	settingsService := &settingsService{ServiceParams: s.ServiceParams}
	workflowConfig, err := GetSetting[*workflowModels.WorkflowConfig](
		settingsService,
		ctx,
		types.SettingKeyCustomerOnboarding,
	)
	if err != nil {
		s.Logger.Debugw("failed to get workflow config",
			"event_id", event.ID,
			"error", err,
		)
		return nil, nil // No config, skip auto-creation
	}

	if workflowConfig == nil || len(workflowConfig.Actions) == 0 {
		s.Logger.Debugw("no workflow config found for customer onboarding",
			"event_id", event.ID,
		)
		return nil, nil // No config, skip auto-creation
	}

	// Check if workflow has create_customer action as the first action
	hasCreateCustomer := false
	if len(workflowConfig.Actions) > 0 {
		if workflowConfig.Actions[0].GetAction() == workflowModels.WorkflowActionCreateCustomer {
			hasCreateCustomer = true
		}
	}

	if !hasCreateCustomer {
		s.Logger.Debugw("workflow config does not have create_customer as first action",
			"event_id", event.ID,
		)
		return nil, nil // No create_customer action, skip auto-creation
	}

	s.Logger.Infow("executing customer onboarding workflow synchronously",
		"event_id", event.ID,
		"external_customer_id", event.ExternalCustomerID,
		"action_count", len(workflowConfig.Actions),
	)

	// Prepare workflow input with ExternalCustomerID and event timestamp
	input := &workflowModels.CustomerOnboardingWorkflowInput{
		ExternalCustomerID: event.ExternalCustomerID,
		EventTimestamp:     &event.Timestamp, // Pass event timestamp for subscription start date
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		UserID:             types.GetUserID(ctx),
		WorkflowConfig:     *workflowConfig,
	}

	// Validate input
	if err := input.Validate(); err != nil {
		s.Logger.Errorw("invalid workflow input for customer onboarding",
			"error", err,
			"event_id", event.ID,
			"external_customer_id", event.ExternalCustomerID,
		)
		return nil, ierr.WithError(err).
			WithHint("Invalid workflow input for customer onboarding").
			WithReportableDetails(map[string]interface{}{
				"event_id":             event.ID,
				"external_customer_id": event.ExternalCustomerID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get global temporal service
	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return nil, ierr.NewError("temporal service not available").
			WithHint("Customer onboarding workflow requires Temporal service").
			WithReportableDetails(map[string]interface{}{
				"event_id":             event.ID,
				"external_customer_id": event.ExternalCustomerID,
			}).
			Mark(ierr.ErrInternal)
	}

	// Execute workflow synchronously with 30-second timeout
	result, err := temporalSvc.ExecuteWorkflowSync(
		ctx,
		types.TemporalCustomerOnboardingWorkflow,
		input,
		30, // 30 seconds timeout per user decision
	)
	if err != nil {
		s.Logger.Errorw("failed to execute customer onboarding workflow synchronously",
			"error", err,
			"event_id", event.ID,
			"external_customer_id", event.ExternalCustomerID,
		)
		return nil, ierr.WithError(err).
			WithHint("Failed to execute customer onboarding workflow").
			WithReportableDetails(map[string]interface{}{
				"event_id":             event.ID,
				"external_customer_id": event.ExternalCustomerID,
			}).
			Mark(ierr.ErrInternal)
	}

	// Check workflow result
	workflowResult, ok := result.(*workflowModels.CustomerOnboardingWorkflowResult)
	if !ok {
		return nil, ierr.NewError("invalid workflow result type").
			WithHint("Expected CustomerOnboardingWorkflowResult").
			WithReportableDetails(map[string]interface{}{
				"event_id":             event.ID,
				"external_customer_id": event.ExternalCustomerID,
			}).
			Mark(ierr.ErrInternal)
	}

	if workflowResult.Status != "completed" {
		errorMsg := "workflow did not complete successfully"
		if workflowResult.ErrorSummary != nil {
			errorMsg = *workflowResult.ErrorSummary
		}
		return nil, ierr.NewError(errorMsg).
			WithHint("Customer onboarding workflow failed").
			WithReportableDetails(map[string]interface{}{
				"event_id":             event.ID,
				"external_customer_id": event.ExternalCustomerID,
				"workflow_status":      workflowResult.Status,
				"actions_executed":     workflowResult.ActionsExecuted,
			}).
			Mark(ierr.ErrInternal)
	}

	// Get the created customer ID from workflow results
	var customerID string
	for _, actionResult := range workflowResult.Results {
		if actionResult.ActionType == workflowModels.WorkflowActionCreateCustomer &&
			actionResult.Status == workflowModels.WorkflowStatusCompleted &&
			actionResult.ResourceID != "" {
			customerID = actionResult.ResourceID
			break
		}
	}

	if customerID == "" {
		return nil, ierr.NewError("customer ID not found in workflow results").
			WithHint("Workflow completed but customer was not created").
			WithReportableDetails(map[string]interface{}{
				"event_id":             event.ID,
				"external_customer_id": event.ExternalCustomerID,
			}).
			Mark(ierr.ErrInternal)
	}

	// Fetch the created customer
	createdCustomer, err := s.CustomerRepo.Get(ctx, customerID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch created customer").
			WithReportableDetails(map[string]interface{}{
				"event_id":             event.ID,
				"external_customer_id": event.ExternalCustomerID,
				"customer_id":          customerID,
			}).
			Mark(ierr.ErrDatabase)
	}

	s.Logger.Infow("customer onboarding workflow completed successfully",
		"event_id", event.ID,
		"external_customer_id", event.ExternalCustomerID,
		"customer_id", customerID,
		"actions_executed", workflowResult.ActionsExecuted,
	)

	return createdCustomer, nil
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
	Customer              *customer.Customer
	Subscriptions         []*subscription.Subscription
	SubscriptionLineItems map[string]*subscription.SubscriptionLineItem // Map of line item ID -> line item
	SubscriptionsMap      map[string]*subscription.Subscription         // Map of subscription ID -> subscription
	Analytics             []*events.DetailedUsageAnalytic
	Features              map[string]*feature.Feature
	Meters                map[string]*meter.Meter
	Prices                map[string]*price.Price
	PriceResponses        map[string]*dto.PriceResponse // Map of price ID -> PriceResponse (used when groups need to be expanded)
	Plans                 map[string]*plan.Plan         // Map of plan ID -> plan
	Addons                map[string]*addon.Addon       // Map of addon ID -> addon
	Currency              string
	Params                *events.UsageAnalyticsParams
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

func (s *featureUsageTrackingService) GetDetailedUsageAnalyticsV2(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error) {
	// 1. Validate request
	if err := s.validateAnalyticsRequestV2(req); err != nil {
		return nil, err
	}

	customers, err := s.fetchCustomers(ctx, req)
	if err != nil {
		return nil, err
	}

	// Initialize aggregated analytics slice
	var allAnalytics []*events.DetailedUsageAnalytic
	var aggregatedData *AnalyticsData
	var currency string

	// Process each customer and aggregate their analytics data
	for i, customer := range customers {
		// Create a customer-specific request
		customerReq := *req
		customerReq.ExternalCustomerID = customer.ExternalID

		// Fetch analytics data for this customer
		data, err := s.fetchAnalyticsData(ctx, &customerReq)
		if err != nil {
			s.Logger.Warnw("failed to fetch analytics data for customer, skipping",
				"customer_id", customer.ID,
				"external_customer_id", customer.ExternalID,
				"error", err,
			)
			continue
		}

		// Append this customer's analytics to the aggregated list
		allAnalytics = append(allAnalytics, data.Analytics...)

		// Use the first customer's data structure as the base for aggregation
		if i == 0 {
			aggregatedData = data
			currency = data.Currency
		} else {
			// Validate currency consistency across customers
			if data.Currency != "" && currency != "" && data.Currency != currency {
				return nil, ierr.NewError("multiple currencies detected across customers").
					WithHint("Analytics V2 is only supported when all customers use the same currency").
					WithReportableDetails(map[string]interface{}{
						"expected_currency": currency,
						"found_currency":    data.Currency,
						"customer_id":       customer.ID,
					}).
					Mark(ierr.ErrValidation)
			}
			// Merge additional data into aggregated structure
			s.mergeAnalyticsData(aggregatedData, data)
		}
	}

	// If no data was collected, return empty response
	if aggregatedData == nil {
		return &dto.GetUsageAnalyticsResponse{
			TotalCost: decimal.Zero,
			Currency:  "",
			Items:     []dto.UsageAnalyticItem{},
		}, nil
	}

	// Update the aggregated data with all analytics
	aggregatedData.Analytics = allAnalytics
	aggregatedData.Currency = currency

	// 3. Process and return response
	return s.buildAnalyticsResponse(ctx, aggregatedData, req)
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

func (s *featureUsageTrackingService) validateAnalyticsRequestV2(req *dto.GetUsageAnalyticsRequest) error {
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
		Customer:              customer,
		Subscriptions:         subscriptions,
		SubscriptionLineItems: make(map[string]*subscription.SubscriptionLineItem),
		SubscriptionsMap:      make(map[string]*subscription.Subscription),
		Analytics:             analytics,
		Currency:              currency,
		Params:                params,
		Features:              make(map[string]*feature.Feature),
		Meters:                make(map[string]*meter.Meter),
		Prices:                make(map[string]*price.Price),
		Plans:                 make(map[string]*plan.Plan),
		Addons:                make(map[string]*addon.Addon),
		PriceResponses:        make(map[string]*dto.PriceResponse),
	}

	// Build subscription maps
	for _, sub := range subscriptions {
		data.SubscriptionsMap[sub.ID] = sub
		for _, lineItem := range sub.LineItems {
			data.SubscriptionLineItems[lineItem.ID] = lineItem
		}
	}

	// 6. Enrich with metadata if we have analytics data
	if len(analytics) > 0 {
		if err := s.enrichWithMetadata(ctx, data, req); err != nil {
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
		return s.ToGetUsageAnalyticsResponseDTO(ctx, data, req)
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
	data.Analytics = s.aggregateAnalyticsByGrouping(data.Analytics, data.Params.GroupBy)

	return s.ToGetUsageAnalyticsResponseDTO(ctx, data, req)
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
		types.SubscriptionStatusPaused,
		types.SubscriptionStatusCancelled,
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

// buildBucketFeatures builds a map of max bucket and sum bucket features from the request parameters
func (s *featureUsageTrackingService) buildBucketFeatures(ctx context.Context, params *events.UsageAnalyticsParams) (map[string]*events.MaxBucketFeatureInfo, map[string]*events.SumBucketFeatureInfo, error) {
	maxBucketFeatures := make(map[string]*events.MaxBucketFeatureInfo)
	sumBucketFeatures := make(map[string]*events.SumBucketFeatureInfo)

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
			return nil, nil, ierr.WithError(err).
				WithHint("Failed to fetch features for bucket analysis").
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
			return nil, nil, ierr.WithError(err).
				WithHint("Failed to fetch features for bucket analysis").
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
			return nil, nil, ierr.WithError(err).
				WithHint("Failed to fetch meters for bucket analysis").
				Mark(ierr.ErrDatabase)
		}

		// Build meter map
		meterMap := make(map[string]*meter.Meter)
		for _, m := range meters {
			meterMap[m.ID] = m
		}

		// Check features for bucketed max/sum meters
		for _, f := range features {
			if meterID := featureToMeterMap[f.ID]; meterID != "" {
				if m, exists := meterMap[meterID]; exists {
					if m.IsBucketedMaxMeter() {
						maxBucketFeatures[f.ID] = &events.MaxBucketFeatureInfo{
							FeatureID:    f.ID,
							MeterID:      meterID,
							BucketSize:   types.WindowSize(m.Aggregation.BucketSize),
							EventName:    m.EventName,
							PropertyName: m.Aggregation.Field,
						}
					} else if m.IsBucketedSumMeter() {
						sumBucketFeatures[f.ID] = &events.SumBucketFeatureInfo{
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
	}

	return maxBucketFeatures, sumBucketFeatures, nil
}

// fetchAnalytics fetches analytics data from repository
func (s *featureUsageTrackingService) fetchAnalytics(ctx context.Context, params *events.UsageAnalyticsParams) ([]*events.DetailedUsageAnalytic, error) {
	// Build bucket features map (this will handle fetching features if needed)
	maxBucketFeatures, sumBucketFeatures, err := s.buildBucketFeatures(ctx, params)
	if err != nil {
		return nil, err
	}

	// Fetch analytics with bucket features
	analytics, err := s.featureUsageRepo.GetDetailedUsageAnalytics(ctx, params, maxBucketFeatures, sumBucketFeatures)
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
func (s *featureUsageTrackingService) enrichWithMetadata(ctx context.Context, data *AnalyticsData, req *dto.GetUsageAnalyticsRequest) error {
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

	if req.Expand != nil && lo.Contains(req.Expand, "plan") {
		planMap, err := s.fetchPlans(ctx, data)
		if err != nil {
			return err
		}
		data.Plans = planMap
	}
	if req.Expand != nil && lo.Contains(req.Expand, "addon") {
		addonMap, err := s.fetchAddons(ctx, data)
		if err != nil {
			return err
		}
		data.Addons = addonMap
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
		priceService := NewPriceService(s.ServiceParams)
		priceFilter := types.NewNoLimitPriceFilter()
		priceFilter.Expand = lo.ToPtr(string(types.ExpandGroups))
		priceFilter.PriceIDs = priceIDs
		priceFilter.WithStatus(types.StatusPublished)
		// CRITICAL: Allow expired prices for price override cases
		// When a price is overridden, the old price is terminated (has end_date)
		// We still need it to calculate costs for historical usage
		priceFilter.AllowExpiredPrices = true
		pricesResponse, err := priceService.GetPrices(ctx, priceFilter)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to fetch subscription prices for cost calculation").
				Mark(ierr.ErrDatabase)
		}

		// Create price map by price ID - this ensures different prices for the same meter
		// (e.g., from cancelled and new subscriptions) are tracked separately
		for _, priceResp := range pricesResponse.Items {
			data.Prices[priceResp.ID] = priceResp.Price
			data.PriceResponses[priceResp.ID] = priceResp
		}

		// Collect parent price IDs for subscription override prices
		parentPriceIDs := make([]string, 0)
		parentPriceIDSet := make(map[string]bool)
		for _, priceResp := range pricesResponse.Items {
			if priceResp.EntityType == types.PRICE_ENTITY_TYPE_SUBSCRIPTION && priceResp.ParentPriceID != "" {
				if !parentPriceIDSet[priceResp.ParentPriceID] {
					parentPriceIDs = append(parentPriceIDs, priceResp.ParentPriceID)
					parentPriceIDSet[priceResp.ParentPriceID] = true
				}
			}
		}

		// Fetch parent prices if needed
		if len(parentPriceIDs) > 0 {
			parentPriceFilter := types.NewNoLimitPriceFilter()
			parentPriceFilter.Expand = lo.ToPtr(string(types.ExpandGroups))
			parentPriceFilter.PriceIDs = parentPriceIDs
			parentPriceFilter.WithStatus(types.StatusPublished)
			// CRITICAL: Allow expired prices for price override cases
			// Parent prices might be expired when subscription-scoped overrides are created
			parentPriceFilter.AllowExpiredPrices = true
			parentPricesResponse, err := priceService.GetPrices(ctx, parentPriceFilter)
			if err != nil {
				return ierr.WithError(err).
					WithHint("Failed to fetch parent prices for subscription overrides").
					Mark(ierr.ErrDatabase)
			}

			// Add parent prices to maps
			for _, priceResp := range parentPricesResponse.Items {
				data.Prices[priceResp.ID] = priceResp.Price
				data.PriceResponses[priceResp.ID] = priceResp
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
				// Use price_id from the analytics item - this ensures we use the correct price
				// that was active when the usage was recorded (important for cancelled/new subscriptions)
				if price, hasPricing := data.Prices[item.PriceID]; hasPricing {
					// Calculate cost based on meter type
					if meter.IsBucketedMaxMeter() {
						s.calculateBucketedCost(ctx, priceService, item, price, data)
					} else if meter.IsBucketedSumMeter() {
						s.calculateSumWithBucketCost(ctx, priceService, item, price, meter, data)
					} else {
						s.calculateRegularCost(ctx, priceService, item, meter, price, data)
					}
				}
			}
		}
	}

	return nil
}

// calculateBucketedCost calculates cost for bucketed max meters
func (s *featureUsageTrackingService) calculateBucketedCost(ctx context.Context, priceService PriceService, item *events.DetailedUsageAnalytic, price *price.Price, data *AnalyticsData) {
	var cost decimal.Decimal

	if len(item.Points) > 0 {
		// Use points as buckets
		bucketedValues := make([]decimal.Decimal, len(item.Points))
		for i, point := range item.Points {
			bucketedValues[i] = s.getCorrectUsageValueForPoint(point, types.AggregationMax)
		}

		// Check for line item commitment
		if item.SubLineItemID != "" {
			// Find the line item
			lineItem := data.SubscriptionLineItems[item.SubLineItemID]

			if lineItem != nil && lineItem.HasCommitment() {
				// For windowed commitments, skip aggregate cost calculation
				// as we'll apply commitment per-point and sum the results
				if lineItem.CommitmentWindowed {
					// Don't apply commitment at aggregate level for windowed commitments
					// Cost will be calculated from sum of point costs after merging
					cost = decimal.Zero
				} else {
					// Non-windowed commitment: apply at aggregate level
					cost = s.applyLineItemCommitment(ctx, priceService, item, lineItem, price, bucketedValues, decimal.Zero)
				}
			} else {
				cost = priceService.CalculateBucketedCost(ctx, price, bucketedValues)
			}
		} else {
			cost = priceService.CalculateBucketedCost(ctx, price, bucketedValues)
		}

		// Calculate cost for each point
		// For windowed commitments, we should apply commitment at each point level
		// For non-windowed or no commitment, calculate standard costs
		if item.SubLineItemID != "" {
			lineItem := data.SubscriptionLineItems[item.SubLineItemID]
			if lineItem != nil && lineItem.HasCommitment() && lineItem.CommitmentWindowed {
				// Apply commitment at each point (window) level
				commitmentCalc := newCommitmentCalculator(s.Logger, priceService)
				for i := range item.Points {
					pointUsage := s.getCorrectUsageValueForPoint(item.Points[i], types.AggregationMax)
					// Apply commitment to this single point/window
					pointCost, commitmentInfo, err := commitmentCalc.applyWindowCommitmentToLineItem(
						ctx, lineItem, []decimal.Decimal{pointUsage}, price)
					if err != nil {
						s.Logger.Warnw("failed to apply window commitment to point",
							"error", err, "point_index", i, "line_item_id", lineItem.ID)
						// Fallback to standard calculation
						pointCost = priceService.CalculateCost(ctx, price, pointUsage)
					}
					item.Points[i].Cost = pointCost
					// Extract commitment fields if available
					if commitmentInfo != nil {
						item.Points[i].ComputedCommitmentUtilizedAmount = commitmentInfo.ComputedCommitmentUtilizedAmount
						item.Points[i].ComputedOverageAmount = commitmentInfo.ComputedOverageAmount
						item.Points[i].ComputedTrueUpAmount = commitmentInfo.ComputedTrueUpAmount
					}
				}
			} else {
				// Standard calculation for non-windowed or no commitment
				for i := range item.Points {
					pointCost := priceService.CalculateCost(ctx, price, s.getCorrectUsageValueForPoint(item.Points[i], types.AggregationMax))
					item.Points[i].Cost = pointCost
				}
			}
		} else {
			// No line item, standard calculation
			for i := range item.Points {
				pointCost := priceService.CalculateCost(ctx, price, s.getCorrectUsageValueForPoint(item.Points[i], types.AggregationMax))
				item.Points[i].Cost = pointCost
			}
		}

		// Merge bucket-level points into request window-level points
		item.Points = s.mergeBucketPointsByWindow(item.Points, types.AggregationMax)

		// For windowed commitments, calculate total cost from merged point costs
		if item.SubLineItemID != "" {
			lineItem := data.SubscriptionLineItems[item.SubLineItemID]
			if lineItem != nil && lineItem.HasCommitment() && lineItem.CommitmentWindowed {
				// Sum all point costs to get total
				totalFromPoints := decimal.Zero
				for _, point := range item.Points {
					totalFromPoints = totalFromPoints.Add(point.Cost)
				}
				cost = totalFromPoints
			}
		}
	} else {
		// Treat total usage as single bucket
		if item.MaxUsage.IsPositive() {
			bucketedValues := []decimal.Decimal{item.MaxUsage}

			// Check for line item commitment
			cost = priceService.CalculateBucketedCost(ctx, price, bucketedValues)
			if item.SubLineItemID != "" {
				// Find the line item
				lineItem := data.SubscriptionLineItems[item.SubLineItemID]

				if lineItem != nil && lineItem.HasCommitment() {
					cost = s.applyLineItemCommitment(ctx, priceService, item, lineItem, price, bucketedValues, cost)
				}
			}
		}
	}

	item.TotalCost = cost
	item.Currency = price.Currency
}

// calculateSumWithBucketCost calculates cost for sum with bucket meters
// Each bucket is priced independently, similar to bucketed max
func (s *featureUsageTrackingService) calculateSumWithBucketCost(ctx context.Context, priceService PriceService, item *events.DetailedUsageAnalytic, price *price.Price, meter *meter.Meter, data *AnalyticsData) {
	var cost decimal.Decimal

	if len(item.Points) > 0 {
		// Use points as buckets (each point represents a bucket's sum)
		bucketedValues := make([]decimal.Decimal, len(item.Points))
		for i, point := range item.Points {
			bucketedValues[i] = s.getCorrectUsageValueForPoint(point, types.AggregationSum)
		}

		// Check for line item commitment
		if item.SubLineItemID != "" {
			// Find the line item
			lineItem := data.SubscriptionLineItems[item.SubLineItemID]

			if lineItem != nil && lineItem.HasCommitment() {
				// For windowed commitments, skip aggregate cost calculation
				// as we'll apply commitment per-point and sum the results
				if lineItem.CommitmentWindowed {
					// Don't apply commitment at aggregate level for windowed commitments
					// Cost will be calculated from sum of point costs after merging
					cost = decimal.Zero
				} else {
					// Non-windowed commitment: apply at aggregate level
					cost = s.applyLineItemCommitment(ctx, priceService, item, lineItem, price, bucketedValues, decimal.Zero)
				}
			} else {
				cost = priceService.CalculateBucketedCost(ctx, price, bucketedValues)
			}
		} else {
			cost = priceService.CalculateBucketedCost(ctx, price, bucketedValues)
		}

		// Calculate cost for each point (bucket)
		// For windowed commitments, apply commitment at each point level
		if item.SubLineItemID != "" {
			lineItem := data.SubscriptionLineItems[item.SubLineItemID]
			if lineItem != nil && lineItem.HasCommitment() && lineItem.CommitmentWindowed {
				// Apply commitment at each point (window) level
				commitmentCalc := newCommitmentCalculator(s.Logger, priceService)
				for i := range item.Points {
					pointUsage := s.getCorrectUsageValueForPoint(item.Points[i], types.AggregationSum)
					// Apply commitment to this single point/window
					pointCost, commitmentInfo, err := commitmentCalc.applyWindowCommitmentToLineItem(
						ctx, lineItem, []decimal.Decimal{pointUsage}, price)
					if err != nil {
						s.Logger.Warnw("failed to apply window commitment to point",
							"error", err, "point_index", i, "line_item_id", lineItem.ID)
						// Fallback to standard calculation
						pointCost = priceService.CalculateCost(ctx, price, pointUsage)
					}
					item.Points[i].Cost = pointCost
					// Extract commitment fields if available
					if commitmentInfo != nil {
						item.Points[i].ComputedCommitmentUtilizedAmount = commitmentInfo.ComputedCommitmentUtilizedAmount
						item.Points[i].ComputedOverageAmount = commitmentInfo.ComputedOverageAmount
						item.Points[i].ComputedTrueUpAmount = commitmentInfo.ComputedTrueUpAmount
					}
				}
			} else {
				// Standard calculation for non-windowed or no commitment
				for i := range item.Points {
					pointUsage := s.getCorrectUsageValueForPoint(item.Points[i], types.AggregationSum)
					pointCost := priceService.CalculateCost(ctx, price, pointUsage)
					item.Points[i].Cost = pointCost
				}
			}
		} else {
			// No line item, standard calculation
			for i := range item.Points {
				pointUsage := s.getCorrectUsageValueForPoint(item.Points[i], types.AggregationSum)
				pointCost := priceService.CalculateCost(ctx, price, pointUsage)
				item.Points[i].Cost = pointCost
			}
		}

		// Merge bucket-level points into request window-level points
		item.Points = s.mergeBucketPointsByWindow(item.Points, types.AggregationSum)

		// For windowed commitments, calculate total cost from merged point costs
		if item.SubLineItemID != "" {
			lineItem := data.SubscriptionLineItems[item.SubLineItemID]
			if lineItem != nil && lineItem.HasCommitment() && lineItem.CommitmentWindowed {
				// Sum all point costs to get total
				totalFromPoints := decimal.Zero
				for _, point := range item.Points {
					totalFromPoints = totalFromPoints.Add(point.Cost)
				}
				cost = totalFromPoints
			}
		}
	} else {
		// Treat total usage as single bucket if no points available
		totalUsage := s.getCorrectUsageValue(item, types.AggregationSum)
		if totalUsage.IsPositive() {
			bucketedValues := []decimal.Decimal{totalUsage}

			// Check for line item commitment
			if item.SubLineItemID != "" {
				// Find the line item

				lineItem := data.SubscriptionLineItems[item.SubLineItemID]

				if lineItem != nil && lineItem.HasCommitment() {
					cost = s.applyLineItemCommitment(ctx, priceService, item, lineItem, price, bucketedValues, decimal.Zero)
				} else {
					cost = priceService.CalculateBucketedCost(ctx, price, bucketedValues)
				}
			} else {
				cost = priceService.CalculateBucketedCost(ctx, price, bucketedValues)
			}
		}
	}

	item.TotalCost = cost
	item.Currency = price.Currency
}

// calculateRegularCost calculates cost for regular meters
func (s *featureUsageTrackingService) calculateRegularCost(ctx context.Context, priceService PriceService, item *events.DetailedUsageAnalytic, meter *meter.Meter, price *price.Price, data *AnalyticsData) {
	// Set correct usage value
	item.TotalUsage = s.getCorrectUsageValue(item, meter.Aggregation.Type)

	// Calculate total cost
	cost := priceService.CalculateCost(ctx, price, item.TotalUsage)

	// Check for line item commitment
	if item.SubLineItemID != "" {
		// Find the line item

		lineItem := data.SubscriptionLineItems[item.SubLineItemID]

		if lineItem != nil && lineItem.HasCommitment() {

			// Regular meters don't support window commitment in this context (usually)
			// effectively treats it as a single window if IsWindowCommitment is true but no buckets are defined
			// But for regular cost, we are dealing with total usage.

			if lineItem.CommitmentWindowed {
				// This shouldn't typically happen for regular meters unless we're aggregating time series points as windows
				// z
				// If we have points, we COULD treat them as windows, but that depends on business logic.
				// For now, let's treat it as standard commitment application on the total amount

				// However, if we want to support window commitment for regular meters (e.g. daily commitment),
				// we would need to check item.Points and use them as buckets.
				// Let's support it if points exist
				if len(item.Points) > 0 {
					bucketedValues := make([]decimal.Decimal, len(item.Points))
					for i, point := range item.Points {
						bucketedValues[i] = s.getCorrectUsageValueForPoint(point, meter.Aggregation.Type)
					}

					cost = s.applyLineItemCommitment(ctx, priceService, item, lineItem, price, bucketedValues, decimal.Zero)
				} else {
					// Fallback to standard commitment if no points (single window)
					// We pass empty bucketedValues to hint that it's not a bucketed calculation unless default cost is zero
					cost = s.applyLineItemCommitment(ctx, priceService, item, lineItem, price, nil, cost)
				}
			} else {
				// Non-window commitment
				cost = s.applyLineItemCommitment(ctx, priceService, item, lineItem, price, nil, cost)
			}
		}
	}

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

			// Merge sources using a set to avoid duplicates
			if len(item.Sources) > 0 {
				sourceSet := make(map[string]struct{})
				for _, s := range existing.Sources {
					sourceSet[s] = struct{}{}
				}
				for _, s := range item.Sources {
					sourceSet[s] = struct{}{}
				}
				existing.Sources = make([]string, 0, len(sourceSet))
				for s := range sourceSet {
					existing.Sources = append(existing.Sources, s)
				}
			}

			// For time series points, we need to merge them by timestamp
			existing.Points = s.mergeTimeSeriesPoints(existing.Points, item.Points)
		} else {
			// Create a new aggregated item
			aggregated := &events.DetailedUsageAnalytic{
				FeatureID:        item.FeatureID,
				PriceID:          item.PriceID,
				MeterID:          item.MeterID,
				SubLineItemID:    item.SubLineItemID,
				SubscriptionID:   item.SubscriptionID,
				FeatureName:      item.FeatureName,
				EventName:        item.EventName,
				Source:           item.Source,
				Sources:          make([]string, len(item.Sources)),
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
				CommitmentInfo:   item.CommitmentInfo,
				Points:           make([]events.UsageAnalyticPoint, len(item.Points)),
			}

			// Copy properties
			for k, v := range item.Properties {
				aggregated.Properties[k] = v
			}

			// Copy points
			copy(aggregated.Points, item.Points)

			// Copy sources
			copy(aggregated.Sources, item.Sources)

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
	// Always include feature_id, price_id, meter_id, sub_line_item_id for granular tracking
	// Note: subscription_id is NOT included in grouping but kept for reference
	keyParts := make([]string, 0, len(groupBy)+4)
	keyParts = append(keyParts, item.FeatureID, item.PriceID, item.MeterID, item.SubLineItemID)

	for _, group := range groupBy {
		switch group {
		case "feature_id":
			// Already included above
			continue
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

// TriggerReprocessEventsWorkflow triggers a Temporal workflow to reprocess events asynchronously
func (s *featureUsageTrackingService) TriggerReprocessEventsWorkflow(ctx context.Context, req *dto.ReprocessEventsRequest) (*workflowModels.TemporalWorkflowResult, error) {
	// Validate request (includes date format and relationship validation)
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Build workflow input
	workflowInput := map[string]interface{}{
		"external_customer_id": req.ExternalCustomerID,
		"event_name":           req.EventName,
		"start_date":           req.StartDate,
		"end_date":             req.EndDate,
		"batch_size":           req.BatchSize,
	}

	// Get global temporal service
	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return nil, ierr.NewError("temporal service not available").
			WithHint("Reprocess events workflow requires Temporal service").
			Mark(ierr.ErrInternal)
	}

	// Execute workflow
	workflowRun, err := temporalSvc.ExecuteWorkflow(
		ctx,
		types.TemporalReprocessEventsWorkflow,
		workflowInput,
	)

	if err != nil {
		s.Logger.Errorw("failed to start reprocess events workflow",
			"error", err,
			"external_customer_id", req.ExternalCustomerID,
			"event_name", req.EventName)
		return nil, ierr.WithError(err).
			WithHint("Failed to start reprocess events workflow").
			WithReportableDetails(map[string]interface{}{
				"external_customer_id": req.ExternalCustomerID,
				"event_name":           req.EventName,
			}).
			Mark(ierr.ErrInternal)
	}

	s.Logger.Infow("reprocess events workflow started successfully",
		"external_customer_id", req.ExternalCustomerID,
		"event_name", req.EventName,
		"workflow_id", workflowRun.GetID(),
		"run_id", workflowRun.GetRunID())

	return &workflowModels.TemporalWorkflowResult{
		Message:    "reprocess events workflow started successfully",
		WorkflowID: workflowRun.GetID(),
		RunID:      workflowRun.GetRunID(),
	}, nil
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

func (s *featureUsageTrackingService) ToGetUsageAnalyticsResponseDTO(ctx context.Context, data *AnalyticsData, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error) {
	response := &dto.GetUsageAnalyticsResponse{
		TotalCost: decimal.Zero,
		Currency:  "",
		Items:     make([]dto.UsageAnalyticItem, 0, len(data.Analytics)),
	}

	// Check which fields should be expanded
	expandMap := make(map[string]bool)
	if req.Expand != nil {
		for _, expand := range req.Expand {
			expandMap[expand] = true
		}
	}

	// Convert analytics to response items
	for _, analytic := range data.Analytics {
		// For bucketed MAX, use TotalUsage (sum of bucket maxes) not MaxUsage
		// TotalUsage already contains the sum from getMaxBucketTotals
		totalUsage := analytic.TotalUsage
		if analytic.AggregationType != types.AggregationMax || analytic.TotalUsage.IsZero() {
			// For non-MAX or when TotalUsage is not set, use the aggregation-specific value
			totalUsage = s.getCorrectUsageValue(analytic, analytic.AggregationType)
		}

		item := dto.UsageAnalyticItem{
			FeatureID:       analytic.FeatureID,
			PriceID:         analytic.PriceID,
			MeterID:         analytic.MeterID,
			SubLineItemID:   analytic.SubLineItemID,
			SubscriptionID:  analytic.SubscriptionID,
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
			CommitmentInfo:  analytic.CommitmentInfo,
			Points:          make([]dto.UsageAnalyticPoint, 0, len(analytic.Points)),
		}

		// Only include Sources array when 'sources' is in expand param
		if expandMap["source"] {
			item.Sources = analytic.Sources
		}

		// Can expand plan and addon
		if analytic.PriceID != "" {
			if price, ok := data.PriceResponses[analytic.PriceID]; ok {
				switch price.EntityType {
				case types.PRICE_ENTITY_TYPE_ADDON:
					item.AddOnID = price.EntityID
				case types.PRICE_ENTITY_TYPE_PLAN:
					item.PlanID = price.EntityID
				case types.PRICE_ENTITY_TYPE_SUBSCRIPTION:
					// For subscription override prices, get plan_id from parent_price_id
					// Parent price should already be fetched in fetchSubscriptionPrices
					if price.ParentPriceID != "" {
						if parentPrice, ok := data.PriceResponses[price.ParentPriceID]; ok {
							switch parentPrice.EntityType {
							case types.PRICE_ENTITY_TYPE_PLAN:
								item.PlanID = parentPrice.EntityID
							case types.PRICE_ENTITY_TYPE_ADDON:
								item.AddOnID = parentPrice.EntityID
							}
						}
					}
				}
				if expandMap["price"] {
					item.Price = price
				}
			}
		}

		if expandMap["meter"] && analytic.MeterID != "" {
			if meter, ok := data.Meters[analytic.MeterID]; ok {
				item.Meter = meter
			}
		}

		// Set window size in response:
		// - For bucketed features: show the feature's bucket size (underlying granularity)
		// - For non-bucketed features: show the request window size
		if analytic.MeterID != "" {
			if meter, ok := data.Meters[analytic.MeterID]; ok {
				if meter.HasBucketSize() {
					// Bucketed feature: show bucket size
					item.WindowSize = meter.Aggregation.BucketSize
				} else {
					// Non-bucketed feature: show request window size
					item.WindowSize = req.WindowSize
				}
			} else {
				// Meter not found, default to request window size
				item.WindowSize = req.WindowSize
			}
		} else {
			// No meter ID, use request window size
			item.WindowSize = req.WindowSize
		}

		if expandMap["feature"] && analytic.FeatureID != "" {
			if feature, ok := data.Features[analytic.FeatureID]; ok {
				item.Feature = feature
			}
		}

		if expandMap["subscription_line_item"] && analytic.SubLineItemID != "" {
			if lineItem, ok := data.SubscriptionLineItems[analytic.SubLineItemID]; ok {
				item.SubscriptionLineItem = lineItem
			}
		}

		// Expand plan if requested
		if expandMap["plan"] && item.PlanID != "" {
			if plan, ok := data.Plans[item.PlanID]; ok {
				item.Plan = plan
			}
		}

		// Expand addon if requested
		if expandMap["addon"] && item.AddOnID != "" {
			if addon, ok := data.Addons[item.AddOnID]; ok {
				item.Addon = addon
			}
		}

		// Map time-series points if available
		if req.WindowSize != "" {
			for _, point := range analytic.Points {
				// Use the correct usage value based on aggregation type
				correctUsage := s.getCorrectUsageValueForPoint(point, analytic.AggregationType)
				item.Points = append(item.Points, dto.UsageAnalyticPoint{
					Timestamp:                        point.Timestamp,
					Usage:                            correctUsage,
					Cost:                             point.Cost,
					EventCount:                       point.EventCount,
					ComputedCommitmentUtilizedAmount: point.ComputedCommitmentUtilizedAmount,
					ComputedOverageAmount:            point.ComputedOverageAmount,
					ComputedTrueUpAmount:             point.ComputedTrueUpAmount,
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

// fetchPlansByIDs fetches plans by their IDs
func (s *featureUsageTrackingService) fetchPlans(ctx context.Context, data *AnalyticsData) (map[string]*plan.Plan, error) {

	// Create filter to fetch plans by IDs
	planFilter := types.NewNoLimitPlanFilter()

	plans, err := s.PlanRepo.List(ctx, planFilter)
	if err != nil {
		s.Logger.Errorw("failed to fetch plans for analytics",
			"error", err,
		)
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch plans for analytics").
			Mark(ierr.ErrDatabase)
	}

	planMap := make(map[string]*plan.Plan)
	for _, p := range plans {
		planMap[p.ID] = p
	}

	return planMap, nil
}

func (s *featureUsageTrackingService) fetchAddons(ctx context.Context, data *AnalyticsData) (map[string]*addon.Addon, error) {
	addonFilter := types.NewNoLimitAddonFilter()

	addons, err := s.AddonRepo.List(ctx, addonFilter)
	if err != nil {
		s.Logger.Errorw("failed to fetch addons for analytics",
			"error", err,
		)
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch addons for analytics").
			Mark(ierr.ErrDatabase)
	}

	addonMap := make(map[string]*addon.Addon)
	for _, a := range addons {
		addonMap[a.ID] = a
	}

	return addonMap, nil
}

// fetchCustomers fetches all customers when no external customer ID is provided
func (s *featureUsageTrackingService) fetchCustomers(ctx context.Context, req *dto.GetUsageAnalyticsRequest) ([]*customer.Customer, error) {
	if req.ExternalCustomerID != "" {
		cust, err := s.fetchCustomer(ctx, req.ExternalCustomerID)
		if err != nil {
			return nil, err
		}
		return []*customer.Customer{cust}, nil
	} else {
		customers, err := s.CustomerRepo.List(ctx, types.NewNoLimitCustomerFilter())
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to fetch customers").
				Mark(ierr.ErrDatabase)
		}
		return customers, nil
	}
}

// mergeAnalyticsData merges additional analytics data into the aggregated data structure
func (s *featureUsageTrackingService) mergeAnalyticsData(aggregated *AnalyticsData, additional *AnalyticsData) {
	// Ensure aggregated is not nil
	if aggregated == nil {
		return
	}

	// Initialize maps if they are nil
	if aggregated.SubscriptionsMap == nil {
		aggregated.SubscriptionsMap = make(map[string]*subscription.Subscription)
	}
	if aggregated.SubscriptionLineItems == nil {
		aggregated.SubscriptionLineItems = make(map[string]*subscription.SubscriptionLineItem)
	}
	if aggregated.Features == nil {
		aggregated.Features = make(map[string]*feature.Feature)
	}
	if aggregated.Meters == nil {
		aggregated.Meters = make(map[string]*meter.Meter)
	}
	if aggregated.Prices == nil {
		aggregated.Prices = make(map[string]*price.Price)
	}
	if aggregated.Plans == nil {
		aggregated.Plans = make(map[string]*plan.Plan)
	}
	if aggregated.Addons == nil {
		aggregated.Addons = make(map[string]*addon.Addon)
	}
	if aggregated.PriceResponses == nil {
		aggregated.PriceResponses = make(map[string]*dto.PriceResponse)
	}

	// Merge customers (though in V2 we process multiple customers, we keep track of all)
	// Note: We don't merge customers as each iteration processes a different customer

	// Merge subscriptions
	for _, sub := range additional.Subscriptions {
		// Check if subscription already exists
		if _, exists := aggregated.SubscriptionsMap[sub.ID]; !exists {
			aggregated.Subscriptions = append(aggregated.Subscriptions, sub)
			aggregated.SubscriptionsMap[sub.ID] = sub

			// Add line items
			for _, lineItem := range sub.LineItems {
				aggregated.SubscriptionLineItems[lineItem.ID] = lineItem
			}
		}
	}

	// Merge features
	for id, feature := range additional.Features {
		if _, exists := aggregated.Features[id]; !exists {
			aggregated.Features[id] = feature
		}
	}

	// Merge meters
	for id, meter := range additional.Meters {
		if _, exists := aggregated.Meters[id]; !exists {
			aggregated.Meters[id] = meter
		}
	}

	// Merge prices
	for id, price := range additional.Prices {
		if _, exists := aggregated.Prices[id]; !exists {
			aggregated.Prices[id] = price
		}
	}

	// Merge plans
	for id, plan := range additional.Plans {
		if _, exists := aggregated.Plans[id]; !exists {
			aggregated.Plans[id] = plan
		}
	}

	// Merge addons
	for id, addon := range additional.Addons {
		if _, exists := aggregated.Addons[id]; !exists {
			aggregated.Addons[id] = addon
		}
	}
}

func (s *featureUsageTrackingService) GetHuggingFaceBillingData(ctx context.Context, params *dto.GetHuggingFaceBillingDataRequest) (*dto.GetHuggingFaceBillingDataResponse, error) {
	if len(params.EventIDs) == 0 {
		return &dto.GetHuggingFaceBillingDataResponse{
			Data: make([]dto.EventCostInfo, 0),
		}, nil
	}

	// Query feature_usage table directly by event IDs
	featureUsageRecords, err := s.featureUsageRepo.GetFeatureUsageByEventIDs(ctx, params.EventIDs)
	if err != nil {
		return nil, err
	}

	if len(featureUsageRecords) == 0 {
		return &dto.GetHuggingFaceBillingDataResponse{
			Data: make([]dto.EventCostInfo, 0),
		}, nil
	}

	// Collect unique price IDs in one pass (removed featureIDSet as features aren't used)
	priceIDSet := make(map[string]struct{}, len(featureUsageRecords))
	for i := range featureUsageRecords {
		if featureUsageRecords[i].PriceID != "" {
			priceIDSet[featureUsageRecords[i].PriceID] = struct{}{}
		}
	}

	// Fetch all prices in bulk
	priceMap := make(map[string]*price.Price, len(priceIDSet))
	if len(priceIDSet) > 0 {
		priceIDs := make([]string, 0, len(priceIDSet))
		for id := range priceIDSet {
			priceIDs = append(priceIDs, id)
		}

		priceFilter := types.NewNoLimitPriceFilter().
			WithPriceIDs(priceIDs).
			WithStatus(types.StatusPublished).
			WithAllowExpiredPrices(true)
		prices, err := s.PriceRepo.List(ctx, priceFilter)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to fetch prices").
				Mark(ierr.ErrDatabase)
		}
		for i := range prices {
			priceMap[prices[i].ID] = prices[i]
		}
	}

	// Pre-allocate response slice with exact capacity
	responseData := make([]dto.EventCostInfo, 0, len(featureUsageRecords))

	// Calculate cost for each request
	priceService := NewPriceService(s.ServiceParams)
	nanoUSDMultiplier := decimal.NewFromInt(1_000_000_000)

	for i := range featureUsageRecords {
		record := featureUsageRecords[i]

		// Get price for this record
		p, ok := priceMap[record.PriceID]
		if !ok {
			s.Logger.Warnw("price not found for feature_usage record",
				"request_id", record.ID,
				"price_id", record.PriceID,
			)
			responseData = append(responseData, dto.EventCostInfo{
				EventID:       record.ID,
				CostInNanoUSD: decimal.Zero,
			})
			continue
		}

		// Calculate cost in the price's currency and convert to nano-USD
		cost := priceService.CalculateCost(ctx, p, record.QtyTotal)
		costInNanoUSD := cost.Mul(nanoUSDMultiplier)

		responseData = append(responseData, dto.EventCostInfo{
			EventID:       record.ID,
			CostInNanoUSD: costInNanoUSD,
		})
	}

	return &dto.GetHuggingFaceBillingDataResponse{
		Data: responseData,
	}, nil
}

// applyLineItemCommitment applies commitment logic to the calculated cost
func (s *featureUsageTrackingService) applyLineItemCommitment(
	ctx context.Context,
	priceService PriceService,
	item *events.DetailedUsageAnalytic,
	lineItem *subscription.SubscriptionLineItem,
	price *price.Price,
	bucketedValues []decimal.Decimal,
	defaultCost decimal.Decimal,
) decimal.Decimal {
	commitmentCalc := newCommitmentCalculator(s.Logger, priceService)
	var cost decimal.Decimal
	var commitmentInfo *types.CommitmentInfo
	var err error

	if lineItem.CommitmentWindowed {
		cost, commitmentInfo, err = commitmentCalc.applyWindowCommitmentToLineItem(
			ctx, lineItem, bucketedValues, price)
		if err == nil {
			item.CommitmentInfo = commitmentInfo
			return cost
		}
		s.Logger.Warnw("failed to apply window commitment", "error", err, "line_item_id", lineItem.ID)
		if defaultCost.IsZero() && len(bucketedValues) > 0 {
			// If default cost wasn't provided, calculate it
			return priceService.CalculateBucketedCost(ctx, price, bucketedValues)
		}
		return defaultCost
	}

	// Non-window commitment
	rawCost := defaultCost
	if rawCost.IsZero() && len(bucketedValues) > 0 {
		rawCost = priceService.CalculateBucketedCost(ctx, price, bucketedValues)
	}

	cost, commitmentInfo, err = commitmentCalc.applyCommitmentToLineItem(
		ctx, lineItem, rawCost, price)

	if err == nil {
		item.CommitmentInfo = commitmentInfo
		return cost
	}

	s.Logger.Warnw("failed to apply commitment", "error", err, "line_item_id", lineItem.ID)
	return rawCost
}

func (s *featureUsageTrackingService) mergeBucketPointsByWindow(points []events.UsageAnalyticPoint, aggregationType types.AggregationType) []events.UsageAnalyticPoint {
	if len(points) == 0 {
		return points
	}

	// Check if points have WindowStart set (bucketed features)
	if points[0].WindowStart.IsZero() {
		// Not bucketed, return as-is
		return points
	}

	// Group points by WindowStart
	windowGroups := make(map[time.Time][]events.UsageAnalyticPoint)
	for _, point := range points {
		windowGroups[point.WindowStart] = append(windowGroups[point.WindowStart], point)
	}

	// Merge each window group
	mergedPoints := make([]events.UsageAnalyticPoint, 0, len(windowGroups))
	for windowStart, bucketPoints := range windowGroups {
		merged := events.UsageAnalyticPoint{
			Timestamp:                        windowStart, // Use window start as the timestamp
			WindowStart:                      windowStart,
			Cost:                             decimal.Zero,
			EventCount:                       0,
			ComputedCommitmentUtilizedAmount: decimal.Zero,
			ComputedOverageAmount:            decimal.Zero,
			ComputedTrueUpAmount:             decimal.Zero,
		}

		// Aggregate values from all buckets in this window
		for _, bucket := range bucketPoints {
			merged.Cost = merged.Cost.Add(bucket.Cost)
			merged.EventCount += bucket.EventCount
			merged.ComputedCommitmentUtilizedAmount = merged.ComputedCommitmentUtilizedAmount.Add(bucket.ComputedCommitmentUtilizedAmount)
			merged.ComputedOverageAmount = merged.ComputedOverageAmount.Add(bucket.ComputedOverageAmount)
			merged.ComputedTrueUpAmount = merged.ComputedTrueUpAmount.Add(bucket.ComputedTrueUpAmount)
		}

		// For usage, aggregation depends on type
		if aggregationType == types.AggregationMax {
			// For MAX, take the maximum usage across all buckets
			maxUsage := decimal.Zero
			for _, bucket := range bucketPoints {
				if bucket.MaxUsage.GreaterThan(maxUsage) {
					maxUsage = bucket.MaxUsage
				}
			}
			merged.Usage = maxUsage
			merged.MaxUsage = maxUsage
		} else {
			// For SUM, sum all bucket usages
			sumUsage := decimal.Zero
			for _, bucket := range bucketPoints {
				sumUsage = sumUsage.Add(bucket.Usage)
			}
			merged.Usage = sumUsage
			merged.MaxUsage = sumUsage
		}

		// Find the chronologically latest bucket to get LatestUsage
		var latestBucket *events.UsageAnalyticPoint
		for i := range bucketPoints {
			if latestBucket == nil || bucketPoints[i].Timestamp.After(latestBucket.Timestamp) {
				latestBucket = &bucketPoints[i]
			}
		}
		if latestBucket != nil {
			merged.LatestUsage = latestBucket.LatestUsage
		}

		mergedPoints = append(mergedPoints, merged)
	}

	// Sort by timestamp
	sort.Slice(mergedPoints, func(i, j int) bool {
		return mergedPoints[i].Timestamp.Before(mergedPoints[j].Timestamp)
	})

	return mergedPoints
}

// BenchmarkPrepareV1 runs the original prepareProcessedEvents function and returns timing info
// This is for benchmarking purposes only - does not persist to ClickHouse
func (s *featureUsageTrackingService) BenchmarkPrepareV1(ctx context.Context, event *events.Event) (*dto.BenchmarkResult, error) {
	result := &dto.BenchmarkResult{
		Version:            "v1",
		EventID:            event.ID,
		ExternalCustomerID: event.ExternalCustomerID,
	}

	startTime := time.Now()

	featureUsages, err := s.prepareProcessedEvents(ctx, event)

	result.DurationMs = float64(time.Since(startTime).Microseconds()) / 1000.0 // Convert to milliseconds with precision

	if err != nil {
		result.Error = err.Error()
		return result, nil // Still return the result with error info
	}

	result.Events = featureUsages

	result.FeatureUsageCount = len(featureUsages)
	if len(featureUsages) > 0 {
		result.CustomerID = featureUsages[0].CustomerID
	}

	s.Logger.Infow("benchmark v1 completed",
		"event_id", event.ID,
		"duration_ms", result.DurationMs,
		"feature_usage_count", result.FeatureUsageCount,
	)

	return result, nil
}

// BenchmarkPrepareV2 runs the optimized prepareProcessedEventsV2 function and returns timing info
// This is for benchmarking purposes only - does not persist to ClickHouse
func (s *featureUsageTrackingService) BenchmarkPrepareV2(ctx context.Context, event *events.Event) (*dto.BenchmarkResult, error) {
	result := &dto.BenchmarkResult{
		Version:            "v2",
		EventID:            event.ID,
		ExternalCustomerID: event.ExternalCustomerID,
	}

	startTime := time.Now()

	featureUsages, err := s.prepareProcessedEventsV2(ctx, event)

	result.DurationMs = float64(time.Since(startTime).Microseconds()) / 1000.0 // Convert to milliseconds with precision

	if err != nil {
		result.Error = err.Error()
		return result, nil // Still return the result with error info
	}

	result.Events = featureUsages

	result.FeatureUsageCount = len(featureUsages)
	if len(featureUsages) > 0 {
		result.CustomerID = featureUsages[0].CustomerID
	}

	s.Logger.Infow("benchmark v2 completed",
		"event_id", event.ID,
		"duration_ms", result.DurationMs,
		"feature_usage_count", result.FeatureUsageCount,
	)

	return result, nil
}

func (s *featureUsageTrackingService) DebugEvent(ctx context.Context, eventID string) (*dto.GetEventByIDResponse, error) {
	// Step 1: Get event by ID
	// If this fails, it means the event doesn't exist in the events table (wrong event ID)
	// In that case, return the error
	event, err := s.eventRepo.GetEventByID(ctx, eventID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get event from events table").
			Mark(ierr.ErrDatabase)
	}

	// Step 2: Check feature_usage for processed events
	processedEvents, err := s.featureUsageRepo.GetFeatureUsageByEventIDs(ctx, []string{eventID})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get event from feature_usage table").
			Mark(ierr.ErrDatabase)
	}

	response := &dto.GetEventByIDResponse{
		Event: &dto.Event{
			ID:                 event.ID,
			EventName:          event.EventName,
			ExternalCustomerID: event.ExternalCustomerID,
			CustomerID:         event.CustomerID,
			Timestamp:          event.Timestamp,
			Properties:         event.Properties,
			Source:             event.Source,
			EnvironmentID:      event.EnvironmentID,
		},
	}

	// If processed events found, return them
	if len(processedEvents) > 0 {
		response.Status = types.EventProcessingStatusTypeProcessed
		response.ProcessedEvents = make([]*dto.FeatureUsageInfo, len(processedEvents))
		for i, pe := range processedEvents {
			response.ProcessedEvents[i] = &dto.FeatureUsageInfo{
				CustomerID:     pe.CustomerID,
				SubscriptionID: pe.SubscriptionID,
				SubLineItemID:  pe.SubLineItemID,
				PriceID:        pe.PriceID,
				MeterID:        pe.MeterID,
				FeatureID:      pe.FeatureID,
				QtyTotal:       pe.QtyTotal.String(),
				ProcessedAt:    pe.ProcessedAt,
			}
		}
		return response, nil
	}

	response.Status = types.EventProcessingStatusTypeProcessing

	// Step 3: Run debug tracker to find where it failed
	// At this point, the event exists in the events table but not in feature_usage table
	// This means the event is either:
	// 1. Still being processed (no failures) -> status "processing"
	// 2. Failed to process due to missing dependencies (customer, meter, price, etc.) -> status "failed"
	debugTracker := s.runDebugTracker(ctx, event)
	response.DebugTracker = debugTracker

	// If processedEvents is empty, check if any lookup failed
	// If there's a failure point, status is "failed", otherwise "processing"
	if debugTracker.FailurePoint != nil {
		response.Status = types.EventProcessingStatusTypeFailed
	}

	return response, nil
}

func (s *featureUsageTrackingService) runDebugTracker(ctx context.Context, event *events.Event) *dto.DebugTracker {
	tracker := &dto.DebugTracker{
		CustomerLookup:             &dto.CustomerLookupResult{Status: types.DebugTrackerStatusUnprocessed},
		MeterMatching:              &dto.MeterMatchingResult{Status: types.DebugTrackerStatusUnprocessed},
		PriceLookup:                &dto.PriceLookupResult{Status: types.DebugTrackerStatusUnprocessed},
		SubscriptionLineItemLookup: &dto.SubscriptionLineItemLookupResult{Status: types.DebugTrackerStatusUnprocessed},
	}

	// Step 1: Customer Lookup
	customer, err := s.CustomerRepo.GetByLookupKey(ctx, event.ExternalCustomerID)
	if err != nil {
		tracker.CustomerLookup.Status = types.DebugTrackerStatusError
		errorResp := &ierr.ErrorResponse{
			Success: false,
			Error: ierr.ErrorDetail{
				Display:       err.Error(),
				InternalError: err.Error(),
			},
		}
		tracker.CustomerLookup.Error = errorResp
		tracker.FailurePoint = &types.FailurePoint{
			FailurePointType: types.FailurePointTypeCustomerLookup,
			Error:            errorResp,
		}
		return tracker
	}

	if customer == nil {
		tracker.CustomerLookup.Status = types.DebugTrackerStatusNotFound
		errorResp := &ierr.ErrorResponse{
			Success: false,
			Error: ierr.ErrorDetail{
				Display:       fmt.Sprintf("Customer not found for external_customer_id: %s", event.ExternalCustomerID),
				InternalError: fmt.Sprintf("Customer not found for external_customer_id: %s", event.ExternalCustomerID),
			},
		}
		tracker.CustomerLookup.Error = errorResp
		tracker.FailurePoint = &types.FailurePoint{
			FailurePointType: types.FailurePointTypeCustomerLookup,
			Error:            errorResp,
		}
		return tracker
	}

	tracker.CustomerLookup.Status = types.DebugTrackerStatusFound
	tracker.CustomerLookup.Customer = customer

	// Step 2: Meter Matching
	meterFilter := types.NewNoLimitMeterFilter()
	meterFilter.EventName = event.EventName
	meters, err := s.MeterRepo.List(ctx, meterFilter)
	if err != nil {
		tracker.MeterMatching.Status = types.DebugTrackerStatusError
		errorResp := &ierr.ErrorResponse{
			Success: false,
			Error: ierr.ErrorDetail{
				Display:       err.Error(),
				InternalError: err.Error(),
			},
		}
		tracker.MeterMatching.Error = errorResp
		tracker.FailurePoint = &types.FailurePoint{
			FailurePointType: types.FailurePointTypeMeterLookup,
			Error:            errorResp,
		}
		return tracker
	}

	matchedMeters := make([]dto.MatchedMeter, 0)
	for _, m := range meters {
		if s.checkMeterFilters(event, m.Filters) {
			matchedMeters = append(matchedMeters, dto.MatchedMeter{
				MeterID:   m.ID,
				EventName: m.EventName,
				Meter:     m,
			})
		}
	}

	if len(matchedMeters) == 0 {
		tracker.MeterMatching.Status = types.DebugTrackerStatusNotFound
		errMessage := fmt.Sprintf("No meters found matching event_name: %s", event.EventName)
		tracker.FailurePoint = &types.FailurePoint{
			FailurePointType: types.FailurePointTypeMeterLookup,
			Error: &ierr.ErrorResponse{
				Success: false,
				Error: ierr.ErrorDetail{
					Display:       errMessage,
					InternalError: errMessage,
				},
			},
		}
		return tracker
	}

	tracker.MeterMatching.Status = types.DebugTrackerStatusFound
	tracker.MeterMatching.MatchedMeters = matchedMeters

	// Step 3: Price Lookup
	meterIDs := make([]string, len(matchedMeters))
	for i, m := range matchedMeters {
		meterIDs[i] = m.MeterID
	}

	priceFilter := types.NewNoLimitPriceFilter().
		WithStatus(types.StatusPublished)
	priceFilter.MeterIDs = meterIDs
	prices, err := s.PriceRepo.List(ctx, priceFilter)
	if err != nil {
		tracker.PriceLookup.Status = types.DebugTrackerStatusError
		errorResp := &ierr.ErrorResponse{
			Success: false,
			Error: ierr.ErrorDetail{
				Display:       err.Error(),
				InternalError: err.Error(),
			},
		}
		tracker.PriceLookup.Error = errorResp
		tracker.FailurePoint = &types.FailurePoint{
			FailurePointType: types.FailurePointTypePriceLookup,
			Error:            errorResp,
		}
		return tracker
	}

	matchedPrices := make([]dto.MatchedPrice, 0)
	for _, p := range prices {
		if p.IsUsage() {
			matchedPrices = append(matchedPrices, dto.MatchedPrice{
				PriceID: p.ID,
				MeterID: p.MeterID,
				Status:  string(p.Status),
				Price:   p,
			})
		}
	}

	if len(matchedPrices) == 0 {
		tracker.PriceLookup.Status = types.DebugTrackerStatusNotFound
		errMessage := "No prices found for matched meters"
		tracker.FailurePoint = &types.FailurePoint{
			FailurePointType: types.FailurePointTypePriceLookup,
			Error: &ierr.ErrorResponse{
				Success: false,
				Error: ierr.ErrorDetail{
					Display:       errMessage,
					InternalError: errMessage,
				},
			},
		}
		return tracker
	}

	tracker.PriceLookup.Status = types.DebugTrackerStatusFound
	tracker.PriceLookup.MatchedPrices = matchedPrices

	// Step 4: Subscription Line Item Lookup
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	subFilter := types.NewSubscriptionFilter()
	subFilter.CustomerID = customer.ID
	subFilter.WithLineItems = true
	subFilter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}

	subscriptionsList, err := subscriptionService.ListSubscriptions(ctx, subFilter)
	if err != nil {
		tracker.SubscriptionLineItemLookup.Status = types.DebugTrackerStatusError
		errorResp := &ierr.ErrorResponse{
			Success: false,
			Error: ierr.ErrorDetail{
				Display:       err.Error(),
				InternalError: err.Error(),
			},
		}
		tracker.SubscriptionLineItemLookup.Error = errorResp
		tracker.FailurePoint = &types.FailurePoint{
			FailurePointType: types.FailurePointTypeSubscriptionLineItemLookup,
			Error:            errorResp,
		}
		return tracker
	}

	// Get subscription IDs from the subscriptions list
	subscriptionIDs := make([]string, len(subscriptionsList.Items))
	for i, sub := range subscriptionsList.Items {
		subscriptionIDs[i] = sub.ID
	}

	// Get price IDs from matched prices
	priceIDs := make([]string, len(matchedPrices))
	for i, p := range matchedPrices {
		priceIDs[i] = p.PriceID
	}

	// meterIDs is already available from Step 3: Price Lookup

	// Create filter for subscription line items
	lineItemFilter := types.NewNoLimitSubscriptionLineItemFilter()
	lineItemFilter.SubscriptionIDs = subscriptionIDs
	lineItemFilter.PriceIDs = priceIDs
	lineItemFilter.MeterIDs = meterIDs
	lineItemFilter.ActiveFilter = false // Get all line items, not just active ones

	// Get subscription line items using repository
	lineItems, err := s.SubscriptionLineItemRepo.List(ctx, lineItemFilter)
	if err != nil {
		tracker.SubscriptionLineItemLookup.Status = types.DebugTrackerStatusError
		errorResp := &ierr.ErrorResponse{
			Success: false,
			Error: ierr.ErrorDetail{
				Display:       err.Error(),
				InternalError: err.Error(),
			},
		}
		tracker.SubscriptionLineItemLookup.Error = errorResp
		tracker.FailurePoint = &types.FailurePoint{
			FailurePointType: types.FailurePointTypeSubscriptionLineItemLookup,
			Error:            errorResp,
		}
		return tracker
	}

	// Map line items to DTOs, including all items even if timestamp validation fails
	matchedLineItems := make([]dto.MatchedSubscriptionLineItem, 0)
	for _, item := range lineItems {
		if !item.IsUsage() {
			continue
		}

		isActive := item.IsActive(event.Timestamp)
		// Check: start_date < event timestamp < end_date
		timestampWithinRange := event.Timestamp.After(item.StartDate) && (item.EndDate.IsZero() || event.Timestamp.Before(item.EndDate))

		matchedLineItems = append(matchedLineItems, dto.MatchedSubscriptionLineItem{
			SubLineItemID:        item.ID,
			SubscriptionID:       item.SubscriptionID,
			PriceID:              item.PriceID,
			StartDate:            item.StartDate,
			EndDate:              item.EndDate,
			IsActiveForEvent:     isActive,
			TimestampWithinRange: timestampWithinRange,
			SubscriptionLineItem: item,
		})
	}

	if len(matchedLineItems) == 0 {
		tracker.SubscriptionLineItemLookup.Status = types.DebugTrackerStatusNotFound
		errorResp := &ierr.ErrorResponse{
			Success: false,
			Error: ierr.ErrorDetail{
				Display:       "No subscription line items found for matched prices",
				InternalError: "No subscription line items found for matched prices",
			},
		}
		tracker.SubscriptionLineItemLookup.Error = errorResp
		tracker.FailurePoint = &types.FailurePoint{
			FailurePointType: types.FailurePointTypeSubscriptionLineItemLookup,
			Error:            errorResp,
		}
		return tracker
	}

	// Check if any line item is active for the event timestamp
	hasActiveLineItem := false
	for _, item := range matchedLineItems {
		if item.TimestampWithinRange {
			hasActiveLineItem = true
			break
		}
	}

	// Always return matched line items, even if timestamp validation fails
	tracker.SubscriptionLineItemLookup.MatchedLineItems = matchedLineItems

	if !hasActiveLineItem {
		// No active line items found - status should be "not_found" even though we found items
		tracker.SubscriptionLineItemLookup.Status = types.DebugTrackerStatusNotFound
		errorMsg := fmt.Sprintf("Found %d subscription line item(s) but none are active for event timestamp %s. Event timestamp must be between line item start_date and end_date",
			len(matchedLineItems),
			event.Timestamp.Format(time.RFC3339))
		errorResp := &ierr.ErrorResponse{
			Success: false,
			Error: ierr.ErrorDetail{
				Display:       "No active subscription line items found for event timestamp",
				InternalError: errorMsg,
			},
		}
		tracker.SubscriptionLineItemLookup.Error = errorResp
		tracker.FailurePoint = &types.FailurePoint{
			FailurePointType: types.FailurePointTypeSubscriptionLineItemLookup,
			Error:            errorResp,
		}
		return tracker
	}

	// At least one active line item found
	tracker.SubscriptionLineItemLookup.Status = types.DebugTrackerStatusFound

	// No failure point if we got here
	tracker.FailurePoint = nil

	return tracker
}
