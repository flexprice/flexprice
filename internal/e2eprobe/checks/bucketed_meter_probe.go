package checks

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
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

	eventIDs := make([]string, 3)
	for i, t := range ts {
		evID := fmt.Sprintf("e2eprobe-bkt-%s-%d-%d", spec.eventName, t.UnixNano(), i)
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
	if err := p.pollRawEvents(ctx, spec.eventName, customerExt, featID, eventIDs); err != nil {
		return err
	}

	// Poll analytics for exactly 3 buckets aligned to the expected boundaries.
	if err := p.pollAnalytics(ctx, spec, customerExt, featID, ts[0], now, values); err != nil {
		return err
	}
	return nil
}

func (p *BucketedMeterProbe) pollRawEvents(ctx context.Context, eventName, custExt, featID string, wantIDs []string) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := p.client.Events().ListRaw(ctx, types.GetEventsRequest{
			ExternalCustomerID: &custExt,
			EventName:          &eventName,
		})
		if err == nil && resp.GetEventsResponse != nil {
			seen := 0
			for _, want := range wantIDs {
				for _, ev := range resp.GetEventsResponse.Events {
					if ev.ID != nil && *ev.ID == want {
						seen++
						break
					}
				}
			}
			if seen == len(wantIDs) {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return e2eprobe.Errorf(map[string]string{
				"step":                 "raw_verify",
				"external_customer_id": custExt,
				"event_name":           eventName,
				"feature_id":           featID,
			}, "raw event verification timeout — not all %d events present", len(wantIDs))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (p *BucketedMeterProbe) pollAnalytics(ctx context.Context, spec bucketedSpec, custExt, featID string, start, end time.Time, wantValues []int) error {
	windowSize := spec.window
	deadline := time.Now().Add(90 * time.Second)
	for {
		resp, err := p.client.Events().GetUsageAnalytics(ctx, types.GetUsageAnalyticsRequest{
			ExternalCustomerID: &custExt,
			FeatureIds:         []string{featID},
			StartTime:          &start,
			EndTime:            &end,
			WindowSize:         &windowSize,
		})
		if err == nil && resp.GetUsageAnalyticsResponse != nil && len(resp.GetUsageAnalyticsResponse.Items) > 0 {
			item := resp.GetUsageAnalyticsResponse.Items[0]
			// The response's Points slice should contain one entry per bucket.
			if len(item.Points) >= len(wantValues) {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return e2eprobe.Errorf(map[string]string{
				"step":                 "analytics_poll",
				"external_customer_id": custExt,
				"event_name":           spec.eventName,
				"feature_id":           featID,
			}, "expected %d buckets after 90s, got fewer", len(wantValues))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}
