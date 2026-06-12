package webhook

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/integration/moyasar"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// Handler handles Moyasar webhook events
type Handler struct {
	client                       moyasar.MoyasarClient
	paymentSvc                   *moyasar.PaymentService
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewHandler creates a new Moyasar webhook handler
func NewHandler(
	client moyasar.MoyasarClient,
	paymentSvc *moyasar.PaymentService,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) *Handler {
	return &Handler{
		client:                       client,
		paymentSvc:                   paymentSvc,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       logger,
	}
}

// ServiceDependencies contains all service dependencies needed by webhook handlers
type ServiceDependencies = interfaces.ServiceDependencies

// HandleWebhookEvent processes a Moyasar webhook event
// This function never returns errors to ensure webhooks always return 200 OK
// All errors are logged internally to prevent Moyasar from retrying
func (h *Handler) HandleWebhookEvent(ctx context.Context, event *MoyasarWebhookEvent, environmentID string, services *ServiceDependencies) error {
	h.logger.Info(ctx, "processing Moyasar webhook event",
		"event_type", event.Type,
		"event_id", event.ID,
		"environment_id", environmentID,
		"created_at", event.CreatedAt,
	)

	// We only care about payment_paid events
	if event.Type == EventPaymentPaid || event.Type == EventPaymentCaptured {
		return h.handlePaymentPaid(ctx, event, environmentID, services)
	}

	h.logger.Info(ctx, "ignoring non-payment_paid event", "type", event.Type)
	return nil
}

// handlePaymentPaid handles payment_paid and payment_captured webhooks
func (h *Handler) handlePaymentPaid(ctx context.Context, event *MoyasarWebhookEvent, environmentID string, services *ServiceDependencies) error {
	payment := event.Data

	h.logger.Info(ctx, "received payment_paid webhook",
		"moyasar_payment_id", payment.ID,
		"amount", payment.Amount,
		"currency", payment.Currency,
		"status", payment.Status,
		"environment_id", environmentID,
	)

	// We only support Invoice Payment (Invoice Link Flow)
	// We need to find the FlexPrice invoice associated with this Moyasar invoice
	// In webhook payload, payment.InvoiceID contains the Moyasar Invoice ID
	if payment.InvoiceID != "" {
		return h.handleInvoicePayment(ctx, payment, services)
	}

	h.logger.Info(ctx, "webhook payment has no invoice_id, skipping",
		"moyasar_payment_id", payment.ID)
	return nil
}

// handleInvoicePayment handles payments for FlexPrice invoices (Invoice Link Flow)
func (h *Handler) handleInvoicePayment(ctx context.Context, payment PaymentEventData, services *ServiceDependencies) error {
	moyasarInvoiceID := payment.InvoiceID
	h.logger.Info(ctx, "processing Moyasar invoice payment",
		"moyasar_payment_id", payment.ID,
		"moyasar_invoice_id", moyasarInvoiceID)

	// Find the FlexPrice invoice using the integration mapping
	filter := &types.EntityIntegrationMappingFilter{
		ProviderTypes:     []string{string(types.SecretProviderMoyasar)},
		ProviderEntityIDs: []string{moyasarInvoiceID},
		EntityType:        types.IntegrationEntityTypeInvoice,
	}

	mappings, err := h.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		h.logger.Error(ctx, "failed to find mapping for Moyasar invoice",
			"error", err,
			"moyasar_invoice_id", moyasarInvoiceID)
		return nil
	}

	if len(mappings) == 0 {
		h.logger.Info(ctx, "no FlexPrice invoice found for Moyasar invoice, skipping",
			"moyasar_invoice_id", moyasarInvoiceID)
		return nil
	}

	flexpriceInvoiceID := mappings[0].EntityID
	h.logger.Info(ctx, "found FlexPrice invoice for Moyasar invoice",
		"flexprice_invoice_id", flexpriceInvoiceID,
		"moyasar_invoice_id", moyasarInvoiceID)

	// Convert payment object for processing
	// We need to reconstruct the MoyasarPaymentObject to reuse the service method
	moyasarPayment := &moyasar.MoyasarPaymentObject{
		ID:          payment.ID,
		Status:      payment.Status,
		Amount:      payment.Amount,
		Currency:    payment.Currency,
		Description: payment.Description,
		CreatedAt:   payment.CreatedAt,
		Metadata:    payment.Metadata,
	}

	if payment.Source != nil {
		moyasarPayment.Source = &moyasar.PaymentSource{
			Type:        moyasar.PaymentSourceType(payment.Source.Type),
			Company:     payment.Source.Company,
			Name:        payment.Source.Name,
			Number:      payment.Source.Number,
			GatewayID:   payment.Source.GatewayID,
			ReferenceID: payment.Source.ReferenceID,
			Message:     payment.Source.Message,
		}
	}

	// Reuse existing logic to create payment record and reconcile
	err = h.paymentSvc.ProcessExternalMoyasarPayment(ctx, moyasarPayment, flexpriceInvoiceID, services.PaymentService, services.InvoiceService)
	if err != nil {
		h.logger.Error(ctx, "failed to process external Moyasar payment",
			"error", err,
			"flexprice_invoice_id", flexpriceInvoiceID,
			"moyasar_payment_id", payment.ID)
		return nil
	}

	h.logger.Info(ctx, "successfully processed invoice payment",
		"flexprice_invoice_id", flexpriceInvoiceID,
		"moyasar_payment_id", payment.ID)

	return nil
}
