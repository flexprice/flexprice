package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto/admin"
	adminsvc "github.com/flexprice/flexprice/internal/ee/service/admin"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/gin-gonic/gin"
)

// RevenueRecognitionHandler enables revenue recognition and runs its backfill through the internal admin router.
type RevenueRecognitionHandler struct {
	service adminsvc.RevenueRecognitionService
}

func NewRevenueRecognitionHandler(service adminsvc.RevenueRecognitionService) *RevenueRecognitionHandler {
	return &RevenueRecognitionHandler{service: service}
}

func (h *RevenueRecognitionHandler) UpdateConfig(c *gin.Context) {
	var req admin.UpdateRevenueRecognitionConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Please check the request payload").Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateConfig(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
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

func (h *RevenueRecognitionHandler) GetBackfill(c *gin.Context) {
	run, err := h.service.GetBackfill(c.Request.Context(), c.Param("workflow_id"), c.Query("run_id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, run)
}
