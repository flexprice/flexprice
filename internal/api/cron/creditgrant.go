package cron

import (
	"net/http"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

type CreditGrantCronHandler struct {
	service            service.SubscriptionService
	creditGrantService service.CreditGrantService
	log                *logger.Logger
}

func NewCreditGrantCronHandler(service service.SubscriptionService, creditGrantService service.CreditGrantService, log *logger.Logger) *CreditGrantCronHandler {
	return &CreditGrantCronHandler{
		service:            service,
		creditGrantService: creditGrantService,
		log:                log,
	}
}

func (h *CreditGrantCronHandler) ProcessRecurringGrants(c *gin.Context) {
	if err := h.service.ProcessRecurringCreditGrants(c.Request.Context()); err != nil {
		h.log.Errorw("failed to process recurring credit grants", "error", err)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Recurring credit grants processed successfully"})
}

func (h *CreditGrantCronHandler) ProcessSubscriptionRecurringGrants(c *gin.Context) {
	subscriptionID := c.Param("id")

	if subscriptionID == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	if err := h.service.ProcessSubscriptionRecurringGrants(c.Request.Context(), subscriptionID); err != nil {
		h.log.Errorw("failed to process recurring credit grants for subscription", "error", err, "subscription_id", subscriptionID)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Recurring credit grants processed successfully"})
}
