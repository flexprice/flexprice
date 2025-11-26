package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type SubscriptionHandler struct {
	service service.SubscriptionService
	log     *logger.Logger
}

func NewSubscriptionHandler(service service.SubscriptionService, log *logger.Logger) *SubscriptionHandler {
	return &SubscriptionHandler{service: service, log: log}
}

// @Summary Create subscription
// @Description Create a new subscription
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param subscription body dto.CreateSubscriptionRequest true "Subscription Request"
// @Success 201 {object} dto.SubscriptionResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /subscriptions [post]
func (h *SubscriptionHandler) CreateSubscription(c *gin.Context) {
	var req dto.CreateSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateSubscription(c.Request.Context(), req)
	if err != nil {
		h.log.Error("Failed to create subscription", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get subscription
// @Description Get a subscription by ID
// @Tags Subscriptions
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Success 200 {object} dto.SubscriptionResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /subscriptions/{id} [get]
func (h *SubscriptionHandler) GetSubscription(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetSubscription(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Failed to get subscription", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary List subscriptions
// @Description Get subscriptions with optional filtering
// @Tags Subscriptions
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.SubscriptionFilter false "Filter"
// @Success 200 {object} dto.ListSubscriptionsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /subscriptions [get]
func (h *SubscriptionHandler) GetSubscriptions(c *gin.Context) {
	var filter types.SubscriptionFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		h.log.Error("Failed to bind query", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.ListSubscriptions(c.Request.Context(), &filter)
	if err != nil {
		h.log.Error("Failed to list subscriptions", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Cancel subscription
// @Description Cancel a subscription with enhanced proration support
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Param request body dto.CancelSubscriptionRequest true "Cancel Subscription Request"
// @Success 200 {object} dto.CancelSubscriptionResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /subscriptions/{id}/cancel [post]
func (h *SubscriptionHandler) CancelSubscription(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.CancelSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	// Always use the enhanced cancellation method with proration support
	response, err := h.service.CancelSubscription(c.Request.Context(), id, &req)
	if err != nil {
		h.log.Error("Failed to cancel subscription", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)

}

// @Summary Get usage by subscription
// @Description Get usage for a subscription
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.GetUsageBySubscriptionRequest true "Usage request"
// @Success 200 {object} dto.GetUsageBySubscriptionResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /subscriptions/usage [post]
func (h *SubscriptionHandler) GetUsageBySubscription(c *gin.Context) {
	var req dto.GetUsageBySubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetUsageBySubscription(c.Request.Context(), &req)
	if err != nil {
		h.log.Error("Failed to get usage", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ListSubscriptionsByFilter godoc
// @Summary List subscriptions by filter
// @Description List subscriptions by filter
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.SubscriptionFilter true "Filter"
// @Success 200 {object} dto.ListSubscriptionsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /subscriptions/search [post]
func (h *SubscriptionHandler) ListSubscriptionsByFilter(c *gin.Context) {
	var filter types.SubscriptionFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if err := filter.Validate(); err != nil {
		h.log.Error("Invalid filter parameters", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Please provide valid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.ListSubscriptions(c.Request.Context(), &filter)
	if err != nil {
		h.log.Error("Failed to list subscriptions", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Add addon to subscription
// @Description Add an addon to a subscription
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.AddAddonToSubscriptionRequest true "Add Addon Request"
// @Success 200 {object} dto.AddonAssociationResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /subscriptions/addon [post]
func (h *SubscriptionHandler) AddAddonToSubscription(c *gin.Context) {
	var req dto.AddAddonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.AddAddonToSubscription(c.Request.Context(), req.SubscriptionID, &req.AddAddonToSubscriptionRequest)
	if err != nil {
		h.log.Error("Failed to add addon to subscription", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Remove addon from subscription
// @Description Remove an addon from a subscription
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.RemoveAddonRequest true "Remove Addon Request"
// @Success 200 {object} gin.H
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /subscriptions/addon [delete]
func (h *SubscriptionHandler) RemoveAddonToSubscription(c *gin.Context) {
	var req dto.RemoveAddonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	if err := h.service.RemoveAddonFromSubscription(c.Request.Context(), &req); err != nil {
		h.log.Error("Failed to remove addon from subscription", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "addon removed from subscription successfully"})
}

// @Summary Get subscription entitlements
// @Description Get all entitlements for a subscription
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Param feature_ids query []string false "Feature IDs to filter by"
// @Success 200 {object} dto.SubscriptionEntitlementsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /subscriptions/{id}/entitlements [get]
func (h *SubscriptionHandler) GetSubscriptionEntitlements(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	// Call the service method with structured response
	var req dto.GetSubscriptionEntitlementsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		h.log.Error("Failed to bind query", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}
	response, err := h.service.GetAggregatedSubscriptionEntitlements(c.Request.Context(), id, &req)
	if err != nil {
		h.log.Error("Failed to get subscription entitlements", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// @Summary Update subscription line item
// @Description Update a subscription line item by terminating the existing one and creating a new one
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Line Item ID"
// @Param request body dto.UpdateSubscriptionLineItemRequest true "Update Line Item Request"
// @Success 200 {object} dto.SubscriptionLineItemResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /subscriptions/lineitems/{id} [put]
func (h *SubscriptionHandler) UpdateSubscriptionLineItem(c *gin.Context) {
	lineItemID := c.Param("id")
	if lineItemID == "" {
		c.Error(ierr.NewError("line item ID is required").
			WithHint("Please provide a valid line item ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdateSubscriptionLineItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateSubscriptionLineItem(c.Request.Context(), lineItemID, req)
	if err != nil {
		h.log.Error("Failed to update subscription line item", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete subscription line item
// @Description Delete a subscription line item by setting its end date
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Line Item ID"
// @Param request body dto.DeleteSubscriptionLineItemRequest true "Delete Line Item Request"
// @Success 200 {object} dto.SubscriptionLineItemResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /subscriptions/lineitems/{id} [delete]
func (h *SubscriptionHandler) DeleteSubscriptionLineItem(c *gin.Context) {
	lineItemID := c.Param("id")
	if lineItemID == "" {
		c.Error(ierr.NewError("line item ID is required").
			WithHint("Please provide a valid line item ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.DeleteSubscriptionLineItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.DeleteSubscriptionLineItem(c.Request.Context(), lineItemID, req)
	if err != nil {
		h.log.Error("Failed to delete subscription line item", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get upcoming credit grant applications
// @Description Get upcoming credit grant applications for a subscription
// @Tags Subscriptions
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Success 200 {object} dto.ListCreditGrantApplicationsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /subscriptions/{id}/grants/upcoming [get]
func (h *SubscriptionHandler) GetUpcomingCreditGrantApplications(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	// Create request DTO with the subscription ID from path parameter
	req := &dto.GetUpcomingCreditGrantApplicationsRequest{
		SubscriptionIDs: []string{id},
	}

	resp, err := h.service.GetUpcomingCreditGrantApplications(c.Request.Context(), req)
	if err != nil {
		h.log.Error("Failed to get upcoming credit grant applications", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
