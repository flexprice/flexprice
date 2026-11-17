package internal

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/flexprice/flexprice/ent"
	entsubscription "github.com/flexprice/flexprice/ent/subscription"
	"github.com/flexprice/flexprice/ent/subscriptionlineitem"
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

// unendedLineItemFinder finds the line items of a plan price that have no end date and sit on an
// active or trialing subscription of that plan.
type unendedLineItemFinder interface {
	Find(ctx context.Context, planID, priceID string, limit int) ([]*subscription.SubscriptionLineItem, error)
	Count(ctx context.Context, planID, priceID string) (int, error)
}

type entUnendedLineItemFinder struct {
	client *ent.Client
}

func (f *entUnendedLineItemFinder) query(ctx context.Context, planID, priceID string) *ent.SubscriptionLineItemQuery {
	return f.client.SubscriptionLineItem.Query().
		Where(
			subscriptionlineitem.TenantID(types.GetTenantID(ctx)),
			subscriptionlineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
			subscriptionlineitem.PriceID(priceID),
			subscriptionlineitem.Status(string(types.StatusPublished)),
			subscriptionlineitem.EndDateIsNil(),
			subscriptionlineitem.HasSubscriptionWith(
				entsubscription.PlanID(planID),
				entsubscription.Status(string(types.StatusPublished)),
				entsubscription.SubscriptionStatusIn(types.SubscriptionStatusActive, types.SubscriptionStatusTrialing),
			),
		)
}

func (f *entUnendedLineItemFinder) Find(ctx context.Context, planID, priceID string, limit int) ([]*subscription.SubscriptionLineItem, error) {
	items, err := f.query(ctx, planID, priceID).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	return subscription.GetLineItemFromEntList(items), nil
}

func (f *entUnendedLineItemFinder) Count(ctx context.Context, planID, priceID string) (int, error) {
	return f.query(ctx, planID, priceID).Count(ctx)
}

type fixLineItemPriceEndDatesScript struct {
	planRepo     plan.Repository
	priceRepo    price.Repository
	lineItemRepo subscription.LineItemRepository
	finder       unendedLineItemFinder

	batchSize int
}

// FixLineItemPriceEndDates sets the price's end date on plan line items of active and trialing
// subscriptions that have no end date. Line items that already have an end date are left alone.
func FixLineItemPriceEndDates() error {
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")
	userID := os.Getenv("USER_ID")
	// Writes only on APPLY=true, since scripts/main.go always sets DRY_RUN=false from its -dry-run default.
	// An explicit -dry-run=true still wins over APPLY=true.
	dryRun := os.Getenv("APPLY") != "true" || os.Getenv("DRY_RUN") == "true"
	outputFile := os.Getenv("OUTPUT_FILE")

	if tenantID == "" || environmentID == "" || userID == "" {
		return fmt.Errorf("TENANT_ID, ENVIRONMENT_ID and USER_ID are required")
	}
	if outputFile == "" {
		outputFile = fmt.Sprintf("line_item_price_end_dates_%s.csv", time.Now().UTC().Format("20060102_150405"))
	}

	log.Printf("Fixing line item price end dates for tenant: %s, environment: %s, dry_run: %v\n", // #nosec G706 -- operator tooling
		tenantID, environmentID, dryRun)

	script, err := newFixLineItemPriceEndDatesScript(dryRun)
	if err != nil {
		return fmt.Errorf("failed to initialize script: %w", err)
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
	ctx = context.WithValue(ctx, types.CtxUserID, userID)

	out, err := os.Create(outputFile) // #nosec G703,G304 -- operator-supplied report path
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	writer := csv.NewWriter(out)

	log.Printf("Report: %s\n", outputFile) // #nosec G706 -- operator tooling
	runErr := script.run(ctx, writer, userID, dryRun)
	writer.Flush()
	return errors.Join(runErr, writer.Error(), out.Close())
}

// run counts (dry run) or fixes the line items with no end date on every plan price that has one.
func (s *fixLineItemPriceEndDatesScript) run(ctx context.Context, writer *csv.Writer, userID string, dryRun bool) error {
	planFilter := types.NewNoLimitPlanFilter()
	planFilter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	plans, err := s.planRepo.ListAll(ctx, planFilter)
	if err != nil {
		return fmt.Errorf("failed to list plans: %w", err)
	}
	log.Printf("Found %d published plans\n", len(plans))

	header := []string{
		"plan_name", "plan_id", "price_id", "subscription_id", "line_item_id",
		"price_start_date", "price_end_date", "line_item_start_date", "line_item_end_date", "action",
	}
	if dryRun {
		header = []string{"plan_name", "plan_id", "price_id", "price_start_date", "price_end_date", "line_items_to_fix"}
	}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("failed to write report header: %w", err)
	}

	var total int
	for _, p := range plans {
		priceFilter := types.NewNoLimitPriceFilter().
			WithEntityType(types.PRICE_ENTITY_TYPE_PLAN).
			WithEntityIDs([]string{p.ID}).
			WithAllowExpiredPrices(true)
		prices, err := s.priceRepo.ListAll(ctx, priceFilter)
		if err != nil {
			return fmt.Errorf("failed to list prices of plan %s: %w", p.ID, err)
		}

		pricesWithEndDate := lo.Filter(prices, func(pr *price.Price, _ int) bool { return pr.EndDate != nil })
		if len(pricesWithEndDate) == 0 {
			continue
		}
		log.Printf("Plan %s: %d prices with an end date\n", p.ID, len(pricesWithEndDate)) // #nosec G706 -- operator tooling

		for _, endingPrice := range pricesWithEndDate {
			var fixed int
			if dryRun {
				fixed, err = s.count(ctx, writer, p, endingPrice)
			} else {
				fixed, err = s.fix(ctx, writer, p, endingPrice, userID)
			}
			total += fixed
			if err != nil {
				return err
			}
			log.Printf("Price %s: %d line items fixed (or to fix)\n", endingPrice.ID, fixed) // #nosec G706 -- operator tooling
		}
	}

	log.Printf("Done. line items fixed (or to fix in a dry run): %d\n", total)
	return nil
}

// count writes how many line items of the price would be fixed.
func (s *fixLineItemPriceEndDatesScript) count(ctx context.Context, writer *csv.Writer, p *plan.Plan, endingPrice *price.Price) (int, error) {
	toFix, err := s.finder.Count(ctx, p.ID, endingPrice.ID)
	if err != nil {
		return 0, fmt.Errorf("failed to count line items of price %s: %w", endingPrice.ID, err)
	}
	if toFix == 0 {
		return 0, nil
	}
	if err := writer.Write([]string{
		p.Name,
		p.ID,
		endingPrice.ID,
		formatOptionalTime(lo.FromPtr(endingPrice.StartDate)),
		endingPrice.EndDate.Format(time.RFC3339),
		strconv.Itoa(toFix),
	}); err != nil {
		return 0, fmt.Errorf("failed to write report row: %w", err)
	}
	return toFix, nil
}

// fix sets the price's end date on its line items a batch at a time. Fixed line items no longer match
// the query, so each batch is read from the start and the loop ends when none are left.
func (s *fixLineItemPriceEndDatesScript) fix(ctx context.Context, writer *csv.Writer, p *plan.Plan, endingPrice *price.Price, userID string) (int, error) {
	var fixed int
	for {
		items, err := s.finder.Find(ctx, p.ID, endingPrice.ID, s.batchSize)
		if err != nil {
			return fixed, fmt.Errorf("failed to list line items of price %s: %w", endingPrice.ID, err)
		}
		if len(items) == 0 {
			return fixed, nil
		}

		var failed int
		for _, item := range items {
			lineItemEndDate := *endingPrice.EndDate
			// Same clamp as plan price sync: a price that ended before the line started ends it at its start
			if lineItemEndDate.Before(item.StartDate) {
				lineItemEndDate = item.StartDate
			}

			baseModel := item.BaseModel
			baseModel.UpdatedBy = userID
			updatedItem := subscription.NewSubscriptionLineItemBuilder(item).
				WithEndDate(lineItemEndDate).
				WithBaseModel(baseModel).
				Build()
			action := "updated"
			if err := s.lineItemRepo.Update(ctx, updatedItem); err != nil {
				log.Printf("Failed to update line item %s: %v\n", item.ID, err) // #nosec G706 -- operator tooling
				action = "failed"
				failed++
			} else {
				fixed++
			}

			if err := writer.Write([]string{
				p.Name,
				p.ID,
				endingPrice.ID,
				item.SubscriptionID,
				item.ID,
				formatOptionalTime(lo.FromPtr(endingPrice.StartDate)),
				endingPrice.EndDate.Format(time.RFC3339),
				item.StartDate.Format(time.RFC3339),
				lineItemEndDate.Format(time.RFC3339),
				action,
			}); err != nil {
				return fixed, fmt.Errorf("failed to write report row: %w", err)
			}
		}

		// A failed line item still matches the query, so stop here and let a rerun pick it up
		if failed > 0 {
			return fixed, fmt.Errorf("%d line items of price %s failed to update", failed, endingPrice.ID)
		}
	}
}

func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func newFixLineItemPriceEndDatesScript(dryRun bool) (*fixLineItemPriceEndDatesScript, error) {
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

	// Applying reads its own updates back, which a lagging replica would not show yet
	finderClient := entClient.Writer
	if dryRun {
		finderClient = entClient.Reader
	}

	return &fixLineItemPriceEndDatesScript{
		planRepo:     entRepo.NewPlanRepository(client, logr, cache.NewInMemoryCache()),
		priceRepo:    entRepo.NewPriceRepository(client, logr, redisCache),
		lineItemRepo: entRepo.NewSubscriptionLineItemRepository(client, logr),
		finder:       &entUnendedLineItemFinder{client: finderClient},
		batchSize:    1000,
	}, nil
}
