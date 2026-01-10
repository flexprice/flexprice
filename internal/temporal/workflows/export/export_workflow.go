package export

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/temporal/activities/export"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

var temporalScheduledStartTimeKey = temporal.NewSearchAttributeKeyTime("TemporalScheduledStartTime")

// ExecuteExportWorkflowInput represents input for the export workflow
type ExecuteExportWorkflowInput struct {
	ScheduledTaskID string
	TenantID        string
	EnvID           string
	UserID          string // User who triggered the workflow (empty for scheduled runs)
	// Optional custom time range for force runs
	CustomStartTime *time.Time
	CustomEndTime   *time.Time
}

// ExecuteExportWorkflowOutput represents output from the export workflow
type ExecuteExportWorkflowOutput struct {
	TaskID      string
	FileURL     string
	RecordCount int
	Success     bool
}

// ExecuteExportWorkflow performs the complete export workflow
// This is the single unified workflow that:
// 1. Fetches scheduled task configuration
// 2. Creates a task record
// 3. Exports the data
// 4. Updates both task and scheduled task statuses
func ExecuteExportWorkflow(ctx workflow.Context, input ExecuteExportWorkflowInput) (*ExecuteExportWorkflowOutput, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting export workflow",
		"scheduled_task_id", input.ScheduledTaskID,
		"tenant_id", input.TenantID,
		"env_id", input.EnvID)

	// Activity options for lightweight operations (fetching config, updating status)
	lightweightActivityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 1 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2,
		},
	}

	// Activity options for heavy operations (data export)
	heavyActivityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute, // Increased from 15 to 30 minutes for large exports and slow connections
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}

	// Activity instances
	var scheduledTaskActivity export.ScheduledTaskActivity
	var taskActivity export.TaskActivity
	var exportActivity export.ExportActivity

	// ================================================================================
	// STEP 1: Fetch Scheduled Task Details
	// ================================================================================
	logger.Info("Step 1: Fetching scheduled task details")
	ctx = workflow.WithActivityOptions(ctx, lightweightActivityOptions)

	// Get the scheduled execution time (not the actual start time)
	// This is the exact time the schedule wanted to run (e.g., 11:00:00)
	// vs actual start time which might be 11:00:00.040005991
	workflowInfo := workflow.GetInfo(ctx)
	executionTime := workflowInfo.WorkflowStartTime

	// If this workflow was triggered by a schedule, extract the scheduled time
	// from search attributes: TemporalScheduledStartTime
	typedSearchAttrs := workflow.GetTypedSearchAttributes(ctx)
	scheduledTime, ok := typedSearchAttrs.GetTime(temporalScheduledStartTimeKey)
	if ok && !scheduledTime.IsZero() {
		executionTime = scheduledTime
		logger.Info("Using scheduled execution time from search attributes",
			"scheduled_time", scheduledTime,
			"actual_start_time", workflowInfo.WorkflowStartTime)
	}

	logger.Info("Using execution time for interval boundaries", "execution_time", executionTime)

	var taskDetails export.ScheduledTaskDetails
	err := workflow.ExecuteActivity(ctx, scheduledTaskActivity.GetScheduledTaskDetails, export.GetScheduledTaskDetailsInput{
		ScheduledTaskID:       input.ScheduledTaskID,
		TenantID:              input.TenantID,
		EnvID:                 input.EnvID,
		WorkflowExecutionTime: executionTime,         // Pass scheduled execution time
		CustomStartTime:       input.CustomStartTime, // Pass custom time range for force runs
		CustomEndTime:         input.CustomEndTime,
	}).Get(ctx, &taskDetails)
	if err != nil {
		logger.Error("Failed to get scheduled task details", "error", err)
		return nil, fmt.Errorf("failed to get scheduled task: %w", err)
	}

	// Check if task is enabled
	if !taskDetails.Enabled {
		logger.Info("Scheduled task is disabled, skipping execution")
		return &ExecuteExportWorkflowOutput{
			Success: true,
		}, nil
	}

	logger.Info("Scheduled task details retrieved",
		"entity_type", taskDetails.EntityType,
		"start_time", taskDetails.StartTime,
		"end_time", taskDetails.EndTime)

	// ================================================================================
	// STEP 2: Use Workflow ID as Task ID
	// ================================================================================
	// IMPORTANT: The task ID must be the same as the workflow ID
	// For force runs: triggerForceRun generates a task ID and uses it as workflow ID
	// For scheduled runs: Temporal generates the workflow ID automatically
	// In both cases, we use the workflow ID as the task ID for consistency
	workflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
	taskID := workflowID
	logger.Debug("Task ID set from workflow ID", "task_id", taskID, "workflow_id", workflowID)

	// ================================================================================
	// STEP 3: Create Task Record
	// ================================================================================
	logger.Info("Step 3: Creating task in database")
	createTaskInput := export.CreateTaskInput{
		TaskID:          taskID,
		WorkflowID:      workflowID,
		ScheduledTaskID: input.ScheduledTaskID,
		TenantID:        taskDetails.TenantID,
		EnvID:           taskDetails.EnvID,
		UserID:          input.UserID, // Use user ID from input (empty for scheduled runs)
		EntityType:      taskDetails.EntityType,
		StartTime:       taskDetails.StartTime,
		EndTime:         taskDetails.EndTime,
	}

	var createTaskOutput export.CreateTaskOutput
	err = workflow.ExecuteActivity(ctx, taskActivity.CreateTask, createTaskInput).Get(ctx, &createTaskOutput)
	if err != nil {
		logger.Error("Failed to create task", "error", err)
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	logger.Info("Task created in database", "task_id", taskID)

	// ================================================================================
	// STEP 4: Update Task to Processing
	// ================================================================================
	logger.Info("Step 4: Updating task to processing")
	updateStatusInput := export.UpdateTaskStatusInput{
		TaskID:   taskID,
		TenantID: taskDetails.TenantID,
		EnvID:    taskDetails.EnvID,
		UserID:   input.UserID,
		Status:   types.TaskStatusProcessing,
	}
	err = workflow.ExecuteActivity(ctx, taskActivity.UpdateTaskStatus, updateStatusInput).Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to update task status", "error", err)
		// Continue anyway - task creation succeeded
	}

	// ================================================================================
	// STEP 5: Export Data (Query + CSV + S3 Upload)
	// ================================================================================
	logger.Info("Step 5: Exporting data")
	ctx = workflow.WithActivityOptions(ctx, heavyActivityOptions)

	exportInput := export.ExportDataInput{
		EntityType:   types.ScheduledTaskEntityType(taskDetails.EntityType),
		ConnectionID: taskDetails.ConnectionID,
		TenantID:     taskDetails.TenantID,
		EnvID:        taskDetails.EnvID,
		StartTime:    taskDetails.StartTime,
		EndTime:      taskDetails.EndTime,
		JobConfig:    taskDetails.JobConfig,
	}

	var exportOutput export.ExportDataOutput
	err = workflow.ExecuteActivity(ctx, exportActivity.ExportData, exportInput).Get(ctx, &exportOutput)

	// Variables for tracking final status
	var success bool

	if err != nil {
		logger.Error("Export failed", "error", err)

		// Mark task as failed
		ctx = workflow.WithActivityOptions(ctx, lightweightActivityOptions)
		failInput := export.UpdateTaskStatusInput{
			TaskID:   taskID,
			TenantID: taskDetails.TenantID,
			EnvID:    taskDetails.EnvID,
			UserID:   input.UserID,
			Status:   types.TaskStatusFailed,
			Error:    err.Error(),
		}
		_ = workflow.ExecuteActivity(ctx, taskActivity.UpdateTaskStatus, failInput).Get(ctx, nil)

		success = false
	} else {
		logger.Info("Export completed",
			"file_url", exportOutput.FileURL,
			"record_count", exportOutput.RecordCount)

		// ================================================================================
		// STEP 6: Complete Task
		// ================================================================================
		logger.Info("Step 6: Completing task")
		ctx = workflow.WithActivityOptions(ctx, lightweightActivityOptions)

		completeInput := export.CompleteTaskInput{
			TaskID:      taskID,
			TenantID:    taskDetails.TenantID,
			EnvID:       taskDetails.EnvID,
			UserID:      input.UserID,
			FileURL:     exportOutput.FileURL,
			RecordCount: exportOutput.RecordCount,
			FileSize:    exportOutput.FileSizeBytes,
		}

		err = workflow.ExecuteActivity(ctx, taskActivity.CompleteTask, completeInput).Get(ctx, nil)
		if err != nil {
			logger.Error("Failed to complete task", "error", err)
			// Continue anyway - export succeeded
		}

		success = true

		logger.Info("Export workflow completed successfully", "task_id", taskID)
	}

	// Return the final result
	if !success {
		return &ExecuteExportWorkflowOutput{
			TaskID:  taskID,
			Success: false,
		}, fmt.Errorf("export workflow failed")
	}

	return &ExecuteExportWorkflowOutput{
		TaskID:      taskID,
		FileURL:     exportOutput.FileURL,
		RecordCount: exportOutput.RecordCount,
		Success:     true,
	}, nil
}
