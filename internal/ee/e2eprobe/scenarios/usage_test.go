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
func (f *usageFixture) meterID(key string) string   { return f.d.World.Fixtures().MeterID(key) }

func (f *usageFixture) meterKey(meterID string) string {
	for _, m := range e2eprobe.Meters() {
		if f.meterID(m.Key) == meterID {
			return m.Key
		}
	}
	return ""
}

func (f *usageFixture) item(key string, usage decimal.Decimal, subscriptionID *string) types.UsageAnalyticItem {
	return types.UsageAnalyticItem{FeatureID: probetest.Ptr(f.featureID(key)), TotalUsage: probetest.Ptr(usage.String()), SubscriptionID: subscriptionID}
}

// oracleItems is one analytics item per unbucketed meter, at the oracle's value.
func (f *usageFixture) oracleItems(subscriptionID *string) []types.UsageAnalyticItem {
	var out []types.UsageAnalyticItem
	for _, key := range f.profile.Meters() {
		if m, _ := e2eprobe.MeterByKey(key); m.Bucketed() {
			continue
		}
		out = append(out, f.item(key, f.expect(key), subscriptionID))
	}
	return out
}

func analyticsOf(items []types.UsageAnalyticItem) func(types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
	return func(types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
		return &dtos.GetUsageAnalyticsResponse{GetUsageAnalyticsResponse: &types.GetUsageAnalyticsResponse{Items: items}}, nil
	}
}

// oracleByMeter answers usage/meter with the oracle's value split over two windows.
func (f *usageFixture) oracleByMeter() func(types.GetUsageByMeterRequest) (*types.GetUsageResponse, error) {
	return func(req types.GetUsageByMeterRequest) (*types.GetUsageResponse, error) {
		key := f.meterKey(req.MeterID)
		if key == "" {
			return nil, probetest.NotFound()
		}
		want, _ := f.expect(key).Float64()
		half := want / 2
		return &types.GetUsageResponse{Results: []types.UsageResult{{Value: &half}, {Value: &half}}}, nil
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
	if len(f.d.fc.Event.AnalyticsRequests)+len(f.d.fc.Event.UsageByMeterRequests) != 0 {
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
	f.d.fc.Event.UsageByMeter = f.oracleByMeter()
	r := run(t, f.d, newUsage(f.d.Deps), "taxed")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if len(f.d.fc.Event.AnalyticsRequests) != 1 || len(f.d.fc.Event.UsageByMeterRequests) != 3 {
		t.Errorf("reads = %d analytics, %d usage/meter; want 1 and 3 (max, max_price_hour, sum_price_hour)", len(f.d.fc.Event.AnalyticsRequests), len(f.d.fc.Event.UsageByMeterRequests))
	}
}

func TestUsage_MismatchListsEveryFeature(t *testing.T) {
	f := newUsageFixture(t, "taxed")
	f.send(f.hourTicks())
	items := []types.UsageAnalyticItem{f.item("count", decimal.NewFromInt(31), nil)}
	for _, key := range []string{"avg", "count_unique", "latest", "sum_multiplier", "sum_filtered"} {
		items = append(items, f.item(key, f.expect(key), nil))
	}
	f.d.fc.Event.Analytics = analyticsOf(items)
	f.d.fc.Event.UsageByMeter = f.oracleByMeter()
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
	for _, key := range []string{"count", "sum", "avg", "count_unique", "latest", "sum_multiplier", "sum_filtered"} {
		want = append(want, f.featureID(key))
	}
	if !reflect.DeepEqual(req.FeatureIds, want) {
		t.Errorf("feature_ids = %v, want the unbucketed meters %v", req.FeatureIds, want)
	}
	for _, mr := range f.d.fc.Event.UsageByMeterRequests {
		if mr.ExternalCustomerID == nil || *mr.ExternalCustomerID != "e2eprobe-cust-persistent-0" || mr.StartTime == nil || !mr.StartTime.Equal(f.window.Start) || mr.EndTime == nil || !mr.EndTime.Equal(f.window.End) {
			t.Errorf("usage/meter request not bounded: %+v", mr)
		}
	}
}

func TestUsage_IgnoresOtherSubscriptionItems(t *testing.T) {
	f := newUsageFixture(t, "taxed")
	f.send(f.hourTicks())
	items := f.oracleItems(probetest.Ptr(f.persona.SubscriptionID()))
	items = append(items, f.oracleItems(probetest.Ptr("sub-quarterly"))...)
	f.d.fc.Event.Analytics = analyticsOf(items)
	f.d.fc.Event.UsageByMeter = f.oracleByMeter()
	if r := run(t, f.d, newUsage(f.d.Deps), "taxed"); r.Status != e2eprobe.StatusPass {
		t.Fatalf("another subscription's items must not be summed: %+v", r)
	}
}

func TestUsage_InfraErrorIsFail(t *testing.T) {
	cases := []struct {
		step  string
		setup func(f *usageFixture)
	}{
		{"analytics", func(f *usageFixture) {
			f.d.fc.Event.Analytics = func(types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
				return nil, probetest.ServerError(503)
			}
		}},
		{"usage_by_meter", func(f *usageFixture) {
			f.d.fc.Event.Analytics = analyticsOf(f.oracleItems(nil))
			f.d.fc.Event.UsageByMeter = func(types.GetUsageByMeterRequest) (*types.GetUsageResponse, error) {
				return nil, probetest.ServerError(502)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.step, func(t *testing.T) {
			f := newUsageFixture(t, "taxed")
			f.send(f.hourTicks())
			tc.setup(f)
			r := run(t, f.d, newUsage(f.d.Deps), "taxed")
			if r.Status != e2eprobe.StatusFail || r.Step != tc.step || !r.Infra {
				t.Fatalf("result = %+v", r)
			}
		})
	}
}

func TestUsage_BucketedUsesBucketWindowSize(t *testing.T) {
	f := newUsageFixture(t, "bucketed")
	f.send(f.hourTicks())
	seven := 7.0
	f.d.fc.Event.UsageByMeter = func(req types.GetUsageByMeterRequest) (*types.GetUsageResponse, error) {
		// The quarter-hour meter answers per window; the others only carry the flat value.
		if f.meterKey(req.MeterID) == "max_15min" {
			return &types.GetUsageResponse{Results: []types.UsageResult{{Value: &seven}, {Value: &seven}, {Value: &seven}, {Value: &seven}}}, nil
		}
		value, _ := f.expect(f.meterKey(req.MeterID)).Float64()
		return &types.GetUsageResponse{Value: &value}, nil
	}
	r := run(t, f.d, newUsage(f.d.Deps), "bucketed")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if len(f.d.fc.Event.AnalyticsRequests) != 0 {
		t.Errorf("a profile of bucketed meters needs no analytics read, got %d", len(f.d.fc.Event.AnalyticsRequests))
	}
	got := map[string]types.WindowSize{}
	for _, req := range f.d.fc.Event.UsageByMeterRequests {
		if req.WindowSize != nil {
			got[f.meterKey(req.MeterID)] = *req.WindowSize
		}
	}
	want := map[string]types.WindowSize{"max_15min": types.WindowSizeFifteenMin, "sum_hour": types.WindowSizeHour, "max_day": types.WindowSizeDay}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("window sizes = %v, want %v", got, want)
	}
}
