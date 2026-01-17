package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/service/subscription_schedules"
	temporalClient "github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/types"
	"go.uber.org/zap"
)

// MigrateCancelSchedules migrates subscriptions with cancel_at_period_end flag to use schedules
func main() {
	// Parse flags
	dryRun := flag.Bool("dry-run", true, "Run in dry-run mode (no actual changes)")
	flag.Parse()

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	if *dryRun {
		logger.Info("running in DRY-RUN mode - no changes will be made")
	}

	// Load config
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
	}

	// Initialize database
	db, err := postgres.NewClient(cfg.Database)
	if err != nil {
		logger.Fatal("failed to connect to database", zap.Error(err))
	}

	// Initialize repositories
	subscriptionRepo := ent.NewSubscriptionRepository(db, logger.Sugar())
	scheduleRepo := ent.NewSubscriptionScheduleRepository(db, logger.Sugar())

	// Initialize Temporal client (only if not dry-run)
	var tc temporalClient.TemporalClient
	if !*dryRun {
		tc, err = temporalClient.NewTemporalClient(cfg.Temporal)
		if err != nil {
			logger.Fatal("failed to create temporal client", zap.Error(err))
		}
		defer tc.Close()
	}

	// Initialize schedule service
	scheduleService := subscription_schedules.NewService(
		scheduleRepo,
		subscriptionRepo,
		logger,
	)

	ctx := context.Background()

	// Find all subscriptions with cancel_at_period_end = true
	logger.Info("querying subscriptions with cancel_at_period_end flag...")

	// We need to query directly since the repository might not support this filter
	query := `
		SELECT id, tenant_id, environment_id, cancel_at, cancel_at_period_end, subscription_status, created_by
		FROM subscriptions
		WHERE cancel_at_period_end = true
		AND subscription_status = 'active'
		AND status = 'published'
	`

	rows, err := db.Client().Query(ctx, query)
	if err != nil {
		logger.Fatal("failed to query subscriptions", zap.Error(err))
	}
	defer rows.Close()

	type subData struct {
		ID                 string
		TenantID           string
		EnvironmentID      string
		CancelAt           *time.Time
		SubscriptionStatus string
		CreatedBy          string
	}

	var subscriptions []subData
	for rows.Next() {
		var s subData
		var cancelAtPeriodEnd bool
		err := rows.Scan(&s.ID, &s.TenantID, &s.EnvironmentID, &s.CancelAt, &cancelAtPeriodEnd, &s.SubscriptionStatus, &s.CreatedBy)
		if err != nil {
			logger.Error("failed to scan row", zap.Error(err))
			continue
		}
		subscriptions = append(subscriptions, s)
	}

	logger.Info("found subscriptions to migrate", zap.Int("count", len(subscriptions)))

	if len(subscriptions) == 0 {
		logger.Info("no subscriptions to migrate")
		return
	}

	migrated := 0
	failed := 0
	skipped := 0

	for _, subData := range subscriptions {
		if subData.CancelAt == nil {
			logger.Warn("subscription has cancel_at_period_end but no cancel_at date, skipping",
				zap.String("subscription_id", subData.ID),
			)
			skipped++
			continue
		}

		logger.Info("migrating subscription",
			zap.String("subscription_id", subData.ID),
			zap.Time("cancel_at", *subData.CancelAt),
		)

		if *dryRun {
			logger.Info("DRY-RUN: would create schedule for subscription",
				zap.String("subscription_id", subData.ID),
				zap.Time("scheduled_at", *subData.CancelAt),
			)
			migrated++
			continue
		}

		// Get full subscription object
		sub, err := subscriptionRepo.Get(ctx, subData.ID)
		if err != nil {
			logger.Error("failed to get subscription",
				zap.String("subscription_id", subData.ID),
				zap.Error(err),
			)
			failed++
			continue
		}

		// Create snapshot
		snapshot := &subscription.CancellationSubscriptionSnapshot{
			SubscriptionStatus: types.SubscriptionStatus(subData.SubscriptionStatus),
			CancelAtPeriodEnd:  true,
			CancelAt:           subData.CancelAt,
			EndDate:            sub.EndDate,
		}

		config := &subscription.CancellationConfiguration{
			CancellationType:     types.CancellationTypeEndOfPeriod,
			Reason:               "Migrated from legacy flag-based cancellation",
			ProrationBehavior:    types.ProrationBehaviorCreateProrations,
			SubscriptionSnapshot: snapshot,
		}

		// Create schedule
		_, err = scheduleService.ScheduleCancellation(ctx, sub.ID, *subData.CancelAt, config)
		if err != nil {
			logger.Error("failed to create schedule",
				zap.String("subscription_id", sub.ID),
				zap.Error(err),
			)
			failed++
			continue
		}

		// Clear old flag (but keep cancel_at for reference)
		sub.CancelAtPeriodEnd = false
		if err := subscriptionRepo.Update(ctx, sub); err != nil {
			logger.Error("failed to clear cancel_at_period_end flag",
				zap.String("subscription_id", sub.ID),
				zap.Error(err),
			)
			failed++
			continue
		}

		migrated++
		logger.Info("successfully migrated subscription",
			zap.String("subscription_id", sub.ID),
			zap.String("schedule_created", "yes"),
		)
	}

	logger.Info("migration complete",
		zap.Int("total", len(subscriptions)),
		zap.Int("migrated", migrated),
		zap.Int("failed", failed),
		zap.Int("skipped", skipped),
		zap.Bool("dry_run", *dryRun),
	)

	if *dryRun {
		fmt.Println("\n=== DRY-RUN MODE ===")
		fmt.Printf("Would migrate %d subscriptions\n", migrated)
		fmt.Printf("Run with --dry-run=false to apply changes\n")
	}
}
