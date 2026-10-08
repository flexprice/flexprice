package cron

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/activity"
)

// RevenueRollupActivities wraps the periodic revenue_facts dirty-rollup entrypoint.
type RevenueRollupActivities struct {
	revenueService interfaces.RevenueService
	cfg            *config.Configuration
	logger         *logger.Logger
}

func NewRevenueRollupActivities(
	revenueService interfaces.RevenueService,
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
func (a *RevenueRollupActivities) RollupDirtyActivity(ctx context.Context, in cronModels.RollupDirtyActivityInput) (*cronModels.RevenueRollupWorkflowResult, error) {
	since := in.Since
	log := activity.GetLogger(ctx)

	if a.cfg == nil || !a.cfg.Analytics.RevenueRollup.Enabled {
		log.Info("Revenue rollup disabled by config, skipping")
		return &cronModels.RevenueRollupWorkflowResult{}, nil
	}

	// Resume where a previous attempt stopped. Without this every retry starts
	// at the first subscription and rewrites the same head of the list, so the
	// tail is never reached.
	var cursor *types.RollupCursor
	// An explicit cursor (a manual resume) wins: the previous run's attempts
	// are spent, so its heartbeat details are gone with it.
	if in.ResumeAfterSubscriptionID != "" && in.ResumeEnvironmentID != "" {
		cursor = &types.RollupCursor{
			EnvironmentID:      in.ResumeEnvironmentID,
			LastSubscriptionID: in.ResumeAfterSubscriptionID,
		}
		log.Info("Resuming revenue rollup dirty scan from an explicit cursor",
			"environment_id", cursor.EnvironmentID,
			"after_subscription_id", cursor.LastSubscriptionID)
	}
	if cursor == nil && activity.HasHeartbeatDetails(ctx) {
		var recorded types.RollupCursor
		if err := activity.GetHeartbeatDetails(ctx, &recorded); err == nil {
			cursor = &recorded
			log.Info("Resuming revenue rollup dirty scan",
				"environment_id", recorded.EnvironmentID,
				"after_subscription_id", recorded.LastSubscriptionID)
		}
	}

	log.Info("Starting revenue rollup dirty scan", "since", since)

	// A scheduled full rebuild repairs anything the incremental triggers miss,
	// so a gap lasts at most a week instead of persisting.
	forceFull := a.cfg.Analytics.RevenueRollup.FullRebuildWeekday == int(since.UTC().Weekday())
	if forceFull {
		log.Info("Revenue rollup running a full rebuild", "weekday", since.UTC().Weekday().String())
	}

	result, err := a.revenueService.RollupDirty(ctx, types.RollupDirtyRequest{
		Since:     since,
		Cursor:    cursor,
		ForceFull: forceFull,
		OnProgress: func(c types.RollupCursor) {
			activity.RecordHeartbeat(ctx, c)
		},
	})
	if err != nil {
		a.logger.Error(ctx, "revenue rollup dirty scan failed", "error", err, "since", since)
		return nil, err
	}

	a.logger.Info(ctx, "revenue rollup dirty scan completed", "rolled", result.Rolled, "skipped", result.Skipped, "since", since)
	log.Info("Completed revenue rollup dirty scan", "rolled", result.Rolled, "skipped", result.Skipped)
	return &cronModels.RevenueRollupWorkflowResult{Rolled: result.Rolled, Skipped: result.Skipped}, nil
}

// ReconcileBookedInvoicesActivity compares recently finalized/voided invoices against their
// booked revenue facts. Same kill switch as the rollup.
func (a *RevenueRollupActivities) ReconcileBookedInvoicesActivity(ctx context.Context, since time.Time) (*cronModels.RevenueSweepResult, error) {
	log := activity.GetLogger(ctx)

	if a.cfg == nil || !a.cfg.Analytics.RevenueRollup.Enabled {
		log.Info("Revenue drift sweep disabled by config, skipping")
		return &cronModels.RevenueSweepResult{}, nil
	}

	log.Info("Starting revenue drift sweep", "since", since)

	checked, drifted, corrected, err := a.revenueService.ReconcileBookedInvoices(ctx, since)
	if err != nil {
		a.logger.Error(ctx, "revenue drift sweep failed", "error", err, "since", since)
		return nil, err
	}

	a.logger.Info(ctx, "revenue drift sweep completed",
		"checked", checked, "drifted", drifted, "corrected", corrected, "since", since)
	log.Info("Completed revenue drift sweep", "checked", checked, "drifted", drifted, "corrected", corrected)
	return &cronModels.RevenueSweepResult{Checked: checked, Drifted: drifted, Corrected: corrected}, nil
}
