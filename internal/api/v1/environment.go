package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	temporalmodels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type EnvironmentHandler struct {
	service service.EnvironmentService
	log     *logger.Logger
}

func NewEnvironmentHandler(service service.EnvironmentService, log *logger.Logger) *EnvironmentHandler {
	return &EnvironmentHandler{service: service, log: log}
}

func (h *EnvironmentHandler) CreateEnvironment(c *gin.Context) {
	var req dto.CreateEnvironmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateEnvironment(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

func (h *EnvironmentHandler) GetEnvironment(c *gin.Context) {
	id := c.Param("id")

	resp, err := h.service.GetEnvironment(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *EnvironmentHandler) GetEnvironments(c *gin.Context) {
	var filter types.Filter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the query parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetEnvironments(c.Request.Context(), filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *EnvironmentHandler) UpdateEnvironment(c *gin.Context) {
	id := c.Param("id")

	var req dto.UpdateEnvironmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateEnvironment(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Clone an environment
// @ID cloneEnvironment
// @Description Clone all published features and plans from the source environment into a target environment. If target_environment_id is provided, entities are cloned into that existing environment. Otherwise a new environment is created from name and type first.
// @Tags Environments
// @x-scope "write"
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Source Environment ID"
// @Param request body dto.CloneEnvironmentRequest true "Clone configuration"
// @Success 202 {object} temporalmodels.TemporalWorkflowResult
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /environments/{id}/clone [post]
func (h *EnvironmentHandler) CloneEnvironment(c *gin.Context) {
	sourceEnvID := c.Param("id")
	if sourceEnvID == "" {
		c.Error(ierr.NewError("source environment ID is required").
			WithHint("Please provide a valid environment ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.CloneEnvironmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	// Validate that the source environment exists
	if _, err := h.service.GetEnvironment(c.Request.Context(), sourceEnvID); err != nil {
		c.Error(err)
		return
	}

	var targetEnvID string

	if req.TargetEnvironmentID != "" {
		// Clone into an existing environment — validate it exists and isn't the source
		if req.TargetEnvironmentID == sourceEnvID {
			c.Error(ierr.NewError("target environment cannot be the same as source environment").
				WithHint("Please provide a different target environment ID").
				Mark(ierr.ErrValidation))
			return
		}

		if _, err := h.service.GetEnvironment(c.Request.Context(), req.TargetEnvironmentID); err != nil {
			c.Error(err)
			return
		}

		targetEnvID = req.TargetEnvironmentID
	} else {
		// Create a new target environment from Name and Type
		newEnv, err := h.service.CreateEnvironment(c.Request.Context(), dto.CreateEnvironmentRequest{
			Name: req.Name,
			Type: string(req.Type),
		})
		if err != nil {
			h.log.Error("failed to create target environment for clone", "error", err)
			c.Error(err)
			return
		}
		targetEnvID = newEnv.ID
	}

	ts := temporalservice.GetGlobalTemporalService()
	if ts == nil {
		h.log.Error("temporal service not available")
		c.Error(ierr.NewError("temporal service not available").
			WithHint("Workflow engine is not configured").
			Mark(ierr.ErrSystem))
		return
	}

	workflowRun, err := ts.ExecuteWorkflow(
		c.Request.Context(),
		types.TemporalEnvironmentCloneWorkflow,
		temporalmodels.EnvironmentCloneWorkflowInput{
			SourceEnvironmentID: sourceEnvID,
			TargetEnvironmentID: targetEnvID,
			TenantID:            types.GetTenantID(c.Request.Context()),
			UserID:              types.GetUserID(c.Request.Context()),
		},
	)
	if err != nil {
		h.log.Error("failed to start environment clone workflow", "error", err, "source_env", sourceEnvID, "target_env", targetEnvID)
		c.Error(err)
		return
	}

	c.JSON(http.StatusAccepted, &temporalmodels.TemporalWorkflowResult{
		WorkflowID: workflowRun.GetID(),
		RunID:      workflowRun.GetRunID(),
		Message:    "Environment clone workflow started successfully",
	})
}
