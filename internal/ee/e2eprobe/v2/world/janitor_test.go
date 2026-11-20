package world

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/suite"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestSweep_ArchivesOldEphemeralsOnly(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	ctx := context.Background()
	if err := w.Provision(ctx); err != nil {
		t.Fatal(err)
	}
	baseline, _ := w.Persona("baseline-a")
	old, err := w.Clone(ctx, baseline, "lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	fc.Clock = func() time.Time { return testStart.Add(2 * time.Hour) }
	young, err := w.Clone(ctx, baseline, "lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	report := w.Sweep(ctx, testStart.Add(time.Hour))
	if report.Archived != 2 || len(report.Failures) != 0 {
		t.Fatalf("report = %+v", report)
	}
	if _, stillThere := fc.Customer.Items[old.ExternalID()]; stillThere {
		t.Error("old ephemeral customer must be deleted")
	}
	if _, kept := fc.Customer.Items[young.ExternalID()]; !kept {
		t.Error("young ephemeral customer must be kept")
	}
	if len(fc.Subscription.Cancelled) != 1 || fc.Subscription.Cancelled[0] != old.SubscriptionID() {
		t.Errorf("the old clone's subscription must be cancelled first: %v", fc.Subscription.Cancelled)
	}
	if w.Ephemerals().Count("customer") != 1 {
		t.Errorf("registry must keep the young clone only, got %d", w.Ephemerals().Count("customer"))
	}
}

func TestSweep_FindsOrphansByPrefixAndMetadata(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	old := testStart.Add(-3 * time.Hour)
	fc.Customer.Add("e2eprobe-eph-lifecycle-1", "cust_o1", nil, old)
	fc.Customer.Add("e2eprobe-cust-eph-legacy", "cust_o2", nil, old)
	fc.Customer.Add("renamed", "cust_o3", map[string]string{"e2eprobe_cohort": EphemeralCohort}, old)
	fc.Customer.Add("tagged-legacy", "cust_o4", map[string]string{"e2eprobe_role": "ephemeral"}, old)
	fc.Customer.Add("e2eprobe-eph-young", "cust_y", nil, testStart.Add(-10*time.Minute))
	fc.Customer.Add("real-customer", "cust_real", nil, old)
	report := w.Sweep(context.Background(), testStart.Add(-time.Hour))
	if report.Orphans != 4 || len(report.Failures) != 0 {
		t.Fatalf("report = %+v", report)
	}
	for _, ext := range []string{"e2eprobe-eph-lifecycle-1", "e2eprobe-cust-eph-legacy", "renamed", "tagged-legacy"} {
		if _, stillThere := fc.Customer.Items[ext]; stillThere {
			t.Errorf("%s must be deleted", ext)
		}
	}
	for _, ext := range []string{"e2eprobe-eph-young", "real-customer"} {
		if _, kept := fc.Customer.Items[ext]; !kept {
			t.Errorf("%s must be kept", ext)
		}
	}
}

func TestSweep_ListsLegacyPersistentCustomers(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	fc.Customer.Add("e2eprobe-cust-persistent-7", "cust_p7", map[string]string{"e2eprobe_cohort": PersistentCohort}, testStart.Add(-time.Hour))
	fc.Customer.Add("e2eprobe-cust-persistent-0", "cust_p0", map[string]string{"e2eprobe_cohort": PersistentCohort}, testStart.Add(-time.Hour))
	fc.Customer.Add("e2eprobe-cust-pay-stripe-cards", "cust_cards", map[string]string{"e2eprobe_cohort": PersistentCohort}, testStart.Add(-time.Hour))
	fc.Customer.Add("e2eprobe-cust-pay-stripe-expiry", "cust_expiry", map[string]string{"e2eprobe_cohort": PersistentCohort}, testStart.Add(-time.Hour))
	report := w.Sweep(context.Background(), testStart)
	if len(report.Legacy) != 1 || report.Legacy[0] != "e2eprobe-cust-persistent-7" {
		t.Errorf("legacy = %v, want only persistent-7", report.Legacy)
	}
	if len(fc.Customer.Deleted) != 0 {
		t.Error("legacy fixtures are reported, never deleted")
	}
}

func TestSweep_NotFoundIsSuccess(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	w.Ephemerals().Register("customer", "e2eprobe-eph-gone", testStart.Add(-3*time.Hour))
	w.Ephemerals().Register("subscription", "sub_gone", testStart.Add(-3*time.Hour))
	report := w.Sweep(context.Background(), testStart)
	if len(report.Failures) != 0 || report.Archived != 2 {
		t.Fatalf("report = %+v", report)
	}
	if w.Ephemerals().Count("customer") != 0 || w.Ephemerals().Count("subscription") != 0 {
		t.Error("vanished entries must leave the registry")
	}
	_ = fc
}

func TestSweep_RemovesOrphanTaxAssociations(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	ctx := context.Background()
	if err := w.Provision(ctx); err != nil {
		t.Fatal(err)
	}
	entityType := types.TaxRateEntityTypeSubscription
	if _, err := fc.TaxAssoc.Create(ctx, types.CreateTaxAssociationRequest{TaxRateCode: w.Fixtures().TaxRateCode(), EntityID: probetest.Ptr("sub_vanished"), EntityType: &entityType}); err != nil {
		t.Fatal(err)
	}
	report := w.Sweep(ctx, testStart)
	if report.TaxAssociations != 1 || len(fc.TaxAssoc.Deleted) != 1 {
		t.Fatalf("report = %+v deleted = %v", report, fc.TaxAssoc.Deleted)
	}
	taxed, _ := w.Persona("taxed")
	kept := false
	for _, ta := range fc.TaxAssoc.Items {
		if ta.EntityID != nil && *ta.EntityID == taxed.SubscriptionID() {
			kept = true
		}
	}
	if !kept {
		t.Error("the persona's association must survive the sweep")
	}
}

func TestSweep_RunsRegisteredSweepers(t *testing.T) {
	w, _ := newWorld(t, suite.Default())
	s := &stubSweeper{removed: 2, failed: []string{"plan_x: 409"}}
	w.AddSweeper(s)
	report := w.Sweep(context.Background(), testStart)
	if !s.called || len(report.Failures) != 1 || report.Failures[0] != "plan_x: 409" {
		t.Errorf("report = %+v called=%v", report, s.called)
	}
}

type stubSweeper struct {
	called  bool
	removed int
	failed  []string
}

func (s *stubSweeper) SweepOrphans(_ context.Context, _ time.Time) (int, []string) {
	s.called = true
	return s.removed, s.failed
}
