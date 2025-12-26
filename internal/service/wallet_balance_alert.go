package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/pubsub"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	// Event sources for tracking where alerts originated
	EventSourceFeatureUsage      = "feature_usage"
	EventSourceWalletTransaction = "wallet_transaction"

	// Throttle duration for wallet balance recalculations
	WalletAlertThrottleDuration = 1 * time.Minute
)

// WalletBalanceAlertService handles wallet balance alert operations via Kafka
type WalletBalanceAlertService interface {
	// PublishEvent publishes a wallet balance alert event to Kafka
	PublishEvent(ctx context.Context, event *wallet.WalletBalanceAlertEvent) error

	// RegisterHandler registers the Kafka consumer handler
	RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration)
}

type walletBalanceAlertService struct {
	ServiceParams
	pubSub pubsub.PubSub
	cache  cache.Cache
}

// NewWalletBalanceAlertService creates a new wallet balance alert service
func NewWalletBalanceAlertService(
	params ServiceParams,
) WalletBalanceAlertService {
	svc := &walletBalanceAlertService{
		ServiceParams: params,
		cache:         cache.NewInMemoryCache(),
	}

	svc.pubSub = params.WalletBalanceAlertPubSub.PubSub

	params.Logger.Infow("wallet alert pubsub initialized successfully",
		"consumer_group", params.Config.WalletBalanceAlert.ConsumerGroup,
		"topic", params.Config.WalletBalanceAlert.Topic,
	)

	return svc
}

// PublishEvent publishes a wallet balance alert event to Kafka
func (s *walletBalanceAlertService) PublishEvent(ctx context.Context, event *wallet.WalletBalanceAlertEvent) error {

	err := event.Validate()
	if err != nil {
		return ierr.WithError(err).
			WithHint("Invalid wallet balance alert event").
			WithReportableDetails(map[string]interface{}{
				"event_id":       event.ID,
				"customer_id":    event.CustomerID,
				"tenant_id":      event.TenantID,
				"environment_id": event.EnvironmentID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Ensure event has ID and timestamp
	if event.ID == "" {
		event.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_ALERT)
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	// Marshal event payload
	payload, err := json.Marshal(event)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to marshal wallet balance alert event").
			WithReportableDetails(map[string]interface{}{
				"event_id":       event.ID,
				"customer_id":    event.CustomerID,
				"tenant_id":      event.TenantID,
				"environment_id": event.EnvironmentID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Create deterministic partition key for consistent routing
	// Events for the same customer always go to the same partition
	partitionKey := s.createPartitionKey(event)

	// Set metadata for additional context
	msg := message.NewMessage(event.ID, payload)

	// Set metadata for additional context
	msg.Metadata.Set("tenant_id", event.TenantID)
	msg.Metadata.Set("environment_id", event.EnvironmentID)
	msg.Metadata.Set("partition_key", partitionKey)

	// Get topic from config
	topic := s.Config.WalletBalanceAlert.Topic

	// Validate PubSub is initialized
	if s.pubSub == nil {
		return ierr.NewError("pubsub not initialized for wallet alerts").
			WithHint("Kafka PubSub failed to initialize during service creation").
			WithReportableDetails(map[string]interface{}{
				"topic":          topic,
				"consumer_group": s.Config.WalletBalanceAlert.ConsumerGroup,
			}).
			Mark(ierr.ErrSystem)
	}

	s.Logger.Infow("publishing wallet balance alert event",
		"event_id", event.ID,
		"customer_id", event.CustomerID,
		"tenant_id", event.TenantID,
		"environment_id", event.EnvironmentID,
		"wallet_id", event.WalletID,
		"partition_key", partitionKey,
		"topic", topic,
		"source", event.Source,
		"force_calculate", event.ForceCalculateBalance,
	)

	// Publish to Kafka
	if err := s.pubSub.Publish(ctx, topic, msg); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to publish wallet balance alert event to Kafka").
			WithReportableDetails(map[string]interface{}{
				"event_id":       event.ID,
				"customer_id":    event.CustomerID,
				"tenant_id":      event.TenantID,
				"environment_id": event.EnvironmentID,
				"topic":          topic,
			}).
			Mark(ierr.ErrSystem)
	}

	s.Logger.Debugw("wallet balance alert event published successfully",
		"event_id", event.ID,
		"customer_id", event.CustomerID,
	)

	return nil
}

// RegisterHandler registers a Kafka consumer handler with rate limiting
func (s *walletBalanceAlertService) RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration) {
	// Add throttle middleware for rate limiting
	throttle := middleware.NewThrottle(cfg.WalletBalanceAlert.RateLimit, time.Second)

	// Register the handler
	router.AddNoPublishHandler(
		"wallet_balance_alert_handler",
		cfg.WalletBalanceAlert.Topic,
		s.pubSub,
		s.processMessage,
		throttle.Middleware,
	)

	s.Logger.Infow("registered wallet balance alert handler",
		"handler_name", "wallet_balance_alert_handler",
		"topic", cfg.WalletBalanceAlert.Topic,
		"consumer_group", cfg.WalletBalanceAlert.ConsumerGroup,
		"rate_limit", cfg.WalletBalanceAlert.RateLimit,
	)
}

// processMessage processes a single Kafka message for wallet balance alerts
func (s *walletBalanceAlertService) processMessage(msg *message.Message) error {
	startTime := time.Now()

	var event wallet.WalletBalanceAlertEvent
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		s.Logger.Errorw("failed to unmarshal wallet balance alert event",
			"error", err,
			"message_uuid", msg.UUID,
			"payload_size", len(msg.Payload),
		)
		return nil
	}
	s.Logger.Infow("processing wallet balance alert message",
		"message_uuid", msg.UUID,
		"tenant_id", event.TenantID,
		"environment_id", event.EnvironmentID,
		"customer_id", event.CustomerID,
		"event_source", event.Source,
		"published_at", event.Timestamp,
	)

	// Create context with tenant and environment IDs
	ctx := context.Background()
	if event.TenantID != "" {
		ctx = context.WithValue(ctx, types.CtxTenantID, event.TenantID)
	}
	if event.EnvironmentID != "" {
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, event.EnvironmentID)
	}

	// Process the event
	if err := s.processEvent(ctx, event); err != nil {
		processingDuration := time.Since(startTime)
		s.Logger.Errorw("failed to process wallet balance alert event",
			"error", err,
			"event_id", event.ID,
			"customer_id", event.CustomerID,
			"tenant_id", event.TenantID,
			"environment_id", event.EnvironmentID,
			"wallet_id", event.WalletID,
			"source", event.Source,
			"processing_duration_ms", processingDuration.Milliseconds(),
		)
		// Return error to trigger retry with backoff
		return err
	}

	processingDuration := time.Since(startTime)
	s.Logger.Infow("wallet balance alert event processed successfully",
		"event_id", event.ID,
		"customer_id", event.CustomerID,
		"tenant_id", event.TenantID,
		"environment_id", event.EnvironmentID,
		"wallet_id", event.WalletID,
		"source", event.Source,
		"processing_duration_ms", processingDuration.Milliseconds(),
	)

	return nil
}

// shouldThrottle checks if we should skip processing for this customer based on cache
// Returns true if we should skip (throttle), false if we should process
func (s *walletBalanceAlertService) shouldThrottle(ctx context.Context, event wallet.WalletBalanceAlertEvent) bool {
	// If force_calculate_balance is true, bypass throttle
	if event.ForceCalculateBalance {
		s.Logger.Debugw("bypassing throttle due to force_calculate_balance flag",
			"customer_id", event.CustomerID,
			"tenant_id", event.TenantID,
			"environment_id", event.EnvironmentID,
		)
		return false
	}

	// Generate cache key for this customer in this tenant/environment
	cacheKey := cache.GenerateKey(
		cache.PrefixWalletAlertThrottle,
		event.TenantID,
		event.EnvironmentID,
		event.CustomerID,
	)

	// Check if we processed this customer recently
	_, exists := s.cache.ForceCacheGet(ctx, cacheKey)
	if exists {
		s.Logger.Infow("throttling wallet balance recalculation - processed recently",
			"customer_id", event.CustomerID,
			"tenant_id", event.TenantID,
			"environment_id", event.EnvironmentID,
			"cache_key", cacheKey,
			"throttle_duration", WalletAlertThrottleDuration,
		)
		return true
	}

	return false
}

// markProcessed marks this customer as processed in cache to enable throttling
func (s *walletBalanceAlertService) markProcessed(ctx context.Context, event wallet.WalletBalanceAlertEvent) {
	cacheKey := cache.GenerateKey(
		cache.PrefixWalletAlertThrottle,
		event.TenantID,
		event.EnvironmentID,
		event.CustomerID,
	)

	// Set cache entry with TTL
	s.cache.ForceCacheSet(ctx, cacheKey, time.Now().Unix(), WalletAlertThrottleDuration)

	s.Logger.Debugw("marked customer as processed in throttle cache",
		"customer_id", event.CustomerID,
		"tenant_id", event.TenantID,
		"environment_id", event.EnvironmentID,
		"cache_key", cacheKey,
		"ttl", WalletAlertThrottleDuration,
	)
}

// processEvent delegates to the wallet service to check balance alerts
func (s *walletBalanceAlertService) processEvent(ctx context.Context, event wallet.WalletBalanceAlertEvent) error {
	// Check if we should throttle this request
	if s.shouldThrottle(ctx, event) {
		s.Logger.Infow("skipping wallet balance recalculation due to throttle",
			"customer_id", event.CustomerID,
			"tenant_id", event.TenantID,
			"environment_id", event.EnvironmentID,
			"source", event.Source,
		)
		return nil
	}

	// Create wallet service instance
	walletService := NewWalletService(s.ServiceParams)

	// Delegate to wallet service for actual processing
	err := walletService.CheckWalletBalanceAlert(ctx, &event)
	if err != nil {
		return err
	}

	// Mark customer as processed after successful balance check
	s.markProcessed(ctx, event)

	return nil
}

// createPartitionKey creates a deterministic partition key for Kafka
// This ensures all events for the same customer go to the same partition
func (s *walletBalanceAlertService) createPartitionKey(event *wallet.WalletBalanceAlertEvent) string {
	// Use tenant:environment:customer as partition key
	// This balances load while ensuring consistency per customer
	return fmt.Sprintf("%s:%s:%s", event.TenantID, event.EnvironmentID, event.CustomerID)
}
