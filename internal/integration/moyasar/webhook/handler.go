package webhook

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/integration/moyasar"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
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
	h.logger.Infow("processing Moyasar webhook event",
		"event_type", event.Type,
		"event_id", event.ID,
		"environment_id", environmentID,
		"created_at", event.CreatedAt,
	)

	switch event.Type {
	case EventPaymentPaid, EventPaymentCaptured:
		return h.handlePaymentPaid(ctx, event, environmentID, services)
	case EventPaymentFailed:
		return h.handlePaymentFailed(ctx, event, environmentID, services)
	case EventPaymentRefunded:
		return h.handlePaymentRefunded(ctx, event, environmentID, services)
	case EventPaymentVoided:
		return h.handlePaymentVoided(ctx, event, environmentID, services)
	default:
		h.logger.Infow("unhandled Moyasar webhook event type", "type", event.Type)
		return nil // Not an error, just unhandled
	}
}

// handlePaymentPaid handles payment_paid and payment_captured webhooks
func (h *Handler) handlePaymentPaid(ctx context.Context, event *MoyasarWebhookEvent, environmentID string, services *ServiceDependencies) error {
	payment := event.Data

	h.logger.Infow("received payment_paid webhook",
		"moyasar_payment_id", payment.ID,
		"amount", payment.Amount,
		"currency", payment.Currency,
		"status", payment.Status,
		"environment_id", environmentID,
	)

	// Get FlexPrice payment ID from metadata
	flexpricePaymentID := ""
	if payment.Metadata != nil {
		flexpricePaymentID = payment.Metadata["flexprice_payment_id"]
	}

	if flexpricePaymentID == "" {
		h.logger.Warnw("no flexprice_payment_id found in payment metadata",
			"moyasar_payment_id", payment.ID,
			"metadata", payment.Metadata)
		return nil // Not a FlexPrice-initiated payment
	}

	h.logger.Infow("processing FlexPrice payment capture",
		"moyasar_payment_id", payment.ID,
		"flexprice_payment_id", flexpricePaymentID)

	// Get payment record
	paymentRecord, err := services.PaymentService.GetPayment(ctx, flexpricePaymentID)
	if err != nil {
		h.logger.Errorw("failed to get payment record",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"moyasar_payment_id", payment.ID)
		return nil // Don't fail webhook processing
	}

	if paymentRecord == nil {
		h.logger.Warnw("no payment record found",
			"flexprice_payment_id", flexpricePaymentID,
			"moyasar_payment_id", payment.ID)
		return nil
	}

	// Check if payment is already processed
	if paymentRecord.PaymentStatus == types.PaymentStatusSucceeded {
		h.logger.Infow("payment already processed",
			"flexprice_payment_id", flexpricePaymentID,
			"moyasar_payment_id", payment.ID,
			"status", paymentRecord.PaymentStatus)
		return nil
	}

	// Update payment status to succeeded
	paymentStatus := string(types.PaymentStatusSucceeded)
	now := time.Now()

	// Convert amount from smallest currency unit (halalah) to standard unit
	amount := decimal.NewFromInt(int64(payment.Amount)).Div(decimal.NewFromInt(100))

	// Determine payment method ID from source
	var paymentMethodID string
	if payment.Source != nil {
		paymentMethodID = payment.Source.GatewayID
		if paymentMethodID == "" {
			paymentMethodID = payment.Source.ReferenceID
		}
	}

	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus:    &paymentStatus,
		SucceededAt:      &now,
		GatewayPaymentID: &payment.ID,
	}

	if paymentMethodID != "" {
		updateReq.PaymentMethodID = &paymentMethodID
	}

	h.logger.Infow("updating payment with gateway details",
		"flexprice_payment_id", flexpricePaymentID,
		"moyasar_payment_id", payment.ID,
		"payment_method_id", paymentMethodID,
		"amount", amount.String())

	_, err = services.PaymentService.UpdatePayment(ctx, flexpricePaymentID, updateReq)
	if err != nil {
		h.logger.Errorw("failed to update payment",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"moyasar_payment_id", payment.ID)
		return nil // Don't return error - webhook should always succeed
	}

	h.logger.Infow("updated payment to succeeded",
		"flexprice_payment_id", flexpricePaymentID,
		"moyasar_payment_id", payment.ID,
		"amount", amount.String(),
		"currency", payment.Currency)

	// Reconcile payment with invoice
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

// handlePaymentFailed handles payment_failed webhook
func (h *Handler) handlePaymentFailed(ctx context.Context, event *MoyasarWebhookEvent, environmentID string, services *ServiceDependencies) error {
	payment := event.Data

	h.logger.Infow("received payment_failed webhook",
		"moyasar_payment_id", payment.ID,
		"amount", payment.Amount,
		"currency", payment.Currency,
		"status", payment.Status,
		"environment_id", environmentID,
	)

	// Get FlexPrice payment ID from metadata
	flexpricePaymentID := ""
	if payment.Metadata != nil {
		flexpricePaymentID = payment.Metadata["flexprice_payment_id"]
	}

	if flexpricePaymentID == "" {
		h.logger.Warnw("no flexprice_payment_id found in payment metadata",
			"moyasar_payment_id", payment.ID,
			"metadata", payment.Metadata)
		return nil // Not a FlexPrice-initiated payment
	}

	h.logger.Infow("processing FlexPrice payment failure",
		"moyasar_payment_id", payment.ID,
		"flexprice_payment_id", flexpricePaymentID)

	// Get payment record
	paymentRecord, err := services.PaymentService.GetPayment(ctx, flexpricePaymentID)
	if err != nil {
		h.logger.Errorw("failed to get payment record",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"moyasar_payment_id", payment.ID)
		return nil // Don't fail webhook processing
	}

	if paymentRecord == nil {
		h.logger.Warnw("no payment record found",
			"flexprice_payment_id", flexpricePaymentID,
			"moyasar_payment_id", payment.ID)
		return nil
	}

	// Check if payment is already processed
	if paymentRecord.PaymentStatus == types.PaymentStatusSucceeded {
		h.logger.Infow("Ignoring payment_failed webhook for succeeded payment",
			"flexprice_payment_id", flexpricePaymentID,
			"moyasar_payment_id", payment.ID)
		return nil
	}

	// Build error message
	errorMsg := "Payment failed"
	if payment.Source != nil && payment.Source.Message != "" {
		errorMsg = payment.Source.Message
	}

	// Update payment status to failed
	paymentStatus := string(types.PaymentStatusFailed)
	now := time.Now()

	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus:    &paymentStatus,
		FailedAt:         &now,
		ErrorMessage:     &errorMsg,
		GatewayPaymentID: &payment.ID,
	}

	h.logger.Infow("updating failed payment with gateway details",
		"flexprice_payment_id", flexpricePaymentID,
		"moyasar_payment_id", payment.ID,
		"error_message", errorMsg)

	_, err = services.PaymentService.UpdatePayment(ctx, flexpricePaymentID, updateReq)
	if err != nil {
		h.logger.Errorw("failed to update payment to failed",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"moyasar_payment_id", payment.ID)
		return nil // Don't return error - webhook should always succeed
	}

	h.logger.Infow("updated payment to failed",
		"flexprice_payment_id", flexpricePaymentID,
		"moyasar_payment_id", payment.ID,
		"error_message", errorMsg)

	return nil
}

// handlePaymentRefunded handles payment_refunded webhook
func (h *Handler) handlePaymentRefunded(ctx context.Context, event *MoyasarWebhookEvent, environmentID string, services *ServiceDependencies) error {
	payment := event.Data

	h.logger.Infow("received payment_refunded webhook",
		"moyasar_payment_id", payment.ID,
		"refunded_amount", payment.RefundedAmount,
		"currency", payment.Currency,
		"status", payment.Status,
		"environment_id", environmentID,
	)

	// Get FlexPrice payment ID from metadata
	flexpricePaymentID := ""
	if payment.Metadata != nil {
		flexpricePaymentID = payment.Metadata["flexprice_payment_id"]
	}

	if flexpricePaymentID == "" {
		h.logger.Warnw("no flexprice_payment_id found in payment metadata for refund",
			"moyasar_payment_id", payment.ID)
		return nil
	}

	// Update payment status to refunded
	paymentStatus := string(types.PaymentStatusRefunded)

	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus: &paymentStatus,
	}

	_, err := services.PaymentService.UpdatePayment(ctx, flexpricePaymentID, updateReq)
	if err != nil {
		h.logger.Errorw("failed to update payment to refunded",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"moyasar_payment_id", payment.ID)
		return nil
	}

	h.logger.Infow("updated payment to refunded",
		"flexprice_payment_id", flexpricePaymentID,
		"moyasar_payment_id", payment.ID,
		"refunded_amount", payment.RefundedAmount)

	return nil
}

// handlePaymentVoided handles payment_voided webhook
func (h *Handler) handlePaymentVoided(ctx context.Context, event *MoyasarWebhookEvent, environmentID string, services *ServiceDependencies) error {
	payment := event.Data

	h.logger.Infow("received payment_voided webhook",
		"moyasar_payment_id", payment.ID,
		"amount", payment.Amount,
		"currency", payment.Currency,
		"status", payment.Status,
		"environment_id", environmentID,
	)

	// Get FlexPrice payment ID from metadata
	flexpricePaymentID := ""
	if payment.Metadata != nil {
		flexpricePaymentID = payment.Metadata["flexprice_payment_id"]
	}

	if flexpricePaymentID == "" {
		h.logger.Warnw("no flexprice_payment_id found in payment metadata for void",
			"moyasar_payment_id", payment.ID)
		return nil
	}

	// Update payment status to failed (voided payments are treated as failed)
	paymentStatus := string(types.PaymentStatusFailed)
	now := time.Now()
	errorMsg := "Payment voided"

	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus: &paymentStatus,
		FailedAt:      &now,
		ErrorMessage:  &errorMsg,
	}

	_, err := services.PaymentService.UpdatePayment(ctx, flexpricePaymentID, updateReq)
	if err != nil {
		h.logger.Errorw("failed to update payment to voided/failed",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"moyasar_payment_id", payment.ID)
		return nil
	}

	h.logger.Infow("updated payment to voided/failed",
		"flexprice_payment_id", flexpricePaymentID,
		"moyasar_payment_id", payment.ID)

	return nil
}
