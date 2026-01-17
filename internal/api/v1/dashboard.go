package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

type DashboardHandler struct {
	dashboardService service.DashboardService
	logger           *logger.Logger
}

func NewDashboardHandler(
	dashboardService service.DashboardService,
	logger *logger.Logger,
) *DashboardHandler {
	return &DashboardHandler{
		dashboardService: dashboardService,
		logger:           logger,
	}
}

func (h *DashboardHandler) GetRevenues(c *gin.Context) {

	var req dto.DashboardRevenuesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	response, err := h.dashboardService.GetRevenues(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}
