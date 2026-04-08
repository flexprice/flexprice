package activities

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
)

// AddonActivities contains all addon-related Temporal activities.
type AddonActivities struct {
	addonService service.AddonService
}

// NewAddonActivities creates a new AddonActivities instance.
func NewAddonActivities(addonService service.AddonService) *AddonActivities {
	return &AddonActivities{addonService: addonService}
}

// SyncAddonPricesInput is the input for the SyncAddonPrices activity.
type SyncAddonPricesInput struct {
	AddonID       string                `json:"addon_id"`
	EntityType    types.PriceEntityType `json:"entity_type"` // always PRICE_ENTITY_TYPE_ADDON
	TenantID      string                `json:"tenant_id"`
	EnvironmentID string                `json:"environment_id"`
	UserID        string                `json:"user_id"`
}

// ActivitySyncAddonPrices is the registered Temporal activity name.
const ActivitySyncAddonPrices = "SyncAddonPrices"

// SyncAddonPrices syncs addon prices to subscription line items.
// Registered as "SyncAddonPrices" in Temporal.
func (a *AddonActivities) SyncAddonPrices(ctx context.Context, input SyncAddonPricesInput) (*dto.SyncAddonPricesResponse, error) {
	if input.AddonID == "" {
		return nil, ierr.NewError("addon ID is required").
			WithHint("Addon ID is required").
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

	lockKey := cache.PrefixAddonPriceSyncLock + input.AddonID
	log := logger.GetLogger()
	defer func() {
		redisCache := cache.GetRedisCache()
		if redisCache == nil {
			log.Warnw("addon_price_sync_lock_release_skipped",
				"addon_id", input.AddonID, "lock_key", lockKey, "reason", "redis_cache_nil")
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		redisCache.Delete(releaseCtx, lockKey)
		log.Infow("addon_price_sync_lock_released", "addon_id", input.AddonID, "lock_key", lockKey)
	}()

	return a.addonService.SyncAddonPrices(ctx, input.AddonID)
}
