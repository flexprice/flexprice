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

	// A QuickBooks customer's currency is immutable once it has transactions, and a customer
	// event carries no currency. Creating one here would lock it to the company's home currency
	// and silently reinterpret every later invoice, so creation is deferred to the first
	// invoice sync, which knows the currency. Matches the Zoho contact lifecycle.
	if input.Currency == "" {
		a.logger.Info(ctx, "SyncCustomerToQuickBooks deferred: no currency known, customer will be created on first invoice sync",
			"customer_id", input.CustomerID,
			"tenant_id", input.TenantID,
			"environment_id", input.EnvironmentID,
		)
		return nil
	}

	if _, err := qbIntegration.CustomerSvc.GetOrCreateQuickBooksCustomer(ctx, custResp.Customer, input.Currency); err != nil {
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
