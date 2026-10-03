package interceptor

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
)

type actorInterceptor struct {
	interceptor.WorkerInterceptorBase
}

// NewActorInterceptor stamps a system actor and workflow source on every activity context.
func NewActorInterceptor() interceptor.WorkerInterceptor {
	return &actorInterceptor{}
}

func (a *actorInterceptor) InterceptActivity(_ context.Context, next interceptor.ActivityInboundInterceptor) interceptor.ActivityInboundInterceptor {
	return &actorActivityInbound{
		ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next},
	}
}

type actorActivityInbound struct {
	interceptor.ActivityInboundInterceptorBase
}

func (a *actorActivityInbound) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
	ctx = types.SetActor(ctx, types.WorkflowActor(actorWorkflowName(activity.GetInfo(ctx))))
	ctx = types.SetSource(ctx, types.SourceWorkflow)
	return a.Next.ExecuteActivity(ctx, in)
}

// actorWorkflowName names the workflow that started the activity, falling back
// to the activity type for activities started outside a workflow.
func actorWorkflowName(info activity.Info) string {
	if info.WorkflowType != nil && info.WorkflowType.Name != "" {
		return info.WorkflowType.Name
	}
	return info.ActivityType.Name
}
