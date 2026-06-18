package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	moyasardto "github.com/flexprice/flexprice/internal/integration/moyasar"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type SetupIntentHandler struct {
	integrationFactory *integration.Factory
	customerService    interfaces.CustomerService
	paymentService     interfaces.PaymentService
	log                *logger.Logger
}

func NewSetupIntentHandler(
	integrationFactory *integration.Factory,
	customerService interfaces.CustomerService,
	paymentService interfaces.PaymentService,
	log *logger.Logger,
) *SetupIntentHandler {
	return &SetupIntentHandler{
		integrationFactory: integrationFactory,
		customerService:    customerService,
		paymentService:     paymentService,
		log:                log,
	}
}

// CreateSetupIntentSession creates a provider-specific setup intent for saving a payment method.
// For Stripe: creates a Stripe SetupIntent and returns the checkout URL / client secret.
// For Moyasar: returns the publishable key needed by Moyasar.js to render the card form.
func (h *SetupIntentHandler) CreateSetupIntentSession(c *gin.Context) {
	// Get customer ID from URL path
	customerID := c.Param("id")
	if customerID == "" {
		h.log.Info(c.Request.Context(), "Missing customer_id in URL path")
		c.Error(ierr.NewError("customer_id is required").
			WithHint("Customer ID must be provided in the URL path").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.CreateSetupIntentRequest
	// Use strict JSON binding to reject unknown fields
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		h.log.Error(c.Request.Context(), "Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format. Unknown fields are not allowed").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		h.log.Error(c.Request.Context(), "Setup Intent request validation failed", "error", err)
		c.Error(err)
		return
	}

	switch types.PaymentMethodProvider(req.Provider) {
	case types.PaymentMethodProviderMoyasar:
		h.createMoyasarSetupIntent(c, customerID)
	case types.PaymentMethodProviderStripe:
		h.createStripeSetupIntent(c, customerID, &req)
	default:
		c.Error(ierr.NewError("unsupported provider for create setup intent").
			Mark(ierr.ErrValidation))
		return
	}
}

func (h *SetupIntentHandler) createStripeSetupIntent(c *gin.Context, customerID string, req *dto.CreateSetupIntentRequest) {
	stripeIntegration, err := h.integrationFactory.GetStripeIntegration(c.Request.Context())
	if err != nil {
		h.log.Error(c.Request.Context(), "Failed to get Stripe integration", "error", err)
		c.Error(err)
		return
	}

	resp, err := stripeIntegration.PaymentSvc.SetupIntent(c.Request.Context(), customerID, req, h.customerService)
	if err != nil {
		h.log.Error(c.Request.Context(), "Failed to create Stripe Setup Intent", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

func (h *SetupIntentHandler) createMoyasarSetupIntent(c *gin.Context, customerID string) {
	moyasarIntegration, err := h.integrationFactory.GetMoyasarIntegration(c.Request.Context())
	if err != nil {
		h.log.Error(c.Request.Context(), "Failed to get Moyasar integration", "error", err)
		c.Error(err)
		return
	}

	resp, err := moyasarIntegration.PaymentSvc.SetupIntent(c.Request.Context(), customerID, h.paymentService)
	if err != nil {
		h.log.Error(c.Request.Context(), "Failed to create Moyasar setup intent", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// SavePaymentMethod stores a provider-specific payment method ID for a customer so future
// invoices are charged automatically (autopay). For Moyasar the payment_method_id is the
// token ID returned by Moyasar.js after the customer enters their card.
func (h *SetupIntentHandler) SavePaymentMethod(c *gin.Context) {
	customerID := c.Param("id")
	if customerID == "" {
		c.Error(ierr.NewError("customer_id is required").
			WithHint("Customer ID must be provided in the URL path").
			Mark(ierr.ErrValidation))
		return
	}

	var req moyasardto.SavePaymentMethodRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request body").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	// provider is validated in Validate() — currently only Moyasar is supported
	h.saveMoyasarPaymentMethod(c, customerID, &req)
}

func (h *SetupIntentHandler) saveMoyasarPaymentMethod(c *gin.Context, customerID string, req *moyasardto.SavePaymentMethodRequest) {
	moyasarIntegration, err := h.integrationFactory.GetMoyasarIntegration(c.Request.Context())
	if err != nil {
		h.log.Error(c.Request.Context(), "Failed to get Moyasar integration", "error", err)
		c.Error(err)
		return
	}

	// Use a detached context so DB writes survive any client redirect.
	saveCtx := context.WithoutCancel(c.Request.Context())

	// Save token to payment_methods table as INACTIVE.
	// It will be activated when the payment is confirmed (via cron or webhook).
	if err := moyasarIntegration.CustomerSvc.SavePaymentMethod(saveCtx, customerID, req.PaymentMethodID, nil); err != nil {
		h.log.Error(c.Request.Context(), "Failed to save Moyasar payment method", "error", err)
		c.Error(err)
		return
	}

	// Link the Moyasar payment ID to our tracking payment and move it to PENDING.
	// The webhook (payment_paid) will advance it to SUCCEEDED and activate the token.
	if req.FlexpricePaymentID != "" && req.PaymentID != "" {
		_, updateErr := h.paymentService.UpdatePayment(saveCtx, req.FlexpricePaymentID, dto.UpdatePaymentRequest{
			PaymentStatus:    lo.ToPtr(string(types.PaymentStatusPending)),
			GatewayPaymentID: lo.ToPtr(req.PaymentID),
			Metadata: &types.Metadata{
				"token_id":  req.PaymentMethodID,
				"auth_type": "card_tokenization",
			},
		})
		if updateErr != nil {
			h.log.Error(saveCtx, "Failed to update auth payment to pending",
				"moyasar_payment_id", req.PaymentID,
				"error", updateErr)
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "succeeded"})
	// Note: token activation is handled by the Moyasar webhook (payment_paid event).
	// The webhook reads flexprice_payment_id from payment metadata, marks the payment
	// SUCCEEDED, and activates the payment method token.
}

func (h *SetupIntentHandler) ListCustomerPaymentMethods(c *gin.Context) {
	// Get customer ID from URL path
	customerID := c.Param("id")
	if customerID == "" {
		h.log.Info(c.Request.Context(), "Missing customer id in URL path")
		c.Error(ierr.NewError("customer id is required").
			WithHint("Customer ID must be provided in the URL path").
			Mark(ierr.ErrValidation))
		return
	}

	// Parse request body for provider and other parameters
	var req dto.ListPaymentMethodsRequest
	// Use strict JSON binding to reject unknown fields
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		h.log.Error(c.Request.Context(), "Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format. Unknown fields are not allowed.").
			Mark(ierr.ErrValidation))
		return
	}

	// Add query parameters for pagination
	req.StartingAfter = c.Query("starting_after")
	req.EndingBefore = c.Query("ending_before")

	// Parse limit parameter from query
	if limitStr := c.Query("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			req.Limit = limit
		} else {
			h.log.Error(c.Request.Context(), "Invalid limit parameter", "limit", limitStr, "error", err)
			c.Error(ierr.NewError("invalid limit parameter").
				WithHint("Limit must be a valid integer").
				Mark(ierr.ErrValidation))
			return
		}
	}

	// Validate the request
	if err := req.Validate(); err != nil {
		h.log.Error(c.Request.Context(), "List Payment Methods request validation failed", "error", err)
		c.Error(err)
		return
	}

	// Get Stripe integration
	stripeIntegration, err := h.integrationFactory.GetStripeIntegration(c.Request.Context())
	if err != nil {
		h.log.Error(c.Request.Context(), "Failed to get Stripe integration", "error", err)
		c.Error(err)
		return
	}

	resp, err := stripeIntegration.PaymentSvc.ListCustomerPaymentMethods(c.Request.Context(), customerID, &req, h.customerService)
	if err != nil {
		h.log.Error(c.Request.Context(), "Failed to list Customer Payment Methods", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
