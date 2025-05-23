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
// @Description Cancel a subscription
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Param cancel_at_period_end query bool false "Cancel at period end"
// @Success 200 {object} gin.H
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

	cancelAtPeriodEnd := c.DefaultQuery("cancel_at_period_end", "false") == "true"

	if err := h.service.CancelSubscription(c.Request.Context(), id, cancelAtPeriodEnd); err != nil {
		h.log.Error("Failed to cancel subscription", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "subscription cancelled successfully"})
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

// @Summary Add new phase to subscription schedule
// @Description Add a new phase to a subscription schedule
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Param request body dto.AddSchedulePhaseRequest true "Add schedule phase request"
// @Success 200 {object} dto.SubscriptionScheduleResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /subscriptions/{id}/phases [post]
func (h *SubscriptionHandler) AddSubscriptionPhase(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.AddSchedulePhaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.AddSubscriptionPhase(c.Request.Context(), id, &req)
	if err != nil {
		h.log.Error("Failed to add subscription phase", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
