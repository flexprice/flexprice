package scenarios

import (
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/ledger"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

func ticksIn(w ledger.Window, interval time.Duration) []time.Time {
	var out []time.Time
	for tick := w.Start.Add(interval); !tick.After(w.End); tick = tick.Add(interval) {
		out = append(out, tick)
	}
	return out
}

// hourlyAnalytics answers a window_size=HOUR request with one point per hour at
// the oracle's value; tamper nudges the first point so a mismatch must surface.
func hourlyAnalytics(f *usageFixture, hours []ledger.Window, tamper bool) func(types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
	return func(req types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
		if req.WindowSize == nil || *req.WindowSize != types.WindowSizeHour {
			return nil, errors.New("window_size HOUR was not requested")
		}
		if req.StartTime == nil || req.EndTime == nil || !req.StartTime.Equal(hours[0].Start) || !req.EndTime.Equal(hours[len(hours)-1].End) {
			return nil, errors.New("the request does not span the hours under test")
		}
		o := ledger.NewOracle(f.profile, f.d.Tick)
		var items []types.UsageAnalyticItem
		for _, featureID := range req.FeatureIds {
			for _, m := range e2eprobe.Meters() {
				if f.featureID(m.Key) != featureID {
					continue
				}
				var points []types.UsageAnalyticPoint
				for i, hw := range hours {
					want := o.Expect(m, f.d.Ledger.Ticks(f.persona.Name(), hw))
					if tamper && i == 0 && len(items) == 0 {
						want = want.Add(decimal.NewFromInt(1))
					}
					points = append(points, types.UsageAnalyticPoint{Timestamp: probetest.Ptr(hw.Start.Format(time.RFC3339)), Usage: probetest.Ptr(want.String())})
				}
				items = append(items, types.UsageAnalyticItem{FeatureID: probetest.Ptr(featureID), Points: points})
			}
		}
		return &dtos.GetUsageAnalyticsResponse{GetUsageAnalyticsResponse: &types.GetUsageAnalyticsResponse{Items: items}}, nil
	}
}

func TestAnalyticsHourly(t *testing.T) {
	cases := []struct {
		name      string
		hoursSent int // of the three hours, how many have every tick, newest first
		tamper    bool
		status    e2eprobe.Status
		step      string
	}{
		{"every hourly point equals the oracle", 3, false, e2eprobe.StatusPass, ""},
		{"a point off by one is a mismatch", 3, true, e2eprobe.StatusFail, "assert_windows"},
		{"an hour with a missing tick skips", 2, false, e2eprobe.StatusSkip, "window"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newUsageFixture(t, "baseline-a")
			f.d.now = testNow.Add(4*time.Hour + 5*time.Minute)
			last := ledger.HourWindow(f.d.now, aggregationGrace)
			var hours []ledger.Window
			for start := last.End.Add(-windowHours * time.Hour); start.Before(last.End); start = start.Add(time.Hour) {
				hours = append(hours, ledger.Window{Start: start, End: start.Add(time.Hour)})
			}
			for i, hw := range hours {
				if i >= len(hours)-tc.hoursSent {
					f.send(ticksIn(hw, f.d.Tick))
				}
			}
			f.d.fc.Event.Analytics = hourlyAnalytics(f, hours, tc.tamper)
			r := run(t, f.d, newAnalyticsHourly(f.d.Deps), "baseline-a")
			if r.Status != tc.status || r.Step != tc.step {
				t.Fatalf("result = %+v", r)
			}
			if tc.tamper && len(r.Mismatches) == 0 {
				t.Error("a wrong hourly point must be reported as a mismatch")
			}
		})
	}
}
