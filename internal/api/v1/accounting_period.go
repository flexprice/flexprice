package v1

import (
	"net/http"

	accountingperiodsvc "github.com/flexprice/flexprice/internal/ee/service/accountingperiod"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	accountingPeriodRepo "github.com/flexprice/flexprice/internal/repository/accountingperiod"
	"github.com/gin-gonic/gin"
)

type AccountingPeriodHandler struct {
	service accountingperiodsvc.AccountingPeriodService
	log     *logger.Logger
}

func NewAccountingPeriodHandler(service accountingperiodsvc.AccountingPeriodService, log *logger.Logger) *AccountingPeriodHandler {
	return &AccountingPeriodHandler{service: service, log: log}
}

// @Summary Query accounting periods
// @ID queryAccountingPeriod
// @Description Use when listing the monthly accounting periods revenue recognition posts to, by period status or a period start range. Returns a paginated list.
// @Tags Accounting Periods
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param filter body accountingPeriodRepo.Filter true "Filter"
// @Success 200 {object} dto.ListAccountingPeriodsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /accounting_periods/search [post]
func (h *AccountingPeriodHandler) QueryAccountingPeriods(c *gin.Context) {
	var filter accountingPeriodRepo.Filter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetAccountingPeriods(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
