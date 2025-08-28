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

// SyncPlanPrices synchronizes all prices from a plan to subscriptions
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

	// Verify the plan exists and belongs to the specified tenant
	p, err := script.planService.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("failed to get plan: %w", err)
	}

	if p.Plan.TenantID != tenantID {
		return fmt.Errorf("plan does not belong to the specified tenant")
	}

	log.Printf("Found plan: %s (%s)\n", p.Plan.ID, p.Plan.Name)

	// Use the existing SyncPlanPrices method from the plan service
	response, err := script.planService.SyncPlanPrices(ctx, planID)
	if err != nil {
		return fmt.Errorf("failed to sync plan prices: %w", err)
	}

	log.Printf("Plan sync completed successfully!")
	log.Printf("Summary: %s", response.Message)
	log.Printf("Subscriptions processed: %d", response.SynchronizationSummary.SubscriptionsProcessed)
	log.Printf("Prices added: %d", response.SynchronizationSummary.PricesAdded)
	log.Printf("Prices removed: %d", response.SynchronizationSummary.PricesRemoved)
	log.Printf("Prices skipped: %d", response.SynchronizationSummary.PricesSkipped)

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

	// Create repositories
	planRepo := entRepo.NewPlanRepository(client, log, cacheClient)
	priceRepo := entRepo.NewPriceRepository(client, log, cacheClient)
	meterRepo := entRepo.NewMeterRepository(client, log, cacheClient)
	subscriptionRepo := entRepo.NewSubscriptionRepository(client, log, cacheClient)
	lineItemRepo := entRepo.NewSubscriptionLineItemRepository(client)
	entitlementRepo := entRepo.NewEntitlementRepository(client, log, cacheClient)
	creditGrantRepo := entRepo.NewCreditGrantRepository(client, log, cacheClient)

	// Create service params
	serviceParams := service.ServiceParams{
		DB:                       client,
		Logger:                   log,
		PlanRepo:                 planRepo,
		PriceRepo:                priceRepo,
		MeterRepo:                meterRepo,
		SubRepo:                  subscriptionRepo,
		SubscriptionLineItemRepo: lineItemRepo,
		EntitlementRepo:          entitlementRepo,
		CreditGrantRepo:          creditGrantRepo,
	}

	// Create plan service
	planService := service.NewPlanService(serviceParams)

	return &syncScript{
		log:         log,
		planService: planService,
	}, nil
}
