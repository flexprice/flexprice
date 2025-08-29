package activities

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/service"
	"go.temporal.io/sdk/activity"
)

const ActivityPrefix = "PlanActivities"

// PlanActivities contains all plan-related activities
type PlanActivities struct {
	planService service.PlanService
}

// NewPlanActivities creates a new PlanActivities instance
func NewPlanActivities(planService service.PlanService) *PlanActivities {
	return &PlanActivities{
		planService: planService,
	}
}

// SyncPlanPrices syncs plan prices
func (a *PlanActivities) SyncPlanPrices(ctx context.Context, planID string) (*dto.SyncPlanPricesResponse, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Starting SyncPlanPrices activity", "planID", planID)

	result, err := a.planService.SyncPlanPrices(ctx, planID)
	if err != nil {
		logger.Error("Failed to sync plan prices", "error", err)
		return nil, err
	}

	logger.Info("Completed SyncPlanPrices activity")
	return result, nil
}
