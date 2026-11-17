package heartbeat

import (
	"context"
	"time"

	"go.temporal.io/sdk/activity"
)

// Start heartbeats the activity every interval until stop is called or ctx ends, so a dead
// worker is detected within the activity's HeartbeatTimeout instead of its StartToCloseTimeout.
func Start(ctx context.Context, interval time.Duration) (stop func()) {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
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
	return func() { close(done) }
}
