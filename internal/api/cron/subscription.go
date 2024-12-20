package cron

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

// SubscriptionHandler handles subscription related cron jobs
type SubscriptionHandler struct {
	subscriptionService service.SubscriptionService
	logger              *logger.Logger
}

// NewSubscriptionHandler creates a new subscription handler
func NewSubscriptionHandler(
	subscriptionService service.SubscriptionService,
	logger *logger.Logger,
) *SubscriptionHandler {
	return &SubscriptionHandler{
		subscriptionService: subscriptionService,
		logger:              logger,
	}
}

func (h *SubscriptionHandler) UpdateBillingPeriods(c *gin.Context) {
	ctx := c.Request.Context()
	err := h.subscriptionService.UpdateBillingPeriods(ctx)
	if err != nil {
		h.logger.Errorw("failed to update billing periods",
			"error", err)

		c.Status(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "billing periods updated",
	})
}
