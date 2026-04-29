package cron

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

// SubscriptionHandler handles subscription related cron jobs.
//
// Deprecated: for automation, use Temporal server schedules (worker creates them on startup).
type SubscriptionHandler struct {
	subscriptionService service.SubscriptionService
	logger              *logger.Logger
}

// NewSubscriptionHandler creates a new subscription handler.
//
// Deprecated: for automation, use Temporal server schedules instead of HTTP cron.
func NewSubscriptionHandler(
	subscriptionService service.SubscriptionService,
	logger *logger.Logger,
) *SubscriptionHandler {
	return &SubscriptionHandler{
		subscriptionService: subscriptionService,
		logger:              logger,
	}
}

// UpdateBillingPeriods is bound to POST /v1/cron/subscriptions/update-periods.
//
// Deprecated: the same work is run by the Temporal server schedule.
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

// ProcessTrialEndDue is bound to POST /v1/cron/subscriptions/process-trial-end-due.
//
// Deprecated: the same work is run by the Temporal server schedule subscription-trial-end-due.
func (h *SubscriptionHandler) ProcessTrialEndDue(c *gin.Context) {
	ctx := c.Request.Context()
	response, err := h.subscriptionService.ProcessTrialEndDue(ctx)
	if err != nil {
		h.logger.Errorw("failed to process trial end due subscriptions",
			"error", err)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// ProcessAutoCancellationSubscriptions processes subscriptions that are eligible for auto-cancellation
// We need to get all unpaid invoices and check if the grace period has expired.
//
// Deprecated: use the Temporal server schedule instead of this HTTP trigger.
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
// and sends webhook notifications.
//
// Deprecated: use the Temporal server schedule instead of this HTTP trigger.
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
