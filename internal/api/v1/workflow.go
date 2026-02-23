package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
)

type WorkflowHandler struct {
	workflowService service.WorkflowService
	log             *logger.Logger
}

func NewWorkflowHandler(workflowService service.WorkflowService, log *logger.Logger) *WorkflowHandler {
	return &WorkflowHandler{
		workflowService: workflowService,
		log:             log,
	}
}

// @Summary Query workflows
// @ID queryWorkflow
// @Description Use when listing or auditing workflow runs (e.g. ops dashboard or debugging). Returns a paginated list; supports filtering by workflow type and status.
// @Tags Workflows
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.WorkflowExecutionFilter true "Filter"
// @Success 200 {object} dto.ListWorkflowsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /workflows/search [post]
func (h *WorkflowHandler) QueryWorkflows(c *gin.Context) {
	var filter types.WorkflowExecutionFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}
	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(50)
	}
	resp, err := h.workflowService.ListWorkflows(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *WorkflowHandler) GetWorkflowSummary(c *gin.Context) {
	workflowID := c.Param("workflow_id")
	runID := c.Param("run_id")
	resp, err := h.workflowService.GetWorkflowSummary(c.Request.Context(), workflowID, runID)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *WorkflowHandler) GetWorkflowDetails(c *gin.Context) {
	workflowID := c.Param("workflow_id")
	runID := c.Param("run_id")
	resp, err := h.workflowService.GetWorkflowDetails(c.Request.Context(), workflowID, runID)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *WorkflowHandler) GetWorkflowTimeline(c *gin.Context) {
	workflowID := c.Param("workflow_id")
	runID := c.Param("run_id")
	resp, err := h.workflowService.GetWorkflowTimeline(c.Request.Context(), workflowID, runID)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *WorkflowHandler) GetWorkflowsBatch(c *gin.Context) {
	var req dto.BatchWorkflowsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}
	resp, err := h.workflowService.GetWorkflowsBatch(c.Request.Context(), &req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
