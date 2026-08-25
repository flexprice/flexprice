package activities

import (
	"context"

	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

type QuickBooksCustomerSyncActivities struct {
	integrationFactory *integration.Factory
	customerService    interfaces.CustomerService
	logger             *logger.Logger
}

func NewQuickBooksCustomerSyncActivities(
	integrationFactory *integration.Factory,
	customerService interfaces.CustomerService,
	logger *logger.Logger,
) *QuickBooksCustomerSyncActivities {
	return &QuickBooksCustomerSyncActivities{
		integrationFactory: integrationFactory,
		customerService:    customerService,
		logger:             logger,
	}
}

func (a *QuickBooksCustomerSyncActivities) SyncCustomerToQuickBooks(ctx context.Context, input models.QuickBooksCustomerSyncWorkflowInput) error {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	qbIntegration, err := a.integrationFactory.GetQuickBooksIntegration(ctx)
	if err != nil {
		a.logger.Error(ctx, "SyncCustomerToQuickBooks activity failed to get QuickBooks integration",
			"error", err,
			"customer_id", input.CustomerID,
			"tenant_id", input.TenantID,
			"environment_id", input.EnvironmentID,
		)
		return err
	}

	custResp, err := a.customerService.GetCustomer(ctx, input.CustomerID)
	if err != nil {
		a.logger.Error(ctx, "SyncCustomerToQuickBooks activity failed to get customer",
			"error", err,
			"customer_id", input.CustomerID,
			"tenant_id", input.TenantID,
			"environment_id", input.EnvironmentID,
		)
		return err
	}

	if _, err := qbIntegration.CustomerSvc.GetOrCreateQuickBooksCustomer(ctx, custResp.Customer); err != nil {
		a.logger.Error(ctx, "SyncCustomerToQuickBooks activity failed",
			"error", err,
			"customer_id", input.CustomerID,
			"tenant_id", input.TenantID,
			"environment_id", input.EnvironmentID,
		)
		return err
	}
	return nil
}
