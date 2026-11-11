package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
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

// @Summary Create scheduled task
// @ID createScheduledTask
// @Description Use when setting up recurring data exports or other scheduled jobs. Ideal for report generation or syncing data on a schedule.
// @Description connection_id is optional. Without it the export is written to a Flexprice-managed bucket (managed_copy=true) and downloaded from the console; job_config may then only set export_metadata_fields and compression. job_config is required when connection_id is set.
// @Description interval is hourly, daily, weekly, monthly or manual; exports run hourly at most, so 15MIN and 30MIN are rejected. Existing exports may still report 15MIN or 30MIN. Clients should tolerate new values. schedule ({hour, minute, day_of_week 0 (Sunday) to 6 (Saturday), day_of_month 1-28, timezone}) is required for weekly and monthly, optional for hourly and daily, and not allowed for manual; timezone is an IANA name and is required inside schedule. Weekly schedules require day_of_week and monthly schedules require day_of_month; each is rejected for other intervals. A schedule time that daylight saving skips in its timezone (for example 02:30 America/Los_Angeles) is rejected for daily, weekly and monthly exports.
// @Description Window = the previous full period before the scheduled time, in the schedule's timezone; weekly = the previous full ISO week (Monday 00:00 to the following Monday 00:00) whatever day_of_week is, so a Sunday run covers the week that ended six days earlier; monthly = previous calendar month; manual = previous UTC day unless a window is given.
// @Description entity_type report or analytics_view exports a predefined report or a saved view and requires source_config ({report_key, report_params} for report, {view_id} for analytics_view); source_config is rejected for other entity types and cannot be changed later. Clients should tolerate new entity_type values.
// @Description Report sources allow interval daily, weekly, monthly or manual; schedule is required unless manual. The window is the period before the scheduled time's date in the schedule's timezone, as whole UTC days (a Dec 1 00:30 Asia/Kolkata monthly run covers November UTC), and a custom run window must be whole UTC days and its end is exclusive (Nov 1 to Nov 10 excludes Nov 10). A timeseries report grain (or the report's default grain) may not be coarser than the interval: week or month on daily and month on weekly are rejected. export_metadata_fields is not allowed. These exports also need analytics read access.
// @Tags Scheduled Tasks
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param scheduled_task body dto.CreateScheduledTaskRequest true "Scheduled Task"
// @Success 201 {object} dto.ScheduledTaskResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /tasks/scheduled [post]
func (h *ScheduledTaskHandler) CreateScheduledTask(c *gin.Context) {
	var req dto.CreateScheduledTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error(c.Request.Context(), "failed to bind request", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateScheduledTask(c.Request.Context(), req)
	if err != nil {
		h.logger.Error(c.Request.Context(), "failed to create scheduled task", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get scheduled task
// @ID getScheduledTask
// @Description Use when you need to load a single scheduled task (e.g. to show details in a UI or check its configuration).
// @Description report and analytics_view exports need analytics read access; without it this returns 403.
// @Tags Scheduled Tasks
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Scheduled Task ID"
// @Success 200 {object} dto.ScheduledTaskResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 403 {object} ierr.ErrorResponse "Forbidden"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /tasks/scheduled/{id} [get]
func (h *ScheduledTaskHandler) GetScheduledTask(c *gin.Context) {
	id := c.Param("id")

	resp, err := h.service.GetScheduledTask(c.Request.Context(), id)
	if err != nil {
		h.logger.Error(c.Request.Context(), "failed to get scheduled task", "id", id, "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary List scheduled tasks
// @ID listScheduledTasks
// @Description Use when listing or managing scheduled tasks in an admin UI. Returns all exports, including those without a connection; supports filtering and pagination.
// @Description report and analytics_view rows omit source_config when the caller lacks analytics read access.
// @Tags Scheduled Tasks
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
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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
		h.logger.Error(c.Request.Context(), "failed to list scheduled tasks", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update a scheduled task
// @ID updateScheduledTask
// @Description Use when pausing, resuming, renaming or rescheduling a scheduled task. Send at least one of enabled, name or schedule.
// @Description Changing schedule.timezone (an export without a schedule runs in UTC) is rejected once the export has a completed run; create a new export instead. A schedule time that daylight saving skips in its timezone is rejected for daily, weekly and monthly exports.
// @Tags Scheduled Tasks
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Scheduled Task ID"
// @Param scheduled_task body dto.UpdateScheduledTaskRequest true "Update request (enabled, name, schedule)"
// @Success 200 {object} dto.ScheduledTaskResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request or task is archived"
// @Failure 404 {object} ierr.ErrorResponse "Scheduled task not found"
// @Failure 500 {object} ierr.ErrorResponse "Failed to update Temporal schedule"
// @Router /tasks/scheduled/{id} [put]
func (h *ScheduledTaskHandler) UpdateScheduledTask(c *gin.Context) {
	id := c.Param("id")

	var req dto.UpdateScheduledTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error(c.Request.Context(), "failed to bind request", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateScheduledTask(c.Request.Context(), id, req)
	if err != nil {
		h.logger.Error(c.Request.Context(), "failed to update scheduled task", "id", id, "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete a scheduled task
// @ID deleteScheduledTask
// @Description Use when removing a scheduled task from the active roster. Archives the task and removes it from the scheduler (soft delete).
// @Tags Scheduled Tasks
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
		h.logger.Error(c.Request.Context(), "failed to delete scheduled task", "id", id, "error", err)
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
// @ID triggerScheduledTaskRun
// @Description Use when you need to run a scheduled export immediately (e.g. on-demand report or catch-up). Supports optional custom time range.
// @Description Without a requested time range, manual exports and exports with a schedule omit start_time and end_time from the response; other exports return the calculated range.
// @Description Returns 400 when the export is paused (enabled is false); turn it on first.
// @Description A custom window's end_time is exclusive: a window from Nov 1 to Nov 10 excludes Nov 10.
// @Tags Scheduled Tasks
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Scheduled Task ID"
// @Param request body dto.TriggerForceRunRequest false "Optional start and end time for custom range"
// @Success 200 {object} dto.TriggerForceRunResponse "Returns workflow details and time range"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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
		h.logger.Debug(c.Request.Context(), "no custom time range provided, using automatic calculation", "id", id)
		req = dto.TriggerForceRunRequest{} // Empty request for automatic
	} else {
		// Validate the request
		if err := req.Validate(); err != nil {
			h.logger.Error(c.Request.Context(), "invalid force run request", "id", id, "error", err)
			c.Error(err)
			return
		}
		h.logger.Info(c.Request.Context(), "force run with custom time range",
			"id", id,
			"start_time", req.StartTime,
			"end_time", req.EndTime)
	}

	response, err := h.service.TriggerForceRun(c.Request.Context(), id, req)
	if err != nil {
		h.logger.Error(c.Request.Context(), "failed to trigger force run", "id", id, "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// @Summary Get scheduled task capabilities
// @ID getScheduledTaskCapabilities
// @Description Use when building the export form to learn whether email delivery is available and the recipient limit.
// @Tags Scheduled Tasks
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Success 200 {object} dto.ScheduledTaskCapabilitiesResponse
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /tasks/scheduled/capabilities [get]
func (h *ScheduledTaskHandler) GetCapabilities(c *gin.Context) {
	c.JSON(http.StatusOK, h.service.GetCapabilities(c.Request.Context()))
}
