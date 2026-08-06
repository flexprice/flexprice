package activities

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/ee/service"
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
	log := logger.NewNoopLogger()
	defer func() {
		redisCache := cache.GetRedisCache()
		if redisCache == nil {
			log.Info(context.Background(), "price_sync_lock_release_skipped", "plan_id", input.PlanID, "lock_key", lockKey, "reason", "redis_cache_nil")
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		redisCache.Delete(releaseCtx, lockKey)
		log.Info(ctx, "price_sync_lock_released", "plan_id", input.PlanID, "lock_key", lockKey)
	}()

	result, err := a.planService.SyncPlanPrices(ctx, input.PlanID)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// SyncPlanPricesV2 is the V2 (sequence-driven) plan-price sync activity. It
// reuses the same input shape, lock-release semantics, and response shape as
// SyncPlanPrices so the workflow code stays minimal — the algorithmic
// difference lives in planService.SyncPlanPricesV2.
func (a *PlanActivities) SyncPlanPricesV2(ctx context.Context, input SyncPlanPricesInput) (*dto.SyncPlanPricesResponse, error) {
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
	log := logger.NewNoopLogger()
	defer func() {
		redisCache := cache.GetRedisCache()
		if redisCache == nil {
			log.Info(context.Background(), "price_sync_lock_release_skipped", "plan_id", input.PlanID, "lock_key", lockKey, "reason", "redis_cache_nil")
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		redisCache.Delete(releaseCtx, lockKey)
		log.Info(ctx, "price_sync_lock_released", "plan_id", input.PlanID, "lock_key", lockKey, "version", "v2")
	}()

	return a.planService.SyncPlanPricesV2(ctx, input.PlanID)
}

