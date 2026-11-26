package world

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestProvision_CreatesCustomerSubscriptionWalletByTraits(t *testing.T) {
	w, fc := newWorld(t, withPayer(suite.Default()))
	if err := w.Provision(context.Background()); err != nil {
		t.Fatalf("provision: %v", err)
	}
	walletA, _ := w.Persona("wallet-a")
	if walletA.CustomerID() == "" || walletA.SubscriptionID() == "" || walletA.WalletID() == "" {
		t.Fatalf("wallet-a ids: %q %q %q", walletA.CustomerID(), walletA.SubscriptionID(), walletA.WalletID())
	}
	cust := fc.Customer.Items["e2eprobe-wallet-a"]
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
	adoptLegacy(t, fc, "e2eprobe-couponed", types.SubscriptionResponse{})
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
	// The server lists associations with tax_rate_id only, never the nested rate.
	for i := range fc.TaxAssoc.Items {
		fc.TaxAssoc.Items[i].TaxRate = nil
	}
	fc.Clock = func() time.Time { return testStart.Add(time.Hour) }
	w = New(fc, suite.Default(), "run-test-restart", nil)
	w.SetClock(fc.Clock)
	if err := w.Provision(ctx); err != nil {
		t.Fatal(err)
	}
	taxed, _ := w.Persona("taxed")
	if len(fc.TaxAssoc.Created) != 1 || !taxed.TaxActiveFrom().Equal(testStart) {
		t.Errorf("after a restart: tax association created %d times, active from %s, want once from %s", len(fc.TaxAssoc.Created), taxed.TaxActiveFrom(), testStart)
	}
	if w.Fixtures().TaxRateID() != "taxrate_1" {
		t.Errorf("a restart must adopt the shared rate by code, got id %q", w.Fixtures().TaxRateID())
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

func TestProvision_TaxAssociationConflictIsAGap(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	fc.TaxAssoc.CreateErr = probetest.Conflict()
	if err := w.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	taxed, _ := w.Persona("taxed")
	if taxed.Gap(e2eprobe.TraitTax) == "" || !taxed.TaxActiveFrom().IsZero() {
		t.Errorf("an association the server rejects as a duplicate but does not list must be a gap, got gap=%q active=%s", taxed.Gap(e2eprobe.TraitTax), taxed.TaxActiveFrom())
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
	w, fc := newWorld(t, withPayer(suite.Default()))
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

// withPayer adds the Stripe payer persona the example suite carries; the
// embedded default has no gateway so a fresh region never pages on one.
func withPayer(s *suite.Suite) *suite.Suite {
	s.Personas = append(s.Personas, suite.Persona{
		Name:    "payer-stripe",
		Traits:  []e2eprobe.Trait{e2eprobe.TraitPayment},
		Payment: &suite.Payment{Provider: "stripe", Currency: "USD", FixedCustomer: "e2eprobe-cust-pay-stripe-cards", DeclineLast4: "0341"},
	})
	s.Tenant.Connections = []string{"stripe"}
	s.Scenarios = append(s.Scenarios,
		suite.Scenario{Name: "checkout-link", Every: suite.Duration{Duration: 30 * time.Minute}},
		suite.Scenario{Name: "payment-methods", Every: suite.Duration{Duration: time.Hour}},
		suite.Scenario{Name: "auto-charge", Every: suite.Duration{Duration: 6 * time.Hour}},
	)
	return s
}

func TestProvision_AdoptedCommitmentMismatchRecordsGap(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	adoptLegacy(t, fc, "e2eprobe-committed", types.SubscriptionResponse{CommitmentAmount: probetest.Ptr("3.00"), OverageFactor: probetest.Ptr("1.5"), EnableTrueUp: probetest.Ptr(true)})
	if err := w.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	committed, _ := w.Persona("committed")
	if gap := committed.Gap(e2eprobe.TraitCommitment); !strings.Contains(gap, "3.00") || !strings.Contains(gap, "5.00") {
		t.Fatalf("a different commitment is a gap naming both amounts, got %q", gap)
	}
}

func TestProvision_AdoptedCommitmentMatchesNumerically(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	adoptLegacy(t, fc, "e2eprobe-committed", types.SubscriptionResponse{CommitmentAmount: probetest.Ptr("5"), OverageFactor: probetest.Ptr("1.50"), EnableTrueUp: probetest.Ptr(true)})
	if err := w.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	committed, _ := w.Persona("committed")
	if gap := committed.Gap(e2eprobe.TraitCommitment); gap != "" {
		t.Fatalf("5 and 5.00 are the same commitment, got gap %q", gap)
	}
}

func TestProvision_ActivationFailureLeavesPersonaNotReady(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	fc.Subscription.CreateStatus = types.SubscriptionStatusDraft
	fc.Subscription.ActivateErr = probetest.ServerError(503)
	if err := w.Provision(context.Background()); err == nil || !strings.Contains(err.Error(), "activate") {
		t.Fatalf("a failed activation must surface, got %v", err)
	}
	baseline, _ := w.Persona("baseline-a")
	if baseline.Ready() {
		t.Fatal("a persona whose subscription is still a draft is not ready")
	}
	fc.Subscription.ActivateErr = nil
	if err := w.Provision(context.Background()); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if !baseline.Ready() {
		t.Fatal("the next pass activates and readies the persona")
	}
}

func TestProvision_UnknownSharedRateDoesNotAdoptForeignAssociation(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	ctx := context.Background()
	if err := w.Provision(ctx); err != nil {
		t.Fatal(err)
	}
	taxed, _ := w.Persona("taxed")
	// A restart after a duplicate-create left the shared rate id unknown, and the
	// persona's only association is on some other rate.
	subscription := types.TaxRateEntityTypeSubscription
	fc.TaxAssoc.Items = []types.TaxAssociationResponse{{ID: probetest.Ptr("ta_foreign"), EntityID: probetest.Ptr(taxed.SubscriptionID()), EntityType: &subscription, TaxRateID: probetest.Ptr("txr_foreign")}}
	w.Fixtures().setTaxRateID("")
	created := len(fc.TaxAssoc.Created)
	if err := w.Provision(ctx); err != nil {
		t.Fatal(err)
	}
	if got := w.Fixtures().TaxRateID(); got == "txr_foreign" {
		t.Fatal("an association on another rate must not become the shared rate")
	}
	if len(fc.TaxAssoc.Created) != created+1 || fc.TaxAssoc.Created[created].TaxRateCode != w.Fixtures().TaxRateCode() {
		t.Fatalf("the shared association must be created for the persona, got %+v", fc.TaxAssoc.Created[created:])
	}
}

func TestPrimarySubscription_SkipsUntaggedQuarterly(t *testing.T) {
	quarterly, monthly := types.BillingPeriodQuarterly, types.BillingPeriodMonthly
	items := []types.SubscriptionResponse{
		{ID: probetest.Ptr("sub_1"), BillingPeriod: &quarterly},
		{ID: probetest.Ptr("sub_2"), BillingPeriod: &monthly},
	}
	if got := primarySubscription(items, "taxed"); got == nil || *got.ID != "sub_2" {
		t.Fatalf("the monthly subscription is the primary one, got %+v", got)
	}
	if got := primarySubscription(items[:1], "taxed"); got != nil {
		t.Fatalf("a lone quarterly subscription is never primary, got %+v", got)
	}
}

func TestProvision_WalletFundingRetriesAfterFailure(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	fc.Wallet.TopUpErr = probetest.ServerError(500)
	if err := w.Provision(context.Background()); err == nil {
		t.Fatal("a failed initial top-up must surface")
	}
	walletA, _ := w.Persona("wallet-a")
	if walletA.Ready() {
		t.Fatal("an unfunded wallet persona is not ready")
	}
	fc.Wallet.TopUpErr = nil
	if err := w.Provision(context.Background()); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	funded := false
	for _, tu := range fc.Wallet.TopUps {
		if tu.WalletID == walletA.WalletID() && tu.Amount == "100.00" {
			funded = true
		}
	}
	if !funded || !walletA.Ready() || len(fc.Wallet.Created) != 2 {
		t.Fatalf("the next pass funds the adopted wallet without creating another: funded=%v ready=%v created=%d", funded, walletA.Ready(), len(fc.Wallet.Created))
	}
}
