package tabs

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/integration/tabs"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/activities/billingentity"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
)

type InvoiceSyncActivities struct {
	integrationFactory *integration.Factory
	entityScope        interfaces.BillingEntityScope
	logger             *logger.Logger
}

func NewInvoiceSyncActivities(integrationFactory *integration.Factory, entityScope interfaces.BillingEntityScope, logger *logger.Logger) *InvoiceSyncActivities {
	return &InvoiceSyncActivities{
		integrationFactory: integrationFactory,
		entityScope:        entityScope,
		logger:             logger,
	}
}

func (a *InvoiceSyncActivities) SyncInvoiceToTabs(ctx context.Context, input models.TabsInvoiceSyncWorkflowInput) error {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	ctx, err := billingentity.ScopeActivity(ctx, a.entityScope, "", input.InvoiceID, "")
	if err != nil {
		return err
	}

	tabsIntegration, err := a.integrationFactory.GetTabsIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			return temporal.NewNonRetryableApplicationError("Tabs connection not configured", ierr.ErrConnectionNotFound, err)
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
