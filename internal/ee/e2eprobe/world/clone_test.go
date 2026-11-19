package world

import (
	"context"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
)

func TestClone_StampsTraitsAndRegisters(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	ctx := context.Background()
	if err := w.Provision(ctx); err != nil {
		t.Fatal(err)
	}
	taxed, _ := w.Persona("taxed")
	eph, err := w.Clone(ctx, taxed, "tax-preview")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(eph.ExternalID(), EphemeralPrefix+"tax-preview-") || eph.CustomerID() == "" || eph.SubscriptionID() == "" || eph.Scenario() != "tax-preview" {
		t.Fatalf("ephemeral = %+v", eph)
	}
	cust := fc.Customer.Items[eph.ExternalID()]
	if cust.Metadata["e2eprobe_cohort"] != EphemeralCohort || cust.Metadata["e2eprobe_persona"] != "taxed" || cust.Metadata["e2eprobe_scenario"] != "tax-preview" || cust.Metadata["e2eprobe_run_id"] != "run-test" {
		t.Errorf("clone metadata = %v", cust.Metadata)
	}
	taxAssoc := 0
	for _, ta := range fc.TaxAssoc.Items {
		if ta.EntityID != nil && *ta.EntityID == eph.SubscriptionID() {
			taxAssoc++
		}
	}
	if taxAssoc != 1 {
		t.Errorf("tax clone must carry one tax association, got %d", taxAssoc)
	}
	sub := fc.Subscription.Items[eph.SubscriptionID()]
	if sub.Metadata["e2eprobe_cohort"] != MultiCadenceCohort && sub.BillingPeriod != nil && *sub.BillingPeriod != "MONTHLY" {
		t.Errorf("clone subscription must be monthly, got %+v", sub)
	}
	if w.Ephemerals().Count("customer") != 1 || w.Ephemerals().Count("subscription") != 1 {
		t.Errorf("clone must register customer and subscription: %d/%d", w.Ephemerals().Count("customer"), w.Ephemerals().Count("subscription"))
	}

	committed, _ := w.Persona("committed")
	ceph, err := w.Clone(ctx, committed, "commitment-preview")
	if err != nil {
		t.Fatal(err)
	}
	if csub := fc.Subscription.Items[ceph.SubscriptionID()]; csub.CommitmentAmount == nil || *csub.CommitmentAmount != "5.00" {
		t.Errorf("committed clone must carry the commitment: %+v", csub)
	}
	couponed, _ := w.Persona("couponed")
	peph, err := w.Clone(ctx, couponed, "coupon-preview")
	if err != nil {
		t.Fatal(err)
	}
	attached := false
	for _, ca := range fc.CouponAssoc.Items {
		if ca.SubscriptionID != nil && *ca.SubscriptionID == peph.SubscriptionID() {
			attached = true
		}
	}
	if !attached {
		t.Error("couponed clone must carry the shared coupon")
	}
}
