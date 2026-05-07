package zoho

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/integration/zoho"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
)

type InvoiceSyncActivities struct {
	integrationFactory *integration.Factory
	logger             *logger.Logger
}

func NewInvoiceSyncActivities(integrationFactory *integration.Factory, logger *logger.Logger) *InvoiceSyncActivities {
	return &InvoiceSyncActivities{
		integrationFactory: integrationFactory,
		logger:             logger,
	}
}

func (a *InvoiceSyncActivities) SyncInvoiceToZoho(ctx context.Context, input models.ZohoBooksInvoiceSyncWorkflowInput) error {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	zohoIntegration, err := a.integrationFactory.GetZohoBooksIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			return temporal.NewNonRetryableApplicationError("Zoho Books connection not configured", "ConnectionNotFound", err)
		}
		return err
	}

	_, err = zohoIntegration.InvoiceSvc.SyncInvoiceToZoho(ctx, zoho.ZohoInvoiceSyncRequest{
		InvoiceID: input.InvoiceID,
	})
	return err
}
