package workflow

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/workflowexecution"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal/tracking"
	"github.com/flexprice/flexprice/internal/types"
)

// WorkflowTrackingActivities contains workflow tracking related activities
type WorkflowTrackingActivities struct {
	serviceParams    service.ServiceParams
	workflowExecRepo workflowexecution.Repository
	logger           *logger.Logger
}

// NewWorkflowTrackingActivities creates a new WorkflowTrackingActivities instance
func NewWorkflowTrackingActivities(
	serviceParams service.ServiceParams,
	workflowExecRepo workflowexecution.Repository,
	logger *logger.Logger,
) *WorkflowTrackingActivities {
	return &WorkflowTrackingActivities{
		serviceParams:    serviceParams,
		workflowExecRepo: workflowExecRepo,
		logger:           logger,
	}
}

// TrackWorkflowStart performs the workflow tracking activity
// This is registered as a local activity and saves workflow metadata to the database
// It uses the TrackWorkflowStartActivityInput type from the tracking package
func (a *WorkflowTrackingActivities) TrackWorkflowStart(ctx context.Context, input tracking.TrackWorkflowStartActivityInput) error {
	// Set context values for proper multi-tenancy
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	if input.CreatedBy != "" {
		ctx = types.SetUserID(ctx, input.CreatedBy)
	}

	a.logger.Info("Tracking workflow start",
		"workflow_id", input.WorkflowID,
		"run_id", input.RunID,
		"workflow_type", input.WorkflowType)

	// Create service on-demand using ServiceParams
	workflowExecService := service.NewWorkflowExecutionService(a.serviceParams, a.workflowExecRepo)

	// Create workflow execution record
	_, err := workflowExecService.CreateWorkflowExecution(ctx, &service.CreateWorkflowExecutionInput{
		WorkflowID:    input.WorkflowID,
		RunID:         input.RunID,
		WorkflowType:  input.WorkflowType,
		TaskQueue:     input.TaskQueue,
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		CreatedBy:     input.CreatedBy,
		Metadata:      input.Metadata,
	})

	if err != nil {
		a.logger.Error("Failed to track workflow start", "error", err)
		// Don't fail the workflow if tracking fails - just log the error
		return nil
	}

	a.logger.Info("Successfully tracked workflow start",
		"workflow_id", input.WorkflowID,
		"run_id", input.RunID)

	return nil
}
