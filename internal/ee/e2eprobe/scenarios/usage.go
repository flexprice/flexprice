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

// aggregationGrace is how long after an hour closes its meter_usage rows are
// taken as landed.
const aggregationGrace = 3 * time.Minute

// usage compares the persona's last closed hour with the oracle through one
// analytics read; the server serves bucketed meters from meter_usage as the sum
// of each bucket's aggregate, which is what the oracle predicts.
type usage struct{}

func newUsage(scenario.Deps) *usage { return &usage{} }

func (*usage) Name() string               { return "usage" }
func (*usage) Kind() e2eprobe.Kind        { return e2eprobe.KindProbe }
func (*usage) Requires() []e2eprobe.Trait { return []e2eprobe.Trait{e2eprobe.TraitEvents} }

func registerUsage(cat *scenario.Catalog) {
	r := entry("usage", e2eprobe.KindProbe, 15*time.Minute, func(d scenario.Deps) scenario.Scenario { return newUsage(d) }, e2eprobe.TraitEvents).checks("every meter's total over the last full hour equals what the ledger sent, read through events/analytics")
	cat.Register(r.info, r.build)
}

func (*usage) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	p := sc.Persona()
	profile, ok := ledger.Lookup(p.Spec().Events)
	if !ok {
		return sc.Skip("profile", "persona has no events profile")
	}
	w := ledger.HourWindow(sc.Now(), aggregationGrace)
	sc.Attr("window", w.Start.Format(time.RFC3339)+".."+w.End.Format(time.RFC3339))
	if !sc.Ledger().Complete(p.Name(), w, sc.Tick()) {
		return sc.Skip("window", "partial hour: probe did not send every tick")
	}
	if landed, ok := sc.Ledger().LastRecordedAt(p.Name(), w); ok {
		if since := sc.Now().Sub(landed); since < aggregationGrace {
			return sc.Skip("window", fmt.Sprintf("last batch landed %s ago; waiting %s for aggregation", since.Round(time.Second), aggregationGrace))
		}
	}
	meters := profileMeters(profile)
	if len(meters) == 0 {
		return sc.Skip("profile", "events profile has no meters")
	}
	ticks := sc.Ledger().Ticks(p.Name(), w)
	o := ledger.NewOracle(profile, sc.Tick())
	fx := sc.World().Fixtures()
	ext := p.ExternalID()

	featureIDs := make([]string, 0, len(meters))
	for _, m := range meters {
		featureIDs = append(featureIDs, fx.FeatureID(m.Key))
	}
	resp, err := sc.Client().Events().GetUsageAnalytics(ctx, types.GetUsageAnalyticsRequest{
		ExternalCustomerID: &ext,
		FeatureIds:         featureIDs,
		StartTime:          &w.Start,
		EndTime:            &w.End,
	})
	if err != nil {
		return sc.Fail("analytics", err)
	}
	got := usageByFeature(resp.GetGetUsageAnalyticsResponse().GetItems(), p.SubscriptionID())
	for _, m := range meters {
		sc.Quantity(m.Key+" usage", o.Expect(m, ticks), got[fx.FeatureID(m.Key)])
	}
	return sc.Result("assert_usage")
}

// profileMeters resolves the profile's meter keys against the catalog, keeping its order.
func profileMeters(p ledger.Profile) []e2eprobe.Meter {
	var out []e2eprobe.Meter
	for _, key := range p.Meters() {
		if m, ok := e2eprobe.MeterByKey(key); ok {
			out = append(out, m)
		}
	}
	return out
}

// usageByFeature sums total_usage per feature over the items of the persona's
// subscription; a second subscription's items repeat the same usage.
func usageByFeature(items []types.UsageAnalyticItem, subscriptionID string) map[string]decimal.Decimal {
	out := map[string]decimal.Decimal{}
	for _, item := range items {
		if item.FeatureID == nil || (item.SubscriptionID != nil && *item.SubscriptionID != subscriptionID) {
			continue
		}
		out[*item.FeatureID] = out[*item.FeatureID].Add(number(item.TotalUsage))
	}
	return out
}

func number(s *string) decimal.Decimal {
	if s == nil {
		return decimal.Zero
	}
	v, err := decimal.NewFromString(*s)
	if err != nil {
		return decimal.Zero
	}
	return v
}
