package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	revenuereportsvc "github.com/flexprice/flexprice/internal/ee/service/revenuereport"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

type RevenueReportHandler struct {
	service revenuereportsvc.RevenueReportService
	log     *logger.Logger
}

func NewRevenueReportHandler(service revenuereportsvc.RevenueReportService, log *logger.Logger) *RevenueReportHandler {
	return &RevenueReportHandler{service: service, log: log}
}

// @Summary Get revenue rollforward
// @ID getRevenueRollforward
// @Description Use when explaining how deferred revenue, unbilled receivable or another ledger account moved in ended months: opening balance, movements by category (invoiced, earned, adjustments, payments, refunds) and closing balance, optionally per customer, contract or performance obligation.
// @Tags Revenue Reports
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param request body dto.RollforwardRequest true "Rollforward request"
// @Success 200 {object} dto.RollforwardResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /revenue_reports/rollforward [post]
func (h *RevenueReportHandler) GetRollforward(c *gin.Context) {
	var req dto.RollforwardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetRollforward(c.Request.Context(), &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get revenue waterfall
// @ID getRevenueWaterfall
// @Description Use when showing revenue per customer, contract or performance obligation and month: actual (posted), pending (earned, not yet posted) and scheduled (invoiced, not yet earned), with optional month-end deferred and unbilled balances. Covers at most 36 months.
// @Tags Revenue Reports
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param request body dto.WaterfallRequest true "Waterfall request"
// @Success 200 {object} dto.WaterfallResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /revenue_reports/waterfall [post]
func (h *RevenueReportHandler) GetWaterfall(c *gin.Context) {
	var req dto.WaterfallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetWaterfall(c.Request.Context(), &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
