package activities

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
)

const ActivityPrefix = "PlanActivities"

// PlanActivities contains all plan-related activities
// When registered with Temporal, methods will be called as "PlanActivities.SyncPlanPrices"
type PlanActivities struct {
	planService service.PlanService
}

// NewPlanActivities creates a new PlanActivities instance
func NewPlanActivities(planService service.PlanService) *PlanActivities {
	return &PlanActivities{
		planService: planService,
	}
}

// SyncPlanPricesInput represents the input for the SyncPlanPrices activity
type SyncPlanPricesInput struct {
	PlanID        string `json:"plan_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
}

// SyncPlanPrices syncs plan prices
// This method will be registered as "SyncPlanPrices" in Temporal
func (a *PlanActivities) SyncPlanPrices(ctx context.Context, input SyncPlanPricesInput) (*dto.SyncPlanPricesResponse, error) {

	// Validate input parameters
	if input.PlanID == "" {
		return nil, ierr.NewError("plan ID is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation)
	}

	if input.TenantID == "" || input.EnvironmentID == "" {
		return nil, ierr.NewError("tenant ID and environment ID are required").
			WithHint("Tenant ID and environment ID are required").
			Mark(ierr.ErrValidation)
	}

	ctx = context.WithValue(ctx, types.CtxTenantID, input.TenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, input.EnvironmentID)

	result, err := a.planService.SyncPlanPrices(ctx, input.PlanID)
	if err != nil {
		return nil, err
	}

	return result, nil
}
