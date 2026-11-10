package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

// CostAnalyticsHandler serves the cost breakdown. Private beta: left out of the public API docs.
type CostAnalyticsHandler struct {
	svc service.CostAnalyticsService
	log *logger.Logger
}

func NewCostAnalyticsHandler(svc service.CostAnalyticsService, log *logger.Logger) *CostAnalyticsHandler {
	return &CostAnalyticsHandler{svc: svc, log: log}
}

// Query returns a customer's billed cost grouped by event properties.
func (h *CostAnalyticsHandler) Query(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.CostAnalyticsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	res, err := h.svc.GetCostBreakdown(ctx, &req)
	if err != nil {
		h.log.Error(ctx, "failed to query cost analytics", "error", err, "external_customer_id", req.ExternalCustomerID)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, res)
}
