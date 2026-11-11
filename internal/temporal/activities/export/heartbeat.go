package export

import (
	"context"
	"time"

	"go.temporal.io/sdk/activity"
)

const heartbeatInterval = 30 * time.Second

// startHeartbeat pings Temporal until the returned stop func is called.
func startHeartbeat(ctx context.Context, every time.Duration) (stop func()) {
	if !activity.IsActivity(ctx) {
		return func() {}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				activity.RecordHeartbeat(ctx)
			}
		}
	}()
	return func() {
		close(done)
		<-finished
	}
}
