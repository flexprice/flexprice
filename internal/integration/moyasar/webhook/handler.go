package webhook

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/paymentmethod"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration/payments"
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
	paymentMethodRepo            paymentmethod.Repository
	lifecycle                    *payments.PaymentLifecycle
	logger                       *logger.Logger
}

// NewHandler creates a new Moyasar webhook handler
func NewHandler(
	client moyasar.MoyasarClient,
	paymentSvc *moyasar.PaymentService,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	paymentMethodRepo paymentmethod.Repository,
	lifecycle *payments.PaymentLifecycle,
	logger *logger.Logger,
) *Handler {
	return &Handler{
		client:                       client,
		paymentSvc:                   paymentSvc,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		paymentMethodRepo:            paymentMethodRepo,
		lifecycle:                    lifecycle,
		logger:                       logger,
	}
}

// ServiceDependencies contains all service dependencies needed by webhook handlers
type ServiceDependencies = interfaces.ServiceDependencies

// HandleWebhookEvent processes a Moyasar webhook event.
// Never returns an error to the caller — Moyasar must always receive 200 OK.
// All errors are logged internally.
func (h *Handler) HandleWebhookEvent(ctx context.Context, event *MoyasarWebhookEvent, environmentID string, services *ServiceDependencies) error {
	h.logger.Info(ctx, "processing Moyasar webhook event",
		"event_type", event.Type,
		"event_id", event.ID,
		"environment_id", environmentID,
		"created_at", event.CreatedAt,
	)

	// Use the webhook payload only as a trigger — fetch the authoritative payment
	// state from Moyasar before acting so we are never misled by tampered payloads.
	paymentID := event.Data.ID
	if paymentID == "" {
		h.logger.Info(ctx, "webhook event has no payment ID, skipping", "event_id", event.ID)
		return nil
	}

	fetchedMoyasarPayment, err := h.client.GetPayment(ctx, paymentID)
	if err != nil {
		if ierr.IsNotFound(err) {
			// Payment genuinely absent in Moyasar — safe to skip.
			h.logger.Info(ctx, "payment not found in Moyasar, skipping",
				"moyasar_payment_id", paymentID,
			)
			return nil
		}
		// Transient failure (network, 5xx) — return error so Moyasar retries the webhook.
		h.logger.Error(ctx, "failed to fetch payment from Moyasar for verification",
			"moyasar_payment_id", paymentID,
			"error", err,
		)
		return err
	}
	if fetchedMoyasarPayment == nil {
		h.logger.Info(ctx, "Moyasar returned nil payment, skipping", "moyasar_payment_id", paymentID)
		return nil
	}

	payment := &moyasar.MoyasarPayment{
		ID:             fetchedMoyasarPayment.ID,
		Status:         fetchedMoyasarPayment.Status,
		Amount:         fetchedMoyasarPayment.Amount,
		Fee:            fetchedMoyasarPayment.Fee,
		Currency:       fetchedMoyasarPayment.Currency,
		RefundedAmount: fetchedMoyasarPayment.RefundedAmount,
		RefundedAt:     fetchedMoyasarPayment.RefundedAt,
		CapturedAmount: fetchedMoyasarPayment.CapturedAmount,
		CapturedAt:     fetchedMoyasarPayment.CapturedAt,
		VoidedAt:       fetchedMoyasarPayment.VoidedAt,
		Description:    fetchedMoyasarPayment.Description,
		InvoiceID:      fetchedMoyasarPayment.InvoiceID,
		CreatedAt:      fetchedMoyasarPayment.CreatedAt,
		UpdatedAt:      fetchedMoyasarPayment.UpdatedAt,
		Metadata:       fetchedMoyasarPayment.Metadata,
	}

	if fetchedMoyasarPayment.Source != nil {
		payment.Source = &moyasar.PaymentSource{
			Type:      fetchedMoyasarPayment.Source.Type,
			Company:   fetchedMoyasarPayment.Source.Company,
			Name:      fetchedMoyasarPayment.Source.Name,
			Number:    fetchedMoyasarPayment.Source.Number,
			Token:     fetchedMoyasarPayment.Source.Token,
			GatewayID: fetchedMoyasarPayment.Source.GatewayID,
			Message:   fetchedMoyasarPayment.Source.Message,
		}
	}

	switch event.Type {
	case EventPaymentPaid, EventPaymentCaptured:
		return h.handlePaymentPaid(ctx, payment, environmentID, services)
	case EventPaymentFailed:
		return h.handlePaymentFailed(ctx, payment, services)
	}

	h.logger.Info(ctx, "ignoring unhandled event type", "type", event.Type)
	return nil
}

// handlePaymentPaid dispatches payment_paid / payment_captured events.
//
// Priority:
//  1. flexprice_payment_id in metadata → lifecycle-managed payment (CUSTOMER or INVOICE autopay)
//  2. invoice_id in payment body      → Moyasar invoice-link flow (external / manual pay)
func (h *Handler) handlePaymentPaid(ctx context.Context, payment *moyasar.MoyasarPayment, environmentID string, services *ServiceDependencies) error {
	h.logger.Info(ctx, "received payment_paid webhook",
		"moyasar_payment_id", payment.ID,
		"amount", payment.Amount,
		"currency", payment.Currency,
		"status", payment.Status,
		"environment_id", environmentID,
	)

	// Path 1: lifecycle-managed payment — flexprice_payment_id is the anchor
	if payment.Metadata != nil {
		if flexpricePaymentID := payment.Metadata["flexprice_payment_id"]; flexpricePaymentID != "" {
			return h.handlePaymentLifecycle(ctx, payment, flexpricePaymentID, services)
		}
	}

	// Path 2: Moyasar invoice-link flow (customer paid via hosted invoice page)
	if payment.InvoiceID != "" {
		return h.handleInvoicePayment(ctx, payment, services)
	}

	h.logger.Info(ctx, "webhook payment has no known anchor, skipping",
		"moyasar_payment_id", payment.ID)
	return nil
}

// handlePaymentLifecycle handles payments that Flexprice initiated (CUSTOMER tokenization or INVOICE autopay).
// The flexprice_payment_id in metadata is the cross-system anchor.
func (h *Handler) handlePaymentLifecycle(ctx context.Context, payment *moyasar.MoyasarPayment, flexpricePaymentID string, services *ServiceDependencies) error {
	h.logger.Info(ctx, "handling lifecycle-managed payment",
		"flexprice_payment_id", flexpricePaymentID,
		"moyasar_payment_id", payment.ID,
	)

	flexpricePayment, err := services.PaymentService.GetPayment(ctx, flexpricePaymentID)
	if err != nil {
		h.logger.Error(ctx, "failed to fetch flexprice payment",
			"flexprice_payment_id", flexpricePaymentID,
			"error", err,
		)
		return nil
	}

	now := time.Now().UTC()

	if err := h.lifecycle.RecordPaymentSuccess(ctx, payments.RecordPaymentSuccessParams{
		FlexpricePaymentID: flexpricePaymentID,
		GatewayPaymentID:   payment.ID,
		SucceededAt:        now,
	}); err != nil {
		h.logger.Error(ctx, "failed to record payment success",
			"flexprice_payment_id", flexpricePaymentID,
			"moyasar_payment_id", payment.ID,
			"error", err,
		)
		return nil
	}

	switch flexpricePayment.DestinationType {
	case types.PaymentDestinationTypeCustomer:
		h.activatePaymentMethod(ctx, payment, flexpricePayment.DestinationID)
		h.voidOrRefundAuthPayment(ctx, flexpricePaymentID, payment.ID)
	case types.PaymentDestinationTypeInvoice:
		// Invoice reconciliation is handled inside RecordPaymentSuccess via InvoiceService.
		// Sync the Moyasar payment status into the Flexprice invoice metadata so the UI reflects the current state.
		if syncErr := h.paymentSvc.SyncMoyasarInvoiceStatus(ctx, flexpricePayment.DestinationID, payment.Status, services.InvoiceService); syncErr != nil {
			h.logger.Error(ctx, "failed to sync Moyasar invoice status to invoice metadata",
				"flexprice_invoice_id", flexpricePayment.DestinationID,
				"moyasar_payment_id", payment.ID,
				"error", syncErr,
			)
		}
	default:
		h.logger.Info(ctx, "unhandled destination type in lifecycle payment",
			"destination_type", flexpricePayment.DestinationType,
			"flexprice_payment_id", flexpricePaymentID,
		)
	}

	return nil
}

// activatePaymentMethod saves the Moyasar token from the payment source as an ACTIVE PaymentMethod.
// Idempotent: skips if a method with the same token already exists for the customer.
func (h *Handler) activatePaymentMethod(ctx context.Context, payment *moyasar.MoyasarPayment, customerID string) {
	if payment.Source == nil || payment.Source.Token == "" {
		h.logger.Error(ctx, "no token in payment source, cannot activate payment method",
			"moyasar_payment_id", payment.ID,
			"customer_id", customerID,
			"error", "missing token in payment source",
		)
		return
	}

	token := payment.Source.Token

	// Idempotency: skip if this exact token is already saved for this customer
	count, err := h.paymentMethodRepo.Count(ctx, &types.PaymentMethodFilter{
		GatewayMethodID: &token,
		CustomerID:      &customerID,
	})
	if err != nil {
		h.logger.Error(ctx, "failed to check existing payment methods",
			"customer_id", customerID,
			"error", err,
		)
		return
	}
	if count > 0 {
		h.logger.Info(ctx, "payment method already exists for token, skipping",
			"customer_id", customerID,
		)
		return
	}

	paymentMethod := &paymentmethod.PaymentMethod{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PAYMENT_METHOD),
		CustomerID:          customerID,
		Type:                types.PaymentMethodTypeCard,
		Gateway:             types.PaymentGatewayTypeMoyasar,
		GatewayMethodID:     token,
		PaymentMethodStatus: types.PaymentMethodStatusActive,
		IsDefault:           false,
		MethodDetails: map[string]interface{}{
			"company": payment.Source.Company,
			"name":    payment.Source.Name,
			"number":  payment.Source.Number,
		},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	if err := h.paymentMethodRepo.Create(ctx, paymentMethod); err != nil {
		h.logger.Error(ctx, "failed to create payment method",
			"customer_id", customerID,
			"error", err,
		)
		return
	}

	h.logger.Info(ctx, "payment method activated",
		"customer_id", customerID,
		"payment_method_id", paymentMethod.ID,
	)
}

// voidOrRefundAuthPayment attempts to void an AUTH payment, falling back to a full refund.
// Errors are logged but not returned so the webhook always returns 200.
func (h *Handler) voidOrRefundAuthPayment(ctx context.Context, flexpricePaymentID string, gatewayPaymentID string) {
	h.logger.Info(ctx, "attempting to void AUTH payment",
		"flexprice_payment_id", flexpricePaymentID,
		"gateway_payment_id", gatewayPaymentID,
	)

	// Try void first
	if _, voidErr := h.client.VoidPayment(ctx, gatewayPaymentID); voidErr == nil {
		if lifecycleErr := h.lifecycle.RecordPaymentVoided(ctx, payments.RecordPaymentVoidedParams{
			FlexpricePaymentID: flexpricePaymentID,
			GatewayPaymentID:   gatewayPaymentID,
			VoidedAt:           time.Now().UTC(),
		}); lifecycleErr != nil {
			h.logger.Error(ctx, "failed to record payment voided",
				"flexprice_payment_id", flexpricePaymentID,
				"gateway_payment_id", gatewayPaymentID,
				"error", lifecycleErr,
			)
		} else {
			h.logger.Info(ctx, "AUTH payment voided successfully",
				"flexprice_payment_id", flexpricePaymentID,
				"gateway_payment_id", gatewayPaymentID,
			)
		}
		return
	} else {
		h.logger.Info(ctx, "void attempt failed, falling back to refund",
			"flexprice_payment_id", flexpricePaymentID,
			"gateway_payment_id", gatewayPaymentID,
			"error", voidErr,
		)
	}

	if _, refundErr := h.client.RefundPayment(ctx, gatewayPaymentID, 0); refundErr != nil {
		h.logger.Error(ctx, "void and refund both failed for AUTH payment",
			"flexprice_payment_id", flexpricePaymentID,
			"gateway_payment_id", gatewayPaymentID,
			"error", refundErr,
		)
		return
	}

	if lifecycleErr := h.lifecycle.RecordPaymentRefunded(ctx, payments.RecordPaymentRefundedParams{
		FlexpricePaymentID: flexpricePaymentID,
		GatewayPaymentID:   gatewayPaymentID,
		RefundedAt:         time.Now().UTC(),
	}); lifecycleErr != nil {
		h.logger.Error(ctx, "failed to record payment refunded",
			"flexprice_payment_id", flexpricePaymentID,
			"gateway_payment_id", gatewayPaymentID,
			"error", lifecycleErr,
		)
		return
	}

	h.logger.Info(ctx, "AUTH payment refunded successfully",
		"flexprice_payment_id", flexpricePaymentID,
		"gateway_payment_id", gatewayPaymentID,
	)
}

// handleInvoicePayment handles payments made via Moyasar-hosted invoice page (external flow).
func (h *Handler) handleInvoicePayment(ctx context.Context, payment *moyasar.MoyasarPayment, services *ServiceDependencies) error {
	moyasarInvoiceID := payment.InvoiceID
	h.logger.Info(ctx, "processing Moyasar invoice payment",
		"moyasar_payment_id", payment.ID,
		"moyasar_invoice_id", moyasarInvoiceID)

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

	if err := h.paymentSvc.ProcessExternalMoyasarPayment(ctx, payment, flexpriceInvoiceID, services.PaymentService, services.InvoiceService); err != nil {
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

// handlePaymentFailed handles payment_failed events.
// For lifecycle-managed payments, records the failure so the Flexprice payment
// transitions to FAILED and the invoice remains unpaid for retry/manual action.
func (h *Handler) handlePaymentFailed(ctx context.Context, payment *moyasar.MoyasarPayment, services *ServiceDependencies) error {
	h.logger.Info(ctx, "received payment_failed webhook",
		"moyasar_payment_id", payment.ID,
		"status", payment.Status,
	)

	if payment.Metadata == nil {
		h.logger.Info(ctx, "payment_failed has no metadata, skipping", "moyasar_payment_id", payment.ID)
		return nil
	}

	flexpricePaymentID := payment.Metadata["flexprice_payment_id"]
	if flexpricePaymentID == "" {
		h.logger.Info(ctx, "payment_failed has no flexprice_payment_id, skipping", "moyasar_payment_id", payment.ID)
		return nil
	}

	errorMessage := "payment failed"
	if payment.Source != nil && payment.Source.Message != "" {
		errorMessage = payment.Source.Message
	}

	if err := h.lifecycle.RecordPaymentFailure(ctx, payments.RecordPaymentFailureParams{
		FlexpricePaymentID: flexpricePaymentID,
		GatewayPaymentID:   payment.ID,
		FailedAt:           time.Now().UTC(),
		ErrorMessage:       errorMessage,
	}); err != nil {
		h.logger.Error(ctx, "failed to record payment failure",
			"flexprice_payment_id", flexpricePaymentID,
			"moyasar_payment_id", payment.ID,
			"error", err,
		)
		return nil
	}

	h.logger.Info(ctx, "payment failure recorded",
		"flexprice_payment_id", flexpricePaymentID,
		"moyasar_payment_id", payment.ID,
	)
	return nil
}
