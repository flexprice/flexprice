package internal

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	chRepo "github.com/flexprice/flexprice/internal/repository/clickhouse"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
)

type assignPlanScript struct {
	log              *logger.Logger
	customerRepo     customer.Repository
	planRepo         plan.Repository
	subscriptionRepo subscription.Repository
	subscriptionSvc  service.SubscriptionService
}

// AssignPlanToCustomers assigns a specific plan to customers who don't already have a subscription for it
func AssignPlanToCustomers() error {
	// Get environment variables for the script
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")
	planID := os.Getenv("PLAN_ID")

	if tenantID == "" || environmentID == "" || planID == "" {
		return fmt.Errorf("tenant_id, environment_id and plan_id are required")
	}

	log.Printf("Starting plan assignment for tenant: %s, environment: %s, plan: %s\n", tenantID, environmentID, planID)

	// Initialize script
	script, err := newAssignPlanScript()
	if err != nil {
		return fmt.Errorf("failed to initialize script: %w", err)
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)

	// Verify the plan exists
	p, err := script.planRepo.Get(ctx, planID)
	if err != nil {
		return fmt.Errorf("failed to get plan: %w", err)
	}

	if p.TenantID != tenantID {
		return fmt.Errorf("plan does not belong to the specified tenant")
	}

	log.Printf("Found plan: %s (%s)\n", p.ID, p.Name)

	// Get all customers for this tenant/environment
	customerFilter := &types.CustomerFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	}
	customers, err := script.customerRepo.ListAll(ctx, customerFilter)
	if err != nil {
		return fmt.Errorf("failed to list customers: %w", err)
	}

	log.Printf("Found %d customers to process\n", len(customers))

	// Get all existing subscriptions for this plan to avoid duplicates
	subscriptionFilter := &types.SubscriptionFilter{}
	subscriptionFilter.PlanID = planID
	subscriptionFilter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
		types.SubscriptionStatusPaused,
	}

	existingSubs, err := script.subscriptionRepo.ListAll(ctx, subscriptionFilter)
	if err != nil {
		return fmt.Errorf("failed to list existing subscriptions: %w", err)
	}

	// Create a map of customers who already have this plan
	customersWithPlan := make(map[string]bool)
	for _, sub := range existingSubs {
		customersWithPlan[sub.CustomerID] = true
	}

	log.Printf("Found %d customers already with this plan\n", len(customersWithPlan))

	totalProcessed := 0
	totalSkipped := 0
	totalCreated := 0
	totalErrors := 0

	// Process each customer
	for _, cust := range customers {
		time.Sleep(100 * time.Millisecond) // Rate limiting

		if cust.TenantID != tenantID || cust.EnvironmentID != environmentID {
			log.Printf("Skipping customer %s - not in the specified tenant/environment\n", cust.ID)
			totalSkipped++
			continue
		}

		if cust.Status != types.StatusPublished {
			log.Printf("Skipping customer %s - not active (status: %s)\n", cust.ID, cust.Status)
			totalSkipped++
			continue
		}

		// Check if customer already has this plan
		if customersWithPlan[cust.ID] {
			log.Printf("Skipping customer %s - already has plan %s\n", cust.ID, planID)
			totalSkipped++
			continue
		}

		now := time.Now().UTC()
		// Create subscription request
		req := dto.CreateSubscriptionRequest{
			CustomerID:         cust.ID,
			PlanID:             planID,
			Currency:           "usd", // Default currency
			StartDate:          &now,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingCycle:       types.BillingCycleCalendar,
		}

		// Create the subscription
		resp, err := script.subscriptionSvc.CreateSubscription(ctx, req)
		if err != nil {
			log.Printf("Error: Failed to create subscription for customer %s: %v\n", cust.ID, err)
			totalErrors++
			continue
		}

		log.Printf("Successfully created subscription %s for customer %s (%s)\n",
			resp.ID, cust.ID, cust.Name)
		totalCreated++
		totalProcessed++
	}

	log.Printf("Plan assignment completed. Total processed: %d, Total created: %d, Total skipped: %d, Total errors: %d\n",
		totalProcessed, totalCreated, totalSkipped, totalErrors)

	return nil
}

func newAssignPlanScript() (*assignPlanScript, error) {
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

	// Initialize ClickHouse client for event repositories
	sentryService := sentry.NewSentryService(cfg, log)
	chStore, err := clickhouse.NewClickHouseStore(cfg, sentryService)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to clickhouse: %w", err)
	}

	// Initialize postgres client
	entClient, err := postgres.NewEntClient(cfg, log)
	if err != nil {
		log.Fatalf("Failed to connect to postgres: %v", err)
	}
	client := postgres.NewClient(entClient, log, sentry.NewSentryService(cfg, log))
	cacheClient := cache.NewInMemoryCache()

	// Create repositories
	customerRepo := entRepo.NewCustomerRepository(client, log, cacheClient)
	planRepo := entRepo.NewPlanRepository(client, log, cacheClient)
	subscriptionRepo := entRepo.NewSubscriptionRepository(client, log, cacheClient)
	priceRepo := entRepo.NewPriceRepository(client, log, cacheClient)
	meterRepo := entRepo.NewMeterRepository(client, log, cacheClient)
	invoiceRepo := entRepo.NewInvoiceRepository(client, log, cacheClient)
	featureRepo := entRepo.NewFeatureRepository(client, log, cacheClient)
	entitlementRepo := entRepo.NewEntitlementRepository(client, log, cacheClient)
	eventRepo := chRepo.NewEventRepository(chStore, log)
	processedEventRepo := chRepo.NewProcessedEventRepository(chStore, log)

	// Create service params (we need this for the subscription service)
	serviceParams := service.ServiceParams{
		Logger:             log,
		Config:             cfg,
		DB:                 client,
		CustomerRepo:       customerRepo,
		PlanRepo:           planRepo,
		SubRepo:            subscriptionRepo,
		PriceRepo:          priceRepo,
		MeterRepo:          meterRepo,
		EntitlementRepo:    entitlementRepo,
		InvoiceRepo:        invoiceRepo,
		FeatureRepo:        featureRepo,
		EventRepo:          eventRepo,
		ProcessedEventRepo: processedEventRepo,
	}

	// Create subscription service
	subscriptionSvc := service.NewSubscriptionService(serviceParams)

	return &assignPlanScript{
		log:              log,
		customerRepo:     customerRepo,
		planRepo:         planRepo,
		subscriptionRepo: subscriptionRepo,
		subscriptionSvc:  subscriptionSvc,
	}, nil
}
