package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/integration/razorpay"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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
	h.logger.Info(ctx, "processing Razorpay webhook event",
		"event_type", event.Event,
		"account_id", event.AccountID,
		"environment_id", environmentID,
		"created_at", event.CreatedAt,
	)

	eventType := RazorpayEventType(event.Event)

	switch eventType {
	case EventPaymentCaptured:
		return h.handlePaymentCaptured(ctx, event, services)
	case EventPaymentFailed:
		return h.handlePaymentFailed(ctx, event, services)
	case EventPaymentLinkPaid:
		return h.handlePaymentLinkPaid(ctx, event, services)
	case EventPaymentLinkCancelled, EventPaymentLinkExpired:
		return h.handlePaymentLinkFailed(ctx, event, services)
	default:
		h.logger.Info(ctx, "unhandled Razorpay webhook event type", "type", event.Event)
		return nil // Not an error, just unhandled
	}
}

// handlePaymentCaptured handles payment.captured webhook
func (h *Handler) handlePaymentCaptured(ctx context.Context, event *RazorpayWebhookEvent, services *ServiceDependencies) error {
	payment := event.Payload.Payment.Entity

	h.logger.Info(ctx, "received payment.captured webhook",
		"razorpay_payment_id", payment.ID,
		"amount", payment.Amount,
		"currency", payment.Currency,
		"status", payment.Status,
	)

	// Get FlexPrice payment ID from notes
	flexpricePaymentID, ok := payment.Notes["flexprice_payment_id"].(string)
	if !ok || flexpricePaymentID == "" {
		h.logger.Info(ctx, "no flexprice_payment_id found in payment notes, checking for external payment",
			"razorpay_payment_id", payment.ID,
			"razorpay_invoice_id", payment.InvoiceID,
			"notes", payment.Notes)

		// No flexprice_payment_id - this might be an external Razorpay payment
		// Convert Payment struct to map for processing
		paymentMap := convertPaymentToMap(payment)
		err := h.paymentSvc.HandleExternalRazorpayPaymentFromWebhook(ctx, paymentMap, services.PaymentService, services.InvoiceService)
		if err != nil {
			h.logger.Error(ctx, "failed to handle external Razorpay payment from webhook, skipping event",
				"error", err,
				"razorpay_payment_id", payment.ID,
				"razorpay_invoice_id", payment.InvoiceID)
			return nil // Don't fail webhook processing
		}
		return nil
	}

	h.logger.Info(ctx, "processing FlexPrice payment capture",
		"razorpay_payment_id", payment.ID,
		"flexprice_payment_id", flexpricePaymentID)

	// Get payment record
	paymentRecord, err := services.PaymentService.GetPayment(ctx, flexpricePaymentID)
	if err != nil {
		h.logger.Error(ctx, "failed to get payment record",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"razorpay_payment_id", payment.ID)
		return nil // Don't fail webhook processing
	}

	if paymentRecord == nil {
		h.logger.Info(ctx, "no payment record found",
			"flexprice_payment_id", flexpricePaymentID,
			"razorpay_payment_id", payment.ID)
		return nil
	}

	// Check if payment is already processed
	if paymentRecord.PaymentStatus == types.PaymentStatusSucceeded {
		h.logger.Info(ctx, "payment already processed",
			"flexprice_payment_id", flexpricePaymentID,
			"razorpay_payment_id", payment.ID,
			"status", paymentRecord.PaymentStatus)
		return nil
	}

	// For authorization-link (mandate) checkouts, payment_link.paid is never fired by
	// Razorpay — only payment.captured is. Delegate entirely to CompleteCheckoutSession,
	// which activates the subscription, finalizes the invoice, marks the payment
	// succeeded, and completes the session — identical to the payment_link.paid path.
	if h.completeCheckoutSessionIfPending(ctx, flexpricePaymentID, payment.ID, services) {
		return nil
	}

	// Standalone payment (no checkout session) — update status and reconcile directly.
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

	h.logger.Info(ctx, "updating payment with gateway details",
		"flexprice_payment_id", flexpricePaymentID,
		"razorpay_payment_id", payment.ID,
		"payment_method", payment.Method,
		"payment_method_id", paymentMethodID,
		"amount", amount.String())

	_, err = services.PaymentService.UpdatePayment(ctx, flexpricePaymentID, updateReq)
	if err != nil {
		h.logger.Error(ctx, "failed to update payment",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"razorpay_payment_id", payment.ID)
		return nil // Don't return error - webhook should always succeed
	}

	h.logger.Info(ctx, "updated payment to succeeded",
		"flexprice_payment_id", flexpricePaymentID,
		"razorpay_payment_id", payment.ID,
		"amount", amount.String(),
		"currency", payment.Currency)

	// Reconcile payment with invoice (update invoice payment status and amounts)
	h.logger.Info(ctx, "reconciling payment with invoice",
		"flexprice_payment_id", flexpricePaymentID,
		"invoice_id", paymentRecord.DestinationID,
		"payment_amount", amount.String())

	err = h.paymentSvc.ReconcilePaymentWithInvoice(ctx, flexpricePaymentID, amount, services.PaymentService, services.InvoiceService)
	if err != nil {
		h.logger.Error(ctx, "failed to reconcile payment with invoice",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"invoice_id", paymentRecord.DestinationID,
			"payment_amount", amount.String())
		// Don't fail - invoice reconciliation is not critical for webhook success
	} else {
		h.logger.Info(ctx, "successfully reconciled payment with invoice",
			"flexprice_payment_id", flexpricePaymentID,
			"invoice_id", paymentRecord.DestinationID,
			"payment_amount", amount.String())
	}

	return nil
}

// handlePaymentFailed handles payment.failed webhook
func (h *Handler) handlePaymentFailed(ctx context.Context, event *RazorpayWebhookEvent, services *ServiceDependencies) error {
	payment := event.Payload.Payment.Entity

	h.logger.Info(ctx, "received payment.failed webhook",
		"razorpay_payment_id", payment.ID,
		"amount", payment.Amount,
		"currency", payment.Currency,
		"status", payment.Status,
		"error_code", payment.ErrorCode,
		"error_description", payment.ErrorDescription,
	)

	// Get FlexPrice payment ID from notes
	flexpricePaymentID, ok := payment.Notes["flexprice_payment_id"].(string)
	if !ok || flexpricePaymentID == "" {
		h.logger.Info(ctx, "no flexprice_payment_id found in payment notes",
			"razorpay_payment_id", payment.ID,
			"notes", payment.Notes)
		return nil // Not a FlexPrice-initiated payment
	}

	h.logger.Info(ctx, "processing FlexPrice payment failure",
		"razorpay_payment_id", payment.ID,
		"flexprice_payment_id", flexpricePaymentID)

	// Get payment record
	paymentRecord, err := services.PaymentService.GetPayment(ctx, flexpricePaymentID)
	if err != nil {
		h.logger.Error(ctx, "failed to get payment record",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"razorpay_payment_id", payment.ID)
		return nil // Don't fail webhook processing
	}

	if paymentRecord == nil {
		h.logger.Info(ctx, "no payment record found",
			"flexprice_payment_id", flexpricePaymentID,
			"razorpay_payment_id", payment.ID)
		return nil
	}

	// Check if payment is already processed
	if paymentRecord.PaymentStatus == types.PaymentStatusSucceeded {
		h.logger.Info(ctx, "Ignoring payment.failed webhook for succeeded payment",
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

	h.logger.Info(ctx, "updating failed payment with gateway details",
		"flexprice_payment_id", flexpricePaymentID,
		"razorpay_payment_id", payment.ID,
		"payment_method", payment.Method,
		"payment_method_id", paymentMethodID,
		"error_code", payment.ErrorCode)

	_, err = services.PaymentService.UpdatePayment(ctx, flexpricePaymentID, updateReq)
	if err != nil {
		h.logger.Error(ctx, "failed to update payment to failed",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"razorpay_payment_id", payment.ID)
		return nil // Don't return error - webhook should always succeed
	}

	h.logger.Info(ctx, "updated payment to failed",
		"flexprice_payment_id", flexpricePaymentID,
		"razorpay_payment_id", payment.ID,
		"error_code", payment.ErrorCode,
		"error_description", payment.ErrorDescription)

	return nil
}

// handlePaymentLinkPaid processes Razorpay payment_link.paid webhook events for
// FlexPrice-initiated checkout sessions.
func (h *Handler) handlePaymentLinkPaid(ctx context.Context, event *RazorpayWebhookEvent, services *ServiceDependencies) error {
	paymentLinkID := event.Payload.PaymentLink.Entity.ID
	if paymentLinkID == "" {
		h.logger.Info(ctx, "payment_link.paid webhook missing payment_link ID", "event_type", event.Event, "payment_link_id", paymentLinkID)
		return nil
	}

	mappings, err := services.EntityIntegrationMappingService.GetEntityIntegrationMappings(
		ctx,
		&types.EntityIntegrationMappingFilter{
			ProviderEntityIDs: []string{paymentLinkID},
			ProviderTypes:     []string{string(types.CheckoutPaymentProviderRazorpay)},
			EntityType:        types.IntegrationEntityTypePayment,
		},
	)
	if err != nil {
		h.logger.Error(ctx, "failed to get EntityIntegrationMapping for Razorpay payment link",
			"error", err,
			"payment_link_id", paymentLinkID)
		return nil
	}
	if mappings == nil || len(mappings.Items) == 0 {
		h.logger.Info(ctx, "no EntityIntegrationMapping found for Razorpay payment link", "payment_link_id", paymentLinkID)
		return nil
	}

	h.completeCheckoutSessionIfPending(ctx, mappings.Items[0].EntityID, event.Payload.Payment.Entity.ID, services)
	return nil
}

// handlePaymentLinkFailed processes payment_link.cancelled and payment_link.expired webhook events.
// If a pending checkout session is associated with the payment link, it is cleaned up as failed.
func (h *Handler) handlePaymentLinkFailed(ctx context.Context, event *RazorpayWebhookEvent, services *ServiceDependencies) error {
	paymentLinkID := event.Payload.PaymentLink.Entity.ID
	if paymentLinkID == "" {
		h.logger.Info(ctx, "payment link webhook missing payment_link ID", "event_type", event.Event)
		return nil
	}

	mappings, err := services.EntityIntegrationMappingService.GetEntityIntegrationMappings(
		ctx,
		&types.EntityIntegrationMappingFilter{
			ProviderEntityIDs: []string{paymentLinkID},
			ProviderTypes:     []string{string(types.CheckoutPaymentProviderRazorpay)},
			EntityType:        types.IntegrationEntityTypePayment,
		},
	)
	if err != nil {
		h.logger.Error(ctx, "failed to get EntityIntegrationMapping for Razorpay payment link",
			"error", err,
			"payment_link_id", paymentLinkID)
		return nil
	}
	if mappings == nil || len(mappings.Items) == 0 {
		h.logger.Info(ctx, "no EntityIntegrationMapping found for Razorpay payment link", "payment_link_id", paymentLinkID)
		return nil
	}

	filter := types.NewDefaultCheckoutSessionFilter()
	filter.CheckoutPaymentIDs = []string{mappings.Items[0].EntityID}
	filter.CheckoutStatuses = []types.CheckoutStatus{types.CheckoutStatusPending}
	filter.Limit = lo.ToPtr(1)
	filter.Status = lo.ToPtr(types.StatusPublished)

	sessions, err := services.CheckoutSessionService.List(ctx, filter)
	if err != nil || sessions == nil || len(sessions.Items) == 0 {
		return nil
	}

	sessionID := sessions.Items[0].ID
	reason := fmt.Errorf("payment link %s by provider", event.Event)
	if err := services.CheckoutSessionService.CleanupCheckoutSession(ctx, sessionID, reason); err != nil {
		h.logger.Error(ctx, "failed to cleanup checkout session on payment link failure",
			"error", err,
			"session_id", sessionID,
			"payment_link_id", paymentLinkID,
			"event_type", event.Event)
		return nil
	}

	return nil
}

// completeCheckoutSessionIfPending looks up a pending checkout session by FlexPrice
// payment ID and calls CompleteCheckoutSession if one exists. It is used by both the
// payment_link.paid path (standard payment links) and the payment.captured path
// (authorization / mandate links) so that both flows correctly activate the
// subscription, finalize the invoice, and mark the session completed.
// Returns true if a checkout session was found and the completion attempt was made
// (regardless of whether it succeeded), false if no session was found (caller should
// proceed with standalone payment handling). Errors are logged but not returned.
func (h *Handler) completeCheckoutSessionIfPending(
	ctx context.Context,
	flexpricePaymentID string,
	razorpayPaymentID string,
	services *ServiceDependencies,
) bool {
	filter := types.NewDefaultCheckoutSessionFilter()
	filter.CheckoutPaymentIDs = []string{flexpricePaymentID}
	filter.CheckoutStatuses = []types.CheckoutStatus{types.CheckoutStatusPending}
	filter.Limit = lo.ToPtr(1)
	filter.Status = lo.ToPtr(types.StatusPublished)

	sessions, err := services.CheckoutSessionService.List(ctx, filter)
	if err != nil || sessions == nil || len(sessions.Items) == 0 {
		return false
	}

	sessionID := sessions.Items[0].ID
	if err := services.CheckoutSessionService.CompleteCheckoutSession(ctx, sessionID, &types.CheckoutProviderResult{
		ProviderPaymentIntentID: razorpayPaymentID,
	}); err != nil {
		h.logger.Error(ctx, "failed to complete checkout session",
			"error", err,
			"session_id", sessionID,
			"flexprice_payment_id", flexpricePaymentID,
			"razorpay_payment_id", razorpayPaymentID,
		)
	}
	return true
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
