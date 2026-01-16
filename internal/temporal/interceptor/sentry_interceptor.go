package interceptor

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/sentry"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/workflow"
)

// SentryInterceptor provides Sentry integration for Temporal workflows and activities
type SentryInterceptor struct {
	interceptor.InterceptorBase
	sentry *sentry.Service
}

// NewSentryInterceptor creates a new Sentry interceptor
func NewSentryInterceptor(sentryService *sentry.Service) *SentryInterceptor {
	return &SentryInterceptor{
		sentry: sentryService,
	}
}

// InterceptWorkflow creates a workflow inbound interceptor
func (s *SentryInterceptor) InterceptWorkflow(ctx workflow.Context, next interceptor.WorkflowInboundInterceptor) interceptor.WorkflowInboundInterceptor {
	return &workflowInboundInterceptor{
		WorkflowInboundInterceptorBase: interceptor.WorkflowInboundInterceptorBase{
			Next: next,
		},
		sentry: s.sentry,
	}
}

// InterceptActivity creates an activity inbound interceptor
func (s *SentryInterceptor) InterceptActivity(ctx context.Context, next interceptor.ActivityInboundInterceptor) interceptor.ActivityInboundInterceptor {
	return &activityInboundInterceptor{
		ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{
			Next: next,
		},
		sentry: s.sentry,
	}
}

// workflowInboundInterceptor intercepts workflow executions
type workflowInboundInterceptor struct {
	interceptor.WorkflowInboundInterceptorBase
	sentry *sentry.Service
}

// ExecuteWorkflow intercepts workflow execution
func (w *workflowInboundInterceptor) ExecuteWorkflow(ctx workflow.Context, in *interceptor.ExecuteWorkflowInput) (interface{}, error) {
	workflowInfo := workflow.GetInfo(ctx)

	// Create a logger for this workflow
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting workflow execution with Sentry monitoring",
		"workflow_type", workflowInfo.WorkflowType.Name,
		"workflow_id", workflowInfo.WorkflowExecution.ID,
		"run_id", workflowInfo.WorkflowExecution.RunID,
	)

	// Execute the workflow
	result, err := w.Next.ExecuteWorkflow(ctx, in)

	// After workflow completes, we're outside the deterministic context
	// Now we CAN capture to Sentry
	if err != nil && w.sentry.IsEnabled() {
		logger.Error("Workflow execution failed, capturing in Sentry",
			"workflow_type", workflowInfo.WorkflowType.Name,
			"workflow_id", workflowInfo.WorkflowExecution.ID,
			"run_id", workflowInfo.WorkflowExecution.RunID,
			"error", err,
		)

		// Capture workflow failure as a Sentry error with full context
		// This will appear in Sentry Issues and can be monitored as a background job
		w.sentry.CaptureException(fmt.Errorf("temporal workflow failed: %s (ID: %s) - %w",
			workflowInfo.WorkflowType.Name,
			workflowInfo.WorkflowExecution.ID,
			err,
		))
	}

	return result, err
}

// activityInboundInterceptor intercepts activity executions
type activityInboundInterceptor struct {
	interceptor.ActivityInboundInterceptorBase
	sentry *sentry.Service
}

// ExecuteActivity intercepts activity execution
func (a *activityInboundInterceptor) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
	// Skip if Sentry is not enabled
	if !a.sentry.IsEnabled() {
		return a.Next.ExecuteActivity(ctx, in)
	}

	// Get activity info from context
	activityInfo := activity.GetInfo(ctx)

	// Start a Sentry span for this activity
	span, spanCtx := a.sentry.StartMonitoringSpan(ctx, fmt.Sprintf("temporal.activity.%s", activityInfo.ActivityType.Name), map[string]interface{}{
		"activity_type": activityInfo.ActivityType.Name,
		"activity_id":   activityInfo.ActivityID,
		"workflow_type": activityInfo.WorkflowType.Name,
		"workflow_id":   activityInfo.WorkflowExecution.ID,
		"run_id":        activityInfo.WorkflowExecution.RunID,
		"task_queue":    activityInfo.TaskQueue,
		"attempt":       activityInfo.Attempt,
	})

	// Execute the activity with the span context
	result, err := a.Next.ExecuteActivity(spanCtx, in)

	// Finish the span
	if span != nil {
		if err != nil {
			span.Status = 1 // Error status
			span.SetData("error", err.Error())
		}
		span.Finish()
	}

	// Capture error if activity failed
	if err != nil {
		a.sentry.CaptureException(fmt.Errorf("temporal activity failed: %s - %w", activityInfo.ActivityType.Name, err))
	}

	return result, err
}
