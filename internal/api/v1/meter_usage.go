package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// MeterUsageHandler handles meter_usage query endpoints
type MeterUsageHandler struct {
	meterUsageService service.MeterUsageService
	log               *logger.Logger
}

func NewMeterUsageHandler(meterUsageService service.MeterUsageService, log *logger.Logger) *MeterUsageHandler {
	return &MeterUsageHandler{
		meterUsageService: meterUsageService,
		log:               log,
	}
}

func (h *MeterUsageHandler) QueryUsage(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.MeterUsageQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.NewError("invalid request payload").
			WithHint("Check your request body").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	params := req.ToParams(tenantID, environmentID)

	result, err := h.meterUsageService.GetUsage(ctx, params)
	if err != nil {
		h.log.Error(ctx, "failed to query meter usage",
			"error", err,
			"meter_id", req.MeterID,
		)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.ToMeterUsageQueryResponse(result))
}

func (h *MeterUsageHandler) GetAnalytics(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.MeterUsageAnalyticsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.NewError("invalid request payload").
			WithHint("Check your request body").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	params := req.ToParams(tenantID, environmentID)

	results, err := h.meterUsageService.GetUsageMultiMeter(ctx, params)
	if err != nil {
		h.log.Error(ctx, "failed to query meter usage analytics",
			"error", err,
			"meter_ids", req.MeterIDs,
		)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.ToMeterUsageAnalyticsResponse(results))
}

func (h *MeterUsageHandler) GetDetailedAnalytics(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.MeterUsageDetailedAnalyticsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.NewError("invalid request payload").
			WithHint("Check your request body").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	params := req.ToParams(tenantID, environmentID)

	response, err := h.meterUsageService.GetDetailedAnalytics(ctx, params)
	if err != nil {
		h.log.Error(ctx, "failed to query detailed meter usage analytics",
			"error", err,
			"meter_ids", req.MeterIDs,
		)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}
