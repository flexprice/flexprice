package internal

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	domainCustomer "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	domainSubscription "github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	chRepo "github.com/flexprice/flexprice/internal/repository/clickhouse"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// BulkReprocessEventsParams holds parameters for bulk reprocessing events
type BulkReprocessEventsParams struct {
	TenantID      string
	EnvironmentID string
	EventName     string
	BatchSize     int
}

// BulkReprocessEventsScript holds all dependencies for the script
type BulkReprocessEventsScript struct {
	log                        *logger.Logger
	serviceParams              service.ServiceParams
	customerRepo               domainCustomer.Repository
	subscriptionRepo           domainSubscription.Repository
	eventPostProcessingService service.EventPostProcessingService
}

// BulkReprocessEvents pulls all customers and triggers reprocessing for their active subscriptions
func BulkReprocessEvents(params BulkReprocessEventsParams) error {
	if params.TenantID == "" || params.EnvironmentID == "" {
		return fmt.Errorf("TenantID and EnvironmentID are required")
	}

	// Set default batch size if not provided
	if params.BatchSize <= 0 {
		params.BatchSize = 100
	}

	// Initialize the script with all dependencies once
	script, err := newBulkReprocessEventsScript()
	if err != nil {
		return fmt.Errorf("failed to initialize script: %w", err)
	}

	log.Printf("Starting bulk event reprocessing for tenant: %s, environment: %s", params.TenantID, params.EnvironmentID)

	// Create context with tenant and environment
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, params.TenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, params.EnvironmentID)

	customer_list := []string{}

	// Process customers in batches
	offset := 0
	batchNum := 0

	for {
		batchNum++

		// Create query filter for the current batch
		customerFilter := &types.CustomerFilter{
			QueryFilter: &types.QueryFilter{
				Limit:  lo.ToPtr(params.BatchSize),
				Offset: lo.ToPtr(offset),
				Sort:   lo.ToPtr("created_at"),
				Order:  lo.ToPtr("asc"), // Use ascending order for consistent pagination
			},
			ExternalIDs: customer_list,
		}

		// Fetch customers in the current batch
		customers, err := script.customerRepo.List(ctx, customerFilter)
		if err != nil {
			return fmt.Errorf("failed to fetch customers: %w", err)
		}

		customerCount := len(customers)
		if customerCount == 0 {
			log.Printf("No more customers found. Bulk reprocessing completed.")
			break
		}

		log.Printf("Processing customer batch: %d-%d (batch size: %d)", offset+1, offset+customerCount, params.BatchSize)

		// Process each customer in the batch
		for i, customer := range customers {
			log.Printf("Processing customer %d: %s (ID: %s)", i+1, customer.Name, customer.ExternalID)

			// Fetch active subscriptions for the customer
			subscriptions, err := script.subscriptionRepo.ListByCustomerID(ctx, customer.ID)
			if err != nil {
				script.log.Errorw("Failed to fetch subscriptions",
					"customerID", customer.ID,
					"externalCustomerID", customer.ExternalID,
					"error", err)
				continue
			}

			// Filter for active and trialing subscriptions
			var activeSubscriptions []*domainSubscription.Subscription
			for _, sub := range subscriptions {
				if sub.SubscriptionStatus == types.SubscriptionStatusActive || sub.SubscriptionStatus == types.SubscriptionStatusTrialing {
					activeSubscriptions = append(activeSubscriptions, sub)
				}
			}

			log.Printf("Found %d active subscriptions for customer %s", len(activeSubscriptions), customer.Name)

			// Process each subscription
			for subIndex, subscription := range activeSubscriptions {
				log.Printf("Processing subscription %d/%d: %s", subIndex+1, len(activeSubscriptions), subscription.ID)

				// Log subscription details for debugging
				log.Printf("Subscription details - StartDate: %s, CurrentPeriodStart: %s, CurrentPeriodEnd: %s, Status: %s",
					subscription.StartDate.Format(time.RFC3339),
					subscription.CurrentPeriodStart.Format(time.RFC3339),
					subscription.CurrentPeriodEnd.Format(time.RFC3339),
					subscription.SubscriptionStatus)

				// Calculate the effective start time - use the later of subscription start date and current period start
				// This ensures we don't reprocess events that occurred before the subscription was created
				startTime := subscription.CurrentPeriodStart
				if subscription.StartDate.After(startTime) {
					startTime = subscription.StartDate
					log.Printf("Using subscription start date as effective start time (subscription started after current period)")
				}
				endTime := subscription.CurrentPeriodEnd

				// Validate that the time range makes sense
				if startTime.After(endTime) || startTime.Equal(endTime) {
					log.Printf("Skipping subscription %s - invalid time range: start=%s, end=%s",
						subscription.ID, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
					continue
				}

				log.Printf("Reprocessing events for customer %s, subscription %s, period: %s to %s",
					customer.ExternalID, subscription.ID, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))

				// Prepare reprocessing parameters
				reprocessParams := &events.ReprocessEventsParams{
					ExternalCustomerID: customer.ExternalID,
					EventName:          params.EventName,
					StartTime:          startTime,
					EndTime:            endTime,
					BatchSize:          params.BatchSize,
				}

				// Call the service method directly instead of creating new connections
				if err := script.eventPostProcessingService.ReprocessEvents(ctx, reprocessParams); err != nil {
					script.log.Errorw("Failed to reprocess events",
						"customerID", customer.ID,
						"externalCustomerID", customer.ExternalID,
						"subscriptionID", subscription.ID,
						"error", err)
					continue
				}
			}

			log.Printf("Completed processing customer %s", customer.Name)
		}

		log.Printf("Completed processing batch %d", batchNum)

		// If we didn't get a full batch, we're done
		if customerCount < params.BatchSize {
			log.Printf("Reached end of customers. Bulk reprocessing completed.")
			break
		}

		// Move to next batch
		offset += customerCount
	}

	log.Printf("Bulk event reprocessing completed successfully")
	return nil
}

// Initialize all services and dependencies once
func newBulkReprocessEventsScript() (*BulkReprocessEventsScript, error) {
	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize logger
	log, err := logger.NewLogger(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize Postgres client for customer, meter, feature repositories
	entClient, err := postgres.NewEntClient(cfg, log)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}
	pgClient := postgres.NewClient(entClient, log, sentry.NewSentryService(cfg, log))
	cacheClient := cache.NewInMemoryCache()

	// Initialize ClickHouse client for event repositories
	sentryService := sentry.NewSentryService(cfg, log)
	chStore, err := clickhouse.NewClickHouseStore(cfg, sentryService)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to clickhouse: %w", err)
	}

	// Initialize repositories
	eventRepo := chRepo.NewEventRepository(chStore, log)
	processedEventRepo := chRepo.NewProcessedEventRepository(chStore, log)
	customerRepo := entRepo.NewCustomerRepository(pgClient, log, cacheClient)
	subscriptionRepo := entRepo.NewSubscriptionRepository(pgClient, log, cacheClient)
	meterRepo := entRepo.NewMeterRepository(pgClient, log, cacheClient)
	priceRepo := entRepo.NewPriceRepository(pgClient, log, cacheClient)
	featureRepo := entRepo.NewFeatureRepository(pgClient, log, cacheClient)

	// Create service parameters
	serviceParams := service.ServiceParams{
		Config:       cfg,
		Logger:       log,
		CustomerRepo: customerRepo,
		MeterRepo:    meterRepo,
		PriceRepo:    priceRepo,
		FeatureRepo:  featureRepo,
	}

	// Initialize event post-processing service (this creates Kafka connections once)
	eventPostProcessingService := service.NewEventPostProcessingService(
		serviceParams,
		eventRepo,
		processedEventRepo,
	)

	return &BulkReprocessEventsScript{
		log:                        log,
		serviceParams:              serviceParams,
		customerRepo:               customerRepo,
		subscriptionRepo:           subscriptionRepo,
		eventPostProcessingService: eventPostProcessingService,
	}, nil
}
