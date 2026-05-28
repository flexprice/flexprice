package interceptor

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/tracing"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/workflow"
)

// TracingInterceptor adds OTel spans around activity executions and forwards
// workflow / activity errors to Sentry via the shared tracing.Service.
//
// For Temporal-native span/trace propagation across workflow boundaries, the
// recommended upstream package is github.com/temporalio/sdk-contrib/opentelemetry.
// We stay manual here to avoid adding a new module dependency; activities still
// get spans through the standard OTel tracer (exported to SigNoz / OTLP).
type TracingInterceptor struct {
	interceptor.InterceptorBase
	tracing *tracing.Service
}

// NewTracingInterceptor constructs the interceptor.
func NewTracingInterceptor(tracingSvc *tracing.Service) *TracingInterceptor {
	return &TracingInterceptor{tracing: tracingSvc}
}

// InterceptWorkflow creates a workflow inbound interceptor.
func (s *TracingInterceptor) InterceptWorkflow(_ workflow.Context, next interceptor.WorkflowInboundInterceptor) interceptor.WorkflowInboundInterceptor {
	return &workflowInboundInterceptor{
		WorkflowInboundInterceptorBase: interceptor.WorkflowInboundInterceptorBase{Next: next},
		tracing:                        s.tracing,
	}
}

// InterceptActivity creates an activity inbound interceptor.
func (s *TracingInterceptor) InterceptActivity(_ context.Context, next interceptor.ActivityInboundInterceptor) interceptor.ActivityInboundInterceptor {
	return &activityInboundInterceptor{
		ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next},
		tracing:                        s.tracing,
	}
}

// workflowInboundInterceptor intercepts workflow executions.
type workflowInboundInterceptor struct {
	interceptor.WorkflowInboundInterceptorBase
	tracing *tracing.Service
}

// ExecuteWorkflow intercepts workflow execution.
func (w *workflowInboundInterceptor) ExecuteWorkflow(ctx workflow.Context, in *interceptor.ExecuteWorkflowInput) (interface{}, error) {
	workflowInfo := workflow.GetInfo(ctx)

	logger := workflow.GetLogger(ctx)
	logger.Info("Starting workflow execution with tracing",
		"workflow_type", workflowInfo.WorkflowType.Name,
		"workflow_id", workflowInfo.WorkflowExecution.ID,
		"run_id", workflowInfo.WorkflowExecution.RunID,
	)

	result, err := w.Next.ExecuteWorkflow(ctx, in)

	// Workflow body is deterministic; CaptureException is safe only after Next returns.
	if err != nil && w.tracing.IsSentryEnabled() {
		logger.Error("Workflow execution failed, capturing in Sentry",
			"workflow_type", workflowInfo.WorkflowType.Name,
			"workflow_id", workflowInfo.WorkflowExecution.ID,
			"run_id", workflowInfo.WorkflowExecution.RunID,
			"error", err,
		)
		w.tracing.CaptureException(fmt.Errorf("temporal workflow failed: %s (ID: %s) - %w",
			workflowInfo.WorkflowType.Name,
			workflowInfo.WorkflowExecution.ID,
			err,
		))
	}

	return result, err
}

// activityInboundInterceptor intercepts activity executions.
type activityInboundInterceptor struct {
	interceptor.ActivityInboundInterceptorBase
	tracing *tracing.Service
}

// ExecuteActivity intercepts activity execution.
func (a *activityInboundInterceptor) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
	activityInfo := activity.GetInfo(ctx)

	span, spanCtx := a.tracing.StartMonitoringSpan(ctx, fmt.Sprintf("temporal.activity.%s", activityInfo.ActivityType.Name), map[string]interface{}{
		"activity_type": activityInfo.ActivityType.Name,
		"activity_id":   activityInfo.ActivityID,
		"workflow_type": activityInfo.WorkflowType.Name,
		"workflow_id":   activityInfo.WorkflowExecution.ID,
		"run_id":        activityInfo.WorkflowExecution.RunID,
		"task_queue":    activityInfo.TaskQueue,
		"attempt":       activityInfo.Attempt,
	})

	result, err := a.Next.ExecuteActivity(spanCtx, in)

	if span != nil {
		if err != nil {
			span.SetStatusError(err)
		} else {
			span.SetStatusOK()
		}
		span.Finish()
	}

	if err != nil && a.tracing.IsSentryEnabled() {
		a.tracing.CaptureException(fmt.Errorf("temporal activity failed: %s - %w", activityInfo.ActivityType.Name, err))
	}

	return result, err
}
