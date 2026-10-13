package interceptor

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
)

// ActorInterceptor attributes writes made inside an activity to the system, named after the workflow type.
type ActorInterceptor struct {
	interceptor.InterceptorBase
}

func NewActorInterceptor() *ActorInterceptor {
	return &ActorInterceptor{}
}

func (a *ActorInterceptor) InterceptActivity(_ context.Context, next interceptor.ActivityInboundInterceptor) interceptor.ActivityInboundInterceptor {
	return &actorActivityInterceptor{
		ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next},
	}
}

type actorActivityInterceptor struct {
	interceptor.ActivityInboundInterceptorBase
}

func (a *actorActivityInterceptor) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
	name := activity.GetInfo(ctx).WorkflowType.Name
	ctx = types.SetActor(ctx, types.Actor{Type: types.ActorTypeSystem, ID: name}, types.SourceWorkflow)
	return a.Next.ExecuteActivity(ctx, in)
}
