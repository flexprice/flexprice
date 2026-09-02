package tabs

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/integration/tabs"
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

func (a *InvoiceSyncActivities) SyncInvoiceToTabs(ctx context.Context, input models.TabsInvoiceSyncWorkflowInput) error {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	tabsIntegration, err := a.integrationFactory.GetTabsIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			return temporal.NewNonRetryableApplicationError("Tabs connection not configured", "ConnectionNotFound", err)
		}
		a.logger.Error(ctx, "SyncInvoiceToTabs activity failed to get Tabs integration",
			"error", err,
			"invoice_id", input.InvoiceID,
			"tenant_id", input.TenantID,
			"environment_id", input.EnvironmentID,
		)
		return err
	}

	if _, err := tabsIntegration.InvoiceSvc.SyncInvoiceToTabs(ctx, tabs.TabsInvoiceSyncRequest{
		InvoiceID: input.InvoiceID,
	}); err != nil {
		a.logger.Error(ctx, "SyncInvoiceToTabs activity failed",
			"error", err,
			"invoice_id", input.InvoiceID,
			"tenant_id", input.TenantID,
			"environment_id", input.EnvironmentID,
		)
		return err
	}
	return nil
}
