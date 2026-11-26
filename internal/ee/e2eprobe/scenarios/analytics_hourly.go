package scenarios

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/ledger"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// windowHours is how many closed hours one analytics-hourly run compares.
const windowHours = 3

// analyticsHourly reads the last closed hours with window_size=HOUR and
// compares every hourly point with the oracle, so the windowed shape of the
// analytics API is checked, not only its total. Bucketed meters are left to
// usage: their hourly points are bucket aggregates, not hourly ones.
type analyticsHourly struct{}

func newAnalyticsHourly(scenario.Deps) *analyticsHourly { return &analyticsHourly{} }

func (*analyticsHourly) Name() string               { return "analytics-hourly" }
func (*analyticsHourly) Kind() e2eprobe.Kind        { return e2eprobe.KindProbe }
func (*analyticsHourly) Requires() []e2eprobe.Trait { return []e2eprobe.Trait{e2eprobe.TraitEvents} }

func (*analyticsHourly) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	p := sc.Persona()
	profile, ok := ledger.Lookup(p.Spec().Events)
	if !ok {
		return sc.Skip("profile", "persona has no events profile")
	}
	last := ledger.HourWindow(sc.Now(), aggregationGrace)
	span := ledger.Window{Start: last.End.Add(-windowHours * time.Hour), End: last.End}
	sc.Attr("window", span.Start.Format(time.RFC3339)+".."+span.End.Format(time.RFC3339))
	var hours []ledger.Window
	for start := span.Start; start.Before(span.End); start = start.Add(time.Hour) {
		hw := ledger.Window{Start: start, End: start.Add(time.Hour)}
		if !sc.Ledger().Complete(p.Name(), hw, sc.Tick()) {
			return sc.Skip("window", fmt.Sprintf("partial hours: probe did not send every tick of the last %d hours", windowHours))
		}
		hours = append(hours, hw)
	}
	if landed, ok := sc.Ledger().LastRecordedAt(p.Name(), last); ok {
		if since := sc.Now().Sub(landed); since < aggregationGrace {
			return sc.Skip("window", fmt.Sprintf("last batch landed %s ago; waiting %s for aggregation", since.Round(time.Second), aggregationGrace))
		}
	}
	var meters []e2eprobe.Meter
	for _, m := range profileMeters(profile) {
		if m.MeterBucket == "" && m.PriceBucket == "" {
			meters = append(meters, m)
		}
	}
	if len(meters) == 0 {
		return sc.Skip("profile", "events profile has no unbucketed meters")
	}
	fx := sc.World().Fixtures()
	ext := p.ExternalID()
	featureIDs := make([]string, 0, len(meters))
	for _, m := range meters {
		featureIDs = append(featureIDs, fx.FeatureID(m.Key))
	}
	hour := types.WindowSizeHour
	resp, err := sc.Client().Events().GetUsageAnalytics(ctx, types.GetUsageAnalyticsRequest{
		ExternalCustomerID: &ext,
		FeatureIds:         featureIDs,
		StartTime:          &span.Start,
		EndTime:            &span.End,
		WindowSize:         &hour,
	})
	if err != nil {
		return sc.Fail("analytics", err)
	}
	got := pointsByFeature(resp.GetGetUsageAnalyticsResponse().GetItems(), p.SubscriptionID())
	o := ledger.NewOracle(profile, sc.Tick())
	for _, m := range meters {
		for _, hw := range hours {
			want := o.Expect(m, sc.Ledger().Ticks(p.Name(), hw))
			sc.Quantity(fmt.Sprintf("%s usage %s", m.Key, hw.Start.Format("15:04")), want, got[fx.FeatureID(m.Key)][hw.Start])
		}
	}
	return sc.Result("assert_windows")
}

// pointsByFeature sums each item's hourly points for the persona's subscription,
// keyed by feature id and by the hour the point starts.
func pointsByFeature(items []types.UsageAnalyticItem, subscriptionID string) map[string]map[time.Time]decimal.Decimal {
	out := map[string]map[time.Time]decimal.Decimal{}
	for _, item := range items {
		if item.FeatureID == nil || (item.SubscriptionID != nil && *item.SubscriptionID != subscriptionID) {
			continue
		}
		hours := out[*item.FeatureID]
		if hours == nil {
			hours = map[time.Time]decimal.Decimal{}
			out[*item.FeatureID] = hours
		}
		for _, pt := range item.Points {
			if pt.Timestamp == nil {
				continue
			}
			at, err := time.Parse(time.RFC3339, *pt.Timestamp)
			if err != nil {
				continue
			}
			start := at.UTC().Truncate(time.Hour)
			hours[start] = hours[start].Add(number(pt.Usage))
		}
	}
	return out
}
