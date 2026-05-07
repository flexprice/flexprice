package paddle

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
)

type CustomerSyncActivities struct {
	integrationFactory *integration.Factory
	customerService    interfaces.CustomerService
	invoiceRepo        invoice.Repository
	logger             *logger.Logger
}

func NewCustomerSyncActivities(
	integrationFactory *integration.Factory,
	customerService interfaces.CustomerService,
	invoiceRepo invoice.Repository,
	logger *logger.Logger,
) *CustomerSyncActivities {
	return &CustomerSyncActivities{
		integrationFactory: integrationFactory,
		customerService:    customerService,
		invoiceRepo:        invoiceRepo,
		logger:             logger,
	}
}

// SyncCustomerToPaddle is called from PaddleCustomerSyncWorkflow (triggered on customer creation).
// Errors are not wrapped as NonRetryableApplicationError; Temporal will retry transient failures.
func (a *CustomerSyncActivities) SyncCustomerToPaddle(ctx context.Context, input models.PaddleCustomerSyncWorkflowInput) error {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	paddleIntegration, err := a.integrationFactory.GetPaddleIntegration(ctx)
	if err != nil {
		// Let Temporal retry transient integration lookup failures.
		return err
	}

	_, err = paddleIntegration.CustomerSvc.EnsureCustomerSyncedToPaddle(ctx, input.CustomerID, a.customerService)
	return err
}

// EnsureCustomerSyncedToPaddle is called from PaddleInvoiceSyncWorkflow as an explicit pre-check
// step before invoice sync. It ensures the customer exists in Paddle (creating them if needed).
//
// Validation errors (e.g. missing email, missing address country) are returned as
// NonRetryableApplicationError so the workflow fails immediately with a clear message rather than
// burning through retry attempts on a problem that retrying cannot fix.
func (a *CustomerSyncActivities) EnsureCustomerSyncedToPaddle(ctx context.Context, input models.PaddleCustomerSyncWorkflowInput) error {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	customerID := input.CustomerID
	if customerID == "" && input.InvoiceID != "" {
		if a.invoiceRepo == nil {
			a.logger.Errorw("invoice repository not configured for Paddle customer pre-check",
				"invoice_id", input.InvoiceID)
			err := ierr.NewError("customer ID or invoice-backed resolution is unavailable").
				Mark(ierr.ErrInternal)
			return temporal.NewNonRetryableApplicationError(
				err.Error(),
				"CustomerValidationError",
				err,
			)
		}
		inv, err := a.invoiceRepo.Get(ctx, input.InvoiceID)
		if err != nil {
			a.logger.Errorw("failed to load invoice for Paddle customer pre-check",
				"error", err,
				"invoice_id", input.InvoiceID)
			return err
		}
		customerID = inv.CustomerID
		if customerID == "" {
			a.logger.Warnw("invoice has no customer_id, cannot sync to Paddle",
				"invoice_id", input.InvoiceID)
			err := ierr.NewError("invoice has no customer id").
				WithHint("Link the invoice to a customer before Paddle sync").
				Mark(ierr.ErrValidation)
			return temporal.NewNonRetryableApplicationError(
				err.Error(),
				"CustomerValidationError",
				err,
			)
		}
	}
	if customerID == "" {
		err := ierr.NewError("customer ID is required").
			Mark(ierr.ErrValidation)
		return temporal.NewNonRetryableApplicationError(
			err.Error(),
			"CustomerValidationError",
			err,
		)
	}

	a.logger.Infow("ensuring customer synced to Paddle before invoice sync",
		"customer_id", customerID,
		"invoice_id", input.InvoiceID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	paddleIntegration, err := a.integrationFactory.GetPaddleIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			a.logger.Warnw("Paddle connection not configured, skipping customer pre-check",
				"customer_id", customerID)
			return temporal.NewNonRetryableApplicationError(
				"Paddle connection not configured",
				"ConnectionNotFound",
				err,
			)
		}
		a.logger.Errorw("failed to get Paddle integration for customer pre-check",
			"error", err,
			"customer_id", customerID)
		return err
	}

	_, err = paddleIntegration.CustomerSvc.EnsureCustomerSyncedToPaddle(ctx, customerID, a.customerService)
	if err != nil {
		if ierr.IsValidation(err) {
			a.logger.Warnw("customer cannot be synced to Paddle: validation error (non-retryable)",
				"customer_id", customerID,
				"error", err)
			return temporal.NewNonRetryableApplicationError(
				err.Error(),
				"CustomerValidationError",
				err,
			)
		}
		a.logger.Errorw("failed to ensure customer synced to Paddle",
			"error", err,
			"customer_id", customerID)
		return err
	}

	a.logger.Infow("customer successfully synced to Paddle",
		"customer_id", customerID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)
	return nil
}
