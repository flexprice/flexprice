package cron

import (
	"net/http"

	ierr "github.com/flexprice/flexprice/internal/errors"
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

		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *SubscriptionHandler) GenerateInvoice(c *gin.Context) {
	ctx := c.Request.Context()

	// Get subscription ID from query params
	subscriptionID := c.Query("subscription_id")
	if subscriptionID == "" {
		c.Error(ierr.NewError("subscription_id is required").
			WithHint("Please provide a subscription_id").
			Mark(ierr.ErrValidation))
		return
	}

	// Use the GetSubscription method to get subscription details
	subscription, err := h.subscriptionService.GetSubscription(ctx, subscriptionID)
	if err != nil {
		h.logger.Errorw("failed to get subscription",
			"error", err,
			"subscription_id", subscriptionID)
		c.Error(err)
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
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// ProcessAutoCancellationSubscriptions processes subscriptions that are eligible for auto-cancellation
// We need to get all unpaid invoices and check if the grace period has expired
func (h *SubscriptionHandler) ProcessAutoCancellationSubscriptions(c *gin.Context) {
	h.logger.Infow("starting auto-cancellation processing cron job")

	if err := h.subscriptionService.ProcessAutoCancellationSubscriptions(c.Request.Context()); err != nil {
		h.logger.Errorw("failed to process auto-cancellation subscriptions",
			"error", err)
		c.Error(err)
		return
	}

	h.logger.Infow("completed auto-cancellation processing cron job")
	c.JSON(http.StatusOK, gin.H{"status": "completed"})
}

// ProcessSubscriptionRenewalDueAlerts processes subscriptions that are due for renewal in 24 hours
// and sends webhook notifications
func (h *SubscriptionHandler) ProcessSubscriptionRenewalDueAlerts(c *gin.Context) {
	h.logger.Infow("starting subscription renewal due alerts cron job")

	if err := h.subscriptionService.ProcessSubscriptionRenewalDueAlert(c.Request.Context()); err != nil {
		h.logger.Errorw("failed to process subscription renewal due alerts",
			"error", err)
		c.Error(err)
		return
	}

	h.logger.Infow("completed subscription renewal due alerts cron job")
	c.JSON(http.StatusOK, gin.H{"status": "completed"})
}
