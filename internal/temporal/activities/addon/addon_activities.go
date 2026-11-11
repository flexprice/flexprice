package activities

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

const ActivityPrefix = "AddonActivities"

// AddonActivities hosts addon-related temporal activities.
// Registered as "AddonActivities.SyncAddonPrices" → called as "SyncAddonPrices".
type AddonActivities struct {
	addonService service.AddonService
	logger       *logger.Logger
}

func NewAddonActivities(addonService service.AddonService, logger *logger.Logger) *AddonActivities {
	return &AddonActivities{
		addonService: addonService,
		logger:       logger,
	}
}

// SyncAddonPricesInput is the input for the SyncAddonPrices activity.
type SyncAddonPricesInput struct {
	AddonID       string `json:"addon_id"`
	TenantID      string `json:"tenant_id"`
	UserID        string `json:"user_id"`
	EnvironmentID string `json:"environment_id"`
}

// SyncAddonPrices runs the addon usage-price sync and releases the addon-level
// Redis lock when done (mirrors the plan price sync activity's lock handling).
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

	result, err := a.addonService.SyncAddonPrices(ctx, input.AddonID)
	if err != nil {
		a.logger.Error(ctx, "SyncAddonPrices activity failed",
			"error", err,
			"addon_id", input.AddonID,
			"tenant_id", input.TenantID,
			"environment_id", input.EnvironmentID,
		)
		return nil, err
	}

	return result, nil
}
