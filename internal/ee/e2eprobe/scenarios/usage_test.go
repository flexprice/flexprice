package scenarios

import (
	"reflect"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/ledger"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/world"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// usageFixture is a persona two hours into the day, so the hour before the
// clock is closed and can be sent tick by tick.
type usageFixture struct {
	d       *testDeps
	persona *world.Persona
	profile ledger.Profile
	window  ledger.Window
}

func newUsageFixture(t *testing.T, persona string) *usageFixture {
	t.Helper()
	d := newTestDeps(t)
	p, ok := d.World.Persona(persona)
	if !ok {
		t.Fatalf("no persona %s", persona)
	}
	profile, ok := ledger.Lookup(p.Spec().Events)
	if !ok {
		t.Fatalf("persona %s has no profile", persona)
	}
	d.now = testNow.Add(2*time.Hour + 5*time.Minute)
	return &usageFixture{d: d, persona: p, profile: profile, window: ledger.HourWindow(d.now, 3*time.Minute)}
}

func (f *usageFixture) hourTicks() []time.Time {
	var out []time.Time
	for tick := f.window.Start.Add(f.d.Tick); !tick.After(f.window.End); tick = tick.Add(f.d.Tick) {
		out = append(out, tick)
	}
	return out
}

func (f *usageFixture) send(ticks []time.Time) {
	for _, tick := range ticks {
		f.d.Ledger.Record(f.persona.Name(), tick)
	}
}

func (f *usageFixture) expect(key string) decimal.Decimal {
	m, _ := e2eprobe.MeterByKey(key)
	return ledger.NewOracle(f.profile, f.d.Tick).Expect(m, f.d.Ledger.Ticks(f.persona.Name(), f.window))
}

func (f *usageFixture) featureID(key string) string { return f.d.World.Fixtures().FeatureID(key) }

func (f *usageFixture) item(key string, usage decimal.Decimal, subscriptionID *string) types.UsageAnalyticItem {
	return types.UsageAnalyticItem{FeatureID: probetest.Ptr(f.featureID(key)), TotalUsage: probetest.Ptr(usage.String()), SubscriptionID: subscriptionID}
}

// oracleItems is one analytics item per profile meter, bucketed ones included,
// at the oracle's value.
func (f *usageFixture) oracleItems(subscriptionID *string) []types.UsageAnalyticItem {
	var out []types.UsageAnalyticItem
	for _, key := range f.profile.Meters() {
		out = append(out, f.item(key, f.expect(key), subscriptionID))
	}
	return out
}

func analyticsOf(items []types.UsageAnalyticItem) func(types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
	return func(types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
		return &dtos.GetUsageAnalyticsResponse{GetUsageAnalyticsResponse: &types.GetUsageAnalyticsResponse{Items: items}}, nil
	}
}

func TestUsage_Registration(t *testing.T) {
	cat := scenario.NewCatalog()
	registerUsage(cat)
	info, ok := cat.Scenario("usage")
	if !ok || info.Kind != e2eprobe.KindProbe || info.Every != 15*time.Minute || !reflect.DeepEqual(info.Traits, []e2eprobe.Trait{e2eprobe.TraitEvents}) {
		t.Fatalf("info = %+v, ok = %v", info, ok)
	}
}

func TestUsage_SkipsPartialWindow(t *testing.T) {
	f := newUsageFixture(t, "taxed")
	f.send(f.hourTicks()[1:])
	r := run(t, f.d, newUsage(f.d.Deps), "taxed")
	if r.Status != e2eprobe.StatusSkip || r.Step != "window" || r.Reason != "partial hour: probe did not send every tick" {
		t.Fatalf("result = %+v", r)
	}
	if want := f.window.Start.Format(time.RFC3339) + ".." + f.window.End.Format(time.RFC3339); r.Attrs["window"] != want {
		t.Errorf("attrs = %v, want window %s", r.Attrs, want)
	}
	if len(f.d.fc.Event.AnalyticsRequests) != 0 {
		t.Error("a partial window must not be read")
	}
}

func TestUsage_SkipsPersonaWithoutProfile(t *testing.T) {
	d := newTestDeps(t)
	if r := run(t, d, newUsage(d.Deps), "wallet-a"); r.Status != e2eprobe.StatusSkip || r.Step != "profile" {
		t.Fatalf("result = %+v", r)
	}
}

func TestUsage_PassesWhenAnalyticsMatchesOracle(t *testing.T) {
	f := newUsageFixture(t, "taxed")
	f.send(f.hourTicks())
	f.d.fc.Event.Analytics = analyticsOf(f.oracleItems(nil))
	r := run(t, f.d, newUsage(f.d.Deps), "taxed")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if len(f.d.fc.Event.AnalyticsRequests) != 1 {
		t.Errorf("reads = %d analytics; want exactly one", len(f.d.fc.Event.AnalyticsRequests))
	}
}

func TestUsage_MismatchListsEveryFeature(t *testing.T) {
	f := newUsageFixture(t, "taxed")
	f.send(f.hourTicks())
	var items []types.UsageAnalyticItem
	for _, key := range f.profile.Meters() {
		switch key {
		case "count":
			items = append(items, f.item(key, decimal.NewFromInt(31), nil))
		case "sum":
			// no item: the server reports nothing for this feature
		default:
			items = append(items, f.item(key, f.expect(key), nil))
		}
	}
	f.d.fc.Event.Analytics = analyticsOf(items)
	r := run(t, f.d, newUsage(f.d.Deps), "taxed")
	if r.Status != e2eprobe.StatusFail || r.Step != "assert_usage" || r.Infra {
		t.Fatalf("result = %+v", r)
	}
	want := []string{"count usage: want 30, got 31", "sum usage: want 180, got 0"}
	if !reflect.DeepEqual(r.Mismatches, want) {
		t.Errorf("mismatches = %v, want %v", r.Mismatches, want)
	}
}

func TestUsage_RequestIsBoundedToPersonaAndWindow(t *testing.T) {
	f := newUsageFixture(t, "taxed")
	f.send(f.hourTicks())
	run(t, f.d, newUsage(f.d.Deps), "taxed")
	if len(f.d.fc.Event.AnalyticsRequests) != 1 {
		t.Fatalf("analytics requests = %d, want 1", len(f.d.fc.Event.AnalyticsRequests))
	}
	req := f.d.fc.Event.AnalyticsRequests[0]
	if req.ExternalCustomerID == nil || *req.ExternalCustomerID != "e2eprobe-cust-persistent-0" {
		t.Errorf("external_customer_id = %v", req.ExternalCustomerID)
	}
	if req.StartTime == nil || req.EndTime == nil || !req.StartTime.Equal(f.window.Start) || !req.EndTime.Equal(f.window.End) {
		t.Errorf("window = %v..%v, want %v..%v", req.StartTime, req.EndTime, f.window.Start, f.window.End)
	}
	var want []string
	for _, key := range []string{"count", "sum", "avg", "count_unique", "latest", "max", "sum_multiplier", "sum_filtered", "max_price_hour", "sum_price_hour"} {
		want = append(want, f.featureID(key))
	}
	if !reflect.DeepEqual(req.FeatureIds, want) {
		t.Errorf("feature_ids = %v, want every profile meter %v", req.FeatureIds, want)
	}
}

func TestUsage_IgnoresOtherSubscriptionItems(t *testing.T) {
	f := newUsageFixture(t, "taxed")
	f.send(f.hourTicks())
	items := f.oracleItems(probetest.Ptr(f.persona.SubscriptionID()))
	items = append(items, f.oracleItems(probetest.Ptr("sub-quarterly"))...)
	f.d.fc.Event.Analytics = analyticsOf(items)
	if r := run(t, f.d, newUsage(f.d.Deps), "taxed"); r.Status != e2eprobe.StatusPass {
		t.Fatalf("another subscription's items must not be summed: %+v", r)
	}
}

func TestUsage_AnalyticsErrorIsInfra(t *testing.T) {
	f := newUsageFixture(t, "taxed")
	f.send(f.hourTicks())
	f.d.fc.Event.Analytics = func(types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
		return nil, probetest.ServerError(503)
	}
	r := run(t, f.d, newUsage(f.d.Deps), "taxed")
	if r.Status != e2eprobe.StatusFail || r.Step != "analytics" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
}

// Bucketed meters are served by analytics as the sum of each bucket's
// aggregate, which the oracle predicts; no per-meter read is made.
func TestUsage_BucketedMetersReadThroughAnalytics(t *testing.T) {
	f := newUsageFixture(t, "bucketed")
	f.send(f.hourTicks())
	f.d.fc.Event.Analytics = analyticsOf(f.oracleItems(nil))
	r := run(t, f.d, newUsage(f.d.Deps), "bucketed")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if len(f.d.fc.Event.AnalyticsRequests) != 1 {
		t.Fatalf("analytics requests = %d, want 1", len(f.d.fc.Event.AnalyticsRequests))
	}
	var want []string
	for _, key := range []string{"max_15min", "sum_hour", "max_day"} {
		want = append(want, f.featureID(key))
	}
	if got := f.d.fc.Event.AnalyticsRequests[0].FeatureIds; !reflect.DeepEqual(got, want) {
		t.Errorf("feature_ids = %v, want the bucketed meters %v", got, want)
	}
	// A whole-window max instead of the per-bucket sum is a mismatch the probe must see.
	items := f.oracleItems(nil)
	items[0] = f.item("max_15min", decimal.NewFromInt(7), nil)
	f.d.fc.Event.Analytics = analyticsOf(items)
	if r := run(t, f.d, newUsage(f.d.Deps), "bucketed"); r.Status != e2eprobe.StatusFail || len(r.Mismatches) != 1 || r.Mismatches[0] != "max_15min usage: want "+f.expect("max_15min").String()+", got 7" {
		t.Errorf("result = %+v", r)
	}
}
