package activities

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/planpricesync"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	eventsModels "github.com/flexprice/flexprice/internal/temporal/models/events"
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
	UserID        string `json:"user_id"`
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

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	lockKey := cache.PrefixPriceSyncLock + input.PlanID
	defer func() {
		if redisCache := cache.GetRedisCache(); redisCache != nil {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			redisCache.Delete(releaseCtx, lockKey)
		}
	}()

	result, err := a.planService.SyncPlanPrices(ctx, input.PlanID)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ReprocessEventsForPlanInput is the activity input (same shape as workflow input).
type ReprocessEventsForPlanInput = eventsModels.ReprocessEventsForPlanWorkflowInput

const ActivityReprocessEventsForPlan = "ReprocessEventsForPlan"

// ReprocessEventsForPlan triggers event reprocessing for the given missing pairs (grouped by price, then per customer).
func (a *PlanActivities) ReprocessEventsForPlan(ctx context.Context, input ReprocessEventsForPlanInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	if len(input.MissingPairs) == 0 {
		return nil
	}

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	pairs := make([]planpricesync.PlanLineItemCreationDelta, len(input.MissingPairs))
	for i, p := range input.MissingPairs {
		pairs[i] = planpricesync.PlanLineItemCreationDelta{
			SubscriptionID: p.SubscriptionID,
			PriceID:        p.PriceID,
			CustomerID:     p.CustomerID,
		}
	}

	return a.planService.ReprocessEventsForMissingPairs(ctx, pairs)
}
