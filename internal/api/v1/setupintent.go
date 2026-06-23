package v1

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/flexprice/flexprice/internal/api/dto"
	flexpricejwt "github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type SetupIntentHandler struct {
	integrationFactory *integration.Factory
	customerService    interfaces.CustomerService
	config             *config.Configuration
	log                *logger.Logger
}

func NewSetupIntentHandler(integrationFactory *integration.Factory, customerService interfaces.CustomerService, cfg *config.Configuration, log *logger.Logger) *SetupIntentHandler {
	return &SetupIntentHandler{
		integrationFactory: integrationFactory,
		customerService:    customerService,
		config:             cfg,
		log:                log,
	}
}

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

	// Validate the request
	if err := req.Validate(); err != nil {
		h.log.Error(c.Request.Context(), "Setup Intent request validation failed", "error", err)
		c.Error(err)
		return
	}

	switch req.Provider {
	case string(types.SecretProviderMoyasar):
		h.createMoyasarSetupIntent(c, customerID)
	default:
		h.createStripeSetupIntent(c, customerID, &req)
	}
}

func (h *SetupIntentHandler) createMoyasarSetupIntent(c *gin.Context, customerID string) {
	ctx := c.Request.Context()

	moyasarIntegration, err := h.integrationFactory.GetMoyasarIntegration(ctx)
	if err != nil {
		h.log.Error(ctx, "Failed to get Moyasar integration", "error", err)
		c.Error(err)
		return
	}

	flexpricePaymentID, publishableKey, err := moyasarIntegration.InitiateTokenization(ctx, customerID)
	if err != nil {
		h.log.Error(ctx, "Failed to initiate Moyasar tokenization", "error", err)
		c.Error(err)
		return
	}

	authProvider := flexpricejwt.NewProvider(h.config)
	checkoutToken, err := authProvider.GenerateCheckoutToken(map[string]interface{}{
		"publishable_key":      publishableKey,
		"flexprice_payment_id": flexpricePaymentID,
	})
	if err != nil {
		h.log.Error(ctx, "Failed to generate checkout token", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, dto.SetupIntentResponse{
		Status:        "pending_card_entry",
		CustomerID:    customerID,
		CheckoutToken: checkoutToken,
	})
}

func (h *SetupIntentHandler) createStripeSetupIntent(c *gin.Context, customerID string, req *dto.CreateSetupIntentRequest) {
	ctx := c.Request.Context()

	// Get Stripe integration
	stripeIntegration, err := h.integrationFactory.GetStripeIntegration(ctx)
	if err != nil {
		h.log.Error(ctx, "Failed to get Stripe integration", "error", err)
		c.Error(err)
		return
	}

	resp, err := stripeIntegration.PaymentSvc.SetupIntent(ctx, customerID, req, h.customerService)
	if err != nil {
		h.log.Error(ctx, "Failed to create Setup Intent", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
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
