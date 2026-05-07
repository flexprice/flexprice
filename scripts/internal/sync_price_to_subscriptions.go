package internal

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type syncPriceToSubscriptionsScript struct {
	log              *logger.Logger
	priceRepo        price.Repository
	planRepo         plan.Repository
	meterRepo        meter.Repository
	subscriptionRepo subscription.Repository
	lineItemRepo     subscription.LineItemRepository
}

// SyncPriceToSubscriptions adds a new subscription line item to all subscriptions
// with the same plan and start date
func SyncPriceToSubscriptions() error {
	// Get environment variables for the script
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")
	priceID := os.Getenv("PRICE_ID")
	startDateStr := os.Getenv("START_DATE")
	userID := os.Getenv("USER_ID")
	dryRun := os.Getenv("DRY_RUN") == "true"

	if tenantID == "" || environmentID == "" || priceID == "" || startDateStr == "" || userID == "" {
		return fmt.Errorf("TENANT_ID, ENVIRONMENT_ID, PRICE_ID, START_DATE, and USER_ID are required")
	}

	// Parse start date (supports both YYYY-MM-DD and RFC3339 formats)
	var startDate time.Time
	var err error
	startDate, err = time.Parse(time.RFC3339, startDateStr)
	if err != nil {
		// Try date-only format as fallback
		startDate, err = time.Parse("2006-01-02", startDateStr)
		if err != nil {
			return fmt.Errorf("invalid START_DATE format, expected YYYY-MM-DD or RFC3339 (e.g., 2025-12-31T18:30:00Z): %w", err)
		}
	}

	log.Printf("Starting price sync for tenant: %s, environment: %s, price: %s, start_date: %s, user_id: %s, dry_run: %v\n",
		tenantID, environmentID, priceID, startDateStr, userID, dryRun)

	// Initialize script
	script, err := newSyncPriceToSubscriptionsScript()
	if err != nil {
		return fmt.Errorf("failed to initialize script: %w", err)
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)

	// Get the price
	p, err := script.priceRepo.Get(ctx, priceID)
	if err != nil {
		return fmt.Errorf("failed to get price: %w", err)
	}

	log.Printf("Found price: %s (type: %s, entity_id: %s, entity_type: %s)\n",
		p.ID, p.Type, p.EntityID, p.EntityType)

	// Validate price entity type is PLAN
	if p.EntityType != types.PRICE_ENTITY_TYPE_PLAN {
		return fmt.Errorf("price entity type must be PLAN, got: %s", p.EntityType)
	}

	planID := p.EntityID

	// Get the plan to verify it exists
	pln, err := script.planRepo.Get(ctx, planID)
	if err != nil {
		return fmt.Errorf("failed to get plan: %w", err)
	}
	log.Printf("Found plan: %s (%s)\n", pln.ID, pln.Name)

	// Get meter display name if meter_id exists
	var meterDisplayName string
	if p.MeterID != "" {
		m, err := script.meterRepo.GetMeter(ctx, p.MeterID)
		if err != nil {
			log.Printf("Warning: Failed to get meter %s: %v\n", p.MeterID, err)
		} else {
			meterDisplayName = m.Name
		}
	}

	// startOfDay and endOfDay for the given start date

	// Get all subscriptions with the same plan
	subscriptionFilter := &types.SubscriptionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		PlanID:      planID,
		SubscriptionStatus: []types.SubscriptionStatus{
			types.SubscriptionStatusActive,
			types.SubscriptionStatusTrialing,
		},
	}

	subscriptions, err := script.subscriptionRepo.ListAll(ctx, subscriptionFilter)
	if err != nil {
		return fmt.Errorf("failed to list subscriptions: %w", err)
	}

	log.Printf("Found %d subscriptions with plan %s and start_date %s (out of %d total)\n",
		len(subscriptions), planID, startDateStr, len(subscriptions))

	if len(subscriptions) == 0 {
		log.Printf("No subscriptions found matching the criteria\n")
		return nil
	}

	// Track statistics
	totalProcessed := 0
	totalCreated := 0
	totalSkipped := 0
	totalErrors := 0

	// Process each subscription
	for _, sub := range subscriptions {
		time.Sleep(50 * time.Millisecond) // Rate limiting

		// Check if line item with this price already exists for this subscription
		existingLineItems, err := script.lineItemRepo.List(ctx, &types.SubscriptionLineItemFilter{
			SubscriptionIDs: []string{sub.ID},
			PriceIDs:        []string{priceID},
			QueryFilter:     types.NewNoLimitQueryFilter(),
		})
		if err != nil {
			log.Printf("Error: Failed to check existing line items for subscription %s: %v\n", sub.ID, err)
			totalErrors++
			continue
		}

		if len(existingLineItems) > 0 {
			log.Printf("Skipping subscription %s - line item with price %s already exists\n", sub.ID, priceID)
			totalSkipped++
			continue
		}

		// Create new subscription line item
		lineItem := &subscription.SubscriptionLineItem{
			ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:   sub.ID,
			CustomerID:       sub.CustomerID,
			EntityID:         planID,
			EntityType:       types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName:  pln.Name,
			PriceID:          p.ID,
			PriceType:        p.Type,
			MeterID:          p.MeterID,
			MeterDisplayName: meterDisplayName,
			PriceUnitID:      p.PriceUnitID,
			PriceUnit:        p.PriceUnit,
			DisplayName:      p.DisplayName,
			Quantity:         decimal.Zero,
			Currency:         sub.Currency,
			BillingPeriod:    sub.BillingPeriod,
			InvoiceCadence:   p.InvoiceCadence,
			StartDate:        startDate,
			Metadata:         nil,
			EnvironmentID:    sub.EnvironmentID,
			BaseModel: types.BaseModel{
				TenantID:  sub.TenantID,
				Status:    types.StatusPublished,
				CreatedBy: userID,
				UpdatedBy: userID,
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			},
		}

		if dryRun {
			log.Printf("[DRY RUN] Would create line item for subscription %s (customer: %s)\n",
				sub.ID, sub.CustomerID)
			totalCreated++
		} else {
			err = script.lineItemRepo.Create(ctx, lineItem)
			if err != nil {
				log.Printf("Error: Failed to create line item for subscription %s: %v\n", sub.ID, err)
				totalErrors++
				continue
			}
			log.Printf("Created line item %s for subscription %s (customer: %s)\n",
				lineItem.ID, sub.ID, sub.CustomerID)
			totalCreated++
		}

		totalProcessed++
	}

	log.Printf("\n=== Sync Complete ===\n")
	log.Printf("Total subscriptions found: %d\n", len(subscriptions))
	log.Printf("Total processed: %d\n", totalProcessed)
	log.Printf("Total created: %d\n", totalCreated)
	log.Printf("Total skipped (already exists): %d\n", totalSkipped)
	log.Printf("Total errors: %d\n", totalErrors)

	if dryRun {
		log.Printf("\n[DRY RUN] No changes were made. Run without DRY_RUN=true to apply changes.\n")
	}

	return nil
}

func newSyncPriceToSubscriptionsScript() (*syncPriceToSubscriptionsScript, error) {
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
	entClient, err := postgres.NewEntClients(cfg, log)
	if err != nil {
		log.Fatalf("Failed to connect to postgres: %v", err)
	}
	client := postgres.NewClient(entClient, log, sentry.NewSentryService(cfg, log))
	cacheClient := cache.NewInMemoryCache()

	// Create repositories
	priceRepo := entRepo.NewPriceRepository(client, log, cacheClient)
	planRepo := entRepo.NewPlanRepository(client, log, cacheClient)
	meterRepo := entRepo.NewMeterRepository(client, log, cacheClient)
	subscriptionRepo := entRepo.NewSubscriptionRepository(client, log, cacheClient)
	lineItemRepo := entRepo.NewSubscriptionLineItemRepository(client, log, cacheClient)

	return &syncPriceToSubscriptionsScript{
		log:              log,
		priceRepo:        priceRepo,
		planRepo:         planRepo,
		meterRepo:        meterRepo,
		subscriptionRepo: subscriptionRepo,
		lineItemRepo:     lineItemRepo,
	}, nil
}
