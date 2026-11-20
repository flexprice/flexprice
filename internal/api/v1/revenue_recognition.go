package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service/recognition"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

type RevenueRecognitionHandler struct {
	service recognition.RunService
	log     *logger.Logger
}

func NewRevenueRecognitionHandler(service recognition.RunService, log *logger.Logger) *RevenueRecognitionHandler {
	return &RevenueRecognitionHandler{service: service, log: log}
}

// @Summary Start revenue recognition run
// @ID startRevenueRecognitionRun
// @Description Use to recognize revenue now instead of waiting for the daily run: for one contract, one customer, or the whole environment. Posts billing the hooks missed, then the recognition and offset entries due up to now. Runs in the background; poll GET /revenue_recognition/runs/{workflow_id}.
// @Tags Revenue Recognition
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param request body dto.StartRevenueRecognitionRunRequest true "Scope: contract_id, customer_id, or empty for the whole environment"
// @Success 202 {object} dto.RevenueRecognitionRunResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Contract or customer not found"
// @Failure 409 {object} ierr.ErrorResponse "A run for this scope is already running"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /revenue_recognition/runs [post]
func (h *RevenueRecognitionHandler) StartRun(c *gin.Context) {
	var req dto.StartRevenueRecognitionRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request body").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.StartRun(c.Request.Context(), &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusAccepted, resp)
}

// @Summary Get revenue recognition run
// @ID getRevenueRecognitionRun
// @Description Use to follow a recognition run started from POST /revenue_recognition/runs: queued, running, completed or failed.
// @Tags Revenue Recognition
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param workflow_id path string true "Workflow ID returned when the run started"
// @Success 200 {object} dto.RevenueRecognitionRunResponse
// @Failure 404 {object} ierr.ErrorResponse "Run not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /revenue_recognition/runs/{workflow_id} [get]
func (h *RevenueRecognitionHandler) GetRun(c *gin.Context) {
	resp, err := h.service.GetRun(c.Request.Context(), c.Param("workflow_id"))
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
