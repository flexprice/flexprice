package v1

import (
	"net/http"

	pobsvc "github.com/flexprice/flexprice/internal/ee/service/pob"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	pobRepo "github.com/flexprice/flexprice/internal/repository/pob"
	"github.com/gin-gonic/gin"
)

type PerformanceObligationHandler struct {
	service pobsvc.PerformanceObligationService
	log     *logger.Logger
}

func NewPerformanceObligationHandler(service pobsvc.PerformanceObligationService, log *logger.Logger) *PerformanceObligationHandler {
	return &PerformanceObligationHandler{service: service, log: log}
}

// @Summary Get performance obligation
// @ID getPerformanceObligation
// @Description Use when you need to load a single performance obligation with the subscription line items it covers. Performance obligations are derived from subscriptions and are read-only.
// @Tags Performance Obligations
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Performance obligation ID"
// @Success 200 {object} dto.PerformanceObligationResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /performance_obligations/{id} [get]
func (h *PerformanceObligationHandler) GetPerformanceObligation(c *gin.Context) {
	resp, err := h.service.GetPerformanceObligation(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Query performance obligations
// @ID queryPerformanceObligation
// @Description Use when searching performance obligations by contract, customer, status or recognition method. Returns a paginated list.
// @Tags Performance Obligations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param filter body pobRepo.Filter true "Filter"
// @Success 200 {object} dto.ListPerformanceObligationsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /performance_obligations/search [post]
func (h *PerformanceObligationHandler) QueryPerformanceObligations(c *gin.Context) {
	var filter pobRepo.Filter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetPerformanceObligations(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
