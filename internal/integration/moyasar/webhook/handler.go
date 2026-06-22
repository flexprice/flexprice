package webhook

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/paymentmethod"
	"github.com/flexprice/flexprice/internal/integration/ledger"
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
	ledger                       *ledger.PaymentsLedger
	logger                       *logger.Logger
}

// NewHandler creates a new Moyasar webhook handler
func NewHandler(
	client moyasar.MoyasarClient,
	paymentSvc *moyasar.PaymentService,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	paymentMethodRepo paymentmethod.Repository,
	ledger *ledger.PaymentsLedger,
	logger *logger.Logger,
) *Handler {
	return &Handler{
		client:                       client,
		paymentSvc:                   paymentSvc,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		paymentMethodRepo:            paymentMethodRepo,
		ledger:                       ledger,
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

	switch event.Type {
	case EventPaymentPaid, EventPaymentCaptured:
		return h.handlePaymentPaid(ctx, event, environmentID, services)
	case EventPaymentFailed:
		return h.handlePaymentFailed(ctx, event, services)
	}

	h.logger.Info(ctx, "ignoring unhandled event type", "type", event.Type)
	return nil
}

// handlePaymentPaid dispatches payment_paid / payment_captured events.
//
// Priority:
//  1. flexprice_payment_id in metadata → ledger-managed payment (AUTH or INVOICE autopay)
//  2. invoice_id in payment body      → Moyasar invoice-link flow (external / manual pay)
func (h *Handler) handlePaymentPaid(ctx context.Context, event *MoyasarWebhookEvent, environmentID string, services *ServiceDependencies) error {
	payment := event.Data

	h.logger.Info(ctx, "received payment_paid webhook",
		"moyasar_payment_id", payment.ID,
		"amount", payment.Amount,
		"currency", payment.Currency,
		"status", payment.Status,
		"environment_id", environmentID,
	)

	// Path 1: ledger-managed payment — flexprice_payment_id is the anchor
	if payment.Metadata != nil {
		if flexpricePaymentID := payment.Metadata["flexprice_payment_id"]; flexpricePaymentID != "" {
			return h.handleLedgerPayment(ctx, payment, flexpricePaymentID, services)
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

// handleLedgerPayment handles payments that Flexprice initiated (AUTH or INVOICE autopay).
// The flexprice_payment_id in metadata is the cross-system anchor.
func (h *Handler) handleLedgerPayment(ctx context.Context, payment PaymentEventData, flexpricePaymentID string, services *ServiceDependencies) error {
	h.logger.Info(ctx, "handling ledger-managed payment",
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

	if err := h.ledger.RecordPaymentSuccess(ctx, ledger.RecordPaymentSuccessParams{
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
	case types.PaymentDestinationTypeAuth:
		h.activatePaymentMethod(ctx, payment, flexpricePayment.DestinationID)
		h.voidOrRefundAuthPayment(ctx, flexpricePaymentID, payment.ID)
	case types.PaymentDestinationTypeInvoice:
		// Invoice reconciliation is handled inside RecordPaymentSuccess via InvoiceService
	default:
		h.logger.Info(ctx, "unhandled destination type in ledger payment",
			"destination_type", flexpricePayment.DestinationType,
			"flexprice_payment_id", flexpricePaymentID,
		)
	}

	return nil
}

// activatePaymentMethod saves the Moyasar token from the webhook source as an ACTIVE PaymentMethod.
// Idempotent: skips if a method with the same token already exists for the customer.
func (h *Handler) activatePaymentMethod(ctx context.Context, payment PaymentEventData, customerID string) {
	if payment.Source == nil || payment.Source.Token == "" {
		h.logger.Error(ctx, "no token in webhook source, cannot activate payment method",
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
			"token", token,
			"error", err,
		)
		return
	}
	if count > 0 {
		h.logger.Info(ctx, "payment method already exists for token, skipping",
			"customer_id", customerID,
			"token", token,
		)
		return
	}

	paymentMethod := &paymentmethod.PaymentMethod{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PAYMENT_METHOD),
		CustomerID:          customerID,
		Type:                types.PaymentMethodTypeCard,
		Gateway:             string(types.SecretProviderMoyasar),
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
			"token", token,
			"error", err,
		)
		return
	}

	h.logger.Info(ctx, "payment method activated",
		"customer_id", customerID,
		"payment_method_id", paymentMethod.ID,
		"token", token,
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
		if ledgerErr := h.ledger.RecordPaymentVoided(ctx, ledger.RecordPaymentVoidedParams{
			FlexpricePaymentID: flexpricePaymentID,
			GatewayPaymentID:   gatewayPaymentID,
			VoidedAt:           time.Now().UTC(),
		}); ledgerErr != nil {
			h.logger.Error(ctx, "failed to record payment voided in ledger",
				"flexprice_payment_id", flexpricePaymentID,
				"gateway_payment_id", gatewayPaymentID,
				"error", ledgerErr,
			)
		} else {
			h.logger.Info(ctx, "AUTH payment voided successfully",
				"flexprice_payment_id", flexpricePaymentID,
				"gateway_payment_id", gatewayPaymentID,
			)
		}
		return
	}

	// Void failed — try full refund
	h.logger.Info(ctx, "void failed, attempting full refund",
		"flexprice_payment_id", flexpricePaymentID,
		"gateway_payment_id", gatewayPaymentID,
	)

	if _, refundErr := h.client.RefundPayment(ctx, gatewayPaymentID, 0); refundErr != nil {
		h.logger.Error(ctx, "void and refund both failed for AUTH payment",
			"flexprice_payment_id", flexpricePaymentID,
			"gateway_payment_id", gatewayPaymentID,
			"error", refundErr,
		)
		return
	}

	if ledgerErr := h.ledger.RecordPaymentRefunded(ctx, ledger.RecordPaymentRefundedParams{
		FlexpricePaymentID: flexpricePaymentID,
		GatewayPaymentID:   gatewayPaymentID,
		RefundedAt:         time.Now().UTC(),
	}); ledgerErr != nil {
		h.logger.Error(ctx, "failed to record payment refunded in ledger",
			"flexprice_payment_id", flexpricePaymentID,
			"gateway_payment_id", gatewayPaymentID,
			"error", ledgerErr,
		)
		return
	}

	h.logger.Info(ctx, "AUTH payment refunded successfully",
		"flexprice_payment_id", flexpricePaymentID,
		"gateway_payment_id", gatewayPaymentID,
	)
}

// handleInvoicePayment handles payments made via Moyasar-hosted invoice page (external flow).
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

	if err := h.paymentSvc.ProcessExternalMoyasarPayment(ctx, moyasarPayment, flexpriceInvoiceID, services.PaymentService, services.InvoiceService); err != nil {
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
// For ledger-managed payments, records the failure so the Flexprice payment
// transitions to FAILED and the invoice remains unpaid for retry/manual action.
func (h *Handler) handlePaymentFailed(ctx context.Context, event *MoyasarWebhookEvent, services *ServiceDependencies) error {
	payment := event.Data

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

	failedAt := time.Now().UTC()

	// Extract error message from source if available
	errorMessage := "payment failed"
	if payment.Source != nil && payment.Source.Message != "" {
		errorMessage = payment.Source.Message
	}

	if err := h.ledger.RecordPaymentFailure(ctx, ledger.RecordPaymentFailureParams{
		FlexpricePaymentID: flexpricePaymentID,
		GatewayPaymentID:   payment.ID,
		FailedAt:           failedAt,
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
