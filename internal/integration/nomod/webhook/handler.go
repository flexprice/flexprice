package webhook

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration/nomod"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Handler handles Nomod webhook events
type Handler struct {
	client                       nomod.NomodClient
	paymentSvc                   *nomod.PaymentService
	invoiceSyncSvc               *nomod.InvoiceSyncService
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewHandler creates a new Nomod webhook handler
func NewHandler(
	client nomod.NomodClient,
	paymentSvc *nomod.PaymentService,
	invoiceSyncSvc *nomod.InvoiceSyncService,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) *Handler {
	return &Handler{
		client:                       client,
		paymentSvc:                   paymentSvc,
		invoiceSyncSvc:               invoiceSyncSvc,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       logger,
	}
}

// ServiceDependencies contains all service dependencies needed by webhook handlers
type ServiceDependencies = interfaces.ServiceDependencies

// HandleWebhookEvent processes a Nomod webhook event
// This function never returns errors to ensure webhooks always return 200 OK
// All errors are logged internally to prevent Nomod from retrying
func (h *Handler) HandleWebhookEvent(ctx context.Context, payload *NomodWebhookPayload, services *ServiceDependencies) error {
	h.logger.Infow("processing Nomod webhook event",
		"charge_id", payload.ID,
		"has_invoice_id", payload.InvoiceID != nil,
		"has_payment_link_id", payload.PaymentLinkID != nil)

	// Step 1: Fetch charge details from Nomod
	charge, err := h.client.GetCharge(ctx, payload.ID)
	if err != nil {
		h.logger.Errorw("failed to fetch charge from Nomod, skipping event",
			"error", err,
			"charge_id", payload.ID)
		return nil // Don't fail webhook processing
	}

	h.logger.Infow("fetched charge details",
		"charge_id", charge.ID,
		"status", charge.Status,
		"total", charge.Total,
		"currency", charge.Currency,
		"payment_method", charge.PaymentMethod)

	// Step 2: Check if charge is paid
	if charge.Status != "paid" {
		h.logger.Infow("charge not paid, skipping event",
			"charge_id", charge.ID,
			"status", charge.Status)
		return nil
	}

	// Step 3: Route based on invoice_id or payment_link_id
	if payload.InvoiceID != nil && *payload.InvoiceID != "" {
		return h.handleInvoicePayment(ctx, charge, *payload.InvoiceID, services)
	} else if payload.PaymentLinkID != nil && *payload.PaymentLinkID != "" {
		return h.handlePaymentLinkPayment(ctx, charge, *payload.PaymentLinkID, services)
	}

	h.logger.Warnw("webhook payload has neither invoice_id nor payment_link_id",
		"charge_id", payload.ID)
	return nil
}

// handleInvoicePayment processes external Nomod invoice payments
func (h *Handler) handleInvoicePayment(ctx context.Context, charge *nomod.ChargeResponse, nomodInvoiceID string, services *ServiceDependencies) error {
	h.logger.Infow("processing invoice payment",
		"charge_id", charge.ID,
		"nomod_invoice_id", nomodInvoiceID)

	// Look up FlexPrice invoice using entity_integration_mapping with List and filters
	filter := &types.EntityIntegrationMappingFilter{
		ProviderTypes:     []string{string(types.SecretProviderNomod)},
		ProviderEntityIDs: []string{nomodInvoiceID},
		EntityType:        types.IntegrationEntityTypeInvoice,
	}

	mappings, err := h.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		h.logger.Errorw("failed to find FlexPrice invoice for Nomod invoice, skipping event",
			"error", err,
			"nomod_invoice_id", nomodInvoiceID,
			"charge_id", charge.ID)
		return nil
	}

	if len(mappings) == 0 {
		h.logger.Warnw("no FlexPrice invoice found for Nomod invoice, skipping event",
			"nomod_invoice_id", nomodInvoiceID,
			"charge_id", charge.ID)
		return nil
	}

	mapping := mappings[0]
	flexpriceInvoiceID := mapping.EntityID

	h.logger.Infow("found FlexPrice invoice for Nomod invoice",
		"flexprice_invoice_id", flexpriceInvoiceID,
		"nomod_invoice_id", nomodInvoiceID)

	// Check if payment already exists for this charge
	exists, err := services.PaymentService.PaymentExistsByGatewayPaymentID(ctx, charge.ID)
	if err != nil {
		h.logger.Errorw("failed to check if payment exists",
			"error", err,
			"charge_id", charge.ID)
		// Continue processing on error
	} else if exists {
		h.logger.Infow("payment already exists for this charge, skipping",
			"charge_id", charge.ID,
			"nomod_invoice_id", nomodInvoiceID)
		return nil
	}

	// Parse amount
	amount, err := decimal.NewFromString(charge.Total)
	if err != nil {
		h.logger.Errorw("failed to parse charge amount, skipping event",
			"error", err,
			"charge_id", charge.ID,
			"total", charge.Total)
		return nil
	}

	// Get invoice to validate
	_, err = services.InvoiceService.GetInvoice(ctx, flexpriceInvoiceID)
	if err != nil {
		h.logger.Errorw("failed to get invoice, skipping event",
			"error", err,
			"invoice_id", flexpriceInvoiceID)
		return nil
	}

	// Create payment record in FlexPrice
	h.logger.Infow("creating payment record for external Nomod invoice payment",
		"flexprice_invoice_id", flexpriceInvoiceID,
		"charge_id", charge.ID,
		"amount", amount.String(),
		"currency", charge.Currency)

	now := time.Now()
	createReq := dto.CreatePaymentRequest{
		Amount:            amount,
		Currency:          charge.Currency,
		DestinationID:     flexpriceInvoiceID,
		DestinationType:   types.PaymentDestinationTypeInvoice,
		PaymentMethodType: types.PaymentMethodTypeCard, // Mark as card payment (external payment on Nomod)
		PaymentGateway:    lo.ToPtr(types.PaymentGatewayTypeNomod),
		ProcessPayment:    false, // Don't trigger another payment flow
		Metadata: types.Metadata{
			"payment_source":   "nomod_external",
			"nomod_charge_id":  charge.ID,
			"webhook_event_id": charge.ID, // For idempotency
		},
	}

	paymentResp, err := services.PaymentService.CreatePayment(ctx, &createReq)
	if err != nil {
		h.logger.Errorw("failed to create payment record, skipping event",
			"error", err,
			"invoice_id", flexpriceInvoiceID,
			"charge_id", charge.ID)
		return nil
	}

	h.logger.Infow("successfully created payment record",
		"payment_id", paymentResp.ID,
		"invoice_id", flexpriceInvoiceID,
		"charge_id", charge.ID)

	// Update payment to succeeded status (same pattern as Stripe)
	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusSucceeded)),
		GatewayPaymentID: lo.ToPtr(charge.ID),
		PaymentGateway:   lo.ToPtr(string(types.PaymentGatewayTypeNomod)),
		SucceededAt:      lo.ToPtr(now),
	}

	_, err = services.PaymentService.UpdatePayment(ctx, paymentResp.ID, updateReq)
	if err != nil {
		h.logger.Errorw("failed to update payment status to succeeded",
			"error", err,
			"payment_id", paymentResp.ID,
			"charge_id", charge.ID)
		// Continue even if update fails - payment record exists
	} else {
		h.logger.Infow("successfully updated payment to succeeded",
			"payment_id", paymentResp.ID,
			"charge_id", charge.ID)
	}

	// Reconcile invoice
	err = h.reconcileInvoice(ctx, flexpriceInvoiceID, amount, services)
	if err != nil {
		h.logger.Errorw("failed to reconcile invoice",
			"error", err,
			"invoice_id", flexpriceInvoiceID,
			"payment_id", paymentResp.ID)
		// Don't fail - invoice reconciliation is not critical for webhook success
	} else {
		h.logger.Infow("successfully reconciled invoice",
			"invoice_id", flexpriceInvoiceID,
			"payment_id", paymentResp.ID,
			"amount", amount.String())
	}

	return nil
}

// handlePaymentLinkPayment processes FlexPrice-initiated payment link payments
func (h *Handler) handlePaymentLinkPayment(ctx context.Context, charge *nomod.ChargeResponse, nomodPaymentLinkID string, services *ServiceDependencies) error {
	h.logger.Infow("processing payment link payment",
		"charge_id", charge.ID,
		"nomod_payment_link_id", nomodPaymentLinkID)

	// Find payment by gateway_tracking_id
	payment, err := services.PaymentService.GetPaymentByGatewayTrackingID(ctx, nomodPaymentLinkID, "nomod")
	if err != nil {
		h.logger.Errorw("failed to find FlexPrice payment for Nomod payment link, skipping event",
			"error", err,
			"nomod_payment_link_id", nomodPaymentLinkID,
			"charge_id", charge.ID)
		return nil
	}

	if payment == nil {
		h.logger.Warnw("no FlexPrice payment found for Nomod payment link, skipping event",
			"nomod_payment_link_id", nomodPaymentLinkID,
			"charge_id", charge.ID)
		return nil
	}

	h.logger.Infow("found FlexPrice payment for Nomod payment link",
		"flexprice_payment_id", payment.ID,
		"nomod_payment_link_id", nomodPaymentLinkID,
		"current_status", payment.PaymentStatus)

	// Check if already succeeded
	if payment.PaymentStatus == types.PaymentStatusSucceeded {
		h.logger.Infow("payment already succeeded, skipping event",
			"flexprice_payment_id", payment.ID,
			"nomod_payment_link_id", nomodPaymentLinkID,
			"charge_id", charge.ID)
		return nil
	}

	// Parse amount
	amount, err := decimal.NewFromString(charge.Total)
	if err != nil {
		h.logger.Errorw("failed to parse charge amount, skipping event",
			"error", err,
			"charge_id", charge.ID,
			"total", charge.Total)
		return nil
	}

	// Update payment to succeeded
	now := time.Now()
	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusSucceeded)),
		SucceededAt:      &now,
		GatewayPaymentID: &charge.ID,
	}

	h.logger.Infow("updating payment to succeeded",
		"flexprice_payment_id", payment.ID,
		"charge_id", charge.ID,
		"amount", amount.String())

	_, err = services.PaymentService.UpdatePayment(ctx, payment.ID, updateReq)
	if err != nil {
		h.logger.Errorw("failed to update payment, skipping event",
			"error", err,
			"flexprice_payment_id", payment.ID,
			"charge_id", charge.ID)
		return nil
	}

	h.logger.Infow("successfully updated payment to succeeded",
		"flexprice_payment_id", payment.ID,
		"charge_id", charge.ID)

	// Reconcile invoice
	err = h.reconcileInvoice(ctx, payment.DestinationID, amount, services)
	if err != nil {
		h.logger.Errorw("failed to reconcile invoice",
			"error", err,
			"invoice_id", payment.DestinationID,
			"payment_id", payment.ID)
		// Don't fail - invoice reconciliation is not critical for webhook success
	} else {
		h.logger.Infow("successfully reconciled invoice",
			"invoice_id", payment.DestinationID,
			"payment_id", payment.ID,
			"amount", amount.String())
	}

	return nil
}

// reconcileInvoice updates invoice payment status and amounts
func (h *Handler) reconcileInvoice(ctx context.Context, invoiceID string, paymentAmount decimal.Decimal, services *ServiceDependencies) error {
	h.logger.Infow("reconciling invoice with payment",
		"invoice_id", invoiceID,
		"payment_amount", paymentAmount.String())

	// Use the invoice service's ReconcilePaymentStatus method
	err := services.InvoiceService.ReconcilePaymentStatus(ctx, invoiceID, types.PaymentStatusSucceeded, &paymentAmount)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to reconcile invoice payment status").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":     invoiceID,
				"payment_amount": paymentAmount.String(),
			}).
			Mark(ierr.ErrDatabase)
	}

	h.logger.Infow("successfully reconciled invoice",
		"invoice_id", invoiceID,
		"payment_amount", paymentAmount.String())

	return nil
}
