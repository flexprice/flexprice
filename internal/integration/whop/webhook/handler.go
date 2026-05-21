package webhook

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// WhopWebhookEvent is the top-level Whop webhook payload
type WhopWebhookEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	CompanyID string          `json:"company_id"`
	Data      WhopInvoiceData `json:"data"`
}

// WhopInvoiceData is the invoice object inside the webhook payload
type WhopInvoiceData struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// ServiceDependencies mirrors interfaces.ServiceDependencies for webhook handlers
type ServiceDependencies = interfaces.ServiceDependencies

// Handler handles Whop webhook events
type Handler struct {
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

func NewHandler(
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) *Handler {
	return &Handler{
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       logger,
	}
}

// HandleWebhookEvent processes an inbound Whop webhook event.
// Always returns nil so Whop receives 200 OK.
func (h *Handler) HandleWebhookEvent(ctx context.Context, event *WhopWebhookEvent, services *ServiceDependencies) error {
	h.logger.Infow("processing Whop webhook event",
		"type", event.Type,
		"whop_invoice_id", event.Data.ID,
		"msg_id", event.ID)

	switch event.Type {
	case "invoice.paid":
		return h.handleInvoicePaid(ctx, &event.Data, services)
	default:
		h.logger.Infow("unhandled Whop webhook type, skipping", "type", event.Type)
	}
	return nil
}

// handleInvoicePaid marks the corresponding Flexprice invoice as paid.
func (h *Handler) handleInvoicePaid(ctx context.Context, data *WhopInvoiceData, services *ServiceDependencies) error {
	h.logger.Infow("handling Whop invoice.paid", "whop_invoice_id", data.ID)

	// Look up Flexprice invoice via entity_integration_mapping
	filter := &types.EntityIntegrationMappingFilter{
		ProviderTypes:     []string{string(types.SecretProviderWhop)},
		ProviderEntityIDs: []string{data.ID},
		EntityType:        types.IntegrationEntityTypeInvoice,
	}
	mappings, err := h.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		h.logger.Errorw("failed to look up entity mapping for Whop invoice",
			"error", err, "whop_invoice_id", data.ID)
		return nil
	}
	if len(mappings) == 0 {
		h.logger.Warnw("no Flexprice invoice found for Whop invoice, skipping",
			"whop_invoice_id", data.ID)
		return nil
	}
	flexpriceInvoiceID := mappings[0].EntityID

	// Idempotency: skip if already paid
	inv, err := services.InvoiceService.GetInvoice(ctx, flexpriceInvoiceID)
	if err != nil {
		h.logger.Errorw("failed to get Flexprice invoice",
			"error", err, "invoice_id", flexpriceInvoiceID)
		return nil
	}
	if inv.PaymentStatus == types.PaymentStatusSucceeded {
		h.logger.Infow("Flexprice invoice already paid, skipping",
			"invoice_id", flexpriceInvoiceID, "whop_invoice_id", data.ID)
		return nil
	}

	// Mark invoice as paid
	amount := inv.AmountDue
	if err := services.InvoiceService.ReconcilePaymentStatus(ctx, flexpriceInvoiceID, types.PaymentStatusSucceeded, &amount); err != nil {
		h.logger.Errorw("failed to reconcile invoice payment status",
			"error", err, "invoice_id", flexpriceInvoiceID, "whop_invoice_id", data.ID)
		return nil
	}

	h.logger.Infow("successfully marked Flexprice invoice as paid from Whop",
		"invoice_id", flexpriceInvoiceID, "whop_invoice_id", data.ID)
	return nil
}
