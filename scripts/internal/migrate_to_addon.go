package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// CopyPlanChargesToAddons copies all charges from plans to addons
func CopyPlanChargesToAddons() error {
	// Get environment variables
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")
	planToAddonMapStr := os.Getenv("PLAN_TO_ADDON_MAP")
	dryRunMode := os.Getenv("DRY_RUN_MODE")

	if tenantID == "" || environmentID == "" || planToAddonMapStr == "" {
		return fmt.Errorf("TENANT_ID, ENVIRONMENT_ID, and PLAN_TO_ADDON_MAP are required")
	}

	isDryRun := dryRunMode == "true"

	// Setup
	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	log, err := logger.NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	entClient, err := postgres.NewEntClient(cfg, log)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}
	defer entClient.Close()

	pgClient := postgres.NewClient(entClient, log, sentry.NewSentryService(cfg, log))
	cacheClient := cache.NewInMemoryCache()
	priceRepo := entRepo.NewPriceRepository(pgClient, log, cacheClient)

	ctx := context.WithValue(context.Background(), types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)

	log.Infow("Starting plan-to-addon charges copy", "tenant_id", tenantID, "environment_id", environmentID, "dry_run", isDryRun)

	// Step 1: Parse plan_id to addon_id mapping
	planToAddonMap := make(map[string]string)
	pairs := strings.Split(planToAddonMapStr, ",")
	for _, pair := range pairs {
		parts := strings.Split(strings.TrimSpace(pair), ":")
		if len(parts) != 2 {
			return fmt.Errorf("invalid mapping format: %s (expected plan_id:addon_id)", pair)
		}
		planID := strings.TrimSpace(parts[0])
		addonID := strings.TrimSpace(parts[1])
		planToAddonMap[planID] = addonID
	}

	log.Infow("Plan-to-addon mapping", "mappings", len(planToAddonMap))

	// Step 2: Get plan IDs from the mapping
	planIDs := make([]string, 0, len(planToAddonMap))
	for planID := range planToAddonMap {
		planIDs = append(planIDs, planID)
	}

	// Step 3: Get all prices for these plans
	priceFilter := types.NewNoLimitPriceFilter()
	priceFilter.Status = lo.ToPtr(types.StatusPublished)
	priceFilter.EntityType = lo.ToPtr(types.PRICE_ENTITY_TYPE_PLAN)
	priceFilter.EntityIDs = planIDs

	prices, err := priceRepo.ListAll(ctx, priceFilter)
	if err != nil {
		return fmt.Errorf("failed to get plan prices: %w", err)
	}

	log.Infow("Found plan prices", "count", len(prices))

	// Step 4: Create new prices for addons
	var newPrices []*price.Price
	for _, p := range prices {
		addonID, exists := planToAddonMap[p.EntityID]
		if !exists {
			log.Warnw("Skipping price - no addon mapping", "price_id", p.ID, "plan_id", p.EntityID)
			continue
		}

		newPrice := &price.Price{
			ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
			Amount:                 p.Amount,
			DisplayAmount:          p.DisplayAmount,
			Currency:               p.Currency,
			PriceUnitType:          p.PriceUnitType,
			PriceUnitID:            p.PriceUnitID,
			PriceUnitAmount:        p.PriceUnitAmount,
			DisplayPriceUnitAmount: p.DisplayPriceUnitAmount,
			PriceUnit:              p.PriceUnit,
			ConversionRate:         p.ConversionRate,
			Type:                   p.Type,
			BillingPeriod:          p.BillingPeriod,
			BillingPeriodCount:     p.BillingPeriodCount,
			BillingModel:           p.BillingModel,
			BillingCadence:         p.BillingCadence,
			InvoiceCadence:         p.InvoiceCadence,
			TrialPeriod:            p.TrialPeriod,
			TierMode:               p.TierMode,
			Tiers:                  p.Tiers,
			PriceUnitTiers:         p.PriceUnitTiers,
			MeterID:                p.MeterID,
			LookupKey:              "",
			Description:            p.Description,
			TransformQuantity:      p.TransformQuantity,
			Metadata:               p.Metadata,
			EnvironmentID:          p.EnvironmentID,
			EntityType:             types.PRICE_ENTITY_TYPE_ADDON, // Change to ADDON
			EntityID:               addonID,                       // Use addon ID
			ParentPriceID:          p.ParentPriceID,
			BaseModel: types.BaseModel{
				TenantID:  p.TenantID,
				Status:    types.StatusPublished,
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
				CreatedBy: p.CreatedBy,
				UpdatedBy: p.UpdatedBy,
			},
		}

		newPrices = append(newPrices, newPrice)
		log.Infow("Prepared price for addon", "plan_price_id", p.ID, "new_price_id", newPrice.ID, "addon_id", addonID)
	}

	// Step 5: Save or output results
	if isDryRun {
		// Write to JSON file
		timestamp := time.Now().Format("20060102_150405")
		filename := fmt.Sprintf("plan_to_addon_copy_dry_run_%s.json", timestamp)

		output := map[string]interface{}{
			"tenant_id":         tenantID,
			"environment_id":    environmentID,
			"plan_to_addon_map": planToAddonMap,
			"prices_to_create":  newPrices,
			"summary": map[string]int{
				"plan_prices_found":      len(prices),
				"addon_prices_to_create": len(newPrices),
			},
		}

		jsonData, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}

		err = os.WriteFile(filename, jsonData, 0644)
		if err != nil {
			return fmt.Errorf("failed to write JSON file: %w", err)
		}

		log.Infow("Dry run completed - output written to file", "filename", filename, "prices_to_create", len(newPrices))
		fmt.Printf("✅ Dry run completed! Output written to: %s\n", filename)
		fmt.Printf("📊 Summary: %d plan prices found, %d addon prices would be created\n", len(prices), len(newPrices))
	} else {
		// Write to database
		if len(newPrices) > 0 {
			err = priceRepo.CreateBulk(ctx, newPrices)
			if err != nil {
				return fmt.Errorf("failed to create prices: %w", err)
			}
			log.Infow("Successfully created addon prices", "count", len(newPrices))
			fmt.Printf("✅ Successfully created %d addon prices!\n", len(newPrices))
		} else {
			fmt.Println("ℹ️  No prices to create")
		}
	}

	return nil
}
