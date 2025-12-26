package internal

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	cg "github.com/flexprice/flexprice/ent/creditgrant"
	cga "github.com/flexprice/flexprice/ent/creditgrantapplication"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/creditgrantapplication"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	_ "github.com/lib/pq"
	"github.com/samber/lo"
)

// MigrateCGA migrates credit grants and CGAs:
// 1. Updates credit grants: sets start_date, end_date, credit_grant_anchor from subscription
// 2. Updates CGAs: sets period_start = scheduled_for, calculates period_end from subscription billing period
func MigrateCGA() error {
	isDryRun := os.Getenv("DRY_RUN") == "true"

	// Setup
	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	log, err := logger.NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	entClient, err := postgres.NewEntClients(cfg, log)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}

	pgClient := postgres.NewClient(entClient, log, sentry.NewSentryService(cfg, log))
	cacheClient := cache.NewInMemoryCache()

	subscriptionRepo := entRepo.NewSubscriptionRepository(pgClient, log, cacheClient)
	cgaRepo := entRepo.NewCreditGrantApplicationRepository(pgClient, log, cacheClient)
	creditGrantRepo := entRepo.NewCreditGrantRepository(pgClient, log, cacheClient)

	baseCtx := context.Background()

	log.Infow("Starting migration", "dry_run", isDryRun)

	// Stats
	var creditGrantsUpdated, creditGrantsSkipped int
	var cgasUpdated, cgasSkipped int

	// Step 1: Migrate Credit Grants
	log.Infow("Migrating credit grants")

	// Query directly using ent client to bypass tenant/environment filtering
	allCreditGrantsEnt, err := entClient.Reader.CreditGrant.Query().
		Where(
			cg.ScopeEQ(types.CreditGrantScopeSubscription),
			cg.StatusEQ(string(types.StatusPublished)),
		).
		All(baseCtx)
	if err != nil {
		return fmt.Errorf("failed to list credit grants: %w", err)
	}

	allCreditGrants := creditgrant.FromEntList(allCreditGrantsEnt)
	log.Infow("Found credit grants", "count", len(allCreditGrants))

	// Open DB connection for raw SQL updates
	db, err := sql.Open("postgres", cfg.Postgres.GetDSN())
	if err != nil {
		return fmt.Errorf("failed to open database connection: %w", err)
	}
	defer db.Close()

	query := "UPDATE credit_grants SET start_date = $1, end_date = $2, credit_grant_anchor = $3 WHERE id = $4 AND tenant_id = $5 AND environment_id = $6"

	for _, creditGrant := range allCreditGrants {
		if creditGrant.SubscriptionID == nil || lo.FromPtr(creditGrant.SubscriptionID) == "" {
			creditGrantsSkipped++
			continue
		}

		subCtx := context.WithValue(baseCtx, types.CtxTenantID, creditGrant.TenantID)
		subCtx = context.WithValue(subCtx, types.CtxEnvironmentID, creditGrant.EnvironmentID)

		sub, err := subscriptionRepo.Get(subCtx, lo.FromPtr(creditGrant.SubscriptionID))
		if err != nil {
			log.Warnw("Failed to get subscription", "credit_grant_id", creditGrant.ID, "error", err)
			creditGrantsSkipped++
			continue
		}

		if isDryRun {
			creditGrantsUpdated++
			continue
		}

		_, err = db.ExecContext(subCtx, query,
			sub.StartDate,
			sub.EndDate,
			sub.StartDate,
			creditGrant.ID,
			creditGrant.TenantID,
			creditGrant.EnvironmentID,
		)

		if err != nil {
			log.Errorw("Failed to update credit grant", "credit_grant_id", creditGrant.ID, "error", err)
			creditGrantsSkipped++
			continue
		}

		creditGrantsUpdated++
	}

	log.Infow("Credit grants migration complete", "updated", creditGrantsUpdated, "skipped", creditGrantsSkipped)

	// Step 2: Migrate CGAs
	log.Infow("Migrating CGAs")

	// Query directly using ent client to bypass tenant/environment filtering
	allCGAs, err := entClient.Reader.CreditGrantApplication.Query().
		Where(
			cga.ApplicationStatusIn(
				types.ApplicationStatusPending,
				types.ApplicationStatusFailed,
			),
			cga.ApplicationReasonIn(
				types.ApplicationReasonRecurringCreditGrant,
				types.ApplicationReasonFirstTimeRecurringCreditGrant,
			),
		).
		All(baseCtx)

	if err != nil {
		return fmt.Errorf("failed to query CGAs: %w", err)
	}

	log.Infow("Found CGAs", "count", len(allCGAs))

	for _, cgaEnt := range allCGAs {
		cga := creditgrantapplication.FromEnt(cgaEnt)

		subCtx := context.WithValue(baseCtx, types.CtxTenantID, cga.TenantID)
		subCtx = context.WithValue(subCtx, types.CtxEnvironmentID, cga.EnvironmentID)

		// Get credit grant to use its period config
		grant, err := creditGrantRepo.Get(subCtx, cga.CreditGrantID)
		if err != nil {
			log.Warnw("Failed to get credit grant", "cga_id", cga.ID, "credit_grant_id", cga.CreditGrantID, "error", err)
			cgasSkipped++
			continue
		}

		periodStart := cga.ScheduledFor
		// Use credit grant's period config instead of subscription's billing period
		_, periodEnd, err := service.CalculateNextCreditGrantPeriod(lo.FromPtr(grant), periodStart)
		if err != nil {
			log.Warnw("Failed to calculate period_end", "cga_id", cga.ID, "error", err)
			cgasSkipped++
			continue
		}

		cga.PeriodStart = &periodStart
		cga.PeriodEnd = &periodEnd

		if isDryRun {
			cgasUpdated++
			continue
		}

		err = cgaRepo.Update(subCtx, cga)
		if err != nil {
			log.Errorw("Failed to update CGA", "cga_id", cga.ID, "error", err)
			cgasSkipped++
			continue
		}

		cgasUpdated++
	}

	log.Infow("CGAs migration complete", "updated", cgasUpdated, "skipped", cgasSkipped)

	// Summary
	fmt.Printf("Credit Grants: %d updated, %d skipped\n", creditGrantsUpdated, creditGrantsSkipped)
	fmt.Printf("CGAs: %d updated, %d skipped\n", cgasUpdated, cgasSkipped)

	return nil
}
