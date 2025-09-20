package v1

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

type SetupIntentHandler struct {
	stripeService *service.StripeService
	log           *logger.Logger
}

func NewSetupIntentHandler(stripeService *service.StripeService, log *logger.Logger) *SetupIntentHandler {
	return &SetupIntentHandler{
		stripeService: stripeService,
		log:           log,
	}
}

// @Summary Create a Setup Intent session
// @Description Create a Setup Intent with checkout session for saving payment methods (supports multiple payment providers)
// @Tags Payments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Customer ID"
// @Param setup_intent body dto.CreateSetupIntentRequest true "Setup Intent configuration"
// @Success 201 {object} dto.SetupIntentResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /payments/customers/{id}/setup/intent [post]
func (h *SetupIntentHandler) CreateSetupIntentSession(c *gin.Context) {
	// Get customer ID from URL path
	customerID := c.Param("id")
	if customerID == "" {
		h.log.Error("Missing customer_id in URL path")
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
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format. Unknown fields are not allowed").
			Mark(ierr.ErrValidation))
		return
	}

	// Validate the request
	if err := req.Validate(); err != nil {
		h.log.Error("Setup Intent request validation failed", "error", err)
		c.Error(err)
		return
	}

	resp, err := h.stripeService.SetupIntent(c.Request.Context(), customerID, &req)
	if err != nil {
		h.log.Error("Failed to create Setup Intent", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary List saved payment methods for a customer
// @Description List only successfully saved payment methods for a customer (clean list without failed attempts)
// @Tags Payments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Customer ID"
// @Param request body dto.ListPaymentMethodsRequest true "Payment methods request"
// @Param limit query int false "Number of results to return (default: 10, max: 100)"
// @Param starting_after query string false "Pagination cursor for results after this ID"
// @Param ending_before query string false "Pagination cursor for results before this ID"
// @Success 200 {object} dto.MultiProviderPaymentMethodsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /payments/customers/{id}/methods [get]
func (h *SetupIntentHandler) ListCustomerPaymentMethods(c *gin.Context) {
	// Get customer ID from URL path
	customerID := c.Param("id")
	if customerID == "" {
		h.log.Error("Missing customer id in URL path")
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
		h.log.Error("Failed to bind JSON", "error", err)
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
			h.log.Error("Invalid limit parameter", "limit", limitStr, "error", err)
			c.Error(ierr.NewError("invalid limit parameter").
				WithHint("Limit must be a valid integer").
				Mark(ierr.ErrValidation))
			return
		}
	}

	// Validate the request
	if err := req.Validate(); err != nil {
		h.log.Error("List Payment Methods request validation failed", "error", err)
		c.Error(err)
		return
	}

	resp, err := h.stripeService.ListCustomerPaymentMethods(c.Request.Context(), customerID, &req)
	if err != nil {
		h.log.Error("Failed to list Customer Payment Methods", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
