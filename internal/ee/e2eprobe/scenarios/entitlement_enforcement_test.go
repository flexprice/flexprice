package scenarios

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/world"
	"github.com/flexprice/go-sdk/v2/models/dtos"
)

// countIngested is how many events of the meter the fake has accepted.
func countIngested(fc *probetest.Client, eventName string) int {
	n := 0
	for _, ev := range fc.Event.Ingested {
		if ev.EventName == eventName {
			n++
		}
	}
	return n
}

func TestEntitlementEnforcement_PassesAt150(t *testing.T) {
	d := newTestDeps(t)
	featureID := d.World.Fixtures().FeatureID("count")
	polls := 0
	// The pipeline lags: the first two reads see nothing, then the rollup lands.
	d.fc.Customer.UsageSummary = func(dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error) {
		polls++
		if polls <= 2 {
			return usageSummary(featureID, "0", true, 100), nil
		}
		return usageSummary(featureID, strconv.Itoa(countIngested(d.fc, "e2eprobe_count")), true, 100), nil
	}
	s := newEntitlementEnforcement(d.Deps)
	s.every = time.Millisecond
	r := run(t, d, s, "entitled")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if polls != 3 {
		t.Errorf("polls = %d, want the summary re-read until it reports 150", polls)
	}
	if len(d.fc.Event.Batches) != 1 || len(d.fc.Event.Batches[0]) != 150 {
		t.Fatalf("batches = %d", len(d.fc.Event.Batches))
	}
	first := d.fc.Event.Batches[0][0]
	if first.EventName != "e2eprobe_count" || first.ExternalCustomerID != r.Attrs["ephemeral_external_id"] || first.EventID == nil || first.Timestamp == nil {
		t.Errorf("event = %+v", first)
	}
	if *first.Timestamp != testNow.Format(time.RFC3339Nano) || !strings.HasPrefix(*first.EventID, r.Attrs["ephemeral_external_id"]) {
		t.Errorf("event id %q at %q must derive from the clone and the clock", *first.EventID, *first.Timestamp)
	}
}

func TestEntitlementEnforcement_TimesOut(t *testing.T) {
	d := newTestDeps(t)
	featureID := d.World.Fixtures().FeatureID("count")
	d.fc.Customer.UsageSummary = func(dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error) {
		return usageSummary(featureID, "149", true, 100), nil
	}
	s := newEntitlementEnforcement(d.Deps)
	s.every, s.timeout = time.Millisecond, 20*time.Millisecond
	r := run(t, d, s, "entitled")
	if r.Status != e2eprobe.StatusFail || r.Step != "assert_enforcement" || len(r.Mismatches) != 1 {
		t.Fatalf("result = %+v", r)
	}
	if r.Mismatches[0] != "current usage: want 150, got 149" {
		t.Errorf("mismatches = %v", r.Mismatches)
	}
}

func TestEntitlementEnforcement_ClonesFromPersona(t *testing.T) {
	d := newTestDeps(t)
	featureID := d.World.Fixtures().FeatureID("count")
	d.fc.Customer.UsageSummary = func(dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error) {
		return usageSummary(featureID, "150", true, 100), nil
	}
	s := newEntitlementEnforcement(d.Deps)
	s.every = time.Millisecond
	r := run(t, d, s, "entitled")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	ext := r.Attrs["ephemeral_external_id"]
	eph, ok := d.fc.Customer.Items[ext]
	if !ok || !strings.HasPrefix(ext, world.EphemeralPrefix) || eph.Metadata["e2eprobe_persona"] != "entitled" || eph.Metadata["e2eprobe_cohort"] != world.EphemeralCohort || eph.Metadata["e2eprobe_scenario"] != "entitlement-enforcement" {
		t.Fatalf("ephemeral %q = %+v", ext, eph)
	}
	sub, ok := d.fc.Subscription.Items[r.Attrs["subscription_id"]]
	if !ok || sub.CustomerID == nil || *sub.CustomerID != *eph.ID || r.Attrs["customer_id"] != *eph.ID {
		t.Errorf("subscription = %+v, attrs = %v", sub, r.Attrs)
	}
	if d.World.Ephemerals().Count("customer") != 1 || d.World.Ephemerals().Count("subscription") != 1 {
		t.Error("the clone must be registered for the janitor")
	}
}

func TestEntitlementEnforcement_SkipsOnGap(t *testing.T) {
	d := newTestDeps(t)
	provisionFresh(t, d, withoutCountSoftLimit)
	r := run(t, d, newEntitlementEnforcement(d.Deps), "entitled")
	if r.Status != e2eprobe.StatusSkip || r.Step != "persona" {
		t.Fatalf("result = %+v", r)
	}
	if d.World.Ephemerals().Count("customer") != 0 {
		t.Error("a skipped run must not clone")
	}
}

func TestEntitlementEnforcement_IngestFailureIsInfra(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Event.IngestErr = probetest.ServerError(503)
	r := run(t, d, newEntitlementEnforcement(d.Deps), "entitled")
	if r.Status != e2eprobe.StatusFail || r.Step != "ingest_bulk" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
}
