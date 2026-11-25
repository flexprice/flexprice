package world

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
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

func TestSweep_RemovesOnlyOwnedEphemerals(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	old := testStart.Add(-3 * time.Hour)
	fc.Customer.Add("e2eprobe-eph-lifecycle-1", "cust_o1", nil, old)
	fc.Customer.Add("renamed", "cust_o2", map[string]string{"e2eprobe_cohort": EphemeralCohort}, old)
	fc.Customer.Add("e2eprobe-eph-young", "cust_y", nil, testStart.Add(-10*time.Minute))
	fc.Customer.Add("e2eprobe-cust-eph-legacy", "cust_l1", nil, old)
	fc.Customer.Add("tagged-legacy", "cust_l2", map[string]string{"e2eprobe_role": "ephemeral"}, old)
	fc.Customer.Add("acme", "cust_real", nil, old)
	named := fc.Customer.Items["acme"]
	named.Name = probetest.Ptr("Ephemeral Labs")
	fc.Customer.Items["acme"] = named
	fc.Customer.Add("e2eprobe-wallet-a", "cust_known", map[string]string{"e2eprobe_cohort": EphemeralCohort}, old)
	report := w.Sweep(context.Background(), testStart.Add(-time.Hour))
	if report.Orphans != 2 || len(report.Failures) != 0 {
		t.Fatalf("report = %+v", report)
	}
	for _, ext := range []string{"e2eprobe-eph-lifecycle-1", "renamed"} {
		if _, stillThere := fc.Customer.Items[ext]; stillThere {
			t.Errorf("%s must be deleted", ext)
		}
	}
	for _, ext := range []string{"e2eprobe-eph-young", "e2eprobe-cust-eph-legacy", "tagged-legacy", "acme", "e2eprobe-wallet-a"} {
		if _, kept := fc.Customer.Items[ext]; !kept {
			t.Errorf("%s must be kept: only this probe's own ephemerals are deleted", ext)
		}
	}
	if want := []string{"e2eprobe-cust-eph-legacy", "tagged-legacy"}; !reflect.DeepEqual(report.Legacy, want) {
		t.Errorf("legacy = %v, want the previous probe's leftovers %v", report.Legacy, want)
	}
}

func TestSweep_ListsLegacyPersistentCustomers(t *testing.T) {
	w, fc := newWorld(t, withPayer(suite.Default()))
	fc.Customer.Add("e2eprobe-cust-persistent-7", "cust_p7", map[string]string{"e2eprobe_cohort": PersistentCohort}, testStart.Add(-time.Hour))
	fc.Customer.Add("e2eprobe-cust-persistent-0", "cust_p0", map[string]string{"e2eprobe_cohort": PersistentCohort}, testStart.Add(-time.Hour))
	fc.Customer.Add("e2eprobe-cust-pay-stripe-cards", "cust_cards", map[string]string{"e2eprobe_cohort": PersistentCohort}, testStart.Add(-time.Hour))
	fc.Customer.Add("e2eprobe-cust-pay-stripe-expiry", "cust_expiry", map[string]string{"e2eprobe_cohort": PersistentCohort}, testStart.Add(-time.Hour))
	report := w.Sweep(context.Background(), testStart)
	if want := []string{"e2eprobe-cust-persistent-0", "e2eprobe-cust-persistent-7"}; !reflect.DeepEqual(report.Legacy, want) {
		t.Errorf("legacy = %v, want the unadopted persistent customers %v", report.Legacy, want)
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
	outage := probetest.ServerError(503)
	s := &stubSweeper{removed: 2, failed: []error{outage}}
	w.AddSweeper("stub", s)
	report := w.Sweep(context.Background(), testStart)
	if !s.called || len(report.Failures) != 1 || !errors.Is(report.Failures[0], outage) || !e2eprobe.IsInfra(report.Failures[0]) {
		t.Errorf("a sweeper's failure must keep its type so the janitor can tell an outage from a mismatch: report = %+v called=%v", report, s.called)
	}
}

type stubSweeper struct {
	called  bool
	removed int
	failed  []error
}

func (s *stubSweeper) Sweep(_ context.Context, _ time.Time) (int, []error) {
	s.called = true
	return s.removed, s.failed
}

func TestSweep_TaxCleanupSparesCustomerAssociations(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	ctx := context.Background()
	if err := w.Provision(ctx); err != nil {
		t.Fatal(err)
	}
	customer := types.TaxRateEntityTypeCustomer
	if _, err := fc.TaxAssoc.Create(ctx, types.CreateTaxAssociationRequest{TaxRateCode: w.Fixtures().TaxRateCode(), EntityID: probetest.Ptr("cust_live"), EntityType: &customer}); err != nil {
		t.Fatal(err)
	}
	report := w.Sweep(ctx, testStart)
	if report.TaxAssociations != 0 || len(fc.TaxAssoc.Deleted) != 0 {
		t.Fatalf("a customer-level association is never the probe's to delete: %+v deleted=%v", report, fc.TaxAssoc.Deleted)
	}
}

type countingSweeper struct{ runs int }

func (c *countingSweeper) Sweep(context.Context, time.Time) (int, []error) {
	c.runs++
	return 0, nil
}

func TestAddSweeper_IgnoresDuplicates(t *testing.T) {
	w, _ := newWorld(t, suite.Default())
	s := &countingSweeper{}
	w.AddSweeper("billing-matrix", s)
	w.AddSweeper("billing-matrix", &countingSweeper{})
	if w.Sweepers() != 1 {
		t.Fatalf("sweepers = %d, want the same sweeper registered once", w.Sweepers())
	}
	w.Sweep(context.Background(), testStart)
	if s.runs != 1 {
		t.Fatalf("runs = %d, want 1", s.runs)
	}
}
