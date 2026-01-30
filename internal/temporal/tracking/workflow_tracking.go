package tracking

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// TrackWorkflowStartInput contains the parameters for tracking a workflow start
type TrackWorkflowStartInput struct {
	WorkflowType  string
	TaskQueue     string
	TenantID      string
	EnvironmentID string
	UserID        string
	Metadata      map[string]interface{}
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
