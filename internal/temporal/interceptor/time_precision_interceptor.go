package interceptor

import (
	"context"
	"reflect"

	"github.com/flexprice/flexprice/internal/clock"
	"go.temporal.io/sdk/interceptor"
)

// TimePrecisionInterceptor truncates every time field in an activity's
// arguments to microseconds before the activity runs. Activities are
// Temporal's ingress into the service layer, as gin binding is HTTP's:
// workflow.Now() and schedule times carry the server's nanoseconds, and
// workflow code cannot call clock.Now() deterministically.
type TimePrecisionInterceptor struct {
	interceptor.InterceptorBase
}

// NewTimePrecisionInterceptor constructs the interceptor.
func NewTimePrecisionInterceptor() *TimePrecisionInterceptor {
	return &TimePrecisionInterceptor{}
}

// InterceptActivity creates an activity inbound interceptor that truncates argument times.
func (s *TimePrecisionInterceptor) InterceptActivity(_ context.Context, next interceptor.ActivityInboundInterceptor) interceptor.ActivityInboundInterceptor {
	return &timePrecisionActivityInterceptor{
		ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next},
	}
}

type timePrecisionActivityInterceptor struct {
	interceptor.ActivityInboundInterceptorBase
}

// ExecuteActivity truncates the decoded arguments, which are values, through addressable copies.
func (a *timePrecisionActivityInterceptor) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
	for i, arg := range in.Args {
		v := reflect.ValueOf(arg)
		if !v.IsValid() {
			continue
		}

		p := reflect.New(v.Type())
		p.Elem().Set(v)
		clock.TruncateTimes(p.Interface())
		in.Args[i] = p.Elem().Interface()
	}

	return a.Next.ExecuteActivity(ctx, in)
}
