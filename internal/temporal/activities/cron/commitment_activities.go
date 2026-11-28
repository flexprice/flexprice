package cron

import (
	"context"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
)

// CommitmentCronActivities wraps commitment cron jobs.
type CommitmentCronActivities struct {
	commitmentService service.CommitmentService
	logger            *logger.Logger
}

// NewCommitmentCronActivities builds activities for commitment cron workflows.
func NewCommitmentCronActivities(commitmentService service.CommitmentService, log *logger.Logger) *CommitmentCronActivities {
	return &CommitmentCronActivities{
		commitmentService: commitmentService,
		logger:            log,
	}
}

// BillDueInstalmentsActivity bills every due separate-invoice instalment.
func (a *CommitmentCronActivities) BillDueInstalmentsActivity(ctx context.Context) error {
	if err := a.commitmentService.BillDueInstalments(ctx); err != nil {
		a.logger.Error(ctx, "BillDueInstalmentsActivity failed", "error", err)
		return err
	}

	return nil
}
