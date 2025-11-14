package internal

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	domainSub "github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/types"
)

// MigrateBillingCycleParams holds parameters for billing cycle migration
type MigrateBillingCycleParams struct {
	TenantID      string
	EnvironmentID string
	DryRun        bool
}

// NOTE: It does not handle proration for subscriptions when changing the billing date.
// MigrateBillingCycle migrates subscriptions from anniversary to calendar billing cycle
func MigrateBillingCycle() error {
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")
	dryRunStr := os.Getenv("DRY_RUN")

	if tenantID == "" || environmentID == "" {
		return fmt.Errorf("TENANT_ID and ENVIRONMENT_ID are required")
	}

	dryRun := dryRunStr == "true" || dryRunStr == "1"

	params := MigrateBillingCycleParams{
		TenantID:      tenantID,
		EnvironmentID: environmentID,
		DryRun:        dryRun,
	}

	return migrateBillingCycle(params)
}

func migrateBillingCycle(params MigrateBillingCycleParams) error {
	fmt.Printf("Starting billing cycle migration...\n")
	fmt.Printf("Tenant ID: %s\n", params.TenantID)
	fmt.Printf("Environment ID: %s\n", params.EnvironmentID)
	fmt.Printf("Dry Run: %t\n", params.DryRun)
	fmt.Println(strings.Repeat("=", 50))

	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create logger
	log, err := logger.NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	// Create database client
	entClient, err := postgres.NewEntClients(cfg, log)
	if err != nil {
		return fmt.Errorf("failed to create ent client: %w", err)
	}

	// Create postgres client
	dbClient := postgres.NewClient(entClient, log, sentry.NewSentryService(cfg, log))

	// Create cache
	cache := cache.NewInMemoryCache()

	// Create repository
	subscriptionRepo := ent.NewSubscriptionRepository(dbClient, log, cache)

	// Create context with tenant and environment
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, params.TenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, params.EnvironmentID)

	// Create filter to get subscriptions with anniversary billing cycle
	filter := &types.SubscriptionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		// We'll filter by billing cycle in the service layer
	}

	// Get all subscriptions for the tenant and environment
	subscriptions, err := subscriptionRepo.List(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to list subscriptions: %w", err)
	}

	// Filter subscriptions with anniversary billing cycle
	var anniversarySubscriptions []*domainSub.Subscription
	for _, sub := range subscriptions {
		if sub.BillingCycle == types.BillingCycleAnniversary {
			anniversarySubscriptions = append(anniversarySubscriptions, sub)
		}
	}

	fmt.Printf("Found %d subscriptions with anniversary billing cycle\n", len(anniversarySubscriptions))

	if len(anniversarySubscriptions) == 0 {
		fmt.Println("No subscriptions found with anniversary billing cycle. Migration complete.")
		return nil
	}

	// Process each subscription
	successCount := 0
	errorCount := 0

	for i, sub := range anniversarySubscriptions {
		fmt.Printf("\nProcessing subscription %d/%d: %s\n", i+1, len(anniversarySubscriptions), sub.ID)
		fmt.Printf("  Current billing cycle: %s\n", sub.BillingCycle)
		fmt.Printf("  Current billing anchor: %s\n", sub.BillingAnchor.Format(time.RFC3339))
		fmt.Printf("  Current period start: %s\n", sub.CurrentPeriodStart.Format(time.RFC3339))
		fmt.Printf("  Current period end: %s\n", sub.CurrentPeriodEnd.Format(time.RFC3339))

		// Calculate new billing anchor for calendar billing
		newBillingAnchor := types.CalculateCalendarBillingAnchor(sub.StartDate, sub.BillingPeriod)
		fmt.Printf("  New billing anchor (calendar): %s\n", newBillingAnchor.Format(time.RFC3339))

		// Calculate new current period end using the subscription start date and new billing anchor
		// This ensures we get the correct period end for calendar billing
		newCurrentPeriodEnd, err := types.NextBillingDate(
			sub.CurrentPeriodStart,
			newBillingAnchor,
			sub.BillingPeriodCount,
			sub.BillingPeriod,
			sub.EndDate,
		)
		if err != nil {
			fmt.Printf("  ERROR: Failed to calculate new period end: %v\n", err)
			errorCount++
			continue
		}

		fmt.Printf("  New current period end: %s\n", newCurrentPeriodEnd.Format(time.RFC3339))

		if params.DryRun {
			fmt.Printf("  [DRY RUN] Would update subscription:\n")
			fmt.Printf("    - Billing cycle: %s -> %s\n", sub.BillingCycle, types.BillingCycleCalendar)
			fmt.Printf("    - Billing anchor: %s -> %s\n", sub.BillingAnchor.Format(time.RFC3339), newBillingAnchor.Format(time.RFC3339))
			fmt.Printf("    - Current period end: %s -> %s\n", sub.CurrentPeriodEnd.Format(time.RFC3339), newCurrentPeriodEnd.Format(time.RFC3339))
			successCount++
		} else {
			// Update the subscription using direct SQL since billing_cycle is immutable in ent
			// We need to update billing_cycle, billing_anchor, and current_period_end
			err = updateSubscriptionBillingCycle(ctx, dbClient, sub.ID, types.BillingCycleCalendar, newBillingAnchor, newCurrentPeriodEnd)
			if err != nil {
				fmt.Printf("  ERROR: Failed to update subscription: %v\n", err)
				errorCount++
				continue
			}

			fmt.Printf("  SUCCESS: Updated subscription successfully\n")
			successCount++
		}
	}

	// Print summary
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Printf("Migration Summary:\n")
	fmt.Printf("  Total subscriptions processed: %d\n", len(anniversarySubscriptions))
	fmt.Printf("  Successful: %d\n", successCount)
	fmt.Printf("  Errors: %d\n", errorCount)

	if params.DryRun {
		fmt.Printf("  Mode: DRY RUN (no actual changes made)\n")
	} else {
		fmt.Printf("  Mode: LIVE (changes applied to database)\n")
	}

	if errorCount > 0 {
		return fmt.Errorf("migration completed with %d errors", errorCount)
	}

	fmt.Println("Migration completed successfully!")
	return nil
}

// updateSubscriptionBillingCycle updates the billing cycle using direct SQL
// since the billing_cycle field is marked as immutable in the ent schema
func updateSubscriptionBillingCycle(ctx context.Context, dbClient postgres.IClient, subscriptionID string, billingCycle types.BillingCycle, billingAnchor, currentPeriodEnd time.Time) error {
	query := `
		UPDATE subscriptions 
		SET 
			billing_cycle = $1,
			billing_anchor = $2,
			current_period_end = $3,
			updated_at = $4
		WHERE 
			id = $5 
			AND tenant_id = $6 
			AND environment_id = $7
			AND status = 'published'
	`

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	now := time.Now().UTC()

	_, err := dbClient.Writer(ctx).ExecContext(ctx, query,
		string(billingCycle),
		billingAnchor,
		currentPeriodEnd,
		now,
		subscriptionID,
		tenantID,
		environmentID,
	)

	return err
}
