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
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type syncScript struct {
	log              *logger.Logger
	planRepo         plan.Repository
	priceRepo        price.Repository
	meterRepo        meter.Repository
	subscriptionRepo subscription.Repository
	lineItemRepo     subscription.LineItemRepository
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
	// Get the plan to be synced
	p, err := script.planRepo.Get(ctx, planID)
	if err != nil {
		return fmt.Errorf("failed to get plan: %w", err)
	}

	if p.TenantID != tenantID {
		return fmt.Errorf("plan does not belong to the specified tenant")
	}

	log.Printf("Found plan: %s (%s)\n", p.ID, p.Name)

	// Get all prices for the plan
	priceFilter := types.NewNoLimitPriceFilter()
	priceFilter = priceFilter.WithStatus(types.StatusPublished)
	priceFilter = priceFilter.WithEntityIDs([]string{planID})

	prices, err := script.priceRepo.List(ctx, priceFilter)
	if err != nil {
		return fmt.Errorf("failed to list prices for plan: %w", err)
	}

	// Filter prices for the plan and environment
	planPrices := make([]*price.Price, 0)
	meterMap := make(map[string]*meter.Meter)
	for _, price := range prices {
		if price.EntityID == planID && price.TenantID == tenantID && price.EnvironmentID == environmentID && price.EntityType == types.PRICE_ENTITY_TYPE_PLAN {
			planPrices = append(planPrices, price)
			if price.MeterID != "" {
				meterMap[price.MeterID] = nil
			}
		}
	}

	meterFilter := types.NewNoLimitMeterFilter()
	meterFilter.MeterIDs = lo.Keys(meterMap)
	meters, err := script.meterRepo.List(ctx, meterFilter)
	if err != nil {
		return fmt.Errorf("failed to list meters: %w", err)
	}

	for _, meter := range meters {
		meterMap[meter.ID] = meter
	}

	if len(planPrices) == 0 {
		return fmt.Errorf("no active prices found for this plan")
	}

	log.Printf("Found %d prices for plan %s\n", len(planPrices), planID)

	// Set up filter for subscriptions
	subscriptionFilter := &types.SubscriptionFilter{}
	subscriptionFilter.PlanID = planID
	// subscriptionFilter.CustomerID = "cust_01JSCQFRJ7WT63S2HJCJ5Z60DG"
	subscriptionFilter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}

	// Get all active subscriptions for this plan
	subs, err := script.subscriptionRepo.ListAll(ctx, subscriptionFilter)
	if err != nil {
		return fmt.Errorf("failed to list subscriptions: %w", err)
	}

	log.Printf("Found %d active subscriptions using plan %s\n", len(subs), planID)

	totalAdded := 0
	totalRemoved := 0
	totalSkipped := 0

	customers_to_skip := []string{
		"",
	}

	// Iterate through each subscription
	for _, sub := range subs {
		time.Sleep(100 * time.Millisecond)
		if sub.TenantID != tenantID || sub.EnvironmentID != environmentID {
			log.Printf("Skipping subscription %s - not in the specified tenant/environment\n", sub.ID)
			continue
		}

		if lo.Contains(customers_to_skip, sub.CustomerID) {
			log.Printf("Skipping subscription %s - in the skip list\n", sub.ID)
			continue
		}

		// filter the eligible price ids for this subscription by currency and period
		eligiblePriceList := make([]*price.Price, 0, len(planPrices))
		for _, p := range planPrices {
			if types.IsMatchingCurrency(p.Currency, sub.Currency) &&
				p.BillingPeriod == sub.BillingPeriod &&
				p.BillingPeriodCount == sub.BillingPeriodCount {
				eligiblePriceList = append(eligiblePriceList, p)
			}
		}

		// Get existing line items for the subscription
		lineItems, err := script.lineItemRepo.ListBySubscription(ctx, sub)
		if err != nil {
			log.Printf("Warning: Failed to get line items for subscription %s: %v\n", sub.ID, err)
			continue
		}

		// Create maps for fast lookups
		existingPriceIDs := make(map[string]*subscription.SubscriptionLineItem)
		for _, item := range lineItems {
			if item.EntityID == planID && item.Status == types.StatusPublished {
				existingPriceIDs[item.PriceID] = item
			}
		}

		addedCount := 0
		removedCount := 0
		skippedCount := 0

		// Map to track which prices we've processed
		processedPrices := make(map[string]bool)

		// Add missing prices from the plan
		for _, pr := range eligiblePriceList {
			processedPrices[pr.ID] = true

			// Check if the subscription already has this price
			_, exists := existingPriceIDs[pr.ID]
			if exists {
				skippedCount++
				continue
			}

			// Create a new line item for the subscription
			newLineItem := &subscription.SubscriptionLineItem{
				ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
				SubscriptionID:  sub.ID,
				CustomerID:      sub.CustomerID,
				EntityID:        planID,
				EntityType:      types.SubscriptionLineItemEntitiyTypePlan,
				PlanDisplayName: p.Name,
				PriceID:         pr.ID,
				PriceType:       pr.Type,
				MeterID:         pr.MeterID,
				Currency:        pr.Currency,
				BillingPeriod:   pr.BillingPeriod,
				InvoiceCadence:  pr.InvoiceCadence,
				TrialPeriod:     pr.TrialPeriod,
				PriceUnitID:     pr.PriceUnitID,
				PriceUnit:       pr.PriceUnit,
				StartDate:       sub.StartDate, // Use subscription's start date
				Metadata:        map[string]string{"added_by": "plan_sync_script"},
				EnvironmentID:   environmentID,
				BaseModel: types.BaseModel{
					TenantID:  tenantID,
					Status:    types.StatusPublished,
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
					CreatedBy: types.DefaultUserID,
					UpdatedBy: types.DefaultUserID,
				},
			}

			if pr.Type == types.PRICE_TYPE_USAGE && pr.MeterID != "" {
				newLineItem.MeterID = pr.MeterID
				newLineItem.MeterDisplayName = meterMap[pr.MeterID].Name
				newLineItem.DisplayName = meterMap[pr.MeterID].Name
				newLineItem.Quantity = decimal.Zero
			} else {
				newLineItem.DisplayName = p.Name
				newLineItem.Quantity = decimal.NewFromInt(1)
			}

			err = script.lineItemRepo.Create(ctx, newLineItem)
			if err != nil {
				log.Printf("Error: Failed to create line item for subscription %s: %v\n", sub.ID, err)
				continue
			}

			log.Printf("Added price %s to subscription %s\n", pr.ID, sub.ID)
			addedCount++
		}

		// Remove prices that are no longer in the plan
		for priceID, item := range existingPriceIDs {
			if !processedPrices[priceID] {
				// Mark the line item as deleted
				item.Status = types.StatusDeleted
				item.UpdatedAt = time.Now()

				err = script.lineItemRepo.Update(ctx, item)
				if err != nil {
					log.Printf("Error: Failed to delete line item for subscription %s: %v\n", sub.ID, err)
					continue
				}

				log.Printf("Removed price %s from subscription %s\n", priceID, sub.ID)
				removedCount++
			}
		}

		log.Printf("Subscription %s: added %d prices, removed %d prices, skipped %d prices\n",
			sub.ID, addedCount, removedCount, skippedCount)

		totalAdded += addedCount
		totalRemoved += removedCount
		totalSkipped += skippedCount
	}

	log.Printf("Plan sync completed. Total added: %d, Total removed: %d, Total skipped: %d\n",
		totalAdded, totalRemoved, totalSkipped)

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

	// Create repositories directly
	return &syncScript{
		log:              log,
		planRepo:         entRepo.NewPlanRepository(client, log, cacheClient),
		priceRepo:        entRepo.NewPriceRepository(client, log, cacheClient),
		meterRepo:        entRepo.NewMeterRepository(client, log, cacheClient),
		subscriptionRepo: entRepo.NewSubscriptionRepository(client, log, cacheClient),
		lineItemRepo:     entRepo.NewSubscriptionLineItemRepository(client, log, cacheClient),
	}, nil
}
