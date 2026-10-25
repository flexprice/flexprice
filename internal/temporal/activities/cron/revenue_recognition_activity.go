package cron

import (
	"context"

	journalsvc "github.com/flexprice/flexprice/internal/ee/service/journal"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/activity"
)

// RevenueRecognitionActivities wraps the daily revenue recognition run.
type RevenueRecognitionActivities struct {
	journalService journalsvc.JournalService
	logger         *logger.Logger
}

func NewRevenueRecognitionActivities(journalService journalsvc.JournalService, log *logger.Logger) *RevenueRecognitionActivities {
	return &RevenueRecognitionActivities{journalService: journalService, logger: log}
}

// RecognizeRevenueActivity runs recognition and heartbeats its cursor, so a retry resumes after the last finished contract.
func (a *RevenueRecognitionActivities) RecognizeRevenueActivity(ctx context.Context, in cronModels.RecognizeRevenueActivityInput) error {
	err := a.journalService.RunRecognition(ctx, journalsvc.RecognitionRunRequest{
		Since:      in.Since,
		AsOf:       in.AsOf,
		Resume:     heartbeatCursor(ctx),
		OnProgress: func(c journalsvc.RecognitionCursor) { activity.RecordHeartbeat(ctx, c) },
	})
	if err != nil {
		a.logger.Error(ctx, "revenue recognition run failed", "error", err, "since", in.Since, "as_of", in.AsOf)
	}
	return err
}

// BackfillRevenueActivity rebuilds one environment from history, heartbeating its cursor like the daily run.
func (a *RevenueRecognitionActivities) BackfillRevenueActivity(ctx context.Context, in cronModels.BackfillRevenueActivityInput) error {
	ctx = types.SetEnvironmentID(types.SetTenantID(ctx, in.TenantID), in.EnvironmentID)

	err := a.journalService.Backfill(ctx, journalsvc.BackfillRequest{
		AsOf:       in.AsOf,
		Resume:     heartbeatCursor(ctx),
		OnProgress: func(c journalsvc.RecognitionCursor) { activity.RecordHeartbeat(ctx, c) },
	})
	if err != nil {
		a.logger.Error(ctx, "revenue recognition backfill failed", "error", err, "tenant_id", in.TenantID, "environment_id", in.EnvironmentID)
	}
	return err
}

// heartbeatCursor is the cursor a previous attempt recorded, nil on a first attempt.
func heartbeatCursor(ctx context.Context) *journalsvc.RecognitionCursor {
	var recorded journalsvc.RecognitionCursor
	if !activity.HasHeartbeatDetails(ctx) || activity.GetHeartbeatDetails(ctx, &recorded) != nil {
		return nil
	}
	return &recorded
}
