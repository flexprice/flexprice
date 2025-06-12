package cron

import (
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

type CreditGrantCronHandler struct {
	creditGrantService service.CreditGrantService
	logger             *logger.Logger
}

func NewCreditGrantCronHandler(creditGrantService service.CreditGrantService, log *logger.Logger) *CreditGrantCronHandler {
	return &CreditGrantCronHandler{
		creditGrantService: creditGrantService,
		logger:             log,
	}
}

func (h *CreditGrantCronHandler) ProcessScheduledCreditGrantApplications(c *gin.Context) {
	h.logger.Infow("starting credit grant scheduled applications cron job - %s", time.Now().UTC().Format(time.RFC3339))

	err := h.creditGrantService.ProcessScheduledCreditGrantApplications(c.Request.Context())
	if err != nil {
		h.logger.Errorw("failed to process scheduled credit grant applications", "error", err)
		c.Error(err)
		return
	}
}
