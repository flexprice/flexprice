package scenarios

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/world"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// provisionFresh swaps d onto a new fake shaped by prepare and provisions it
// again, for gaps the world records only on a fresh tenant.
func provisionFresh(t *testing.T, d *testDeps, prepare func(fc *probetest.Client)) {
	t.Helper()
	d.fc = probetest.New()
	d.fc.Clock = func() time.Time { return d.now }
	prepare(d.fc)
	w := world.New(d.fc, d.Suite, "run-test", nil)
	w.SetClock(d.fc.Clock)
	if err := w.Provision(context.Background()); err != nil {
		t.Fatalf("provision: %v", err)
	}
	d.Client, d.World = d.fc, w
}

// withoutCountSoftLimit lists a count feature without an id, which takes no
// soft limit, so the entitled persona records the gap.
func withoutCountSoftLimit(fc *probetest.Client) {
	count, _ := e2eprobe.MeterByKey("count")
	fc.Feature.Items = append(fc.Feature.Items, types.FeatureResponse{LookupKey: probetest.Ptr(count.LookupKey)})
}

// usageSummary is the API's usage summary for one feature.
func usageSummary(featureID, usage string, soft bool, limit int64) *dtos.GetCustomerUsageSummaryResponse {
	return &dtos.GetCustomerUsageSummaryResponse{CustomerUsageSummaryResponse: &types.CustomerUsageSummaryResponse{
		Features: []types.FeatureUsageSummary{{
			Feature:      &types.FeatureResponse{ID: probetest.Ptr(featureID)},
			CurrentUsage: probetest.Ptr(usage),
			IsSoftLimit:  probetest.Ptr(soft),
			TotalLimit:   probetest.Ptr(limit),
		}},
	}}
}

func TestEntitlementUsage_PassesWithSoftLimitAndLowerBound(t *testing.T) {
	d := newTestDeps(t)
	entitled, _ := d.World.Persona("entitled")
	featureID := d.World.Fixtures().FeatureID("count")
	// One tick before the period opened, two inside it and one still in flight.
	for _, age := range []time.Duration{3 * time.Hour, 2 * time.Hour, time.Hour, 10 * time.Minute} {
		d.Ledger.Record("entitled", testNow.Add(-age))
	}
	var got dtos.GetCustomerUsageSummaryRequest
	d.fc.Customer.UsageSummary = func(req dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error) {
		got = req
		resp := usageSummary(featureID, "10", true, 100)
		resp.CustomerUsageSummaryResponse.Period = &types.BillingPeriodInfo{StartTime: probetest.Ptr(testNow.Add(-150 * time.Minute))}
		return resp, nil
	}
	r := run(t, d, newEntitlementUsage(d.Deps), "entitled")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if got.CustomerID == nil || *got.CustomerID != entitled.CustomerID() || !reflect.DeepEqual(got.FeatureIds, []string{featureID}) {
		t.Errorf("request = %+v", got)
	}
	if r.Attrs["customer_id"] != entitled.CustomerID() || r.Attrs["subscription_id"] != entitled.SubscriptionID() {
		t.Errorf("attrs = %v", r.Attrs)
	}
}

func TestEntitlementUsage_LowerBoundUsesLedger(t *testing.T) {
	d := newTestDeps(t)
	featureID := d.World.Fixtures().FeatureID("count")
	for _, age := range []time.Duration{3 * time.Hour, 2 * time.Hour, time.Hour} {
		d.Ledger.Record("entitled", testNow.Add(-age))
	}
	d.fc.Customer.UsageSummary = func(dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error) {
		return usageSummary(featureID, "14", true, 100), nil
	}
	r := run(t, d, newEntitlementUsage(d.Deps), "entitled")
	if r.Status != e2eprobe.StatusFail || r.Step != "assert_entitlement_usage" || len(r.Mismatches) != 1 {
		t.Fatalf("result = %+v", r)
	}
	if !strings.Contains(r.Mismatches[0], "current usage 14 >= 15") {
		t.Errorf("mismatches = %v", r.Mismatches)
	}
}

func TestEntitlementUsage_MismatchOnHardLimit(t *testing.T) {
	d := newTestDeps(t)
	featureID := d.World.Fixtures().FeatureID("count")
	d.fc.Customer.UsageSummary = func(dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error) {
		return usageSummary(featureID, "0", false, 50), nil
	}
	r := run(t, d, newEntitlementUsage(d.Deps), "entitled")
	if r.Status != e2eprobe.StatusFail || r.Step != "assert_entitlement_usage" {
		t.Fatalf("result = %+v", r)
	}
	want := []string{"count entitlement is a soft limit", "count entitlement limit: want 100, got 50"}
	if !reflect.DeepEqual(r.Mismatches, want) {
		t.Errorf("mismatches = %v", r.Mismatches)
	}
}

func TestEntitlementUsage_MismatchWhenFeatureMissing(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Customer.UsageSummary = func(dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error) {
		return usageSummary("feat_other", "0", true, 100), nil
	}
	r := run(t, d, newEntitlementUsage(d.Deps), "entitled")
	if r.Status != e2eprobe.StatusFail || r.Step != "assert_entitlement_usage" || r.Reason != "count feature missing from usage summary" {
		t.Fatalf("result = %+v", r)
	}
}

func TestEntitlementUsage_SkipsOnGap(t *testing.T) {
	d := newTestDeps(t)
	provisionFresh(t, d, withoutCountSoftLimit)
	r := run(t, d, newEntitlementUsage(d.Deps), "entitled")
	if r.Status != e2eprobe.StatusSkip || r.Step != "persona" || !strings.Contains(r.Reason, "soft-limit") {
		t.Fatalf("result = %+v", r)
	}
}

func TestEntitlementUsage_InfraErrorIsFail(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Customer.UsageSummary = func(dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error) {
		return nil, probetest.ServerError(503)
	}
	r := run(t, d, newEntitlementUsage(d.Deps), "entitled")
	if r.Status != e2eprobe.StatusFail || r.Step != "usage_summary" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
}

func TestRegisterEntitlements_DeclaresEveryScenario(t *testing.T) {
	cat := scenario.NewCatalog()
	registerEntitlements(cat)
	want := []suite.ScenarioInfo{
		{Name: "entitlement-usage", Kind: e2eprobe.KindProbe, Traits: []e2eprobe.Trait{e2eprobe.TraitSoftLimit, e2eprobe.TraitEvents}, Every: 30 * time.Minute},
		{Name: "entitlement-enforcement", Kind: e2eprobe.KindFlow, Traits: []e2eprobe.Trait{e2eprobe.TraitSoftLimit}, Every: time.Hour, EventsPerRun: overLimitEvents},
		{Name: "grant-inheritance", Kind: e2eprobe.KindFlow, Traits: []e2eprobe.Trait{e2eprobe.TraitAdditiveGrant}, Every: time.Hour, EventsPerRun: grantEvents},
	}
	for _, w := range want {
		info, ok := cat.Scenario(w.Name)
		if info.Summary == "" {
			t.Errorf("%s: no summary", w.Name)
		}
		info.Summary = ""
		if !ok || !reflect.DeepEqual(info, w) {
			t.Errorf("%s: info = %+v, registered = %v", w.Name, info, ok)
			continue
		}
		s, err := cat.Build(w.Name, scenario.Deps{})
		if err != nil || s.Name() != w.Name || s.Kind() != w.Kind || !reflect.DeepEqual(s.Requires(), w.Traits) {
			t.Errorf("%s: built scenario disagrees with its registration: %v", w.Name, err)
		}
	}
}
