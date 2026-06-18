package moyasar

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/integration/moyasar"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
)

// InvoiceSyncActivities handles Moyasar invoice sync activities
type InvoiceSyncActivities struct {
	integrationFactory           *integration.Factory
	customerService              interfaces.CustomerService
	paymentService               interfaces.PaymentService
	invoiceService               interfaces.InvoiceService
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewInvoiceSyncActivities creates a new Moyasar invoice sync activities handler
func NewInvoiceSyncActivities(
	integrationFactory *integration.Factory,
	customerService interfaces.CustomerService,
	paymentService interfaces.PaymentService,
	invoiceService interfaces.InvoiceService,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) *InvoiceSyncActivities {
	return &InvoiceSyncActivities{
		integrationFactory:           integrationFactory,
		customerService:              customerService,
		paymentService:               paymentService,
		invoiceService:               invoiceService,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       logger,
	}
}

// invoiceAlreadySynced returns true when entity_integration_mappings already has a record
// for this invoice + Moyasar, meaning the hosted invoice was previously created.
func (a *InvoiceSyncActivities) invoiceAlreadySynced(ctx context.Context, invoiceID string) bool {
	filter := &types.EntityIntegrationMappingFilter{
		EntityID:      invoiceID,
		EntityType:    types.IntegrationEntityTypeInvoice,
		ProviderTypes: []string{string(types.SecretProviderMoyasar)},
	}
	mappings, err := a.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		a.logger.Error(ctx, "failed to check invoice sync status", "invoice_id", invoiceID, "error", err)
		return false
	}
	return len(mappings) > 0
}

// SyncInvoiceToMoyasar syncs an invoice to Moyasar
func (a *InvoiceSyncActivities) SyncInvoiceToMoyasar(
	ctx context.Context,
	input models.MoyasarInvoiceSyncWorkflowInput,
) error {
	a.logger.Info(ctx, "syncing invoice to Moyasar",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	moyasarIntegration, err := a.integrationFactory.GetMoyasarIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			a.logger.Debug(ctx, "Moyasar connection not configured",
				"invoice_id", input.InvoiceID,
				"customer_id", input.CustomerID)
			return temporal.NewNonRetryableApplicationError(
				"Moyasar connection not configured",
				"ConnectionNotFound",
				err,
			)
		}
		a.logger.Error(ctx, "failed to get Moyasar integration",
			"error", err,
			"invoice_id", input.InvoiceID,
			"customer_id", input.CustomerID)
		return err
	}

	// Autopay: always attempt first, regardless of whether the invoice was previously synced.
	// A customer may have added a saved token after the initial sync.
	charged, autopayErr := moyasarIntegration.PaymentSvc.ChargeInvoiceWithSavedToken(
		ctx,
		input.InvoiceID,
		a.paymentService,
		a.invoiceService,
	)
	if autopayErr != nil {
		a.logger.Error(ctx, "autopay failed, falling back to hosted invoice",
			"error", autopayErr,
			"invoice_id", input.InvoiceID)
	} else if charged {
		a.logger.Info(ctx, "invoice auto-paid with saved token",
			"invoice_id", input.InvoiceID,
			"customer_id", input.CustomerID)
		return nil
	}

	// Hosted invoice path: skip if already synced (idempotency).
	if a.invoiceAlreadySynced(ctx, input.InvoiceID) {
		a.logger.Info(ctx, "invoice already synced to Moyasar and no saved token, skipping hosted sync",
			"invoice_id", input.InvoiceID)
		return nil
	}

	_, err = moyasarIntegration.InvoiceSyncSvc.SyncInvoiceToMoyasar(ctx, moyasar.MoyasarInvoiceSyncRequest{
		InvoiceID: input.InvoiceID,
	}, a.customerService)
	if err != nil {
		a.logger.Error(ctx, "failed to sync invoice to Moyasar",
			"error", err,
			"invoice_id", input.InvoiceID)
		return err
	}

	a.logger.Info(ctx, "successfully synced invoice to Moyasar",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID)

	return nil
}
