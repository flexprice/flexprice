package cron

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/gin-gonic/gin"
)

// SubscriptionHandler handles subscription related cron jobs
type SubscriptionHandler struct {
	subscriptionService service.SubscriptionService
	temporalService     *temporal.Service
	logger              *logger.Logger
}

// NewSubscriptionHandler creates a new subscription handler
func NewSubscriptionHandler(
	subscriptionService service.SubscriptionService,
	temporalService *temporal.Service,
	logger *logger.Logger,
) *SubscriptionHandler {
	return &SubscriptionHandler{
		subscriptionService: subscriptionService,
		temporalService:     temporalService,
		logger:              logger,
	}
}

func (h *SubscriptionHandler) UpdateBillingPeriods(c *gin.Context) {
	ctx := c.Request.Context()
	response, err := h.subscriptionService.UpdateBillingPeriods(ctx)
	if err != nil {
		h.logger.Errorw("failed to update billing periods",
			"error", err)

		c.Status(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *SubscriptionHandler) GenerateInvoice(c *gin.Context) {
	ctx := c.Request.Context()

	// Get subscription ID from query params
	subscriptionID := c.Query("subscription_id")
	if subscriptionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "subscription_id is required"})
		return
	}

	// Use the GetSubscription method to get subscription details
	subscription, err := h.subscriptionService.GetSubscription(ctx, subscriptionID)
	if err != nil {
		h.logger.Errorw("failed to get subscription",
			"error", err,
			"subscription_id", subscriptionID)
		c.Status(http.StatusInternalServerError)
		return
	}

	// Create workflow input
	input := models.BillingWorkflowInput{
		CustomerID:     subscription.CustomerID,
		SubscriptionID: subscription.ID,
		PeriodStart:    subscription.CurrentPeriodStart,
		PeriodEnd:      subscription.CurrentPeriodEnd,
	}

	// Start billing workflow
	result, err := h.temporalService.StartBillingWorkflow(ctx, input)
	if err != nil {
		h.logger.Errorw("failed to start billing workflow",
			"error", err,
			"subscription_id", subscriptionID)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, result)
}
