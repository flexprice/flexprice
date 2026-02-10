package v1

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal/queries"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
)

type WorkflowHandler struct {
	workflowExecService *service.WorkflowExecutionService
	temporalService     temporalservice.TemporalService
	workflowQuerier     *queries.WorkflowQuerier
	log                 *logger.Logger
}

func normalizeWorkflowStatus(in string) string {
	s := strings.TrimSpace(in)
	if s == "" {
		return ""
	}
	switch strings.ToLower(s) {
	case "running":
		return string(types.WorkflowExecutionStatusRunning)
	case "completed":
		return string(types.WorkflowExecutionStatusCompleted)
	case "failed":
		return string(types.WorkflowExecutionStatusFailed)
	case "canceled", "cancelled":
		return string(types.WorkflowExecutionStatusCanceled)
	case "terminated":
		return string(types.WorkflowExecutionStatusTerminated)
	case "continuedasnew", "continued_as_new", "continued-as-new":
		return string(types.WorkflowExecutionStatusContinuedAsNew)
	case "timedout", "timed_out", "timed-out":
		return string(types.WorkflowExecutionStatusTimedOut)
	case "unknown":
		return string(types.WorkflowExecutionStatusUnknown)
	default:
		// Return original to avoid rejecting future/new statuses.
		return s
	}
}

func NewWorkflowHandler(
	workflowExecService *service.WorkflowExecutionService,
	temporalService temporalservice.TemporalService,
	workflowQuerier *queries.WorkflowQuerier,
	log *logger.Logger,
) *WorkflowHandler {
	return &WorkflowHandler{
		workflowExecService: workflowExecService,
		temporalService:     temporalService,
		workflowQuerier:     workflowQuerier,
		log:                 log,
	}
}

func (h *WorkflowHandler) ListWorkflows(c *gin.Context) {
	// NOTE: This endpoint is used as POST /workflows/search (body-based filters),
	// consistent with other /search APIs in this codebase.
	var req dto.SearchWorkflowsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	// Get tenant and environment from context
	tenantID := types.GetTenantID(c.Request.Context())
	environmentID := types.GetEnvironmentID(c.Request.Context())

	if tenantID == "" {
		c.Error(ierr.NewError("tenant_id is required").
			WithHint("Tenant ID must be present in context").
			Mark(ierr.ErrValidation))
		return
	}

	// Pagination (match /search semantics: limit/offset are primary; page/page_size are aliases)
	limit := req.Limit
	offset := req.Offset

	if limit <= 0 {
		limit = req.PageSize // alias
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	// If client provided page/page_size but not offset, derive offset.
	if req.Offset == 0 && req.Limit <= 0 && req.PageSize > 0 {
		page := req.Page
		if page <= 0 {
			page = 1
		}
		offset = (page - 1) * limit
	}

	// Query database for workflow executions
	filters := &service.ListWorkflowExecutionsFilters{
		TenantID:       tenantID,
		EnvironmentID:  environmentID,
		WorkflowID:     req.WorkflowID,
		WorkflowType:   req.WorkflowType,
		TaskQueue:      req.TaskQueue,
		WorkflowStatus: normalizeWorkflowStatus(req.WorkflowStatus),
		Entity:         req.Entity,
		EntityID:       req.EntityID,
		Sort:           req.Sort,
		Order:          req.Order,
		Limit:          limit,
		Offset:         offset,
	}

	executions, total, err := h.workflowExecService.ListWorkflowExecutions(c.Request.Context(), filters)
	if err != nil {
		c.Error(err)
		return
	}

	// Convert to DTOs
	workflowDTOs := make([]*dto.WorkflowExecutionDTO, len(executions))
	for i, exec := range executions {
		workflowDTOs[i] = &dto.WorkflowExecutionDTO{
			WorkflowID:   exec.WorkflowID,
			RunID:        exec.RunID,
			WorkflowType: exec.WorkflowType,
			TaskQueue:    exec.TaskQueue,
			Status:       string(exec.WorkflowStatus),
			StartTime:    exec.StartTime,
			Entity:       lo.FromPtr(exec.Entity),
			EntityID:     lo.FromPtr(exec.EntityID),
			CloseTime:    exec.EndTime,
			DurationMs:   exec.DurationMs,
			CreatedBy:    exec.CreatedBy,
		}
	}

	totalPages := service.CalculateTotalPages(total, limit)
	page := 1
	if limit > 0 {
		page = (offset / limit) + 1
	}

	response := &dto.ListWorkflowsResponse{
		Workflows:  workflowDTOs,
		Total:      total,
		Page:       page,
		PageSize:   limit,
		TotalPages: totalPages,
	}

	c.JSON(http.StatusOK, response)
}

func (h *WorkflowHandler) GetWorkflowSummary(c *gin.Context) {
	workflowID := c.Param("workflow_id")
	runID := c.Param("run_id")

	if workflowID == "" || runID == "" {
		c.Error(ierr.NewError("workflow_id and run_id are required").
			WithHint("Both workflow ID and run ID must be provided").
			Mark(ierr.ErrValidation))
		return
	}

	// Query Temporal for workflow details
	info, err := h.workflowQuerier.DescribeWorkflow(c.Request.Context(), workflowID, runID)
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Failed to retrieve workflow summary from Temporal").
			Mark(ierr.ErrInternal))
		return
	}

	// Get activities to count them
	activities, err := h.workflowQuerier.ParseActivitiesFromHistory(c.Request.Context(), workflowID, runID)
	if err != nil {
		h.log.Error("Failed to parse activities for summary", "error", err)
		// Continue without activity counts
		activities = []*queries.ActivityExecutionInfo{}
	}

	// Count failed activities
	failedCount := 0
	for _, activity := range activities {
		if activity.Status == "FAILED" {
			failedCount++
		}
	}

	// Format duration
	totalDuration := formatDuration(info.StartTime, info.CloseTime)

	response := &dto.WorkflowSummaryResponse{
		WorkflowID:       info.WorkflowID,
		RunID:            info.RunID,
		WorkflowType:     info.WorkflowType,
		Status:           info.Status,
		StartTime:        info.StartTime,
		CloseTime:        info.CloseTime,
		DurationMs:       info.DurationMs,
		TotalDuration:    totalDuration,
		ActivityCount:    len(activities),
		FailedActivities: failedCount,
	}

	c.JSON(http.StatusOK, response)
}

func (h *WorkflowHandler) GetWorkflowDetails(c *gin.Context) {
	workflowID := c.Param("workflow_id")
	runID := c.Param("run_id")

	if workflowID == "" || runID == "" {
		c.Error(ierr.NewError("workflow_id and run_id are required").
			WithHint("Both workflow ID and run ID must be provided").
			Mark(ierr.ErrValidation))
		return
	}

	// Query Temporal for workflow details
	info, err := h.workflowQuerier.DescribeWorkflow(c.Request.Context(), workflowID, runID)
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Failed to retrieve workflow details from Temporal").
			Mark(ierr.ErrInternal))
		return
	}

	// Parse activities from history
	activities, err := h.workflowQuerier.ParseActivitiesFromHistory(c.Request.Context(), workflowID, runID)
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Failed to parse workflow activities").
			Mark(ierr.ErrInternal))
		return
	}

	// Parse timeline from history
	timelineEvents, err := h.workflowQuerier.ParseTimelineFromHistory(c.Request.Context(), workflowID, runID)
	if err != nil {
		h.log.Error("Failed to parse timeline", "error", err)
		// Continue without timeline
		timelineEvents = []*queries.TimelineEvent{}
	}

	// Convert activities to DTOs
	activityDTOs := make([]*dto.WorkflowActivityDTO, len(activities))
	for i, activity := range activities {
		activityDTO := &dto.WorkflowActivityDTO{
			ActivityID:   activity.ActivityID,
			ActivityType: activity.ActivityType,
			Status:       activity.Status,
			StartTime:    activity.StartTime,
			CloseTime:    activity.CloseTime,
			DurationMs:   activity.DurationMs,
			RetryAttempt: activity.RetryAttempt,
		}

		if activity.ErrorMessage != "" {
			activityDTO.Error = &dto.ActivityErrorDTO{
				Message: activity.ErrorMessage,
				Type:    activity.ErrorType,
			}
		}

		activityDTOs[i] = activityDTO
	}

	// Convert timeline to DTOs
	timelineDTOs := make([]*dto.WorkflowTimelineItemDTO, len(timelineEvents))
	for i, event := range timelineEvents {
		timelineDTOs[i] = &dto.WorkflowTimelineItemDTO{
			ID:        fmt.Sprintf("event-%d", event.EventID),
			Group:     "events",
			Content:   event.Details,
			Start:     event.EventTime,
			EventType: event.EventType,
		}
	}

	// Try to get metadata from database
	var metadata map[string]interface{}
	if dbExec, err := h.workflowExecService.GetWorkflowExecution(c.Request.Context(), workflowID, runID); err == nil {
		metadata = dbExec.Metadata
	}

	// Format duration
	totalDuration := formatDuration(info.StartTime, info.CloseTime)

	response := &dto.WorkflowDetailsResponse{
		WorkflowID:    info.WorkflowID,
		RunID:         info.RunID,
		WorkflowType:  info.WorkflowType,
		Status:        info.Status,
		StartTime:     info.StartTime,
		CloseTime:     info.CloseTime,
		DurationMs:    info.DurationMs,
		TotalDuration: totalDuration,
		TaskQueue:     info.TaskQueue,
		HistorySize:   info.HistorySize,
		Activities:    activityDTOs,
		Timeline:      timelineDTOs,
		Metadata:      metadata,
	}

	c.JSON(http.StatusOK, response)
}

func (h *WorkflowHandler) GetWorkflowTimeline(c *gin.Context) {
	workflowID := c.Param("workflow_id")
	runID := c.Param("run_id")

	if workflowID == "" || runID == "" {
		c.Error(ierr.NewError("workflow_id and run_id are required").
			WithHint("Both workflow ID and run ID must be provided").
			Mark(ierr.ErrValidation))
		return
	}

	// Query Temporal for basic info
	info, err := h.workflowQuerier.DescribeWorkflow(c.Request.Context(), workflowID, runID)
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Failed to retrieve workflow details from Temporal").
			Mark(ierr.ErrInternal))
		return
	}

	// Parse activities for timeline
	activities, err := h.workflowQuerier.ParseActivitiesFromHistory(c.Request.Context(), workflowID, runID)
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Failed to parse workflow timeline").
			Mark(ierr.ErrInternal))
		return
	}

	// Build timeline items
	items := []*dto.WorkflowTimelineItemDTO{
		// Workflow execution item
		{
			ID:      "workflow",
			Group:   "execution",
			Content: info.WorkflowType,
			Start:   info.StartTime,
			End:     info.CloseTime,
			Status:  info.Status,
		},
	}

	// Add activity items
	for _, activity := range activities {
		item := &dto.WorkflowTimelineItemDTO{
			ID:      activity.ActivityID,
			Group:   "activities",
			Content: activity.ActivityType,
			Status:  activity.Status,
		}
		if activity.StartTime != nil {
			item.Start = *activity.StartTime
		}
		if activity.CloseTime != nil {
			item.End = activity.CloseTime
		}
		items = append(items, item)
	}

	response := &dto.WorkflowTimelineResponse{
		WorkflowID: info.WorkflowID,
		RunID:      info.RunID,
		StartTime:  info.StartTime,
		CloseTime:  info.CloseTime,
		Items:      items,
	}

	c.JSON(http.StatusOK, response)
}

func (h *WorkflowHandler) GetWorkflowsBatch(c *gin.Context) {
	var req dto.BatchWorkflowsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	if len(req.Workflows) == 0 {
		c.Error(ierr.NewError("workflows list cannot be empty").
			WithHint("At least one workflow must be specified").
			Mark(ierr.ErrValidation))
		return
	}

	if len(req.Workflows) > 50 {
		c.Error(ierr.NewError("too many workflows requested").
			WithHint("Maximum 50 workflows can be queried at once").
			Mark(ierr.ErrValidation))
		return
	}

	// Convert to query format
	executions := make([]struct{ WorkflowID, RunID string }, len(req.Workflows))
	for i, wf := range req.Workflows {
		executions[i] = struct{ WorkflowID, RunID string }{
			WorkflowID: wf.WorkflowID,
			RunID:      wf.RunID,
		}
	}

	// Query Temporal for all workflows in parallel
	infos, err := h.workflowQuerier.DescribeWorkflowBatch(c.Request.Context(), executions)
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Failed to retrieve batch workflow details").
			Mark(ierr.ErrInternal))
		return
	}

	// Build response
	workflowDetails := make([]*dto.WorkflowDetailsResponse, 0, len(infos))
	for i, info := range infos {
		if info == nil {
			h.log.Warn("Skipping nil workflow info in batch", "index", i)
			continue
		}

		totalDuration := formatDuration(info.StartTime, info.CloseTime)

		detail := &dto.WorkflowDetailsResponse{
			WorkflowID:    info.WorkflowID,
			RunID:         info.RunID,
			WorkflowType:  info.WorkflowType,
			Status:        info.Status,
			StartTime:     info.StartTime,
			CloseTime:     info.CloseTime,
			DurationMs:    info.DurationMs,
			TotalDuration: totalDuration,
			TaskQueue:     info.TaskQueue,
			HistorySize:   info.HistorySize,
			Activities:    []*dto.WorkflowActivityDTO{},
			Timeline:      []*dto.WorkflowTimelineItemDTO{},
		}

		// Include activities if requested
		if req.IncludeActivities {
			activities, err := h.workflowQuerier.ParseActivitiesFromHistory(c.Request.Context(), info.WorkflowID, info.RunID)
			if err != nil {
				h.log.Error("Failed to parse activities for workflow in batch",
					"workflow_id", info.WorkflowID,
					"run_id", info.RunID,
					"error", err)
				// Continue without activities for this workflow
			} else {
				// Convert activities to DTOs
				activityDTOs := make([]*dto.WorkflowActivityDTO, len(activities))
				for j, activity := range activities {
					activityDTO := &dto.WorkflowActivityDTO{
						ActivityID:   activity.ActivityID,
						ActivityType: activity.ActivityType,
						Status:       activity.Status,
						StartTime:    activity.StartTime,
						CloseTime:    activity.CloseTime,
						DurationMs:   activity.DurationMs,
						RetryAttempt: activity.RetryAttempt,
					}

					if activity.ErrorMessage != "" {
						activityDTO.Error = &dto.ActivityErrorDTO{
							Message: activity.ErrorMessage,
							Type:    activity.ErrorType,
						}
					}

					activityDTOs[j] = activityDTO
				}
				detail.Activities = activityDTOs
			}
		}

		workflowDetails = append(workflowDetails, detail)
	}

	response := &dto.BatchWorkflowsResponse{
		Workflows: workflowDetails,
	}

	c.JSON(http.StatusOK, response)
}

// formatDuration formats the duration between start and close times
func formatDuration(startTime time.Time, closeTime *time.Time) string {
	if closeTime == nil {
		// Workflow is still running
		duration := time.Since(startTime)
		return fmt.Sprintf("running for %s", formatDurationString(duration))
	}

	duration := closeTime.Sub(startTime)
	return formatDurationString(duration)
}

// formatDurationString formats a duration into a human-readable string
func formatDurationString(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		if seconds == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}
