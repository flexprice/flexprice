package chargebee

import (
	"context"

	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

type CustomerSyncActivities struct {
	integrationFactory *integration.Factory
	logger             *logger.Logger
}

func NewCustomerSyncActivities(
	integrationFactory *integration.Factory,
	logger *logger.Logger,
) *CustomerSyncActivities {
	return &CustomerSyncActivities{
		integrationFactory: integrationFactory,
		logger:             logger,
	}
}

func (a *CustomerSyncActivities) SyncCustomerToChargebee(ctx context.Context, input models.ChargebeeCustomerSyncWorkflowInput) error {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	chargebeeIntegration, err := a.integrationFactory.GetChargebeeIntegration(ctx)
	if err != nil {
		return err
	}

	_, err = chargebeeIntegration.CustomerSvc.EnsureCustomerSyncedToChargebee(ctx, input.CustomerID)
	return err
}
