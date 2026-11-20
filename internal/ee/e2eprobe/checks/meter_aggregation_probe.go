package checks

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// MeterAggregationProbe verifies that EACH seed meter is producing usage.
// The event-ingest-driver continuously emits events for every meter; if any
// meter shows zero aggregated usage over the lookback window, the upstream
// aggregation pipeline is broken for that meter (e.g., CEL expression
// failing, ClickHouse mat-view stale, meter config drift).
//
// Each tick picks one meter from the seed list (round-robin via cursor)
// and a rotating customer; asserts aggregated usage > 0 in the lookback
// window. A single probe tick covers one meter, so all 8 meters get
// exercised over 8 ticks.
type MeterAggregationProbe struct {
	client    e2eprobe.Client
	reg       e2eprobe.Registry
	runID     string
	logger    *logger.Logger
	startedAt time.Time
	cursor    int64
}

func NewMeterAggregationProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string, lg *logger.Logger) *MeterAggregationProbe {
	return &MeterAggregationProbe{client: c, reg: r, runID: runID, logger: lg, startedAt: time.Now()}
}

func (p *MeterAggregationProbe) Name() string        { return "meter-aggregation-probe" }
func (p *MeterAggregationProbe) Kind() e2eprobe.Kind { return e2eprobe.KindProbe }

// meterAggLookback is the lookback window for aggregation. Events ingest at
// 5/sec across 8 meters, so each meter sees ~37 events per minute. A 30-min
// window should show hundreds of events per meter if aggregation is healthy.
const meterAggLookback = 30 * time.Minute

// meterAggStartupGrace is how long the probe stays silent after boot before
// asserting anything. Fresh boots have neither events nor aggregated rows in
// ClickHouse yet — checking during that window is a guaranteed false page.
// After the grace elapses, the lookback window is clamped to the elapsed
// uptime until it reaches meterAggLookback.
const meterAggStartupGrace = 5 * time.Minute

func (p *MeterAggregationProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	// Only poll customers that actually receive ingest traffic.
	customers := seeds.IngestCustomerIDs
	if len(customers) == 0 {
		customers = seeds.PersistentCustomerIDs
	}
	if len(customers) == 0 || len(seeds.MeterIDs) == 0 {
		return nil // seed-ensure hasn't completed yet
	}

	uptime := time.Since(p.startedAt)
	if uptime < meterAggStartupGrace {
		p.logDebug(ctx, "meter-aggregation-probe: within startup grace, skipping",
			"uptime_sec", int(uptime.Seconds()),
			"grace_sec", int(meterAggStartupGrace.Seconds()),
			"run_id", p.runID)
		return nil
	}

	// Round-robin through meter event names. Sort so order is stable
	// across process restarts (registry uses a map so iteration order is
	// not deterministic).
	eventNames := sortedMeterEventNames(seeds.MeterIDs)
	if len(eventNames) == 0 {
		return nil
	}
	idx := atomic.AddInt64(&p.cursor, 1)
	eventName := eventNames[int(idx)%len(eventNames)]
	extCustID := customers[int(idx)%len(customers)]

	end := time.Now().UTC()
	// Never look further back than the probe has actually been running.
	// Otherwise, at t=6m into a 30m lookback the empty [t=-24m..t=0] slice
	// dominates and reports sum=0 even though the pipeline is healthy.
	window := meterAggLookback
	if uptime < meterAggLookback {
		window = uptime
	}
	start := end.Add(-window)

	// Usage analytics only returns meters carried by the customer's active
	// subscription line items (see meterUsageService.activeSubscriptionMeterIDs).
	// Line items snapshot the plan at subscription-create time, so a meter
	// seeded after the persistent subs were created reports nothing until
	// seed-ensure's plan price sync lands. That's a seed-drift window, not a
	// broken aggregation pipeline — skip instead of paging.
	onSub, err := p.meterOnActiveSubscription(ctx, extCustID, seeds.MeterIDs[eventName])
	if err != nil {
		return e2eprobe.Errorf(map[string]string{
			"external_customer_id": extCustID,
			"event_name":           eventName,
		}, "resolve subscription line items for %s: %w", extCustID, err)
	}
	if !onSub {
		p.logInfo(ctx, "meter-aggregation-probe: meter not on customer's active subscription, skipping",
			"event_name", eventName,
			"external_customer_id", extCustID,
			"run_id", p.runID)
		return nil
	}

	resp, err := p.client.Events().GetUsageAnalytics(ctx, types.GetUsageAnalyticsRequest{
		ExternalCustomerID: &extCustID,
		StartTime:          &start,
		EndTime:            &end,
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{
			"external_customer_id": extCustID,
			"event_name":           eventName,
			"window":               window.String(),
		}, "analytics for %s/%s: %w", extCustID, eventName, err)
	}

	sum := extractAnalyticsSum(resp, eventName)
	if sum > 0 {
		p.logDebug(ctx, "meter-aggregation-probe: observed non-zero aggregated usage",
			"event_name", eventName,
			"external_customer_id", extCustID,
			"window", window.String(),
			"observed_sum", fmt.Sprintf("%.4f", sum),
			"run_id", p.runID)
		return nil
	}
	return e2eprobe.Errorf(map[string]string{
		"external_customer_id": extCustID,
		"event_name":           eventName,
		"window":               window.String(),
		"observed_sum":         fmt.Sprintf("%.4f", sum),
	}, "meter %s has zero aggregated usage over %s window (event_ingest_driver should have produced events; aggregation pipeline may be broken)",
		eventName, window)
}

// meterOnActiveSubscription reports whether meterID appears on any line item
// of the customer's subscriptions. An unknown meter ID (seed map miss) counts
// as present so the assertion still runs.
func (p *MeterAggregationProbe) meterOnActiveSubscription(ctx context.Context, extCustID, meterID string) (bool, error) {
	if meterID == "" {
		return true, nil
	}
	ext := extCustID
	active := types.SubscriptionStatusActive
	listResp, err := p.client.Subscriptions().Query(ctx, types.SubscriptionFilter{
		ExternalCustomerID: &ext,
		SubscriptionStatus: []types.SubscriptionStatus{active},
	})
	if err != nil {
		return false, err
	}
	if listResp.ListSubscriptionsResponse == nil {
		return false, nil
	}
	for _, sub := range listResp.ListSubscriptionsResponse.Items {
		if sub.ID == nil {
			continue
		}
		// Belt and braces: a server that ignores the status filter must not
		// let a cancelled sub's line items mask a real aggregation failure.
		if sub.SubscriptionStatus != nil && *sub.SubscriptionStatus != active {
			continue
		}
		subResp, err := p.client.Subscriptions().Get(ctx, *sub.ID)
		if err != nil {
			return false, err
		}
		if subResp.SubscriptionResponse == nil {
			continue
		}
		for _, li := range subResp.SubscriptionResponse.LineItems {
			if li.MeterID != nil && *li.MeterID == meterID {
				return true, nil
			}
		}
	}
	return false, nil
}

// sortedMeterEventNames returns the event names from meterIDs in stable sorted order.
func sortedMeterEventNames(meterIDs map[string]string) []string {
	names := make([]string, 0, len(meterIDs))
	for n := range meterIDs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (p *MeterAggregationProbe) logInfo(ctx context.Context, msg string, kv ...any) {
	if p.logger == nil {
		return
	}
	p.logger.Info(ctx, msg, kv...)
}

func (p *MeterAggregationProbe) logDebug(ctx context.Context, msg string, kv ...any) {
	if p.logger == nil {
		return
	}
	p.logger.Debug(ctx, msg, kv...)
}
