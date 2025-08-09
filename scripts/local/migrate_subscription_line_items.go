package local

import (
	"context"
	"fmt"
	"log"

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

// MigrateSubscriptionLineItems migrates existing subscriptions to include line items
func MigrateSubscriptionLineItems() error {
	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize logger
	logger, err := logger.NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	log.Println("Migrating subscription line items")

	ctx := context.Background()
	tenants := []string{"1cd1a8fb-9d1b-461d-accc-d32df594e436", "2c243ba8-9f99-4032-a93d-4e52042ed0e0", "a06e8bde-5bff-438f-821a-326b2f4a0c94", "ae79fbc2-395b-43d5-94e1-cc598717b7ff", "cd204673-7543-4eb3-89e0-47cc5f32192b", "eb7bc8e8-bec0-41a1-95fd-eab43f337641", "f2aaf2a6-a72a-4733-8efb-e9ccc54f6550", "tenant_01JH280NTMS33TWJ8V8V8M2KTR", "tenant_01JH2DPR6C7C90ZEQQ8F56H741", "tenant_01JJ20ZF9M5GQQXJG2AQSMZCVM"}

	// Initialize database client
	entClient, err := postgres.NewEntClient(cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create database client: %w", err)
	}
	client := postgres.NewClient(entClient, logger, sentry.NewSentryService(cfg, logger))

	cache := cache.NewInMemoryCache()

	// Initialize repositories
	subscriptionRepo := entRepo.NewSubscriptionRepository(client, logger, cache)
	subscriptionLineItemRepo := entRepo.NewSubscriptionLineItemRepository(client)
	planRepo := entRepo.NewPlanRepository(client, logger, cache)
	priceRepo := entRepo.NewPriceRepository(client, logger, cache)
	meterRepo := entRepo.NewMeterRepository(client, logger, cache)

	// Get all published subscriptions without line items
	filter := types.NewNoLimitSubscriptionFilter()
	filter.SubscriptionStatus = []types.SubscriptionStatus{types.SubscriptionStatusActive}

	for _, tenantID := range tenants {
		ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
		subs, err := subscriptionRepo.List(ctx, filter)
		if err != nil {
			return fmt.Errorf("failed to list subscriptions: %w", err)
		}

		log.Printf("Found %d subscriptions to process", len(subs))

		// fetch all meters
		meterFilter := types.NewNoLimitMeterFilter()
		meterFilter.QueryFilter.Status = nil
		meters, err := meterRepo.List(ctx, meterFilter)
		if err != nil {
			return fmt.Errorf("failed to list meters: %w", err)
		}
		metersByID := make(map[string]*meter.Meter, len(meters))
		for _, m := range meters {
			metersByID[m.ID] = m
		}

		// fetch all plans
		plans, err := planRepo.List(ctx, types.NewNoLimitPlanFilter())
		if err != nil {
			return fmt.Errorf("failed to list plans: %w", err)
		}
		plansByID := make(map[string]*plan.Plan, len(plans))
		for _, p := range plans {
			plansByID[p.ID] = p
		}

		// fetch all prices
		prices, err := priceRepo.List(ctx, types.NewNoLimitPriceFilter())
		if err != nil {
			return fmt.Errorf("failed to list prices: %w", err)
		}
		pricesByID := make(map[string]*price.Price, len(prices))
		for _, p := range prices {
			pricesByID[p.ID] = p
		}

		// Process each subscription
		for _, sub := range subs {
			// Skip if subscription already has line items
			if len(sub.LineItems) > 0 {
				log.Printf("Skipping subscription %s - already has line items", sub.ID)
				continue
			}

			// get plan
			plan, ok := plansByID[sub.PlanID]
			if !ok {
				log.Printf("Plan not found for subscription %s", sub.ID)
				continue
			}

			validPrices := make([]*price.Price, 0)
			for _, p := range prices {
				if p.EntityID == plan.ID &&
					p.Status == types.StatusPublished &&
					types.IsMatchingCurrency(p.Currency, sub.Currency) &&
					p.EntityType == types.PRICE_ENTITY_TYPE_PLAN &&
					p.BillingPeriod == sub.BillingPeriod {
					validPrices = append(validPrices, p)
				}
			}

			if len(validPrices) == 0 {
				log.Printf("No valid prices found for subscription %s", sub.ID)
				continue
			}

			// Create line items for each price
			lineItems := make([]*subscription.SubscriptionLineItem, 0, len(validPrices))
			for _, price := range validPrices {
				lineItem := &subscription.SubscriptionLineItem{
					ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
					SubscriptionID:  sub.ID,
					CustomerID:      sub.CustomerID,
					EntityID:        plan.ID,
					EntityType:      types.SubscriptionLineItemEntitiyTypePlan,
					PriceID:         price.ID,
					PriceType:       price.Type,
					MeterID:         price.MeterID,
					PlanDisplayName: plan.Name,
					DisplayName:     price.Description,
					Quantity:        decimal.Zero,
					Currency:        sub.Currency,
					BillingPeriod:   sub.BillingPeriod,
					PriceUnitID:     price.PriceUnitID,
					PriceUnit:       price.PriceUnit,
					StartDate:       sub.CurrentPeriodStart,
					EndDate:         sub.CurrentPeriodEnd,
					BaseModel:       types.GetDefaultBaseModel(ctx),
				}

				if price.MeterID != "" {
					if m, ok := metersByID[price.MeterID]; ok {
						lineItem.MeterDisplayName = m.Name
					} else {
						log.Printf("Meter not found for price %s", price.ID)
					}
				}

				lineItems = append(lineItems, lineItem)
			}

			// Update subscription with line items
			err = subscriptionLineItemRepo.CreateBulk(ctx, lineItems)
			if err != nil {
				log.Printf("Failed to create subscription with line items %s: %v", sub.ID, err)
				continue
			}

			log.Printf("Successfully added %d line items to subscription %s", len(lineItems), sub.ID)
		}
	}

	log.Printf("Migration completed")
	return nil
}
