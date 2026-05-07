package cron

import (
	"context"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/service"
	"go.temporal.io/sdk/activity"
)

// CreditGrantActivities wraps credit-grant cron logic as Temporal activities.
type CreditGrantActivities struct {
	creditGrantService service.CreditGrantService
}

// NewCreditGrantActivities builds credit-grant cron activities.
func NewCreditGrantActivities(creditGrantService service.CreditGrantService) *CreditGrantActivities {
	return &CreditGrantActivities{creditGrantService: creditGrantService}
}

// ProcessScheduledCreditGrantApplicationsActivity processes all scheduled credit grant applications.
func (a *CreditGrantActivities) ProcessScheduledCreditGrantApplicationsActivity(ctx context.Context) (*cronModels.CreditGrantProcessingWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Processing scheduled credit grant applications")

	resp, err := a.creditGrantService.ProcessScheduledCreditGrantApplications(ctx)
	if err != nil {
		return nil, err
	}

	result := &cronModels.CreditGrantProcessingWorkflowResult{
		Processed: resp.TotalApplicationsCount,
		Succeeded: resp.SuccessApplicationsCount,
		Failed:    resp.FailedApplicationsCount,
	}

	log.Info("Completed credit grant processing",
		"processed", result.Processed,
		"succeeded", result.Succeeded,
		"failed", result.Failed,
	)
	return result, nil
}
