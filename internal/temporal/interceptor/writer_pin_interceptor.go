package interceptor

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/interceptor"
)

// WriterPinInterceptor installs a per-activity writer pin on the activity
// context. The first postgres write inside the activity flips the pin so all
// subsequent reads in the same activity go to the writer endpoint, giving
// read-after-write consistency despite replica lag. Read-only activities keep
// using the read replica.
type WriterPinInterceptor struct {
	interceptor.InterceptorBase
}

// NewWriterPinInterceptor constructs the interceptor.
func NewWriterPinInterceptor() *WriterPinInterceptor {
	return &WriterPinInterceptor{}
}

// InterceptActivity creates an activity inbound interceptor that pins writes.
func (s *WriterPinInterceptor) InterceptActivity(_ context.Context, next interceptor.ActivityInboundInterceptor) interceptor.ActivityInboundInterceptor {
	return &writerPinActivityInterceptor{
		ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next},
	}
}

type writerPinActivityInterceptor struct {
	interceptor.ActivityInboundInterceptorBase
}

// ExecuteActivity wraps the activity context with a writer pin.
func (a *writerPinActivityInterceptor) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
	return a.Next.ExecuteActivity(types.WithWriterPinning(ctx), in)
}
