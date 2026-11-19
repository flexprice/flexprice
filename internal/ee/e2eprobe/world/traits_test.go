package world

import (
	"context"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestProvision_CreatesCustomerSubscriptionWalletByTraits(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	if err := w.Provision(context.Background()); err != nil {
		t.Fatalf("provision: %v", err)
	}
	walletA, _ := w.Persona("wallet-a")
	if walletA.CustomerID() == "" || walletA.SubscriptionID() == "" || walletA.WalletID() == "" {
		t.Fatalf("wallet-a ids: %q %q %q", walletA.CustomerID(), walletA.SubscriptionID(), walletA.WalletID())
	}
	cust := fc.Customer.Items["e2eprobe-cust-persistent-2"]
	if cust.Metadata["e2eprobe_cohort"] != PersistentCohort || cust.Metadata["e2eprobe_persona"] != "wallet-a" {
		t.Errorf("customer metadata = %v", cust.Metadata)
	}
	if len(fc.Wallet.TopUps) == 0 || fc.Wallet.TopUps[0].Amount != "100.00" {
		t.Errorf("wallet-a must be topped up to its declared balance: %+v", fc.Wallet.TopUps)
	}
	sub := fc.Subscription.Items[walletA.SubscriptionID()]
	if sub.CommitmentAmount != nil || sub.Metadata["e2eprobe_cohort"] != PersistentCohort || sub.Metadata["e2eprobe_persona"] != "wallet-a" {
		t.Errorf("wallet-a subscription = %+v", sub)
	}

	committed, _ := w.Persona("committed")
	csub := fc.Subscription.Items[committed.SubscriptionID()]
	if csub.CommitmentAmount == nil || *csub.CommitmentAmount != "5.00" || csub.OverageFactor == nil || *csub.OverageFactor != "1.5" {
		t.Errorf("committed subscription must carry the declared commitment: %+v", csub)
	}
	if csub.EnableTrueUp == nil || !*csub.EnableTrueUp {
		t.Error("committed subscription must enable true-up, or the server never emits a true-up line")
	}
	if committed.Gap(e2eprobe.TraitCommitment) != "" {
		t.Errorf("fresh commitment must have no gap, got %q", committed.Gap(e2eprobe.TraitCommitment))
	}

	couponed, _ := w.Persona("couponed")
	found := false
	for _, ca := range fc.CouponAssoc.Items {
		if ca.SubscriptionID != nil && *ca.SubscriptionID == couponed.SubscriptionID() && ca.CouponID != nil && *ca.CouponID == w.Fixtures().CouponID() {
			found = true
		}
	}
	if !found {
		t.Error("couponed subscription must be created with the shared coupon")
	}
	payer, _ := w.Persona("payer-stripe")
	if payer.CustomerID() != "" {
		t.Error("a payment-only persona needs no customer of its own")
	}
}

func TestProvision_AlertsSetThresholdsOnNewWallet(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	if err := w.Provision(context.Background()); err != nil {
		t.Fatalf("provision: %v", err)
	}
	canary, _ := w.Persona("alert-canary")
	wallet := fc.Wallet.Items[canary.WalletID()]
	as := wallet.AlertSettings
	if as == nil || as.Info == nil || as.Warning == nil || as.Critical == nil || *as.Info.Threshold != "25" || *as.Warning.Threshold != "10" || *as.Critical.Threshold != "0" || as.AlertEnabled == nil || !*as.AlertEnabled {
		t.Fatalf("alert settings = %+v", as)
	}
	if *wallet.Balance != "30.00" {
		t.Errorf("canary balance = %s, want 30.00", *wallet.Balance)
	}
}

func TestProvision_AdoptedSubscriptionWithoutTrueUpRecordsGap(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	adoptLegacy(t, fc, "e2eprobe-committed", types.SubscriptionResponse{CommitmentAmount: probetest.Ptr("5.00"), OverageFactor: probetest.Ptr("1.5")})
	if err := w.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	committed, _ := w.Persona("committed")
	if !strings.Contains(committed.Gap(e2eprobe.TraitCommitment), "true-up") {
		t.Errorf("a commitment without true-up must be a gap, got %q", committed.Gap(e2eprobe.TraitCommitment))
	}
}

func TestProvision_AdoptedSubscriptionRecordsGaps(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	adoptLegacy(t, fc, "e2eprobe-committed", types.SubscriptionResponse{})
	adoptLegacy(t, fc, "e2eprobe-cust-persistent-1", types.SubscriptionResponse{})
	if err := w.Provision(context.Background()); err != nil {
		t.Fatalf("provision: %v", err)
	}
	committed, _ := w.Persona("committed")
	if !committed.Ready() || !strings.Contains(committed.Gap(e2eprobe.TraitCommitment), "predates") {
		t.Errorf("committed: ready=%v gap=%q", committed.Ready(), committed.Gap(e2eprobe.TraitCommitment))
	}
	couponed, _ := w.Persona("couponed")
	if !couponed.Ready() || !strings.Contains(couponed.Gap(e2eprobe.TraitCoupon), "predates") {
		t.Errorf("couponed: ready=%v gap=%q", couponed.Ready(), couponed.Gap(e2eprobe.TraitCoupon))
	}
	if len(fc.Subscription.Created) != 9 {
		t.Errorf("adopted personas must not get a second subscription: created %d, want 9", len(fc.Subscription.Created))
	}
	if gaps := committed.Gaps(); len(gaps) != 1 {
		t.Errorf("gaps = %v", gaps)
	}
}

func TestProvision_TaxAssociationCreatedOnce(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	ctx := context.Background()
	if err := w.Provision(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.Provision(ctx); err != nil {
		t.Fatal(err)
	}
	taxed, _ := w.Persona("taxed")
	if len(fc.TaxAssoc.Created) != 1 || taxed.TaxActiveFrom().IsZero() {
		t.Errorf("tax association created %d times, active from %s", len(fc.TaxAssoc.Created), taxed.TaxActiveFrom())
	}
	quarterly := 0
	for _, sub := range fc.Subscription.Items {
		if sub.Metadata["e2eprobe_cohort"] == MultiCadenceCohort {
			quarterly++
			if sub.BillingPeriod == nil || *sub.BillingPeriod != types.BillingPeriodQuarterly {
				t.Errorf("multi-cadence sub must be quarterly: %+v", sub)
			}
		}
	}
	if quarterly != 1 || taxed.MultiCadenceSubID() == "" {
		t.Errorf("quarterly subs = %d, id=%q", quarterly, taxed.MultiCadenceSubID())
	}
	if taxed.SubscriptionID() == taxed.MultiCadenceSubID() {
		t.Error("the primary subscription must not be the quarterly one")
	}
}

func TestProvision_BucketedPersonaSyncsMissingPrices(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	adoptLegacy(t, fc, "e2eprobe-bucketed", types.SubscriptionResponse{})
	if err := w.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fc.Plan.Synced) != 1 || fc.Plan.Synced[0] != w.Fixtures().PlanID() {
		t.Errorf("a subscription missing priced meters must trigger one plan price sync, got %v", fc.Plan.Synced)
	}
}

func TestProvision_PaymentPersonaGapWhenFixedCustomerMissing(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	if err := w.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	payer, _ := w.Persona("payer-stripe")
	if !payer.Ready() || payer.Gap(e2eprobe.TraitPayment) == "" {
		t.Errorf("payer: ready=%v gap=%q", payer.Ready(), payer.Gap(e2eprobe.TraitPayment))
	}
	fc.Customer.Add("e2eprobe-cust-pay-stripe-cards", "cust_cards", nil, testStart)
	if err := w.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	payer, _ = w.Persona("payer-stripe")
	if payer.Gap(e2eprobe.TraitPayment) != "" {
		t.Errorf("gap must clear once the fixed customer exists, got %q", payer.Gap(e2eprobe.TraitPayment))
	}
	_ = probetest.Ptr("")
}
