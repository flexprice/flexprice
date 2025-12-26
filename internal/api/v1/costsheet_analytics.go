package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

type RevenueAnalyticsHandler struct {
	revenueAnalyticsService       interfaces.RevenueAnalyticsService
	costsheetUsageTrackingService service.CostSheetUsageTrackingService
	config                        *config.Configuration
	Logger                        *logger.Logger
}

func NewRevenueAnalyticsHandler(
	revenueAnalyticsService interfaces.RevenueAnalyticsService,
	costsheetUsageTrackingService service.CostSheetUsageTrackingService,
	config *config.Configuration,
	logger *logger.Logger,
) *RevenueAnalyticsHandler {
	return &RevenueAnalyticsHandler{
		revenueAnalyticsService:       revenueAnalyticsService,
		costsheetUsageTrackingService: costsheetUsageTrackingService,
		config:                        config,
		Logger:                        logger,
	}
}

// GetCombinedAnalytics retrieves combined cost and revenue analytics with derived metrics
// @Summary Get combined revenue and cost analytics
// @Description Retrieve combined analytics with ROI, margin, and detailed breakdowns. If start_time and end_time are not provided, defaults to last 7 days.
// @Tags Costs
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.GetCostAnalyticsRequest true "Combined analytics request (start_time/end_time optional - defaults to last 7 days)"
// @Success 200 {object} dto.GetDetailedCostAnalyticsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /costs/analytics [post]
func (h *RevenueAnalyticsHandler) GetDetailedCostAnalytics(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.GetCostAnalyticsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	response, err := h.revenueAnalyticsService.GetDetailedCostAnalytics(ctx, &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetCombinedAnalytics retrieves combined cost and revenue analytics with derived metrics
// @Summary Get combined revenue and cost analytics
// @Description Retrieve combined analytics with ROI, margin, and detailed breakdowns. If start_time and end_time are not provided, defaults to last 7 days.
// @Tags Costs
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.GetCostAnalyticsRequest true "Combined analytics request (start_time/end_time optional - defaults to last 7 days)"
// @Success 200 {object} dto.GetDetailedCostAnalyticsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /costs/analytics-v2 [post]
func (h *RevenueAnalyticsHandler) GetDetailedCostAnalyticsV2(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.GetCostAnalyticsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	response, err := h.costsheetUsageTrackingService.GetCostSheetUsageAnalytics(ctx, &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}
