package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration/stripe"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	stripeapi "github.com/stripe/stripe-go/v82"
)

// Handler handles Stripe webhook events
type Handler struct {
	client                       *stripe.Client
	customerSvc                  *stripe.CustomerService
	paymentSvc                   *stripe.PaymentService
	invoiceSyncSvc               *stripe.InvoiceSyncService
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewHandler creates a new Stripe webhook handler
func NewHandler(
	client *stripe.Client,
	customerSvc *stripe.CustomerService,
	paymentSvc *stripe.PaymentService,
	invoiceSyncSvc *stripe.InvoiceSyncService,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) *Handler {
	return &Handler{
		client:                       client,
		customerSvc:                  customerSvc,
		paymentSvc:                   paymentSvc,
		invoiceSyncSvc:               invoiceSyncSvc,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       logger,
	}
}

// HandleWebhookEvent processes a Stripe webhook event
func (h *Handler) HandleWebhookEvent(ctx context.Context, event *stripeapi.Event, environmentID string, services *ServiceDependencies) error {
	h.logger.Infow("processing Stripe webhook event",
		"event_id", event.ID,
		"event_type", event.Type,
		"environment_id", environmentID,
	)

	switch string(event.Type) {
	case string(types.WebhookEventTypeCustomerCreated):
		return h.handleCustomerCreated(ctx, event, environmentID, services)
	case string(types.WebhookEventTypePaymentIntentSucceeded):
		return h.handlePaymentIntentSucceeded(ctx, event, environmentID, services)
	case string(types.WebhookEventTypePaymentIntentPaymentFailed):
		return h.handlePaymentIntentPaymentFailed(ctx, event, environmentID, services)
	case string(types.WebhookEventTypeSetupIntentSucceeded):
		return h.handleSetupIntentSucceeded(ctx, event, environmentID, services)
	case string(types.WebhookEventTypeInvoicePaymentPaid):
		return h.handleInvoicePaymentPaid(ctx, event, environmentID, services)
	default:
		h.logger.Infow("unhandled Stripe webhook event type", "type", event.Type)
		return nil // Not an error, just unhandled
	}
}

// ServiceDependencies contains all service dependencies needed by webhook handlers
type ServiceDependencies struct {
	CustomerService interfaces.CustomerService
	PaymentService  interfaces.PaymentService
	InvoiceService  interfaces.InvoiceService
}

// handleCustomerCreated handles customer.created webhook
func (h *Handler) handleCustomerCreated(ctx context.Context, event *stripeapi.Event, environmentID string, services *ServiceDependencies) error {
	// Parse webhook to get customer data
	var stripeCustomer stripeapi.Customer
	err := json.Unmarshal(event.Data.Raw, &stripeCustomer)
	if err != nil {
		h.logger.Errorw("failed to parse customer from webhook", "error", err)
		return ierr.NewError("failed to parse customer data").
			WithHint("Invalid customer data in webhook").
			Mark(ierr.ErrValidation)
	}

	h.logger.Infow("received customer.created webhook",
		"stripe_customer_id", stripeCustomer.ID,
		"customer_email", stripeCustomer.Email,
		"environment_id", environmentID,
		"event_id", event.ID,
	)

	// Create customer in our system from Stripe data
	err = h.customerSvc.CreateCustomerFromStripe(ctx, &stripeCustomer, environmentID, services.CustomerService)
	if err != nil {
		h.logger.Errorw("failed to create customer from Stripe webhook",
			"error", err,
			"stripe_customer_id", stripeCustomer.ID,
			"event_id", event.ID)
		return ierr.WithError(err).
			WithHint("Failed to create customer from Stripe webhook").
			Mark(ierr.ErrSystem)
	}

	h.logger.Infow("successfully created customer from Stripe webhook",
		"stripe_customer_id", stripeCustomer.ID,
		"customer_email", stripeCustomer.Email,
		"event_id", event.ID)

	return nil
}

// handlePaymentIntentSucceeded handles payment_intent.succeeded webhook
func (h *Handler) handlePaymentIntentSucceeded(ctx context.Context, event *stripeapi.Event, environmentID string, services *ServiceDependencies) error {
	// Parse webhook to get payment intent ID
	var webhookPaymentIntent stripeapi.PaymentIntent
	err := json.Unmarshal(event.Data.Raw, &webhookPaymentIntent)
	if err != nil {
		h.logger.Errorw("failed to parse payment intent from webhook", "error", err)
		return ierr.NewError("failed to parse payment intent data").
			WithHint("Invalid payment intent data in webhook").
			Mark(ierr.ErrValidation)
	}

	paymentIntentID := webhookPaymentIntent.ID
	h.logger.Infow("received payment_intent.succeeded webhook",
		"payment_intent_id", paymentIntentID,
		"environment_id", environmentID,
		"event_id", event.ID,
		"event_type", event.Type,
	)

	// Fetch the latest payment intent data from Stripe API instead of relying on webhook data
	paymentIntent, err := h.paymentSvc.GetPaymentIntent(ctx, paymentIntentID, environmentID)
	if err != nil {
		h.logger.Errorw("failed to fetch payment intent from Stripe API",
			"error", err,
			"payment_intent_id", paymentIntentID,
			"event_id", event.ID)
		return ierr.WithError(err).
			WithHint("Failed to fetch payment intent from Stripe").
			Mark(ierr.ErrSystem)
	}

	h.logger.Infow("fetched payment intent from Stripe API",
		"payment_intent_id", paymentIntent.ID,
		"status", paymentIntent.Status,
		"amount", paymentIntent.Amount,
		"currency", paymentIntent.Currency,
		"metadata", paymentIntent.Metadata,
	)

	// Check if payment already exists with this payment intent ID
	exists, err := h.paymentSvc.PaymentExistsByGatewayPaymentID(ctx, paymentIntent.ID)
	if err != nil {
		h.logger.Errorw("failed to check if payment exists by gateway payment ID",
			"error", err,
			"payment_intent_id", paymentIntent.ID)
		// Continue processing on error
	} else if exists {
		h.logger.Infow("payment already exists for this payment intent, skipping",
			"payment_intent_id", paymentIntent.ID)
		return nil
	}

	// Check payment source to determine processing approach
	paymentSource := paymentIntent.Metadata["payment_source"]
	paymentType := paymentIntent.Metadata["payment_type"]

	if paymentSource == "flexprice" && paymentType == "checkout" {
		return h.handleFlexPriceCheckoutPayment(ctx, paymentIntent, services)
	} else if paymentSource == "flexprice" && paymentType == "charge" {
		return nil // No need to handle charge payments
	} else {
		// This is an external Stripe payment - process reconciliation
		h.logger.Infow("processing external Stripe payment - reconciling invoice",
			"payment_intent_id", paymentIntent.ID)

		// Parse the raw webhook data to get the invoice ID
		var webhookData struct {
			Invoice string `json:"invoice"`
		}
		err = json.Unmarshal(event.Data.Raw, &webhookData)
		if err != nil {
			h.logger.Errorw("failed to parse webhook data for invoice ID", "error", err)
			return ierr.WithError(err).Mark(ierr.ErrValidation)
		}

		stripeInvoiceID := webhookData.Invoice
		if stripeInvoiceID == "" {
			h.logger.Warnw("no Stripe invoice ID found in external payment",
				"payment_intent_id", paymentIntent.ID)
			return nil
		}

		h.logger.Infow("found Stripe invoice ID from webhook data",
			"payment_intent_id", paymentIntent.ID,
			"stripe_invoice_id", stripeInvoiceID)

		// Process external Stripe payment
		if err := h.paymentSvc.ProcessExternalStripePayment(ctx, paymentIntent, stripeInvoiceID, services.PaymentService, services.InvoiceService); err != nil {
			h.logger.Errorw("failed to process external Stripe payment",
				"error", err,
				"payment_intent_id", paymentIntent.ID,
				"stripe_invoice_id", stripeInvoiceID)
			return ierr.WithError(err).
				WithHint("Failed to process external payment").
				Mark(ierr.ErrSystem)
		}

		h.logger.Infow("successfully processed external Stripe payment",
			"payment_intent_id", paymentIntent.ID,
			"stripe_invoice_id", stripeInvoiceID)
		return nil
	}
}

// handleFlexPriceCheckoutPayment handles payment intents from FlexPrice checkout sessions
func (h *Handler) handleFlexPriceCheckoutPayment(ctx context.Context, paymentIntent *stripeapi.PaymentIntent, services *ServiceDependencies) error {
	// Get flexprice_payment_id from metadata
	flexpricePaymentID := ""
	if paymentID, exists := paymentIntent.Metadata["flexprice_payment_id"]; exists {
		flexpricePaymentID = paymentID
	}

	if flexpricePaymentID == "" {
		h.logger.Warnw("no flexprice_payment_id found in payment intent metadata",
			"payment_intent_id", paymentIntent.ID)
		return nil
	}

	h.logger.Infow("processing FlexPrice checkout payment",
		"payment_intent_id", paymentIntent.ID,
		"flexprice_payment_id", flexpricePaymentID,
		"amount", paymentIntent.Amount,
		"currency", paymentIntent.Currency)

	// Get payment record by flexprice_payment_id
	payment, err := services.PaymentService.GetPayment(ctx, flexpricePaymentID)
	if err != nil {
		h.logger.Errorw("failed to get payment record",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"payment_intent_id", paymentIntent.ID)
		return ierr.WithError(err).
			WithHint("Failed to get payment record").
			Mark(ierr.ErrSystem)
	}

	if payment == nil {
		h.logger.Warnw("no payment record found", "flexprice_payment_id", flexpricePaymentID, "payment_intent_id", paymentIntent.ID)
		return nil
	}

	h.logger.Infow("found payment record", "payment_id", payment.ID, "flexprice_payment_id", flexpricePaymentID)

	// Check if payment is already successful - prevent any updates to successful payments
	if payment.PaymentStatus == types.PaymentStatusSucceeded {
		h.logger.Warnw("payment is already successful, skipping update",
			"payment_id", payment.ID,
			"flexprice_payment_id", flexpricePaymentID,
			"payment_intent_id", paymentIntent.ID,
			"current_status", payment.PaymentStatus)
		return nil
	}

	// Since this is payment_intent.succeeded webhook, we know payment is successful
	paymentStatus := string(types.PaymentStatusSucceeded)

	// Extract payment method ID from payment intent
	var paymentMethodID string
	if paymentIntent.PaymentMethod != nil {
		paymentMethodID = paymentIntent.PaymentMethod.ID
	}

	h.logger.Infow("processing successful payment",
		"payment_intent_id", paymentIntent.ID,
		"payment_method_id", paymentMethodID,
		"amount", paymentIntent.Amount,
		"currency", paymentIntent.Currency)

	// Update payment record
	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus:    &paymentStatus,
		GatewayPaymentID: &paymentIntent.ID,
		PaymentMethodID:  &paymentMethodID,
	}

	if paymentMethodID != "" {
		updateReq.PaymentMethodID = &paymentMethodID
		// Set payment method as default if save_card_and_make_default was requested
		if payment.GatewayMetadata != nil {
			if saveCard, exists := payment.GatewayMetadata["save_card_and_make_default"]; exists && saveCard == "true" {
				// Get customer ID from invoice
				invoiceResp, err := services.InvoiceService.GetInvoice(ctx, payment.DestinationID)
				if err != nil {
					h.logger.Errorw("failed to get invoice for customer ID",
						"error", err,
						"payment_id", payment.ID,
						"invoice_id", payment.DestinationID)
				} else {
					h.logger.Infow("setting payment method as default for customer",
						"payment_id", payment.ID,
						"customer_id", invoiceResp.CustomerID,
						"payment_method_id", paymentMethodID)

					err := h.paymentSvc.SetDefaultPaymentMethod(ctx, invoiceResp.CustomerID, paymentMethodID, services.CustomerService)
					if err != nil {
						h.logger.Errorw("failed to set default payment method",
							"error", err,
							"payment_id", payment.ID,
							"customer_id", invoiceResp.CustomerID,
							"payment_method_id", paymentMethodID)
						// Don't fail the entire webhook processing
					} else {
						h.logger.Infow("successfully set default payment method",
							"payment_id", payment.ID,
							"customer_id", invoiceResp.CustomerID,
							"payment_method_id", paymentMethodID)
					}
				}
			}
		}
	}

	_, err = services.PaymentService.UpdatePayment(ctx, payment.ID, updateReq)
	if err != nil {
		h.logger.Errorw("failed to update payment record",
			"error", err,
			"payment_id", payment.ID,
			"new_status", paymentStatus)
		return ierr.WithError(err).
			WithHint("Failed to update payment record").
			Mark(ierr.ErrSystem)
	}

	h.logger.Infow("successfully updated payment record",
		"payment_id", payment.ID,
		"new_status", paymentStatus,
		"flexprice_payment_id", flexpricePaymentID)

	// If payment succeeded, attach to Stripe invoice if available and reconcile with FlexPrice invoice
	h.attachPaymentToStripeInvoiceAndReconcile(ctx, payment, paymentIntent, services)

	return nil
}

// handlePaymentIntentPaymentFailed handles payment_intent.payment_failed webhook
func (h *Handler) handlePaymentIntentPaymentFailed(ctx context.Context, event *stripeapi.Event, environmentID string, services *ServiceDependencies) error {
	// Parse webhook to get payment intent data
	var paymentIntent stripeapi.PaymentIntent
	err := json.Unmarshal(event.Data.Raw, &paymentIntent)
	if err != nil {
		h.logger.Errorw("failed to parse payment intent from webhook", "error", err)
		return ierr.NewError("failed to parse payment intent data").
			WithHint("Invalid payment intent data in webhook").
			Mark(ierr.ErrValidation)
	}

	paymentIntentID := paymentIntent.ID
	h.logger.Infow("received payment_intent.payment_failed webhook",
		"payment_intent_id", paymentIntentID,
		"environment_id", environmentID,
		"event_id", event.ID)

	// Check if this is a FlexPrice payment
	paymentSource := paymentIntent.Metadata["payment_source"]
	if paymentSource != "flexprice" {
		h.logger.Infow("ignoring payment intent failure from external source",
			"payment_intent_id", paymentIntentID,
			"payment_source", paymentSource)
		return nil
	}

	// Get flexprice_payment_id from metadata
	flexpricePaymentID := ""
	if paymentID, exists := paymentIntent.Metadata["flexprice_payment_id"]; exists {
		flexpricePaymentID = paymentID
	}

	if flexpricePaymentID == "" {
		h.logger.Warnw("no flexprice_payment_id found in payment intent metadata",
			"payment_intent_id", paymentIntentID)
		return nil
	}

	h.logger.Infow("processing FlexPrice payment failure",
		"payment_intent_id", paymentIntentID,
		"flexprice_payment_id", flexpricePaymentID)

	// Get payment record by flexprice_payment_id
	payment, err := services.PaymentService.GetPayment(ctx, flexpricePaymentID)
	if err != nil {
		h.logger.Errorw("failed to get payment record",
			"error", err,
			"flexprice_payment_id", flexpricePaymentID,
			"payment_intent_id", paymentIntentID)
		return ierr.WithError(err).
			WithHint("Failed to get payment record").
			Mark(ierr.ErrSystem)
	}

	if payment == nil {
		h.logger.Warnw("no payment record found", "flexprice_payment_id", flexpricePaymentID, "payment_intent_id", paymentIntentID)
		return nil
	}

	// Update payment status to failed
	paymentStatus := string(types.PaymentStatusFailed)
	now := time.Now()
	errorMsg := "Payment failed"
	if paymentIntent.LastPaymentError != nil && paymentIntent.LastPaymentError.Msg != "" {
		errorMsg = paymentIntent.LastPaymentError.Msg
	}

	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus: &paymentStatus,
		FailedAt:      &now,
		ErrorMessage:  &errorMsg,
	}

	_, err = services.PaymentService.UpdatePayment(ctx, payment.ID, updateReq)
	if err != nil {
		h.logger.Errorw("failed to update payment record for failed payment",
			"error", err,
			"payment_id", payment.ID,
			"payment_intent_id", paymentIntentID)
		return ierr.WithError(err).
			WithHint("Failed to update payment record").
			Mark(ierr.ErrSystem)
	}

	h.logger.Infow("successfully updated payment record for failed payment",
		"payment_id", payment.ID,
		"payment_intent_id", paymentIntentID,
		"new_status", paymentStatus,
		"error_message", errorMsg)

	return nil
}

// handleSetupIntentSucceeded handles setup_intent.succeeded webhook
func (h *Handler) handleSetupIntentSucceeded(ctx context.Context, event *stripeapi.Event, environmentID string, services *ServiceDependencies) error {
	// Parse webhook to get setup intent data
	var setupIntent stripeapi.SetupIntent
	err := json.Unmarshal(event.Data.Raw, &setupIntent)
	if err != nil {
		h.logger.Errorw("failed to parse setup intent from webhook", "error", err)
		return ierr.NewError("failed to parse setup intent data").
			WithHint("Invalid setup intent data in webhook").
			Mark(ierr.ErrValidation)
	}

	h.logger.Infow("received setup_intent.succeeded webhook",
		"setup_intent_id", setupIntent.ID,
		"status", setupIntent.Status,
		"environment_id", environmentID,
		"event_id", event.ID,
		"event_type", event.Type,
		"has_payment_method", setupIntent.PaymentMethod != nil,
		"payment_method_id", func() string {
			if setupIntent.PaymentMethod != nil {
				return setupIntent.PaymentMethod.ID
			}
			return ""
		}(),
		"metadata", setupIntent.Metadata,
	)

	// Check if setup intent succeeded and has a payment method
	if setupIntent.Status != stripeapi.SetupIntentStatusSucceeded || setupIntent.PaymentMethod == nil {
		h.logger.Infow("setup intent not in succeeded status or no payment method attached",
			"setup_intent_id", setupIntent.ID,
			"status", setupIntent.Status,
			"has_payment_method", setupIntent.PaymentMethod != nil)
		return nil
	}

	// Check if set_default was requested
	setAsDefault, exists := setupIntent.Metadata["set_default"]
	h.logger.Infow("checking set_default metadata",
		"setup_intent_id", setupIntent.ID,
		"set_default_exists", exists,
		"set_default_value", setAsDefault,
		"all_metadata", setupIntent.Metadata)

	if !exists || setAsDefault != "true" {
		h.logger.Infow("set_as_default not requested for this setup intent",
			"setup_intent_id", setupIntent.ID,
			"set_default_exists", exists,
			"set_default_value", setAsDefault)
		return nil
	}

	// Get customer ID from metadata
	customerID, exists := setupIntent.Metadata["customer_id"]
	if !exists {
		h.logger.Errorw("customer_id not found in setup intent metadata",
			"setup_intent_id", setupIntent.ID)
		return ierr.NewError("customer ID not found in setup intent metadata").
			WithHint("Customer ID is required to set default payment method").
			Mark(ierr.ErrValidation)
	}

	// Set the payment method as default
	paymentMethodID := setupIntent.PaymentMethod.ID
	h.logger.Infow("attempting to set payment method as default",
		"setup_intent_id", setupIntent.ID,
		"customer_id", customerID,
		"payment_method_id", paymentMethodID)

	err = h.paymentSvc.SetDefaultPaymentMethod(ctx, customerID, paymentMethodID, services.CustomerService)
	if err != nil {
		h.logger.Errorw("failed to set payment method as default",
			"error", err,
			"error_type", fmt.Sprintf("%T", err),
			"setup_intent_id", setupIntent.ID,
			"customer_id", customerID,
			"payment_method_id", paymentMethodID)
		return ierr.WithError(err).
			WithHint("Failed to set payment method as default").
			Mark(ierr.ErrSystem)
	}

	h.logger.Infow("successfully processed setup intent and set payment method as default",
		"setup_intent_id", setupIntent.ID,
		"customer_id", customerID,
		"payment_method_id", paymentMethodID)

	return nil
}

// Helper methods

// attachPaymentToStripeInvoiceAndReconcile attempts to attach payment to Stripe invoice and reconcile with FlexPrice invoice
func (h *Handler) attachPaymentToStripeInvoiceAndReconcile(ctx context.Context, payment *dto.PaymentResponse, paymentIntent *stripeapi.PaymentIntent, services *ServiceDependencies) {
	// Find Stripe invoice ID using entity integration mapping
	stripeInvoiceID, err := h.getStripeInvoiceIDFromMapping(ctx, payment.DestinationID)
	if err != nil {
		h.logger.Debugw("no Stripe invoice found for FlexPrice invoice, skipping attachment",
			"flexprice_invoice_id", payment.DestinationID,
			"payment_id", payment.ID,
			"error", err)
		return
	}

	h.logger.Infow("attempting to attach payment to Stripe invoice",
		"payment_id", payment.ID,
		"payment_intent_id", paymentIntent.ID,
		"stripe_invoice_id", stripeInvoiceID)

	// Get Stripe client
	stripeClient, _, err := h.client.GetStripeClient(ctx)
	if err != nil {
		h.logger.Errorw("failed to get Stripe client for invoice attachment",
			"error", err,
			"payment_id", payment.ID)
		return
	}

	if paymentIntent.ID != "" {
		err = h.paymentSvc.AttachPaymentToStripeInvoice(ctx, stripeClient, paymentIntent.ID, stripeInvoiceID)
	} else {
		h.logger.Warnw("no payment method found in payment intent, skipping Stripe invoice attachment",
			"payment_intent_id", paymentIntent.ID,
			"stripe_invoice_id", stripeInvoiceID)
		err = nil
	}

	if err != nil {
		h.logger.Errorw("failed to attach payment to Stripe invoice",
			"error", err,
			"payment_id", payment.ID,
			"payment_intent_id", paymentIntent.ID,
			"stripe_invoice_id", stripeInvoiceID)
		// Don't fail the entire webhook processing
	} else {
		h.logger.Infow("successfully attached payment to Stripe invoice",
			"payment_id", payment.ID,
			"payment_intent_id", paymentIntent.ID,
			"stripe_invoice_id", stripeInvoiceID)
	}

	// Reconcile with FlexPrice invoice
	// Convert amount from cents to decimal
	amount := decimal.NewFromInt(paymentIntent.Amount).Div(decimal.NewFromInt(100))
	err = h.paymentSvc.ReconcilePaymentWithInvoice(ctx, payment.ID, amount, services.PaymentService, services.InvoiceService)
	if err != nil {
		h.logger.Errorw("failed to reconcile payment with invoice",
			"error", err,
			"payment_id", payment.ID,
			"amount", amount.String())
		// Don't fail the entire webhook processing
	} else {
		h.logger.Infow("successfully reconciled payment with invoice",
			"payment_id", payment.ID,
			"amount", amount.String())
	}
}

// getStripeInvoiceIDFromMapping gets the Stripe invoice ID from entity integration mapping
func (h *Handler) getStripeInvoiceIDFromMapping(ctx context.Context, flexpriceInvoiceID string) (string, error) {
	// Find Stripe invoice ID using entity integration mapping (same pattern as StripeService)
	filter := &types.EntityIntegrationMappingFilter{
		EntityIDs:     []string{flexpriceInvoiceID},
		EntityType:    types.IntegrationEntityTypeInvoice,
		ProviderTypes: []string{string(types.SecretProviderStripe)},
		QueryFilter:   types.NewDefaultQueryFilter(),
	}

	mappings, err := h.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		h.logger.Debugw("failed to query entity integration mapping",
			"error", err,
			"flexprice_invoice_id", flexpriceInvoiceID)
		return "", ierr.WithError(err).Mark(ierr.ErrDatabase)
	}

	if len(mappings) == 0 {
		h.logger.Debugw("no Stripe invoice mapping found",
			"flexprice_invoice_id", flexpriceInvoiceID)
		return "", ierr.NewError("stripe invoice mapping not found").Mark(ierr.ErrNotFound)
	}

	stripeInvoiceID := mappings[0].ProviderEntityID
	h.logger.Debugw("found Stripe invoice mapping",
		"flexprice_invoice_id", flexpriceInvoiceID,
		"stripe_invoice_id", stripeInvoiceID)

	return stripeInvoiceID, nil
}

// handleInvoicePaymentPaid handles invoice.paid webhook for external Stripe payments without invoice field in payment_intent
func (h *Handler) handleInvoicePaymentPaid(ctx context.Context, event *stripeapi.Event, environmentID string, services *ServiceDependencies) error {
	// Parse it manually from the raw JSON since the invoice field is separate from id
	var rawData map[string]interface{}
	if err := json.Unmarshal(event.Data.Raw, &rawData); err != nil {
		h.logger.Errorw("failed to parse raw webhook data",
			"error", err,
			"event_id", event.ID)
		return ierr.WithError(err).Mark(ierr.ErrValidation)
	}

	// Extract invoice ID (the actual invoice ID, not the invoice payment ID)
	stripeInvoiceID := ""
	if invoiceID, exists := rawData["invoice"]; exists {
		if invoiceStr, ok := invoiceID.(string); ok {
			stripeInvoiceID = invoiceStr
		}
	}

	// Extract payment intent ID from nested payment object
	paymentIntentID := ""
	if payment, exists := rawData["payment"]; exists {
		if paymentMap, ok := payment.(map[string]interface{}); ok {
			if piID, exists := paymentMap["payment_intent"]; exists {
				if piStr, ok := piID.(string); ok {
					paymentIntentID = piStr
				}
			}
		}
	}

	h.logger.Infow("received invoice.paid webhook",
		"event_id", event.ID,
		"stripe_invoice_id", stripeInvoiceID,
		"payment_intent_id", paymentIntentID,
		"environment_id", environmentID)

	if stripeInvoiceID == "" || paymentIntentID == "" {
		h.logger.Warnw("missing invoice ID or payment intent ID in invoice.paid webhook",
			"stripe_invoice_id", stripeInvoiceID,
			"payment_intent_id", paymentIntentID)
		return nil
	}

	h.logger.Infow("processing invoice.paid webhook",
		"stripe_invoice_id", stripeInvoiceID,
		"payment_intent_id", paymentIntentID,
		"event_id", event.ID)

	// Check if payment already exists with this payment intent ID
	exists, err := h.paymentSvc.PaymentExistsByGatewayPaymentID(ctx, paymentIntentID)
	if err != nil {
		h.logger.Errorw("failed to check if payment exists by gateway payment ID",
			"error", err,
			"payment_intent_id", paymentIntentID)
		// Continue processing on error
	} else if exists {
		h.logger.Infow("payment already exists for this payment intent, skipping",
			"payment_intent_id", paymentIntentID,
			"stripe_invoice_id", stripeInvoiceID)
		return nil
	}

	// Get payment intent details from Stripe
	paymentIntent, err := h.paymentSvc.GetPaymentIntent(ctx, paymentIntentID, environmentID)
	if err != nil {
		h.logger.Errorw("failed to get payment intent from Stripe",
			"error", err,
			"payment_intent_id", paymentIntentID)
		return ierr.WithError(err).Mark(ierr.ErrSystem)
	}

	// Check payment source - skip FlexPrice payments as they're already processed
	paymentSource := paymentIntent.Metadata["payment_source"]
	if paymentSource == "flexprice" {
		h.logger.Infow("skipping FlexPrice-initiated payment to avoid double processing",
			"payment_intent_id", paymentIntentID)
		return nil
	}

	// Process external Stripe payment
	h.logger.Infow("processing external Stripe payment from invoice.paid webhook",
		"payment_intent_id", paymentIntentID,
		"stripe_invoice_id", stripeInvoiceID)

	if err := h.paymentSvc.ProcessExternalStripePayment(ctx, paymentIntent, stripeInvoiceID, services.PaymentService, services.InvoiceService); err != nil {
		h.logger.Errorw("failed to process external Stripe payment",
			"error", err,
			"payment_intent_id", paymentIntentID,
			"stripe_invoice_id", stripeInvoiceID)
		return ierr.WithError(err).Mark(ierr.ErrSystem)
	}

	h.logger.Infow("successfully processed invoice.paid webhook",
		"payment_intent_id", paymentIntentID,
		"stripe_invoice_id", stripeInvoiceID)

	return nil
}
