package cron

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	subscriptionCron *SubscriptionCron
	logger           *logger.Logger
}

func NewHandler(
	subscriptionCron *SubscriptionCron,
	logger *logger.Logger,
) *Handler {
	return &Handler{
		subscriptionCron: subscriptionCron,
		logger:           logger,
	}
}

// UpdateSubscriptionPeriods handles the cron request to update subscription billing periods
func (h *Handler) UpdateSubscriptionPeriods(c *gin.Context) {
	if err := h.subscriptionCron.UpdateBillingPeriods(c.Request.Context()); err != nil {
		h.logger.Errorw("failed to update subscription periods",
			"error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update subscription periods"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "success"})
}
