package internal

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
)

type syncScript struct {
	log         *logger.Logger
	planService service.PlanService
}

// SyncPlanPrices synchronizes all prices from a plan to subscriptions using the plan service
func SyncPlanPrices() error {
	// Get environment variables for the script
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")
	planID := os.Getenv("PLAN_ID")

	if tenantID == "" || environmentID == "" || planID == "" {
		return fmt.Errorf("tenant_id, environment_id and plan_id are required")
	}

	log.Printf("Starting plan price synchronization for tenant: %s, environment: %s, plan: %s\n", tenantID, environmentID, planID)

	// Initialize script
	script, err := newSyncScript()
	if err != nil {
		return fmt.Errorf("failed to initialize script: %w", err)
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)

	// Use the plan service to sync plan prices
	result, err := script.planService.SyncPlanPrices(ctx, planID)
	if err != nil {
		return fmt.Errorf("failed to sync plan prices: %w", err)
	}

	log.Printf("Plan sync completed successfully for plan: %s (%s)\n", result.PlanID, result.PlanName)
	log.Printf("Summary: %d subscriptions processed, %d prices processed, %d line items created, %d line items terminated, %d line items skipped, %d line items failed\n",
		result.SynchronizationSummary.SubscriptionsProcessed,
		result.SynchronizationSummary.PricesProcessed,
		result.SynchronizationSummary.LineItemsCreated,
		result.SynchronizationSummary.LineItemsTerminated,
		result.SynchronizationSummary.LineItemsSkipped,
		result.SynchronizationSummary.LineItemsFailed,
	)

	return nil
}

func newSyncScript() (*syncScript, error) {
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

	// Initialize postgres client
	entClient, err := postgres.NewEntClient(cfg, log)
	if err != nil {
		log.Fatalf("Failed to connect to postgres: %v", err)
	}
	client := postgres.NewClient(entClient, log, sentry.NewSentryService(cfg, log))
	cacheClient := cache.NewInMemoryCache()

	// Create service params
	serviceParams := service.ServiceParams{
		DB:                       client,
		PlanRepo:                 entRepo.NewPlanRepository(client, log, cacheClient),
		PriceRepo:                entRepo.NewPriceRepository(client, log, cacheClient),
		MeterRepo:                entRepo.NewMeterRepository(client, log, cacheClient),
		SubRepo:                  entRepo.NewSubscriptionRepository(client, log, cacheClient),
		SubscriptionLineItemRepo: entRepo.NewSubscriptionLineItemRepository(client, log, cacheClient),
		Logger:                   log,
	}

	// Create plan service
	planService := service.NewPlanService(serviceParams)

	return &syncScript{
		log:         log,
		planService: planService,
	}, nil
}
