package internal

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/tracing"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type fixLineItemPriceEndDatesScript struct {
	planRepo         plan.Repository
	priceRepo        price.Repository
	subscriptionRepo subscription.Repository
	lineItemRepo     subscription.LineItemRepository
}

// FixLineItemPriceEndDates sets the price's end date on plan line items of active and trialing
// subscriptions that have no end date. Line items that already have an end date are left alone.
func FixLineItemPriceEndDates() error {
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")
	userID := os.Getenv("USER_ID")
	// Writes only on APPLY=true, since scripts/main.go always sets DRY_RUN=false from its -dry-run default
	dryRun := os.Getenv("APPLY") != "true"
	outputFile := os.Getenv("OUTPUT_FILE")

	if tenantID == "" || environmentID == "" || userID == "" {
		return fmt.Errorf("TENANT_ID, ENVIRONMENT_ID and USER_ID are required")
	}
	if outputFile == "" {
		outputFile = fmt.Sprintf("line_item_price_end_dates_%s.csv", time.Now().UTC().Format("20060102_150405"))
	}

	log.Printf("Fixing line item price end dates for tenant: %s, environment: %s, dry_run: %v\n", // #nosec G706 -- operator tooling
		tenantID, environmentID, dryRun)

	script, err := newFixLineItemPriceEndDatesScript()
	if err != nil {
		return fmt.Errorf("failed to initialize script: %w", err)
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
	ctx = context.WithValue(ctx, types.CtxUserID, userID)

	out, err := os.Create(outputFile) // #nosec G304 -- operator-supplied report path
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer out.Close()
	writer := csv.NewWriter(out)
	defer writer.Flush()

	log.Printf("Report: %s\n", outputFile) // #nosec G706 -- operator tooling
	return script.run(ctx, writer, userID, dryRun)
}

// run walks every published plan and writes one report row per line item on a price with an end date.
func (s *fixLineItemPriceEndDatesScript) run(ctx context.Context, writer *csv.Writer, userID string, dryRun bool) error {
	planFilter := types.NewNoLimitPlanFilter()
	planFilter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	plans, err := s.planRepo.ListAll(ctx, planFilter)
	if err != nil {
		return fmt.Errorf("failed to list plans: %w", err)
	}
	log.Printf("Found %d published plans\n", len(plans))

	if err := writer.Write([]string{
		"plan_name", "plan_id", "price_id", "subscription_id", "line_item_id",
		"price_start_date", "price_end_date", "line_item_start_date", "line_item_end_date", "action",
	}); err != nil {
		return fmt.Errorf("failed to write report header: %w", err)
	}

	var updated, skipped, failed int
	for _, p := range plans {
		priceFilter := types.NewNoLimitPriceFilter().
			WithEntityType(types.PRICE_ENTITY_TYPE_PLAN).
			WithEntityIDs([]string{p.ID}).
			WithAllowExpiredPrices(true)
		prices, err := s.priceRepo.ListAll(ctx, priceFilter)
		if err != nil {
			return fmt.Errorf("failed to list prices of plan %s: %w", p.ID, err)
		}

		pricesWithEndDate := make(map[string]*price.Price)
		for _, pr := range prices {
			if pr.EndDate != nil {
				pricesWithEndDate[pr.ID] = pr
			}
		}
		if len(pricesWithEndDate) == 0 {
			continue
		}

		subs, err := s.subscriptionRepo.ListAll(ctx, &types.SubscriptionFilter{
			QueryFilter: types.NewNoLimitQueryFilter(),
			PlanID:      p.ID,
			SubscriptionStatus: []types.SubscriptionStatus{
				types.SubscriptionStatusActive,
				types.SubscriptionStatusTrialing,
			},
			WithLineItems: true,
		})
		if err != nil {
			return fmt.Errorf("failed to list subscriptions of plan %s: %w", p.ID, err)
		}
		log.Printf("Plan %s: %d prices with an end date, %d subscriptions\n", p.ID, len(pricesWithEndDate), len(subs)) // #nosec G706 -- operator tooling

		for _, sub := range subs {
			for _, item := range sub.GetLineItems() {
				endingPrice, ok := pricesWithEndDate[item.PriceID]
				if !ok {
					continue
				}

				// A line item with an end date may have been adjusted on purpose, so it is left as is
				action := "skipped_has_end_date"
				lineItemEndDate := item.EndDate
				if item.EndDate.IsZero() {
					action = "dry_run"
					lineItemEndDate = *endingPrice.EndDate
					if !dryRun {
						baseModel := item.BaseModel
						baseModel.UpdatedBy = userID
						fixed := subscription.NewSubscriptionLineItemBuilder(item).
							WithEndDate(lineItemEndDate).
							WithBaseModel(baseModel).
							Build()
						if err := s.lineItemRepo.Update(ctx, fixed); err != nil {
							log.Printf("Failed to update line item %s: %v\n", item.ID, err) // #nosec G706 -- operator tooling
							action = "failed"
						} else {
							action = "updated"
						}
					}
				}

				switch action {
				case "skipped_has_end_date":
					skipped++
				case "failed":
					failed++
				default:
					updated++
				}

				if err := writer.Write([]string{
					p.Name,
					p.ID,
					endingPrice.ID,
					sub.ID,
					item.ID,
					formatOptionalTime(lo.FromPtr(endingPrice.StartDate)),
					endingPrice.EndDate.Format(time.RFC3339),
					item.StartDate.Format(time.RFC3339),
					lineItemEndDate.Format(time.RFC3339),
					action,
				}); err != nil {
					return fmt.Errorf("failed to write report row: %w", err)
				}
			}
		}
	}

	log.Printf("Done. updated (or dry_run): %d, failed: %d, skipped_has_end_date: %d\n", updated, failed, skipped)
	if failed > 0 {
		return fmt.Errorf("%d line items failed to update", failed)
	}
	return nil
}

func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func newFixLineItemPriceEndDatesScript() (*fixLineItemPriceEndDatesScript, error) {
	cfg, err := config.NewConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	logr, err := logger.NewLogger(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	entClient, err := postgres.NewEntClients(cfg, logr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}
	client := postgres.NewClient(entClient, logr, tracing.NewService(cfg, logr))
	redisCache := cache.NewRedisCache()

	return &fixLineItemPriceEndDatesScript{
		planRepo:         entRepo.NewPlanRepository(client, logr, cache.NewInMemoryCache()),
		priceRepo:        entRepo.NewPriceRepository(client, logr, redisCache),
		subscriptionRepo: entRepo.NewSubscriptionRepository(client, logr, redisCache),
		lineItemRepo:     entRepo.NewSubscriptionLineItemRepository(client, logr),
	}, nil
}
