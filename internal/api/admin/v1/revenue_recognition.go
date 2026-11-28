package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto/admin"
	adminsvc "github.com/flexprice/flexprice/internal/ee/service/admin"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/gin-gonic/gin"
)

// RevenueRecognitionHandler starts revenue recognition backfills and runs, and reports them, through the internal admin router.
type RevenueRecognitionHandler struct {
	service adminsvc.RevenueRecognitionService
}

func NewRevenueRecognitionHandler(service adminsvc.RevenueRecognitionService) *RevenueRecognitionHandler {
	return &RevenueRecognitionHandler{service: service}
}

func (h *RevenueRecognitionHandler) StartBackfill(c *gin.Context) {
	var req admin.StartRevenueRecognitionBackfillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Please check the request payload").Mark(ierr.ErrValidation))
		return
	}

	run, err := h.service.StartBackfill(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusAccepted, run)
}

func (h *RevenueRecognitionHandler) StartRun(c *gin.Context) {
	var req admin.StartRevenueRecognitionRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Please check the request payload").Mark(ierr.ErrValidation))
		return
	}

	run, err := h.service.StartRun(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusAccepted, run)
}

func (h *RevenueRecognitionHandler) GetRun(c *gin.Context) {
	run, err := h.service.GetRun(c.Request.Context(), c.Param("workflow_id"), c.Query("run_id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, run)
}
