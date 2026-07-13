package checks

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
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
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	cursor int64
}

func NewMeterAggregationProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string) *MeterAggregationProbe {
	return &MeterAggregationProbe{client: c, reg: r, runID: runID}
}

func (p *MeterAggregationProbe) Name() string        { return "meter-aggregation-probe" }
func (p *MeterAggregationProbe) Kind() e2eprobe.Kind { return e2eprobe.KindProbe }

// meterAggLookback is the lookback window for aggregation. Events ingest at
// 5/sec across 8 meters, so each meter sees ~37 events per minute. A 30-min
// window should show hundreds of events per meter if aggregation is healthy.
const meterAggLookback = 30 * time.Minute

func (p *MeterAggregationProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	if len(seeds.PersistentCustomerIDs) == 0 || len(seeds.MeterIDs) == 0 {
		return nil // seed-ensure hasn't completed yet
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
	extCustID := seeds.PersistentCustomerIDs[int(idx)%len(seeds.PersistentCustomerIDs)]

	end := time.Now().UTC()
	start := end.Add(-meterAggLookback)

	resp, err := p.client.Events().GetUsageAnalytics(ctx, types.GetUsageAnalyticsRequest{
		ExternalCustomerID: &extCustID,
		StartTime:          &start,
		EndTime:            &end,
	})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{
			"external_customer_id": extCustID,
			"event_name":           eventName,
			"window":               meterAggLookback.String(),
		}, "analytics for %s/%s: %w", extCustID, eventName, err)
	}

	sum := extractAnalyticsSum(resp, eventName)
	if sum > 0 {
		return nil
	}
	return e2eprobe.Errorf(map[string]string{
		"external_customer_id": extCustID,
		"event_name":           eventName,
		"window":               meterAggLookback.String(),
		"observed_sum":         fmt.Sprintf("%.4f", sum),
	}, "meter %s has zero aggregated usage over %s window (event_ingest_driver should have produced events; aggregation pipeline may be broken)",
		eventName, meterAggLookback)
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
