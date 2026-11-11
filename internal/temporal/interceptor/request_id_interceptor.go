package interceptor

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
)

// RequestIDInterceptor sets the workflow run ID as the activity's request ID, so every log line from one run shares it.
type RequestIDInterceptor struct {
	interceptor.InterceptorBase
}

func NewRequestIDInterceptor() *RequestIDInterceptor {
	return &RequestIDInterceptor{}
}

func (r *RequestIDInterceptor) InterceptActivity(_ context.Context, next interceptor.ActivityInboundInterceptor) interceptor.ActivityInboundInterceptor {
	return &requestIDActivityInterceptor{
		ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next},
	}
}

type requestIDActivityInterceptor struct {
	interceptor.ActivityInboundInterceptorBase
}

func (r *requestIDActivityInterceptor) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
	runID := activity.GetInfo(ctx).WorkflowExecution.RunID
	ctx = context.WithValue(ctx, types.CtxRequestID, runID)
	return r.Next.ExecuteActivity(ctx, in)
}
