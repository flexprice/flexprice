package checks

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// BucketedMeterProbe asserts that events ingested with backdated timestamps
// across N consecutive bucket boundaries aggregate into exactly N buckets
// with the expected per-bucket value. Rotates across the three bucketed
// seed features on each run so all bucket sizes get coverage.
//
// The probe uses persistent cust #0 (already receiving ingest traffic) and
// unique event_ids to prevent double-counting across retries.
type BucketedMeterProbe struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	lg     *logger.Logger
	cursor int64
}

func NewBucketedMeterProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string, lg *logger.Logger) *BucketedMeterProbe {
	return &BucketedMeterProbe{client: c, reg: r, runID: runID, lg: lg}
}

func (p *BucketedMeterProbe) Name() string        { return "bucketed-meter-probe" }
func (p *BucketedMeterProbe) Kind() e2eprobe.Kind { return e2eprobe.KindProbe }

type bucketedSpec struct {
	featureKey string
	eventName  string
	window     types.WindowSize
	duration   time.Duration
}

var bucketedProbeSpecs = []bucketedSpec{
	{featureKey: "e2eprobe_max_15min_feature", eventName: "e2eprobe_max_15min", window: types.WindowSizeFifteenMin, duration: 15 * time.Minute},
	{featureKey: "e2eprobe_sum_hour_feature", eventName: "e2eprobe_sum_hour", window: types.WindowSizeHour, duration: 1 * time.Hour},
	{featureKey: "e2eprobe_max_day_feature", eventName: "e2eprobe_max_day", window: types.WindowSizeDay, duration: 24 * time.Hour},
	// Bucket resolved from the PRICE, not the meter. Same assertions as the
	// meter-bucketed specs above — a price-level bucket must produce identical
	// windowing, which is the whole point of the migration.
	{featureKey: "e2eprobe_max_price_hour_feature", eventName: "e2eprobe_max_price_hour", window: types.WindowSizeHour, duration: 1 * time.Hour},
	{featureKey: "e2eprobe_sum_price_hour_feature", eventName: "e2eprobe_sum_price_hour", window: types.WindowSizeHour, duration: 1 * time.Hour},
}

func (p *BucketedMeterProbe) Run(ctx context.Context) error {
	seeds := p.reg.Seeds()
	if len(seeds.PersistentCustomerIDs) == 0 || len(seeds.BucketedFeatureIDs) == 0 {
		return nil
	}

	idx := atomic.AddInt64(&p.cursor, 1)
	spec := bucketedProbeSpecs[int(idx-1)%len(bucketedProbeSpecs)]
	featID, ok := seeds.BucketedFeatureIDs[spec.featureKey]
	if !ok {
		return nil
	}
	customerExt := seeds.PersistentCustomerIDs[0]

	// Compute 3 backdated timestamps aligned to bucket boundaries in UTC.
	now := time.Now().UTC()
	aligned := now.Truncate(spec.duration)
	ts := []time.Time{
		aligned.Add(-3 * spec.duration),
		aligned.Add(-2 * spec.duration),
		aligned.Add(-1 * spec.duration),
	}
	values := []int{10, 20, 30}

	// The bucket-aligned timestamp repeats for every Run inside the same
	// bucket, so the id must also carry this Run's nonce — otherwise an
	// earlier Run's row satisfies the verification below and the check passes
	// without its own events ever landing.
	nonce := now.UnixNano()
	eventIDs := make([]string, 3)
	for i, t := range ts {
		evID := fmt.Sprintf("e2eprobe-bkt-%s-%d-%d-%d", spec.eventName, t.UnixNano(), i, nonce)
		eventIDs[i] = evID
		tsStr := t.Format(time.RFC3339Nano)
		if _, err := p.client.Events().Ingest(ctx, types.IngestEventRequest{
			EventID:            &evID,
			EventName:          spec.eventName,
			ExternalCustomerID: customerExt,
			Timestamp:          &tsStr,
			Properties: map[string]string{
				"amount":          fmt.Sprintf("%d", values[i]),
				"e2eprobe":        "true",
				"e2eprobe_run_id": p.runID,
			},
		}); err != nil {
			return e2eprobe.Errorf(map[string]string{
				"step":                 "ingest",
				"external_customer_id": customerExt,
				"event_name":           spec.eventName,
				"event_id":             evID,
				"feature_id":           featID,
			}, "ingest bucketed event %d: %w", i, err)
		}
	}

	// Verify raw ingestion first — separates ingest failures from aggregation lag.
	if err := p.pollRawEvents(ctx, spec, customerExt, featID, eventIDs, ts[0]); err != nil {
		return err
	}

	// Poll analytics until every seeded bucket boundary is visible.
	if err := p.pollAnalytics(ctx, spec, customerExt, featID, ts, now); err != nil {
		return err
	}
	return nil
}

// pollRawEvents verifies each ingested event by its event_id.
//
// The events are deliberately backdated by up to 3 bucket durations, and the
// persistent customer also receives continuous event-ingest-driver traffic on
// the same event names. A plain (customer, event_name) query returns the most
// recent page in timestamp-DESC order, so the backdated rows sit far below the
// page window and are never observed. Filtering by event_id sidesteps
// pagination entirely; the explicit time range keeps the older buckets inside
// the server's default 7-day window.
func (p *BucketedMeterProbe) pollRawEvents(ctx context.Context, spec bucketedSpec, custExt, featID string, wantIDs []string, oldest time.Time) error {
	// Staging drains the ingest topic at roughly ten events/second, so allow
	// generous headroom over the burst this probe (and its neighbours) creates.
	deadline := time.Now().Add(90 * time.Second)
	start := oldest.Add(-spec.duration)
	pageSize := int64(10)
	found := make(map[string]bool, len(wantIDs))
	for {
		end := time.Now().UTC()
		for _, want := range wantIDs {
			if found[want] {
				continue
			}
			eventID := want
			resp, err := p.client.Events().ListRaw(ctx, types.GetEventsRequest{
				ExternalCustomerID: &custExt,
				EventName:          &spec.eventName,
				EventID:            &eventID,
				StartTime:          &start,
				EndTime:            &end,
				PageSize:           &pageSize,
			})
			if err != nil || resp.GetEventsResponse == nil {
				continue
			}
			for _, ev := range resp.GetEventsResponse.Events {
				if ev.ID != nil && *ev.ID == want {
					found[want] = true
					break
				}
			}
		}
		if len(found) == len(wantIDs) {
			return nil
		}
		if time.Now().After(deadline) {
			missing := make([]string, 0, len(wantIDs))
			for _, want := range wantIDs {
				if !found[want] {
					missing = append(missing, want)
				}
			}
			return e2eprobe.Errorf(map[string]string{
				"step":                 "raw_verify",
				"external_customer_id": custExt,
				"event_name":           spec.eventName,
				"feature_id":           featID,
				"missing_event_ids":    strings.Join(missing, ","),
			}, "raw event verification timeout — %d of %d events present", len(found), len(wantIDs))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// analyticsPollTimeout bounds the wait for the analytics view. Ingestion is
// already verified by pollRawEvents, and the seeded rows are queryable in
// meter_usage within a second of the ingest call, so a longer wait here buys
// nothing: when this check fails it is because the assertion below did not
// match, not because a rollup was still materializing.
const analyticsPollTimeout = 90 * time.Second

// pollAnalytics waits until every seeded bucket boundary is visible in the
// analytics view.
//
// The response carries one item per subscription line item, in no guaranteed
// order, and a persistent customer holds more than one subscription on the
// probe plan — seed-ensure keeps a second, quarterly one for multi-cadence
// coverage. A young or cancelled subscription contributes an item with few or
// zero points, so reading Items[0] asserts against whichever subscription the
// server happened to list first. Assert over the union of the items for this
// feature instead, and on the bucket boundaries themselves rather than a count,
// so an unrelated bucket can never stand in for a seeded one.
func (p *BucketedMeterProbe) pollAnalytics(ctx context.Context, spec bucketedSpec, custExt, featID string, buckets []time.Time, end time.Time) error {
	windowSize := spec.window
	start := buckets[0]
	deadline := time.Now().Add(analyticsPollTimeout)
	for {
		resp, err := p.client.Events().GetUsageAnalytics(ctx, types.GetUsageAnalyticsRequest{
			ExternalCustomerID: &custExt,
			FeatureIds:         []string{featID},
			StartTime:          &start,
			EndTime:            &end,
			WindowSize:         &windowSize,
		})
		var missing []time.Time
		if err == nil && resp.GetUsageAnalyticsResponse != nil {
			missing = missingBuckets(buckets, seenBuckets(resp.GetUsageAnalyticsResponse.Items, featID))
			if len(missing) == 0 {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return e2eprobe.Errorf(map[string]string{
				"step":                 "analytics_poll",
				"external_customer_id": custExt,
				"event_name":           spec.eventName,
				"feature_id":           featID,
				"window_size":          string(windowSize),
				"missing_buckets":      formatBuckets(missing),
			}, "expected %d buckets after %s, missing %d", len(buckets), analyticsPollTimeout, len(missing))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// seenBuckets collects every bucket boundary present across the analytics items
// for featID, keyed by unix seconds. Items with no feature_id are counted: the
// query is already scoped to one feature, and some server versions omit the
// field on the response.
func seenBuckets(items []types.UsageAnalyticItem, featID string) map[int64]bool {
	seen := make(map[int64]bool)
	for _, item := range items {
		if item.FeatureID != nil && *item.FeatureID != "" && *item.FeatureID != featID {
			continue
		}
		for _, point := range item.Points {
			if point.Timestamp == nil {
				continue
			}
			ts, err := time.Parse(time.RFC3339Nano, *point.Timestamp)
			if err != nil {
				continue
			}
			seen[ts.UTC().Unix()] = true
		}
	}
	return seen
}

func missingBuckets(want []time.Time, seen map[int64]bool) []time.Time {
	missing := make([]time.Time, 0, len(want))
	for _, b := range want {
		if !seen[b.UTC().Unix()] {
			missing = append(missing, b)
		}
	}
	return missing
}

func formatBuckets(ts []time.Time) string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.UTC().Format(time.RFC3339))
	}
	return strings.Join(out, ",")
}
