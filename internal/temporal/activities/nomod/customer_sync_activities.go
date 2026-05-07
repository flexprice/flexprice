package nomod

import (
	"context"

	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

type CustomerSyncActivities struct {
	integrationFactory *integration.Factory
	customerService    interfaces.CustomerService
	logger             *logger.Logger
}

func NewCustomerSyncActivities(
	integrationFactory *integration.Factory,
	customerService interfaces.CustomerService,
	logger *logger.Logger,
) *CustomerSyncActivities {
	return &CustomerSyncActivities{
		integrationFactory: integrationFactory,
		customerService:    customerService,
		logger:             logger,
	}
}

func (a *CustomerSyncActivities) SyncCustomerToNomod(ctx context.Context, input models.NomodCustomerSyncWorkflowInput) error {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	nomodIntegration, err := a.integrationFactory.GetNomodIntegration(ctx)
	if err != nil {
		return err
	}

	_, err = nomodIntegration.CustomerSvc.EnsureCustomerSyncedToNomod(ctx, input.CustomerID, a.customerService)
	return err
}
