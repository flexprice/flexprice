package cron

import (
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

// AlertCronHandler handles alert related cron jobs
type AlertCronHandler struct {
	alertService service.AlertService
	logger       *logger.Logger
}

// NewAlertCronHandler creates a new alert cron handler
func NewAlertCronHandler(
	alertService service.AlertService,
	logger *logger.Logger,
) *AlertCronHandler {
	return &AlertCronHandler{
		alertService: alertService,
		logger:       logger,
	}
}

// CheckAlerts checks alerts for all entities
func (h *AlertCronHandler) CheckAlerts(c *gin.Context) {
	h.logger.Infow("starting alert check cron job", "time", time.Now().UTC().Format(time.RFC3339))

	// Parse request body
	var req dto.CheckAlertsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorw("failed to parse request body", "error", err)
		c.Error(err)
		return
	}

	// Check alerts
	if err := h.alertService.CheckAlerts(c.Request.Context(), &req); err != nil {
		h.logger.Errorw("failed to check alerts", "error", err)
		c.Error(err)
		return
	}

	h.logger.Infow("completed alert check cron job")
	c.JSON(http.StatusOK, gin.H{"status": "success"})
}
