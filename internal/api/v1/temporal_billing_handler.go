package v1

import (
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/domain"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

type TemporalBillingHandler struct {
	temporalService domain.TemporalService
	logger          *logger.Logger
}

func NewTemporalBillingHandler(ts domain.TemporalService, logger *logger.Logger) *TemporalBillingHandler {
	return &TemporalBillingHandler{
		temporalService: ts,
		logger:          logger,
	}
}

type StartTemporalBillingRequest struct {
	CustomerID     string    `json:"customer_id" binding:"required"`
	SubscriptionID string    `json:"subscription_id" binding:"required"`
	PeriodStart    time.Time `json:"period_start" binding:"required"`
	PeriodEnd      time.Time `json:"period_end" binding:"required"`
}

// StartTemporalBilling godoc
// @Summary Start a temporal billing workflow
// @Description Initiates a temporal workflow for billing calculation
// @Tags Temporal Billing
// @Accept json
// @Produce json
// @Param request body StartTemporalBillingRequest true "Billing workflow parameters"
// @Success 200 {object} domain.BillingWorkflowResult
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /temporal/billing/start [post]
func (h *TemporalBillingHandler) StartTemporalBilling(c *gin.Context) {
	var req StartTemporalBillingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.temporalService.StartBillingWorkflow(
		c.Request.Context(),
		req.CustomerID,
		req.SubscriptionID,
		req.PeriodStart,
		req.PeriodEnd,
	)
	if err != nil {
		h.logger.Error("Failed to start temporal billing workflow",
			"error", err,
			"customerID", req.CustomerID,
			"subscriptionID", req.SubscriptionID,
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}
