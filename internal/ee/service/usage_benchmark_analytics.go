package service

import (
	"context"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/domain/events"
)

// processAnalyticsMessage will replay the captured analytics request against
// meter_usage twice (no-FINAL and FINAL) and write one analytics_benchmark row
// with per-side perf + result totals. Implemented in a follow-up task.
func (s *usageBenchmarkService) processAnalyticsMessage(ctx context.Context, msg *message.Message, evt *events.UsageBenchmarkEvent) error {
	_ = ctx
	_ = msg
	_ = evt
	return nil
}
