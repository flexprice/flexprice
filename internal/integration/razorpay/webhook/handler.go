package webhook

import (
	"context"
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/integration/razorpay"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Handler handles Razorpay webhook events
type Handler struct {
	client                       razorpay.RazorpayClient
	paymentSvc                   *razorpay.PaymentService
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewHandler creates a new Razorpay webhook handler
func NewHandler(
	client razorpay.RazorpayClient,
	paymentSvc *razorpay.PaymentService,
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

// getPaymentMethodID extracts the payment method ID based on the payment method type
func getPaymentMethodID(payment Payment) string {
	switch RazorpayPaymentMethod(payment.Method) {
	case RazorpayPaymentMethodCard:
		return payment.CardID
	case RazorpayPaymentMethodUPI:
		return payment.VPA
	case RazorpayPaymentMethodWallet:
		return payment.Wallet
	case RazorpayPaymentMethodNetbanking:
		return payment.Bank
	default:
		return ""
	}
}

// HandleWebhookEvent processes a Razorpay webhook event
// This function never returns errors to ensure webhooks always return 200 OK
// All errors are logged internally to prevent Razorpay from retrying
func (h *Handler) HandleWebhookEvent(ctx context.Context, event *RazorpayWebhookEvent, environmentID string, services *ServiceDependencies) error {
	h.logger.Infow("processing Razorpay webhook event",
		"event_type", event.Event,
		"account_id", event.AccountID,
		"environment_id", environmentID,
		"created_at", event.CreatedAt,
	)

	eventType := RazorpayEventType(event.Event)

	switch eventType {
	case EventPaymentCaptured:
		return h.handlePaymentCaptured(ctx, event, environmentID, services)
	case EventPaymentFailed:
		return h.handlePaymentFailed(ctx, event, environmentID, services)
	default:
		h.logger.Infow("unhandled Razorpay webhook event type", "type", event.Event)
		return nil // Not an error, just unhandled
	}
}

// handlePaymentCaptured handles payment.captured webhook
func (h *Handler) handlePaymentCaptured(ctx context.Context, event *RazorpayWebhookEvent, environmentID string, services *ServiceDependencies) error {
	payment := event.Payload.Payment.Entity

	h.logger.Infow("received payment.captured webhook",
		"razorpay_payment_id", payment.ID,
		"amount", payment.Amount,
		"currency", payment.Currency,
		"status", payment.Status,
		"environment_id", environmentID,
	)

	// Get FlexPrice payment ID from notes
	flexpricePaymentID, ok := payment.Notes["flexprice_payment_id"].(string)
	if !ok || flexpricePaymentID == "" {
		h.logger.Infow("no flexprice_payment_id found in payment notes, checking for external payment",
			"razorpay_payment_id", payment.ID,
			"razorpay_invoice_id", payment.InvoiceID,
			"notes", payment.Notes)

		// No flexprice_payment_id - this might be an external Razorpay payment
		// Convert Payment struct to map for processing
		paymentMap := convertPaymentToMap(payment)
		err := h.paymentSvc.HandleExternalRazorpayPaymentFromWebhook(ctx, paymentMap, services.PaymentService, services.InvoiceService)
		if err != nil {
			h.logger.Errorw("failed to handle external Razorpay payment from webhook, skipping event",
				"error", err,
				"razorpay_payment_id", payment.ID,
				"razorpay_invoice_id", payment.InvoiceID)
			return nil // Don't fail webhook processing
		}
		return nil
	}

	h.logger.Infow("processing FlexPrice payment capture",
		"razorpay_payment_id", payment.ID,
		"flexprice_payment_id", flexpricePaymentID)

	// Get payment record
	paymentRecord, err := services.PaymentService.GetPayment(ctx, flexpricePaymentID)
	if err != nil {
		h.logger.Errorw("failed to get payment record",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"razorpay_payment_id", payment.ID)
		return nil // Don't fail webhook processing
	}

	if paymentRecord == nil {
		h.logger.Warnw("no payment record found",
			"flexprice_payment_id", flexpricePaymentID,
			"razorpay_payment_id", payment.ID)
		return nil
	}

	// Check if payment is already processed
	if paymentRecord.PaymentStatus == types.PaymentStatusSucceeded {
		h.logger.Infow("payment already processed",
			"flexprice_payment_id", flexpricePaymentID,
			"razorpay_payment_id", payment.ID,
			"status", paymentRecord.PaymentStatus)
		return nil
	}

	// Update payment status to succeeded
	paymentStatus := string(types.PaymentStatusSucceeded)
	now := time.Now()

	// Convert amount from smallest currency unit (paise) to standard unit
	amount := decimal.NewFromInt(payment.Amount).Div(decimal.NewFromInt(100))

	// Determine payment method ID based on payment method type
	paymentMethodID := getPaymentMethodID(payment)

	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus:    &paymentStatus,
		SucceededAt:      &now,
		GatewayPaymentID: &payment.ID, // Set Razorpay payment ID (e.g., pay_ReLTtNd9exrNsW)
	}

	// Set payment_method_id if available (could be card_id, VPA, wallet, or bank)
	if paymentMethodID != "" {
		updateReq.PaymentMethodID = &paymentMethodID
	}

	h.logger.Infow("updating payment with gateway details",
		"flexprice_payment_id", flexpricePaymentID,
		"razorpay_payment_id", payment.ID,
		"payment_method", payment.Method,
		"payment_method_id", paymentMethodID,
		"amount", amount.String())

	_, err = services.PaymentService.UpdatePayment(ctx, flexpricePaymentID, updateReq)
	if err != nil {
		h.logger.Errorw("failed to update payment",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"razorpay_payment_id", payment.ID)
		return nil // Don't return error - webhook should always succeed
	}

	h.logger.Infow("updated payment to succeeded",
		"flexprice_payment_id", flexpricePaymentID,
		"razorpay_payment_id", payment.ID,
		"amount", amount.String(),
		"currency", payment.Currency)

	// Reconcile payment with invoice (update invoice payment status and amounts)
	h.logger.Infow("reconciling payment with invoice",
		"flexprice_payment_id", flexpricePaymentID,
		"invoice_id", paymentRecord.DestinationID,
		"payment_amount", amount.String())

	err = h.paymentSvc.ReconcilePaymentWithInvoice(ctx, flexpricePaymentID, amount, services.PaymentService, services.InvoiceService)
	if err != nil {
		h.logger.Errorw("failed to reconcile payment with invoice",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"invoice_id", paymentRecord.DestinationID,
			"payment_amount", amount.String())
		// Don't fail - invoice reconciliation is not critical for webhook success
	} else {
		h.logger.Infow("successfully reconciled payment with invoice",
			"flexprice_payment_id", flexpricePaymentID,
			"invoice_id", paymentRecord.DestinationID,
			"payment_amount", amount.String())
	}

	return nil
}

// handlePaymentFailed handles payment.failed webhook
func (h *Handler) handlePaymentFailed(ctx context.Context, event *RazorpayWebhookEvent, environmentID string, services *ServiceDependencies) error {
	payment := event.Payload.Payment.Entity

	h.logger.Infow("received payment.failed webhook",
		"razorpay_payment_id", payment.ID,
		"amount", payment.Amount,
		"currency", payment.Currency,
		"status", payment.Status,
		"error_code", payment.ErrorCode,
		"error_description", payment.ErrorDescription,
		"environment_id", environmentID,
	)

	// Get FlexPrice payment ID from notes
	flexpricePaymentID, ok := payment.Notes["flexprice_payment_id"].(string)
	if !ok || flexpricePaymentID == "" {
		h.logger.Warnw("no flexprice_payment_id found in payment notes",
			"razorpay_payment_id", payment.ID,
			"notes", payment.Notes)
		return nil // Not a FlexPrice-initiated payment
	}

	h.logger.Infow("processing FlexPrice payment failure",
		"razorpay_payment_id", payment.ID,
		"flexprice_payment_id", flexpricePaymentID)

	// Get payment record
	paymentRecord, err := services.PaymentService.GetPayment(ctx, flexpricePaymentID)
	if err != nil {
		h.logger.Errorw("failed to get payment record",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"razorpay_payment_id", payment.ID)
		return nil // Don't fail webhook processing
	}

	if paymentRecord == nil {
		h.logger.Warnw("no payment record found",
			"flexprice_payment_id", flexpricePaymentID,
			"razorpay_payment_id", payment.ID)
		return nil
	}

	// Check if payment is already processed
	if paymentRecord.PaymentStatus == types.PaymentStatusSucceeded {
		h.logger.Infow("Ignoring payment.failed webhook for succeeded payment",
			"flexprice_payment_id", flexpricePaymentID,
			"razorpay_payment_id", payment.ID)
		return nil
	}

	// Build error message
	errorMsg := "Payment failed"
	if payment.ErrorDescription != "" {
		errorMsg = payment.ErrorDescription
	}

	// Update payment status to failed
	paymentStatus := string(types.PaymentStatusFailed)
	now := time.Now()

	// Determine payment method ID based on payment method type
	paymentMethodID := getPaymentMethodID(payment)

	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus:    &paymentStatus,
		FailedAt:         &now,
		ErrorMessage:     &errorMsg,
		GatewayPaymentID: &payment.ID, // Set Razorpay payment ID even for failed payments
	}

	// Set payment_method_id if available
	if paymentMethodID != "" {
		updateReq.PaymentMethodID = &paymentMethodID
	}

	h.logger.Infow("updating failed payment with gateway details",
		"flexprice_payment_id", flexpricePaymentID,
		"razorpay_payment_id", payment.ID,
		"payment_method", payment.Method,
		"payment_method_id", paymentMethodID,
		"error_code", payment.ErrorCode)

	_, err = services.PaymentService.UpdatePayment(ctx, flexpricePaymentID, updateReq)
	if err != nil {
		h.logger.Errorw("failed to update payment to failed",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"razorpay_payment_id", payment.ID)
		return nil // Don't return error - webhook should always succeed
	}

	h.logger.Infow("updated payment to failed",
		"flexprice_payment_id", flexpricePaymentID,
		"razorpay_payment_id", payment.ID,
		"error_code", payment.ErrorCode,
		"error_description", payment.ErrorDescription)

	return nil
}

// convertPaymentToMap converts a Payment struct to a map using JSON marshaling
func convertPaymentToMap(payment Payment) map[string]interface{} {
	// Use JSON marshal/unmarshal to convert struct to map (leverages existing struct tags)
	var paymentMap map[string]interface{}

	// Marshal to JSON bytes
	jsonBytes, err := json.Marshal(payment)
	if err != nil {
		// Fallback to empty map if marshaling fails (should never happen)
		return make(map[string]interface{})
	}

	// Unmarshal to map
	if err := json.Unmarshal(jsonBytes, &paymentMap); err != nil {
		// Fallback to empty map if unmarshaling fails (should never happen)
		return make(map[string]interface{})
	}

	return paymentMap
}
