package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
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
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /subscriptions [post]
func (h *SubscriptionHandler) CreateSubscription(c *gin.Context) {
	var req dto.CreateSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.service.CreateSubscription(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /subscriptions/{id} [get]
func (h *SubscriptionHandler) GetSubscription(c *gin.Context) {
	id := c.Param("id")
	resp, err := h.service.GetSubscription(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary List subscriptions
// @Description Get subscriptions with optional filtering
// @Tags Subscriptions
// @Produce json
// @Security ApiKeyAuth
// @Param customer_id query string false "Filter by customer ID"
// @Param subscription_status query string false "Filter by subscription status"
// @Param plan_id query string false "Filter by plan ID"
// @Param offset query int false "Offset for pagination"
// @Param limit query int false "Limit for pagination"
// @Success 200 {object} dto.ListSubscriptionsResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /subscriptions [get]
func (h *SubscriptionHandler) GetSubscriptions(c *gin.Context) {
	var filter types.SubscriptionFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.service.ListSubscriptions(c.Request.Context(), &filter)
	if err != nil {
		h.log.Error("Failed to list subscriptions", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /subscriptions/{id}/cancel [post]
func (h *SubscriptionHandler) CancelSubscription(c *gin.Context) {
	id := c.Param("id")
	cancelAtPeriodEnd := c.DefaultQuery("cancel_at_period_end", "false") == "true"

	err := h.service.CancelSubscription(c.Request.Context(), id, cancelAtPeriodEnd)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Subscription cancelled successfully"})
}

// @Summary Get usage by subscription
// @Description Get usage by subscription
// @Tags Subscriptions
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.GetUsageBySubscriptionRequest true "Request"
// @Success 200 {object} dto.GetUsageBySubscriptionResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /subscriptions/usage [post]
func (h *SubscriptionHandler) GetUsageBySubscription(c *gin.Context) {
	var req dto.GetUsageBySubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.service.GetUsageBySubscription(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}
