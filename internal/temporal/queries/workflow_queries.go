package queries

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"

	"github.com/flexprice/flexprice/internal/logger"
)

// WorkflowQuerier provides methods for querying Temporal workflows
type WorkflowQuerier struct {
	client client.Client
	logger *logger.Logger
}

// NewWorkflowQuerier creates a new WorkflowQuerier instance
func NewWorkflowQuerier(temporalClient client.Client, logger *logger.Logger) *WorkflowQuerier {
	return &WorkflowQuerier{
		client: temporalClient,
		logger: logger,
	}
}

// WorkflowExecutionInfo contains basic workflow execution information
type WorkflowExecutionInfo struct {
	WorkflowID   string
	RunID        string
	WorkflowType string
	Status       string
	StartTime    time.Time
	CloseTime    *time.Time
	DurationMs   *int64
	TaskQueue    string
	HistorySize  int64
}

// ActivityExecutionInfo contains activity execution details
type ActivityExecutionInfo struct {
	ActivityID   string
	ActivityType string
	Status       string
	StartTime    *time.Time
	CloseTime    *time.Time
	DurationMs   *int64
	RetryAttempt int32
	ErrorMessage string
	ErrorType    string
}

// TimelineEvent represents a workflow event for timeline visualization
type TimelineEvent struct {
	EventID   int64
	EventType string
	EventTime time.Time
	Details   string
}

// DescribeWorkflow retrieves workflow execution details
func (q *WorkflowQuerier) DescribeWorkflow(ctx context.Context, workflowID, runID string) (*WorkflowExecutionInfo, error) {
	q.logger.Info("Describing workflow execution", "workflow_id", workflowID, "run_id", runID)

	resp, err := q.client.DescribeWorkflowExecution(ctx, workflowID, runID)
	if err != nil {
		q.logger.Error("Failed to describe workflow execution", "error", err, "workflow_id", workflowID, "run_id", runID)
		return nil, fmt.Errorf("failed to describe workflow: %w", err)
	}

	info := &WorkflowExecutionInfo{
		WorkflowID:   resp.WorkflowExecutionInfo.Execution.WorkflowId,
		RunID:        resp.WorkflowExecutionInfo.Execution.RunId,
		WorkflowType: resp.WorkflowExecutionInfo.Type.Name,
		Status:       resp.WorkflowExecutionInfo.Status.String(),
		StartTime:    resp.WorkflowExecutionInfo.StartTime.AsTime(),
		TaskQueue:    resp.ExecutionConfig.TaskQueue.Name,
		HistorySize:  resp.WorkflowExecutionInfo.HistoryLength,
	}

	// Set close time and duration if workflow is closed
	if resp.WorkflowExecutionInfo.CloseTime != nil {
		closeTime := resp.WorkflowExecutionInfo.CloseTime.AsTime()
		info.CloseTime = &closeTime
		durationMs := closeTime.Sub(info.StartTime).Milliseconds()
		info.DurationMs = &durationMs
	}

	return info, nil
}

// GetWorkflowHistory retrieves the workflow execution history
func (q *WorkflowQuerier) GetWorkflowHistory(ctx context.Context, workflowID, runID string) (client.HistoryEventIterator, error) {
	q.logger.Info("Getting workflow history", "workflow_id", workflowID, "run_id", runID)

	iter := q.client.GetWorkflowHistory(ctx, workflowID, runID, false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	return iter, nil
}

// ParseActivitiesFromHistory parses activity executions from workflow history events
func (q *WorkflowQuerier) ParseActivitiesFromHistory(ctx context.Context, workflowID, runID string) ([]*ActivityExecutionInfo, error) {
	iter, err := q.GetWorkflowHistory(ctx, workflowID, runID)
	if err != nil {
		return nil, err
	}

	activities := make(map[int64]*ActivityExecutionInfo)
	activityOrder := []int64{}

	for iter.HasNext() {
		event, err := iter.Next()
		if err != nil {
			q.logger.Error("Error iterating history events", "error", err)
			return nil, fmt.Errorf("failed to iterate history: %w", err)
		}

		switch event.EventType {
		case enums.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED:
			attrs := event.GetActivityTaskScheduledEventAttributes()
			activityInfo := &ActivityExecutionInfo{
				ActivityID:   fmt.Sprintf("%d", event.EventId),
				ActivityType: attrs.ActivityType.Name,
				Status:       "SCHEDULED",
				RetryAttempt: 0,
			}
			activities[event.EventId] = activityInfo
			activityOrder = append(activityOrder, event.EventId)

		case enums.EVENT_TYPE_ACTIVITY_TASK_STARTED:
			attrs := event.GetActivityTaskStartedEventAttributes()
			if activity, exists := activities[attrs.ScheduledEventId]; exists {
				activity.Status = "STARTED"
				startTime := event.EventTime.AsTime()
				activity.StartTime = &startTime
				activity.RetryAttempt = attrs.Attempt
			}

		case enums.EVENT_TYPE_ACTIVITY_TASK_COMPLETED:
			attrs := event.GetActivityTaskCompletedEventAttributes()
			if activity, exists := activities[attrs.ScheduledEventId]; exists {
				activity.Status = "COMPLETED"
				closeTime := event.EventTime.AsTime()
				activity.CloseTime = &closeTime
				if activity.StartTime != nil {
					duration := closeTime.Sub(*activity.StartTime).Milliseconds()
					activity.DurationMs = &duration
				}
			}

		case enums.EVENT_TYPE_ACTIVITY_TASK_FAILED:
			attrs := event.GetActivityTaskFailedEventAttributes()
			if activity, exists := activities[attrs.ScheduledEventId]; exists {
				activity.Status = "FAILED"
				closeTime := event.EventTime.AsTime()
				activity.CloseTime = &closeTime
				if activity.StartTime != nil {
					duration := closeTime.Sub(*activity.StartTime).Milliseconds()
					activity.DurationMs = &duration
				}
				if attrs.Failure != nil {
					activity.ErrorMessage = attrs.Failure.Message
					activity.ErrorType = "ActivityError"
				}
				if attrs.RetryState.String() != "RETRY_STATE_IN_PROGRESS" {
					activity.RetryAttempt = activity.RetryAttempt + 1
				}
			}

		case enums.EVENT_TYPE_ACTIVITY_TASK_TIMED_OUT:
			attrs := event.GetActivityTaskTimedOutEventAttributes()
			if activity, exists := activities[attrs.ScheduledEventId]; exists {
				activity.Status = "TIMED_OUT"
				closeTime := event.EventTime.AsTime()
				activity.CloseTime = &closeTime
				if activity.StartTime != nil {
					duration := closeTime.Sub(*activity.StartTime).Milliseconds()
					activity.DurationMs = &duration
				}
				if attrs.Failure != nil {
					activity.ErrorMessage = attrs.Failure.Message
					activity.ErrorType = "TimeoutError"
				}
			}

		case enums.EVENT_TYPE_ACTIVITY_TASK_CANCELED:
			attrs := event.GetActivityTaskCanceledEventAttributes()
			if activity, exists := activities[attrs.ScheduledEventId]; exists {
				activity.Status = "CANCELED"
				closeTime := event.EventTime.AsTime()
				activity.CloseTime = &closeTime
				if activity.StartTime != nil {
					duration := closeTime.Sub(*activity.StartTime).Milliseconds()
					activity.DurationMs = &duration
				}
			}
		}
	}

	// Convert map to ordered slice
	result := make([]*ActivityExecutionInfo, 0, len(activityOrder))
	for _, eventID := range activityOrder {
		if activity, exists := activities[eventID]; exists {
			result = append(result, activity)
		}
	}

	return result, nil
}

// ParseTimelineFromHistory parses workflow events into a timeline format
func (q *WorkflowQuerier) ParseTimelineFromHistory(ctx context.Context, workflowID, runID string) ([]*TimelineEvent, error) {
	iter, err := q.GetWorkflowHistory(ctx, workflowID, runID)
	if err != nil {
		return nil, err
	}

	timeline := []*TimelineEvent{}

	for iter.HasNext() {
		event, err := iter.Next()
		if err != nil {
			return nil, fmt.Errorf("failed to iterate history: %w", err)
		}

		// Only include significant events in timeline
		var details string
		includeEvent := true

		switch event.EventType {
		case enums.EVENT_TYPE_WORKFLOW_EXECUTION_STARTED:
			details = "Workflow execution started"
		case enums.EVENT_TYPE_WORKFLOW_EXECUTION_COMPLETED:
			details = "Workflow execution completed successfully"
		case enums.EVENT_TYPE_WORKFLOW_EXECUTION_FAILED:
			attrs := event.GetWorkflowExecutionFailedEventAttributes()
			if attrs.Failure != nil {
				details = fmt.Sprintf("Workflow execution failed: %s", attrs.Failure.Message)
			} else {
				details = "Workflow execution failed"
			}
		case enums.EVENT_TYPE_WORKFLOW_EXECUTION_TIMED_OUT:
			details = "Workflow execution timed out"
		case enums.EVENT_TYPE_WORKFLOW_EXECUTION_CANCELED:
			details = "Workflow execution canceled"
		case enums.EVENT_TYPE_WORKFLOW_EXECUTION_TERMINATED:
			details = "Workflow execution terminated"
		case enums.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED:
			attrs := event.GetActivityTaskScheduledEventAttributes()
			details = fmt.Sprintf("Activity %s scheduled", attrs.ActivityType.Name)
		case enums.EVENT_TYPE_ACTIVITY_TASK_STARTED:
			attrs := event.GetActivityTaskStartedEventAttributes()
			details = fmt.Sprintf("Activity started (attempt %d)", attrs.Attempt)
		case enums.EVENT_TYPE_ACTIVITY_TASK_COMPLETED:
			details = "Activity completed"
		case enums.EVENT_TYPE_ACTIVITY_TASK_FAILED:
			attrs := event.GetActivityTaskFailedEventAttributes()
			if attrs.Failure != nil {
				details = fmt.Sprintf("Activity failed: %s", attrs.Failure.Message)
			} else {
				details = "Activity failed"
			}
		case enums.EVENT_TYPE_ACTIVITY_TASK_TIMED_OUT:
			details = "Activity timed out"
		default:
			includeEvent = false
		}

		if includeEvent {
			timeline = append(timeline, &TimelineEvent{
				EventID:   event.EventId,
				EventType: event.EventType.String(),
				EventTime: event.EventTime.AsTime(),
				Details:   details,
			})
		}
	}

	return timeline, nil
}

// DescribeWorkflowBatch retrieves details for multiple workflows in parallel
func (q *WorkflowQuerier) DescribeWorkflowBatch(ctx context.Context, executions []struct{ WorkflowID, RunID string }) ([]*WorkflowExecutionInfo, error) {
	type result struct {
		info *WorkflowExecutionInfo
		err  error
		idx  int
	}

	results := make(chan result, len(executions))

	// Query all workflows in parallel
	for idx, exec := range executions {
		go func(i int, wfID, rID string) {
			info, err := q.DescribeWorkflow(ctx, wfID, rID)
			results <- result{info: info, err: err, idx: i}
		}(idx, exec.WorkflowID, exec.RunID)
	}

	// Collect results
	infos := make([]*WorkflowExecutionInfo, len(executions))
	for i := 0; i < len(executions); i++ {
		res := <-results
		if res.err != nil {
			q.logger.Error("Failed to describe workflow in batch", "error", res.err, "index", res.idx)
			// Continue collecting other results, don't fail the entire batch
			continue
		}
		infos[res.idx] = res.info
	}

	return infos, nil
}
