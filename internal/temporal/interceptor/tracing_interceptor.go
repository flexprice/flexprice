package interceptor

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/spanerr"
	"github.com/flexprice/flexprice/internal/tracing"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/workflow"
)

// TracingInterceptor adds OTel spans around activity executions and records
// workflow / activity failures as OTel exception events (SigNoz Exceptions tab)
// via the shared tracing.Service.
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

	if err != nil {
		logger.Error("Workflow execution failed, recording exception",
			"workflow_type", workflowInfo.WorkflowType.Name,
			"workflow_id", workflowInfo.WorkflowExecution.ID,
			"run_id", workflowInfo.WorkflowExecution.RunID,
			"error", err,
		)
		// Guard against replay: replayed workflows re-run deterministically and
		// would emit duplicate exception events for the same failure. No OTel span
		// exists for workflows here, so CaptureException synthesizes one.
		if !workflow.IsReplaying(ctx) {
			w.tracing.CaptureException(context.Background(), fmt.Errorf("temporal workflow failed: %s (ID: %s) - %w",
				workflowInfo.WorkflowType.Name,
				workflowInfo.WorkflowExecution.ID,
				err,
			))
		}
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

	// Seed a per-activity exception dedup scope BEFORE creating the span so the
	// span's context (and thus SetStatusError below) shares the same scope as the
	// inner activity code's auto-capture — an error that is both logged and
	// recorded via span status then produces a single exception event.
	ctx = spanerr.WithDedup(ctx)

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

	// On failure, SetStatusError records the error as an exception event (with
	// stacktrace) on the activity span — which is exactly what SigNoz's
	// Exceptions tab reads. No separate CaptureException is needed here.
	if span != nil {
		if err != nil {
			span.SetStatusError(err)
		} else {
			span.SetStatusOK()
		}
		span.Finish()
	}

	return result, err
}
