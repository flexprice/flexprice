package tracking

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// ShouldTrackWorkflow returns false if the workflow type (by name) is excluded from tracking.
// Delegates to types.ShouldTrackWorkflowType so exclusion uses the canonical TemporalWorkflowType enums.
func ShouldTrackWorkflow(workflowTypeName string) bool {
	return types.ShouldTrackWorkflowType(types.TemporalWorkflowType(workflowTypeName))
}

// TrackWorkflowStartInput contains the parameters for tracking a workflow start
type TrackWorkflowStartInput struct {
	WorkflowType  string
	TaskQueue     string
	TenantID      string
	EnvironmentID string
	UserID        string
	Entity        string // e.g. plan, invoice, subscription (for efficient filtering)
	EntityID      string // e.g. plan ID, invoice ID
	Metadata      map[string]interface{}
}

// TrackWorkflowEndInput contains the parameters for tracking a workflow end
type TrackWorkflowEndInput struct {
	WorkflowStatus types.WorkflowExecutionStatus
	Error          string
	EndTime        time.Time
	DurationMs     int64
}

// TrackWorkflowEndActivityInput is the input for the end tracking activity
type TrackWorkflowEndActivityInput struct {
	WorkflowID     string
	RunID          string
	WorkflowStatus types.WorkflowExecutionStatus
	ErrorMessage   string
	EndTime        *time.Time
	DurationMs     *int64
}

// TrackWorkflowStartActivityInput is the input for the tracking activity
type TrackWorkflowStartActivityInput struct {
	WorkflowID    string
	RunID         string
	WorkflowType  string
	TaskQueue     string
	TenantID      string
	EnvironmentID string
	CreatedBy     string
	Entity        string // e.g. plan, invoice, subscription
	EntityID      string // e.g. plan ID, invoice ID
	Metadata      map[string]interface{}
}

// ExecuteTrackWorkflowStart is a helper function to be called at the start of each workflow
// It executes a local activity to save the workflow execution to the database
func ExecuteTrackWorkflowStart(ctx workflow.Context, input TrackWorkflowStartInput) {
	logger := workflow.GetLogger(ctx)
	info := workflow.GetInfo(ctx)

	// Extract workflow execution details
	workflowID := info.WorkflowExecution.ID
	runID := info.WorkflowExecution.RunID

	logger.Info("Tracking workflow start",
		"workflow_id", workflowID,
		"run_id", runID,
		"workflow_type", input.WorkflowType,
		"task_queue", input.TaskQueue)

	// Prepare activity input
	activityInput := TrackWorkflowStartActivityInput{
		WorkflowID:    workflowID,
		RunID:         runID,
		WorkflowType:  input.WorkflowType,
		TaskQueue:     input.TaskQueue,
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		CreatedBy:     input.UserID,
		Entity:        input.Entity,
		EntityID:      input.EntityID,
		Metadata:      input.Metadata,
	}

	// Execute as local activity (fast, doesn't need to be in history)
	localActivityOptions := workflow.LocalActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}

	ctx = workflow.WithLocalActivityOptions(ctx, localActivityOptions)

	// Execute local activity by name (activity is registered in registration.go)
	err := workflow.ExecuteLocalActivity(ctx, "TrackWorkflowStart", activityInput).Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to execute tracking activity", "error", err)
		// Don't fail the workflow - tracking is non-critical
	}

	logger.Info("Successfully tracked workflow start", "workflow_id", workflowID, "run_id", runID)
}

// ExecuteTrackWorkflowEnd is a helper function to be called at the end of each workflow
// It executes a local activity to update the workflow execution status in the database
func ExecuteTrackWorkflowEnd(ctx workflow.Context, input TrackWorkflowEndInput) {
	logger := workflow.GetLogger(ctx)
	info := workflow.GetInfo(ctx)

	// Extract workflow execution details
	workflowID := info.WorkflowExecution.ID
	runID := info.WorkflowExecution.RunID

	logger.Info("Tracking workflow end",
		"workflow_id", workflowID,
		"run_id", runID,
		"status", input.WorkflowStatus)

	// Prepare activity input
	activityInput := TrackWorkflowEndActivityInput{
		WorkflowID:     workflowID,
		RunID:          runID,
		WorkflowStatus: input.WorkflowStatus,
		ErrorMessage:   input.Error,
		EndTime:        &input.EndTime,
		DurationMs:     &input.DurationMs,
	}

	// Execute as local activity (fast, doesn't need to be in history)
	localActivityOptions := workflow.LocalActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}

	ctx = workflow.WithLocalActivityOptions(ctx, localActivityOptions)

	// Execute local activity by name (activity is registered in registration.go)
	err := workflow.ExecuteLocalActivity(ctx, "TrackWorkflowEnd", activityInput).Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to execute end tracking activity", "error", err)
		// Don't fail the workflow - tracking is non-critical
	}

	logger.Info("Successfully tracked workflow end", "workflow_id", workflowID, "run_id", runID)
}
