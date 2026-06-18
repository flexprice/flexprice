package webhook

import (
	"context"
	"time"

	apidto "github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/paymentmethod"
	"github.com/flexprice/flexprice/internal/integration/moyasar"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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
	log *logger.Logger,
) *Handler {
	return &Handler{
		client:                       client,
		paymentSvc:                   paymentSvc,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       log,
	}
}

// ServiceDependencies contains all service dependencies needed by webhook handlers
type ServiceDependencies = interfaces.ServiceDependencies

// HandleWebhookEvent processes a Moyasar webhook event.
// Always returns nil — we never return errors to Moyasar (prevent retries); all errors are logged.
func (h *Handler) HandleWebhookEvent(ctx context.Context, event *MoyasarWebhookEvent, _ string, services *ServiceDependencies) error {
	h.logger.Info(ctx, "processing Moyasar webhook event",
		"event_type", event.Type,
		"event_id", event.ID,
	)

	switch event.Type {
	case EventPaymentPaid, EventPaymentCaptured:
		h.handlePaymentPaid(ctx, event.Data, services)
	case EventPaymentFailed:
		h.handlePaymentFailed(ctx, event.Data, services)
	default:
		h.logger.Info(ctx, "ignoring unhandled event type", "type", event.Type)
	}

	return nil
}

// handlePaymentPaid is the single entry point for all paid/captured payment webhooks.
//
// Resolution order for the Flexprice payment:
//  1. metadata["flexprice_payment_id"] — always preferred; we inject this when creating
//     payments on our side (invoice link and saved-token charges). For Moyasar.js card-
//     tokenisation charges we pass it via the form metadata field.
//  2. invoice_id lookup via entity_integration_mapping — fallback for legacy payments or
//     externally-initiated Moyasar invoice payments where we never had a flexprice_payment_id.
func (h *Handler) handlePaymentPaid(ctx context.Context, payment PaymentEventData, services *ServiceDependencies) {
	h.logger.Info(ctx, "received payment_paid webhook",
		"moyasar_payment_id", payment.ID,
		"amount", payment.Amount,
		"currency", payment.Currency,
		"status", payment.Status,
	)

	// ── Path 1: flexprice_payment_id in metadata ─────────────────────────────
	if payment.Metadata != nil {
		if flexpricePaymentID := payment.Metadata["flexprice_payment_id"]; flexpricePaymentID != "" {
			h.handleByFlexpricePaymentID(ctx, payment, flexpricePaymentID, services)
			return
		}
	}

	// ── Path 2: Moyasar invoice_id → entity_integration_mapping lookup ───────
	if payment.InvoiceID != "" {
		h.handleByMoyasarInvoiceID(ctx, payment, services)
		return
	}

	h.logger.Warn(ctx, "webhook payment has no flexprice_payment_id or invoice_id, skipping",
		"moyasar_payment_id", payment.ID)
}

// handlePaymentFailed marks the Flexprice payment as FAILED when Moyasar fires payment_failed.
// Looks up by flexprice_payment_id from metadata (same as the paid path).
func (h *Handler) handlePaymentFailed(ctx context.Context, payment PaymentEventData, services *ServiceDependencies) {
	h.logger.Info(ctx, "received payment_failed webhook",
		"moyasar_payment_id", payment.ID,
		"status", payment.Status,
	)

	flexpricePaymentID := ""
	if payment.Metadata != nil {
		flexpricePaymentID = payment.Metadata["flexprice_payment_id"]
	}
	if flexpricePaymentID == "" {
		h.logger.Warn(ctx, "payment_failed has no flexprice_payment_id, nothing to update",
			"moyasar_payment_id", payment.ID)
		return
	}

	_, err := services.PaymentService.UpdatePayment(ctx, flexpricePaymentID, apidto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusFailed)),
		GatewayPaymentID: lo.ToPtr(payment.ID),
	})
	if err != nil {
		h.logger.Error(ctx, "failed to mark Flexprice payment as failed",
			"flexprice_payment_id", flexpricePaymentID,
			"moyasar_payment_id", payment.ID,
			"error", err)
		return
	}

	h.logger.Info(ctx, "marked Flexprice payment as failed",
		"flexprice_payment_id", flexpricePaymentID,
		"moyasar_payment_id", payment.ID)
}

// handleByFlexpricePaymentID resolves and processes a payment using the flexprice_payment_id
// stored in the Moyasar payment metadata.
func (h *Handler) handleByFlexpricePaymentID(ctx context.Context, payment PaymentEventData, flexpricePaymentID string, services *ServiceDependencies) {
	h.logger.Info(ctx, "handling webhook via flexprice_payment_id",
		"flexprice_payment_id", flexpricePaymentID,
		"moyasar_payment_id", payment.ID,
	)

	flexpricePayment, err := services.PaymentService.GetPayment(ctx, flexpricePaymentID)
	if err != nil {
		h.logger.Error(ctx, "failed to get Flexprice payment for webhook",
			"flexprice_payment_id", flexpricePaymentID,
			"error", err,
		)
		return
	}

	now := time.Now().UTC()
	_, err = services.PaymentService.UpdatePayment(ctx, flexpricePaymentID, apidto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusSucceeded)),
		GatewayPaymentID: lo.ToPtr(payment.ID),
		SucceededAt:      lo.ToPtr(now),
	})
	if err != nil {
		h.logger.Error(ctx, "failed to mark Flexprice payment as succeeded",
			"flexprice_payment_id", flexpricePaymentID,
			"error", err,
		)
		return
	}

	h.logger.Info(ctx, "marked Flexprice payment as succeeded",
		"flexprice_payment_id", flexpricePaymentID,
		"destination_type", flexpricePayment.DestinationType,
	)

	switch flexpricePayment.DestinationType {

	case types.PaymentDestinationTypeAuth:
		// Card tokenisation: activate the saved payment method token.
		h.activatePaymentMethodToken(ctx, flexpricePayment.DestinationID, flexpricePayment.Metadata, payment, services.PaymentMethodRepo)

	case types.PaymentDestinationTypeInvoice:
		// Autopay / invoice charge: reconcile the invoice.
		if flexpricePayment.DestinationID == "" {
			h.logger.Warn(ctx, "invoice payment has no destination_id, cannot reconcile",
				"flexprice_payment_id", flexpricePaymentID)
			return
		}
		amount := moyasar.ConvertFromSmallestUnitPublic(int64(payment.Amount), payment.Currency)
		if err := services.InvoiceService.ReconcilePaymentStatus(ctx, flexpricePayment.DestinationID, types.PaymentStatusSucceeded, &amount); err != nil {
			h.logger.Error(ctx, "failed to reconcile invoice payment",
				"invoice_id", flexpricePayment.DestinationID,
				"error", err,
			)
		} else {
			h.logger.Info(ctx, "reconciled invoice payment",
				"invoice_id", flexpricePayment.DestinationID,
				"amount", amount.String(),
			)
		}
	}
}

// activatePaymentMethodToken activates the INACTIVE payment method for the customer
// whose card was just tokenised.
//
// Token resolution order (most reliable first):
//  1. payment.Source.Token  — Moyasar returns this directly on save_card webhooks
//  2. payment.Source.GatewayID — alternative Moyasar field for the same token
//  3. metadata["token_id"]  — stored by us in saveMoyasarPaymentMethod as a fallback
func (h *Handler) activatePaymentMethodToken(
	ctx context.Context,
	customerID string,
	metadata types.Metadata,
	payment PaymentEventData,
	paymentMethodRepo paymentmethod.Repository,
) {
	tokenID := ""
	if payment.Source != nil {
		if payment.Source.Token != "" {
			tokenID = payment.Source.Token
		} else if payment.Source.GatewayID != "" {
			tokenID = payment.Source.GatewayID
		}
	}
	if tokenID == "" && metadata != nil {
		tokenID = metadata["token_id"]
	}
	if tokenID == "" {
		h.logger.Error(ctx, "cannot activate payment method: no token found in source or metadata",
			"customer_id", customerID,
			"moyasar_payment_id", payment.ID,
		)
		return
	}

	if paymentMethodRepo == nil {
		h.logger.Error(ctx, "paymentMethodRepo not available in webhook service dependencies")
		return
	}

	inactive := types.PaymentMethodStatusInactive
	methods, err := paymentMethodRepo.List(ctx, &types.PaymentMethodFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		CustomerID:  customerID,
		Status:      &inactive,
	})
	if err != nil {
		h.logger.Error(ctx, "failed to list inactive payment methods",
			"customer_id", customerID, "error", err)
		return
	}

	for _, method := range methods {
		if method.GatewayMethodID == tokenID {
			method.PaymentMethodStatus = types.PaymentMethodStatusActive
			if err := paymentMethodRepo.Update(ctx, method); err != nil {
				h.logger.Error(ctx, "failed to activate payment method",
					"payment_method_id", method.ID, "token_id", tokenID, "error", err)
			} else {
				h.logger.Info(ctx, "activated payment method",
					"payment_method_id", method.ID, "token_id", tokenID, "customer_id", customerID)
			}
			return
		}
	}

	h.logger.Error(ctx, "no inactive payment method found for token",
		"token_id", tokenID, "customer_id", customerID)
}

// handleByMoyasarInvoiceID handles a payment where the Moyasar invoice_id is known
// but there is no flexprice_payment_id in metadata (legacy / external flow).
func (h *Handler) handleByMoyasarInvoiceID(ctx context.Context, payment PaymentEventData, services *ServiceDependencies) {
	moyasarInvoiceID := payment.InvoiceID
	h.logger.Info(ctx, "handling webhook via Moyasar invoice_id (legacy path)",
		"moyasar_invoice_id", moyasarInvoiceID,
		"moyasar_payment_id", payment.ID,
	)

	// Resolve Flexprice invoice via entity_integration_mapping
	filter := &types.EntityIntegrationMappingFilter{
		ProviderTypes:     []string{string(types.SecretProviderMoyasar)},
		ProviderEntityIDs: []string{moyasarInvoiceID},
		EntityType:        types.IntegrationEntityTypeInvoice,
	}
	mappings, err := h.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		h.logger.Error(ctx, "failed to find mapping for Moyasar invoice",
			"moyasar_invoice_id", moyasarInvoiceID, "error", err)
		return
	}
	if len(mappings) == 0 {
		h.logger.Warn(ctx, "no Flexprice invoice found for Moyasar invoice, skipping",
			"moyasar_invoice_id", moyasarInvoiceID)
		return
	}

	flexpriceInvoiceID := mappings[0].EntityID
	h.logger.Info(ctx, "found Flexprice invoice",
		"flexprice_invoice_id", flexpriceInvoiceID,
		"moyasar_invoice_id", moyasarInvoiceID,
	)

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
			"flexprice_invoice_id", flexpriceInvoiceID,
			"moyasar_payment_id", payment.ID,
			"error", err,
		)
	} else {
		h.logger.Info(ctx, "processed invoice payment via legacy path",
			"flexprice_invoice_id", flexpriceInvoiceID,
			"moyasar_payment_id", payment.ID,
		)
	}
}
