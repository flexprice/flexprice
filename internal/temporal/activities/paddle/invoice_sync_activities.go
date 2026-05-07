package paddle

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/integration/paddle"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
)

// InvoiceSyncActivities handles Paddle invoice sync activities
type InvoiceSyncActivities struct {
	integrationFactory *integration.Factory
	customerService    interfaces.CustomerService
	logger             *logger.Logger
}

// NewInvoiceSyncActivities creates a new Paddle invoice sync activities handler
func NewInvoiceSyncActivities(
	integrationFactory *integration.Factory,
	customerService interfaces.CustomerService,
	logger *logger.Logger,
) *InvoiceSyncActivities {
	return &InvoiceSyncActivities{
		integrationFactory: integrationFactory,
		customerService:    customerService,
		logger:             logger,
	}
}

// SyncInvoiceToPaddle syncs an invoice to Paddle.
//
// Validation errors (e.g. missing Paddle address) are returned as NonRetryableApplicationError
// because retrying cannot fix a data-quality problem — the operator must update the customer
// first and then re-trigger the workflow.
func (a *InvoiceSyncActivities) SyncInvoiceToPaddle(
	ctx context.Context,
	input models.PaddleInvoiceSyncWorkflowInput,
) error {
	a.logger.Infow("syncing invoice to Paddle",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	// Set context values for tenant and environment
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	// Get Paddle integration with runtime context
	paddleIntegration, err := a.integrationFactory.GetPaddleIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			a.logger.Warnw("Paddle connection not configured",
				"invoice_id", input.InvoiceID,
				"customer_id", input.CustomerID)
			return temporal.NewNonRetryableApplicationError(
				"Paddle connection not configured",
				"ConnectionNotFound",
				err,
			)
		}
		a.logger.Errorw("failed to get Paddle integration",
			"error", err,
			"invoice_id", input.InvoiceID,
			"customer_id", input.CustomerID)
		return err
	}

	// Sync the invoice to Paddle.
	syncReq := paddle.PaddleInvoiceSyncRequest{
		InvoiceID: input.InvoiceID,
	}

	_, err = paddleIntegration.InvoiceSyncSvc.SyncInvoiceToPaddle(ctx, syncReq, a.customerService)
	if err != nil {
		if ierr.IsValidation(err) {
			a.logger.Warnw("invoice cannot be synced to Paddle: validation error (non-retryable)",
				"invoice_id", input.InvoiceID,
				"error", err)
			return temporal.NewNonRetryableApplicationError(
				err.Error(),
				"InvoiceValidationError",
				err,
			)
		}
		a.logger.Errorw("failed to sync invoice to Paddle",
			"error", err,
			"invoice_id", input.InvoiceID)
		return err
	}

	a.logger.Infow("successfully synced invoice to Paddle",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID)

	return nil
}
