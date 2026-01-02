package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type ScheduledTaskHandler struct {
	service service.ScheduledTaskService
	logger  *logger.Logger
}

func NewScheduledTaskHandler(
	service service.ScheduledTaskService,
	logger *logger.Logger,
) *ScheduledTaskHandler {
	return &ScheduledTaskHandler{
		service: service,
		logger:  logger,
	}
}

// @Summary Create a scheduled task
// @Description Create a new scheduled task for data export
// @Tags ScheduledTasks
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param scheduled_task body dto.CreateScheduledTaskRequest true "Scheduled Task"
// @Success 201 {object} dto.ScheduledTaskResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /tasks/scheduled [post]
func (h *ScheduledTaskHandler) CreateScheduledTask(c *gin.Context) {
	var req dto.CreateScheduledTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorw("failed to bind request", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateScheduledTask(c.Request.Context(), req)
	if err != nil {
		h.logger.Errorw("failed to create scheduled task", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get a scheduled task
// @Description Get a scheduled task by ID
// @Tags ScheduledTasks
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Scheduled Task ID"
// @Success 200 {object} dto.ScheduledTaskResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /tasks/scheduled/{id} [get]
func (h *ScheduledTaskHandler) GetScheduledTask(c *gin.Context) {
	id := c.Param("id")

	resp, err := h.service.GetScheduledTask(c.Request.Context(), id)
	if err != nil {
		h.logger.Errorw("failed to get scheduled task", "id", id, "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary List scheduled tasks
// @Description Get a list of scheduled tasks with optional filters
// @Tags ScheduledTasks
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param limit query int false "Limit"
// @Param offset query int false "Offset"
// @Param connection_id query string false "Filter by connection ID"
// @Param entity_type query string false "Filter by entity type"
// @Param interval query string false "Filter by interval"
// @Param enabled query bool false "Filter by enabled status"
// @Success 200 {object} dto.ListScheduledTasksResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /tasks/scheduled [get]
func (h *ScheduledTaskHandler) ListScheduledTasks(c *gin.Context) {
	var filter types.QueryFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	// Get additional query parameters
	connectionID := c.Query("connection_id")
	entityTypeStr := c.Query("entity_type")
	intervalStr := c.Query("interval")
	enabledStr := c.Query("enabled")

	// Convert string parameters to enum types
	var entityType types.ScheduledTaskEntityType
	if entityTypeStr != "" {
		entityType = types.ScheduledTaskEntityType(entityTypeStr)
	}

	var interval types.ScheduledTaskInterval
	if intervalStr != "" {
		interval = types.ScheduledTaskInterval(intervalStr)
	}

	resp, err := h.service.ListScheduledTasks(c.Request.Context(), &filter, connectionID, entityType, interval, enabledStr)
	if err != nil {
		h.logger.Errorw("failed to list scheduled tasks", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update a scheduled task
// @Description Update a scheduled task by ID - Only enabled field can be changed (pause/resume)
// @Tags ScheduledTasks
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Scheduled Task ID"
// @Param scheduled_task body dto.UpdateScheduledTaskRequest true "Update request (enabled: true/false to pause/resume)"
// @Success 200 {object} dto.ScheduledTaskResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request or task is archived"
// @Failure 404 {object} ierr.ErrorResponse "Scheduled task not found"
// @Failure 500 {object} ierr.ErrorResponse "Failed to update Temporal schedule"
// @Router /tasks/scheduled/{id} [put]
func (h *ScheduledTaskHandler) UpdateScheduledTask(c *gin.Context) {
	id := c.Param("id")

	var req dto.UpdateScheduledTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorw("failed to bind request", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateScheduledTask(c.Request.Context(), id, req)
	if err != nil {
		h.logger.Errorw("failed to update scheduled task", "id", id, "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete a scheduled task
// @Description Archive a scheduled task by ID (soft delete) - Sets status to archived and deletes from Temporal
// @Tags ScheduledTasks
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Scheduled Task ID"
// @Success 204 "Scheduled task archived successfully"
// @Failure 400 {object} ierr.ErrorResponse "Task already archived"
// @Failure 404 {object} ierr.ErrorResponse "Scheduled task not found"
// @Failure 500 {object} ierr.ErrorResponse "Failed to archive task"
// @Router /tasks/scheduled/{id} [delete]
func (h *ScheduledTaskHandler) DeleteScheduledTask(c *gin.Context) {
	id := c.Param("id")

	err := h.service.DeleteScheduledTask(c.Request.Context(), id)
	if err != nil {
		h.logger.Errorw("failed to delete scheduled task", "id", id, "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Scheduled task archived successfully",
		"id":      id,
		"status":  "archived",
	})
}

// @Summary Trigger force run
// @Description Trigger a force run export immediately for a scheduled task with optional custom time range
// @Tags ScheduledTasks
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Scheduled Task ID"
// @Param request body dto.TriggerForceRunRequest false "Optional start and end time for custom range"
// @Success 200 {object} dto.TriggerForceRunResponse "Returns workflow details and time range"
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /tasks/scheduled/{id}/run [post]
func (h *ScheduledTaskHandler) TriggerForceRun(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Scheduled task ID must be provided").
			Mark(ierr.ErrValidation))
		return
	}

	// Parse request body (optional)
	var req dto.TriggerForceRunRequest

	// Try to bind JSON - if empty body or no JSON, continue with automatic time calculation
	if err := c.ShouldBindJSON(&req); err != nil {
		// Empty body or invalid JSON - use automatic calculation
		h.logger.Debugw("no custom time range provided, using automatic calculation", "id", id)
		req = dto.TriggerForceRunRequest{} // Empty request for automatic
	} else {
		// Validate the request
		if err := req.Validate(); err != nil {
			h.logger.Errorw("invalid force run request", "id", id, "error", err)
			c.Error(err)
			return
		}
		h.logger.Infow("force run with custom time range",
			"id", id,
			"start_time", req.StartTime,
			"end_time", req.EndTime)
	}

	response, err := h.service.TriggerForceRun(c.Request.Context(), id, req)
	if err != nil {
		h.logger.Errorw("failed to trigger force run", "id", id, "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// @Summary Schedule update billing period
// @Description Schedule an update billing period workflow
// @Tags ScheduledTasks
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body object true "Schedule Update Billing Period Request"
// @Success 200 {object} object
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /tasks/scheduled/schedule-update-billing-period [post]
func (h *ScheduledTaskHandler) ScheduleUpdateBillingPeriod(c *gin.Context) {

	response, err := h.service.ScheduleUpdateBillingPeriod(c.Request.Context())
	if err != nil {
		h.logger.Errorw("failed to schedule update billing period", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": response,
	})
}
