package scenarios

import (
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/probetest"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// grantedUsage is the multiplied sum of every grant-meter event the fake accepted.
func grantedUsage(fc *probetest.Client) string {
	total := decimal.Zero
	for _, ev := range fc.Event.Ingested {
		if ev.EventName == "e2eprobe_sum_multiplier" {
			total = total.Add(decimal.RequireFromString(ev.Properties["amount"]))
		}
	}
	return total.Mul(decimal.NewFromInt(1000)).String()
}

// inheriting lists the feature on every subscription's entitlements.
func inheriting(featureID string) func(string) (*dtos.GetSubscriptionEntitlementsResponse, error) {
	return func(string) (*dtos.GetSubscriptionEntitlementsResponse, error) {
		return &dtos.GetSubscriptionEntitlementsResponse{SubscriptionEntitlementsResponse: &types.SubscriptionEntitlementsResponse{
			Features: []types.AggregatedFeature{{Feature: &types.FeatureResponse{ID: probetest.Ptr(featureID)}}},
		}}, nil
	}
}

// grantScenario wires the fake for a healthy run and returns the scenario with short polls.
func grantScenario(d *testDeps) *grantInheritance {
	featureID := d.World.Fixtures().FeatureID("sum_multiplier")
	d.fc.Subscription.Entitlements = inheriting(featureID)
	d.fc.Customer.UsageSummary = func(dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error) {
		return usageSummary(featureID, grantedUsage(d.fc), false, 0), nil
	}
	s := newGrantInheritance(d.Deps)
	s.every, s.timeout = time.Millisecond, 20*time.Millisecond
	return s
}

func TestGrantInheritance_Passes(t *testing.T) {
	d := newTestDeps(t)
	s := grantScenario(d)
	r := run(t, d, s, "granted")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	grantID := d.World.Fixtures().GrantEntitlementID()
	if calls := d.fc.Entitlement.GetRawCalls; calls[len(calls)-1] != grantID || r.Attrs["entitlement_id"] != grantID {
		t.Errorf("the grant must be read back: calls = %v, attrs = %v", calls, r.Attrs)
	}
	if len(d.fc.Event.Batches) != 1 || len(d.fc.Event.Batches[0]) != 200 {
		t.Fatalf("batches = %d", len(d.fc.Event.Batches))
	}
	ev := d.fc.Event.Batches[0][0]
	if ev.EventName != "e2eprobe_sum_multiplier" || ev.Properties["amount"] != "1" || ev.ExternalCustomerID != r.Attrs["ephemeral_external_id"] {
		t.Errorf("event = %+v", ev)
	}
	if eph := d.fc.Customer.Items[r.Attrs["ephemeral_external_id"]]; eph.Metadata["e2eprobe_persona"] != "granted" || r.Attrs["subscription_id"] == "" {
		t.Errorf("clone = %+v, attrs = %v", eph, r.Attrs)
	}
}

func TestGrantInheritance_MismatchOnGrantEcho(t *testing.T) {
	d := newTestDeps(t)
	s := grantScenario(d)
	d.fc.Entitlement.GetRawFn = func(id string) (*e2eprobe.GrantEntitlementResponse, error) {
		return &e2eprobe.GrantEntitlementResponse{ID: id, GrantMeasure: "amount", AggregationMode: "additive", IsEnabled: true}, nil
	}
	r := run(t, d, s, "granted")
	if r.Status != e2eprobe.StatusFail || r.Step != "assert_grant" || len(r.Mismatches) != 1 || r.Mismatches[0] != "grant echoes additive quantity grant" {
		t.Fatalf("result = %+v", r)
	}
}

func TestGrantInheritance_MismatchOnInheritanceAndUsage(t *testing.T) {
	cases := []struct {
		name  string
		steer func(d *testDeps)
		want  string
	}{
		{"clone lacks the feature", func(d *testDeps) { d.fc.Subscription.Entitlements = nil }, "clone inherits the grant feature"},
		{"usage never lands", func(d *testDeps) {
			d.fc.Customer.UsageSummary = func(dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error) {
				return usageSummary(d.World.Fixtures().FeatureID("sum_multiplier"), "199000", false, 0), nil
			}
		}, "current usage: want 200000, got 199000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDeps(t)
			s := grantScenario(d)
			tc.steer(d)
			r := run(t, d, s, "granted")
			if r.Status != e2eprobe.StatusFail || r.Step != "assert_grant" || len(r.Mismatches) != 1 || r.Mismatches[0] != tc.want {
				t.Fatalf("result = %+v", r)
			}
		})
	}
}

func TestGrantInheritance_SkipsWithoutGrant(t *testing.T) {
	d := newTestDeps(t)
	provisionFresh(t, d, func(fc *probetest.Client) { fc.Entitlement.CreateGrantErr = probetest.ServerError(500) })
	r := run(t, d, newGrantInheritance(d.Deps), "granted")
	if r.Status != e2eprobe.StatusSkip || r.Step != "persona" || !strings.Contains(r.Reason, "grant") {
		t.Fatalf("result = %+v", r)
	}
	if d.World.Ephemerals().Count("customer") != 0 {
		t.Error("a skipped run must not clone")
	}
}

func TestGrantInheritance_InfraErrorIsFail(t *testing.T) {
	d := newTestDeps(t)
	s := grantScenario(d)
	d.fc.Entitlement.GetRawErr = probetest.ServerError(503)
	r := run(t, d, s, "granted")
	if r.Status != e2eprobe.StatusFail || r.Step != "get_grant" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
}
