package activities

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
)

const ActivityPrefix = "QuickBooksPriceSyncActivities"

// QuickBooksPriceSyncActivities contains all QuickBooks price sync activities
type QuickBooksPriceSyncActivities struct {
	integrationFactory *integration.Factory
	planRepo           plan.Repository
	priceRepo          price.Repository
	logger             *logger.Logger
}

// NewQuickBooksPriceSyncActivities creates a new QuickBooksPriceSyncActivities instance
func NewQuickBooksPriceSyncActivities(
	integrationFactory *integration.Factory,
	planRepo plan.Repository,
	priceRepo price.Repository,
	logger *logger.Logger,
) *QuickBooksPriceSyncActivities {
	return &QuickBooksPriceSyncActivities{
		integrationFactory: integrationFactory,
		planRepo:           planRepo,
		priceRepo:          priceRepo,
		logger:             logger,
	}
}

// SyncPriceToQuickBooksInput represents the input for the SyncPriceToQuickBooks activity
type SyncPriceToQuickBooksInput struct {
	PriceID       string `json:"price_id"`
	PlanID        string `json:"plan_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	UserID        string `json:"user_id"`
}

// SyncPriceToQuickBooksOutput represents the output of the sync activity
type SyncPriceToQuickBooksOutput struct {
	Success          bool   `json:"success"`
	QuickBooksItemID string `json:"quickbooks_item_id,omitempty"`
}

// SyncPriceToQuickBooks syncs a single price to QuickBooks
// This method will be registered as "SyncPriceToQuickBooks" in Temporal
func (a *QuickBooksPriceSyncActivities) SyncPriceToQuickBooks(ctx context.Context, input SyncPriceToQuickBooksInput) (*SyncPriceToQuickBooksOutput, error) {
	// Validate input parameters
	if input.PriceID == "" || input.PlanID == "" {
		return nil, ierr.NewError("price ID and plan ID are required").
			WithHint("Price ID and Plan ID are required").
			Mark(ierr.ErrValidation)
	}

	if input.TenantID == "" || input.EnvironmentID == "" {
		return nil, ierr.NewError("tenant ID and environment ID are required").
			WithHint("Tenant ID and Environment ID are required").
			Mark(ierr.ErrValidation)
	}

	// Set context values for database queries
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	// Get QuickBooks integration
	quickbooksIntegration, err := a.integrationFactory.GetQuickBooksIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			a.logger.Debugw("QuickBooks connection not configured",
				"price_id", input.PriceID)
			// Return NON-RETRYABLE error - connection doesn't exist, retrying won't help
			return nil, temporal.NewNonRetryableApplicationError(
				"QuickBooks connection not configured",
				"ConnectionNotFound",
				ierr.NewError("QuickBooks connection not configured").
					WithHint("QuickBooks connection must be configured before syncing prices").
					WithReportableDetails(map[string]interface{}{
						"price_id": input.PriceID,
						"plan_id":  input.PlanID,
					}).
					Mark(ierr.ErrNotFound),
			)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get QuickBooks integration").
			Mark(ierr.ErrInternal)
	}

	// Get plan
	plan, err := a.planRepo.Get(ctx, input.PlanID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get plan").
			WithReportableDetails(map[string]interface{}{
				"plan_id": input.PlanID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Get price
	priceModel, err := a.priceRepo.Get(ctx, input.PriceID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get price").
			WithReportableDetails(map[string]interface{}{
				"price_id": input.PriceID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Sync to QuickBooks
	if err := quickbooksIntegration.ItemSyncSvc.SyncPriceToQuickBooks(ctx, plan, priceModel); err != nil {
		a.logger.Errorw("failed to sync price to QuickBooks",
			"price_id", input.PriceID,
			"plan_id", input.PlanID,
			"error", err)
		// Return the actual error so Temporal marks the workflow as FAILED
		return nil, ierr.WithError(err).
			WithHint("Failed to sync price to QuickBooks").
			WithReportableDetails(map[string]interface{}{
				"price_id": input.PriceID,
				"plan_id":  input.PlanID,
			}).
			Mark(ierr.ErrInternal)
	}

	a.logger.Infow("price synced to QuickBooks successfully",
		"price_id", input.PriceID,
		"plan_id", input.PlanID)

	return &SyncPriceToQuickBooksOutput{
		Success: true,
	}, nil
}
