package cron

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/activity"
)

// RevenueRollupActivities wraps the periodic revenue_facts dirty-rollup entrypoint.
type RevenueRollupActivities struct {
	revenueService service.RevenueService
	cfg            *config.Configuration
	logger         *logger.Logger
}

func NewRevenueRollupActivities(
	revenueService service.RevenueService,
	cfg *config.Configuration,
	log *logger.Logger,
) *RevenueRollupActivities {
	return &RevenueRollupActivities{
		revenueService: revenueService,
		cfg:            cfg,
		logger:         log,
	}
}

// RollupDirtyActivity re-upserts revenue_facts rows for every subscription with
// activity since `since`. The schedule always fires; this early exit is the
// deployment-wide kill switch, and the tenant-level settings flag gates the
// actual scan.
func (a *RevenueRollupActivities) RollupDirtyActivity(ctx context.Context, since time.Time) (*cronModels.RevenueRollupWorkflowResult, error) {
	log := activity.GetLogger(ctx)

	if a.cfg == nil || !a.cfg.Analytics.RevenueRollup.Enabled {
		log.Info("Revenue rollup disabled by config, skipping")
		return &cronModels.RevenueRollupWorkflowResult{}, nil
	}

	log.Info("Starting revenue rollup dirty scan", "since", since)

	rolled, skipped, err := a.revenueService.RollupDirty(ctx, since)
	if err != nil {
		a.logger.Error(ctx, "revenue rollup dirty scan failed", "error", err, "since", since)
		return nil, err
	}

	a.logger.Info(ctx, "revenue rollup dirty scan completed", "rolled", rolled, "skipped", skipped, "since", since)
	log.Info("Completed revenue rollup dirty scan", "rolled", rolled, "skipped", skipped)
	return &cronModels.RevenueRollupWorkflowResult{Rolled: rolled, Skipped: skipped}, nil
}

// SweepDriftActivity compares recently finalized/voided invoices against their
// booked revenue facts. Same kill switch as the rollup.
func (a *RevenueRollupActivities) SweepDriftActivity(ctx context.Context, since time.Time) (*cronModels.RevenueSweepResult, error) {
	log := activity.GetLogger(ctx)

	if a.cfg == nil || !a.cfg.Analytics.RevenueRollup.Enabled {
		log.Info("Revenue drift sweep disabled by config, skipping")
		return &cronModels.RevenueSweepResult{}, nil
	}

	log.Info("Starting revenue drift sweep", "since", since)

	checked, drifted, corrected, err := a.revenueService.SweepDrift(ctx, since)
	if err != nil {
		a.logger.Error(ctx, "revenue drift sweep failed", "error", err, "since", since)
		return nil, err
	}

	a.logger.Info(ctx, "revenue drift sweep completed",
		"checked", checked, "drifted", drifted, "corrected", corrected, "since", since)
	log.Info("Completed revenue drift sweep", "checked", checked, "drifted", drifted, "corrected", corrected)
	return &cronModels.RevenueSweepResult{Checked: checked, Drifted: drifted, Corrected: corrected}, nil
}
