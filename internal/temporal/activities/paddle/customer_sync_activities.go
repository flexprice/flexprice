package paddle

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

func (a *CustomerSyncActivities) SyncCustomerToPaddle(ctx context.Context, input models.PaddleCustomerSyncWorkflowInput) error {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	paddleIntegration, err := a.integrationFactory.GetPaddleIntegration(ctx)
	if err != nil {
		return err
	}

	_, err = paddleIntegration.CustomerSvc.EnsureCustomerSyncedToPaddle(ctx, input.CustomerID, a.customerService)
	return err
}
