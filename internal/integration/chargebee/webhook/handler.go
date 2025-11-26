package webhook

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/integration/chargebee"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/shopspring/decimal"
)

// Handler handles Chargebee webhook events
type Handler struct {
	client     chargebee.ChargebeeClient
	invoiceSvc *chargebee.InvoiceService
	logger     *logger.Logger
}

// NewHandler creates a new Chargebee webhook handler
func NewHandler(
	client chargebee.ChargebeeClient,
	invoiceSvc *chargebee.InvoiceService,
	logger *logger.Logger,
) *Handler {
	return &Handler{
		client:     client,
		invoiceSvc: invoiceSvc,
		logger:     logger,
	}
}

// HandleWebhookEvent processes a Chargebee webhook event
func (h *Handler) HandleWebhookEvent(ctx context.Context, event *ChargebeeWebhookEvent, environmentID string) error {
	h.logger.Infow("processing Chargebee webhook event",
		"event_type", event.EventType,
		"event_id", event.ID,
		"environment_id", environmentID,
		"occurred_at", timestampToTime(event.OccurredAt),
	)

	eventType := ChargebeeEventType(event.EventType)

	switch eventType {
	case EventPaymentSucceeded:
		return h.handlePaymentSucceeded(ctx, event, environmentID)
	default:
		h.logger.Infow("unhandled Chargebee webhook event type", "type", event.EventType)
		return nil // Not an error, just unhandled
	}
}

// handlePaymentSucceeded handles payment_succeeded webhook
func (h *Handler) handlePaymentSucceeded(ctx context.Context, event *ChargebeeWebhookEvent, environmentID string) error {
	// Parse the webhook content
	var content ChargebeeWebhookContent
	if err := json.Unmarshal(event.Content, &content); err != nil {
		h.logger.Errorw("failed to parse webhook content",
			"error", err,
			"event_id", event.ID)
		return nil
	}

	if content.Transaction == nil {
		h.logger.Warnw("no transaction found in payment_succeeded event",
			"event_id", event.ID)
		return nil
	}

	if content.Invoice == nil {
		h.logger.Warnw("no invoice found in payment_succeeded event",
			"event_id", event.ID)
		return nil
	}

	transaction := content.Transaction
	invoice := content.Invoice

	h.logger.Infow("received payment_succeeded webhook",
		"chargebee_transaction_id", transaction.ID,
		"chargebee_invoice_id", invoice.ID,
	)

	// Get FlexPrice invoice ID from entity mapping via service method
	flexpriceInvoiceID, err := h.invoiceSvc.GetFlexPriceInvoiceIDByChargebeeInvoiceID(ctx, invoice.ID)
	if err != nil {
		h.logger.Errorw("failed to get FlexPrice invoice ID for Chargebee invoice",
			"error", err,
			"chargebee_invoice_id", invoice.ID,
			"event_id", event.ID)
		return nil // Don't fail webhook processing
	}

	h.logger.Infow("found FlexPrice invoice for payment",
		"flexprice_invoice_id", flexpriceInvoiceID,
		"chargebee_invoice_id", invoice.ID,
		"chargebee_transaction_id", transaction.ID)

	// Convert amount from cents to standard unit
	paymentAmount := decimal.NewFromInt(transaction.Amount).Div(decimal.NewFromInt(100))

	// Process payment via service method
	err = h.invoiceSvc.ProcessChargebeePaymentFromWebhook(
		ctx,
		flexpriceInvoiceID,
		transaction.ID,
		invoice.ID,
		paymentAmount,
		transaction.CurrencyCode,
		transaction.PaymentMethod,
	)
	if err != nil {
		h.logger.Errorw("failed to process Chargebee payment",
			"error", err,
			"flexprice_invoice_id", flexpriceInvoiceID,
			"chargebee_transaction_id", transaction.ID)
		return nil // Don't fail webhook processing
	}

	h.logger.Infow("successfully processed payment_succeeded webhook",
		"flexprice_invoice_id", flexpriceInvoiceID,
		"chargebee_invoice_id", invoice.ID,
		"chargebee_transaction_id", transaction.ID,
		"payment_amount", paymentAmount.String())

	return nil
}
