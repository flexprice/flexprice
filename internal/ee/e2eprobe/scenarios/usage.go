package scenarios

import (
	"context"
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

// usage compares the persona's last closed hour with the oracle: one analytics
// read covers the plain meters, one usage/meter read per bucketed meter.
type usage struct{}

func newUsage(scenario.Deps) *usage { return &usage{} }

func (*usage) Name() string               { return "usage" }
func (*usage) Kind() e2eprobe.Kind        { return e2eprobe.KindProbe }
func (*usage) Requires() []e2eprobe.Trait { return []e2eprobe.Trait{e2eprobe.TraitEvents} }

func registerUsage(cat *scenario.Catalog) {
	r := entry("usage", e2eprobe.KindProbe, 15*time.Minute, func(d scenario.Deps) scenario.Scenario { return newUsage(d) }, e2eprobe.TraitEvents)
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
	plain, bucketed := splitMeters(profile)
	if len(plain)+len(bucketed) == 0 {
		return sc.Skip("profile", "events profile has no meters")
	}
	ticks := sc.Ledger().Ticks(p.Name(), w)
	o := ledger.NewOracle(profile, sc.Tick())
	fx := sc.World().Fixtures()
	ext := p.ExternalID()

	if len(plain) > 0 {
		featureIDs := make([]string, 0, len(plain))
		for _, m := range plain {
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
		for _, m := range plain {
			sc.Quantity(m.Key+" usage", o.Expect(m, ticks), got[fx.FeatureID(m.Key)])
		}
	}
	for _, m := range bucketed {
		bucket := m.Bucket()
		resp, err := sc.Client().Events().GetUsageByMeter(ctx, types.GetUsageByMeterRequest{
			MeterID:            fx.MeterID(m.Key),
			ExternalCustomerID: &ext,
			StartTime:          &w.Start,
			EndTime:            &w.End,
			WindowSize:         &bucket,
		})
		if err != nil {
			return sc.Fail("usage_by_meter", err)
		}
		sc.Quantity(m.Key+" usage", o.Expect(m, ticks), windowTotal(resp))
	}
	return sc.Result("assert_usage")
}

// splitMeters sorts the profile's meters into the analytics-readable ones and
// the bucketed ones, keeping catalog order.
func splitMeters(p ledger.Profile) (plain, bucketed []e2eprobe.Meter) {
	for _, key := range p.Meters() {
		m, ok := e2eprobe.MeterByKey(key)
		switch {
		case !ok:
		case m.Bucketed():
			bucketed = append(bucketed, m)
		default:
			plain = append(plain, m)
		}
	}
	return plain, bucketed
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

// windowTotal sums the per-window results, falling back to the flat value
// when the server returned none.
func windowTotal(resp *types.GetUsageResponse) decimal.Decimal {
	if resp == nil {
		return decimal.Zero
	}
	if len(resp.Results) == 0 {
		if resp.Value == nil {
			return decimal.Zero
		}
		return decimal.NewFromFloat(*resp.Value)
	}
	total := decimal.Zero
	for _, r := range resp.Results {
		if r.Value != nil {
			total = total.Add(decimal.NewFromFloat(*r.Value))
		}
	}
	return total
}
