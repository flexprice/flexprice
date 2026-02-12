package dto

import "time"

// WorkflowExecutionDTO represents a workflow execution summary
type WorkflowExecutionDTO struct {
	WorkflowID    string     `json:"workflow_id"`
	RunID         string     `json:"run_id"`
	WorkflowType  string     `json:"workflow_type"`
	TaskQueue     string     `json:"task_queue"`
	Status        string     `json:"status,omitempty"`
	Entity        string     `json:"entity,omitempty"`    // e.g. plan, invoice, subscription
	EntityID      string     `json:"entity_id,omitempty"` // e.g. plan ID, invoice ID
	StartTime     time.Time  `json:"start_time"`
	CloseTime     *time.Time `json:"close_time,omitempty"`
	DurationMs    *int64     `json:"duration_ms,omitempty"`
	TotalDuration string     `json:"total_duration,omitempty"`
	CreatedBy     string     `json:"created_by,omitempty"`
}

// WorkflowActivityDTO represents an activity execution within a workflow
type WorkflowActivityDTO struct {
	ActivityID   string            `json:"activity_id"`
	ActivityType string            `json:"activity_type"`
	Status       string            `json:"status"`
	StartTime    *time.Time        `json:"start_time,omitempty"`
	CloseTime    *time.Time        `json:"close_time,omitempty"`
	DurationMs   *int64            `json:"duration_ms,omitempty"`
	RetryAttempt int32             `json:"retry_attempt"`
	Error        *ActivityErrorDTO `json:"error,omitempty"`
}

// ActivityErrorDTO represents an activity error
type ActivityErrorDTO struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// WorkflowTimelineItemDTO represents a timeline event
type WorkflowTimelineItemDTO struct {
	ID        string     `json:"id"`
	Group     string     `json:"group"`
	Content   string     `json:"content"`
	Start     time.Time  `json:"start"`
	End       *time.Time `json:"end,omitempty"`
	Status    string     `json:"status,omitempty"`
	EventType string     `json:"event_type,omitempty"`
}

// SearchWorkflowsRequest represents the request body for searching workflows (POST /workflows/search).
// Mirrors ListWorkflowsRequest but takes params in JSON body like other /search endpoints.
type SearchWorkflowsRequest struct {
	WorkflowID     string `json:"workflow_id,omitempty"`
	WorkflowType   string `json:"workflow_type,omitempty"`
	TaskQueue      string `json:"task_queue,omitempty"`
	WorkflowStatus string `json:"workflow_status,omitempty"` // e.g. Running, Completed, Failed

	// Entity column filters (efficient; not metadata JSONB)
	Entity   string `json:"entity,omitempty"`    // e.g. plan, invoice, subscription
	EntityID string `json:"entity_id,omitempty"` // e.g. plan_01ABC123

	// Sorting (optional)
	Sort  string `json:"sort,omitempty"`  // e.g. start_time, end_time, created_at
	Order string `json:"order,omitempty"` // asc | desc

	// Pagination (primary: limit/offset like other /search endpoints; page/page_size are aliases)
	PageSize int `json:"page_size,omitempty"` // alias for limit
	Page     int `json:"page,omitempty"`      // alias for offset via (page-1)*page_size
	Limit    int `json:"limit,omitempty"`
	Offset   int `json:"offset,omitempty"`
}

// ListWorkflowsResponse represents the response for listing workflows
type ListWorkflowsResponse struct {
	Workflows  []*WorkflowExecutionDTO `json:"workflows"`
	Total      int64                   `json:"total"`
	Page       int                     `json:"page"`
	PageSize   int                     `json:"page_size"`
	TotalPages int                     `json:"total_pages"`
}

// WorkflowDetailsResponse represents the full details of a workflow execution
type WorkflowDetailsResponse struct {
	WorkflowID    string                     `json:"workflow_id"`
	RunID         string                     `json:"run_id"`
	WorkflowType  string                     `json:"workflow_type"`
	Status        string                     `json:"status"`
	StartTime     time.Time                  `json:"start_time"`
	CloseTime     *time.Time                 `json:"close_time,omitempty"`
	DurationMs    *int64                     `json:"duration_ms,omitempty"`
	TotalDuration string                     `json:"total_duration,omitempty"`
	TaskQueue     string                     `json:"task_queue"`
	HistorySize   int64                      `json:"history_size"`
	Activities    []*WorkflowActivityDTO     `json:"activities"`
	Timeline      []*WorkflowTimelineItemDTO `json:"timeline,omitempty"`
	Metadata      map[string]interface{}     `json:"metadata,omitempty"`
}

// WorkflowSummaryResponse represents a lightweight summary of a workflow execution
type WorkflowSummaryResponse struct {
	WorkflowID       string     `json:"workflow_id"`
	RunID            string     `json:"run_id"`
	WorkflowType     string     `json:"workflow_type"`
	Status           string     `json:"status"`
	StartTime        time.Time  `json:"start_time"`
	CloseTime        *time.Time `json:"close_time,omitempty"`
	DurationMs       *int64     `json:"duration_ms,omitempty"`
	TotalDuration    string     `json:"total_duration,omitempty"`
	ActivityCount    int        `json:"activity_count"`
	FailedActivities int        `json:"failed_activities"`
}

// WorkflowTimelineResponse represents the timeline view of a workflow execution
type WorkflowTimelineResponse struct {
	WorkflowID string                     `json:"workflow_id"`
	RunID      string                     `json:"run_id"`
	StartTime  time.Time                  `json:"start_time"`
	CloseTime  *time.Time                 `json:"close_time,omitempty"`
	Items      []*WorkflowTimelineItemDTO `json:"items"`
}

// BatchWorkflowsRequest represents a batch request for multiple workflows
type BatchWorkflowsRequest struct {
	Workflows         []WorkflowIdentifier `json:"workflows" binding:"required"`
	IncludeActivities bool                 `json:"include_activities"`
}

// WorkflowIdentifier identifies a specific workflow execution
type WorkflowIdentifier struct {
	WorkflowID string `json:"workflow_id" binding:"required"`
	RunID      string `json:"run_id" binding:"required"`
}

// BatchWorkflowsResponse represents the response for batch workflow query
type BatchWorkflowsResponse struct {
	Workflows []*WorkflowDetailsResponse `json:"workflows"`
}
