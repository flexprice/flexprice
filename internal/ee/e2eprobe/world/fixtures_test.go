package world

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
)

type counts struct{ features, plans, prices, entitlements, grants, coupons, taxRates, customers, subs, wallets, taxAssocs int }

func count(fc *probetest.Client) counts {
	return counts{
		features:     len(fc.Feature.Created),
		plans:        len(fc.Plan.Created),
		prices:       len(fc.Price.Created),
		entitlements: len(fc.Entitlement.Created),
		grants:       len(fc.Entitlement.CreatedWithGrant),
		coupons:      len(fc.Coupon.Created),
		taxRates:     len(fc.TaxRate.Created),
		customers:    len(fc.Customer.Created),
		subs:         len(fc.Subscription.Created),
		wallets:      len(fc.Wallet.Created),
		taxAssocs:    len(fc.TaxAssoc.Created),
	}
}

func TestFixtures_ProvisionIsIdempotent(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	ctx := context.Background()
	if err := w.Provision(ctx); err != nil {
		t.Fatalf("provision: %v", err)
	}
	fx := w.Fixtures()
	if !fx.Ready() || fx.PlanID() == "" || fx.TaxRateCode() != e2eprobe.SharedTaxRateCode || fx.TaxRateID() == "" || fx.CouponID() == "" || fx.CouponCode() != e2eprobe.SharedCouponCode {
		t.Fatalf("fixtures incomplete: plan=%q tax=%q/%q coupon=%q/%q", fx.PlanID(), fx.TaxRateID(), fx.TaxRateCode(), fx.CouponID(), fx.CouponCode())
	}
	for _, m := range e2eprobe.Meters() {
		if fx.MeterID(m.Key) == "" || fx.FeatureID(m.Key) == "" || fx.PriceID(m.Key) == "" {
			t.Errorf("meter %s: meter=%q feature=%q price=%q", m.Key, fx.MeterID(m.Key), fx.FeatureID(m.Key), fx.PriceID(m.Key))
		}
	}
	if fx.SoftLimitEntitlementID("count") == "" || fx.SoftLimitEntitlementID("sum_commit") != "" || fx.SoftLimitEntitlementID("sum_multiplier") != "" || fx.GrantEntitlementID() == "" {
		t.Errorf("entitlements: count=%q sum_commit=%q sum_multiplier=%q grant=%q", fx.SoftLimitEntitlementID("count"), fx.SoftLimitEntitlementID("sum_commit"), fx.SoftLimitEntitlementID("sum_multiplier"), fx.GrantEntitlementID())
	}
	first := count(fc)
	want := counts{features: 14, plans: 1, prices: 15, entitlements: 6, grants: 1, coupons: 1, taxRates: 1, customers: 10, subs: 11, wallets: 2, taxAssocs: 1}
	if first != want {
		t.Fatalf("first provision created %+v, want %+v", first, want)
	}
	if err := w.Provision(ctx); err != nil {
		t.Fatalf("second provision: %v", err)
	}
	if second := count(fc); second != first {
		t.Errorf("second provision must create nothing: %+v vs %+v", second, first)
	}
}

func TestFixtures_TaxRateAdoptedByCode(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	fc.TaxRate.ByCode[e2eprobe.SharedTaxRateCode] = "taxrate_existing"
	if err := w.Provision(context.Background()); err != nil {
		t.Fatalf("provision: %v", err)
	}
	fx := w.Fixtures()
	if fx.TaxRateCode() != e2eprobe.SharedTaxRateCode {
		t.Error("tax rate code must be known even when the rate pre-exists")
	}
	if fx.TaxRateID() != "taxrate_existing" {
		t.Errorf("tax rate id must be adopted from the listed rate, got %q", fx.TaxRateID())
	}
	if len(fc.TaxRate.Created) != 0 {
		t.Error("an existing tax rate must not be re-created")
	}
}

func TestFixtures_GrantFailureIsNotFatal(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	fc.Entitlement.CreateGrantErr = probetest.ServerError(500)
	if err := w.Provision(context.Background()); err != nil {
		t.Fatalf("grant provisioning must not fail the world: %v", err)
	}
	if w.Fixtures().GrantEntitlementID() != "" {
		t.Error("grant id must stay empty when the grant was not created")
	}
	granted, _ := w.Persona("granted")
	if granted.Gap(e2eprobe.TraitAdditiveGrant) == "" {
		t.Error("granted persona must record the missing grant as a gap")
	}
}

func TestFixtures_IndexesAdoptedPriceWithoutLookupKey(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	ctx := context.Background()
	if err := w.Provision(ctx); err != nil {
		t.Fatal(err)
	}
	want := w.Fixtures().PriceID("sum_commit")
	for i := range fc.Price.Items {
		if fc.Price.Items[i].ID != nil && *fc.Price.Items[i].ID == want {
			fc.Price.Items[i].LookupKey = nil // a price created by hand carries no lookup key
		}
	}
	created := len(fc.Price.Created)
	again := New(fc, suite.Default(), "run-2", nil)
	if err := again.Provision(ctx); err != nil {
		t.Fatal(err)
	}
	if got := again.Fixtures().PriceID("sum_commit"); got != want {
		t.Fatalf("sum_commit price = %q, want the adopted %q", got, want)
	}
	if len(fc.Price.Created) != created {
		t.Error("an adopted price must not be created again")
	}
}
