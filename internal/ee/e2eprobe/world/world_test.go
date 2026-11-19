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

var testStart = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

func newWorld(t *testing.T, s *suite.Suite) (*World, *probetest.Client) {
	t.Helper()
	fc := probetest.New()
	fc.Clock = func() time.Time { return testStart }
	w := New(fc, s, "run-test", nil)
	w.SetClock(func() time.Time { return fc.Clock() })
	return w, fc
}

// adoptLegacy seeds the probe plan, a customer and an existing subscription on
// it, the way a previous probe generation left them.
func adoptLegacy(t *testing.T, fc *probetest.Client, ext string, sub types.SubscriptionResponse) string {
	t.Helper()
	ctx := context.Background()
	planID := ""
	if resp, _ := fc.Plan.Query(ctx, types.PlanFilter{LookupKey: probetest.Ptr(e2eprobe.PlanLookupKey)}); len(resp.ListPlansResponse.Items) > 0 {
		planID = *resp.ListPlansResponse.Items[0].ID
	} else {
		created, _ := fc.Plan.Create(ctx, types.CreatePlanRequest{Name: "legacy", LookupKey: probetest.Ptr(e2eprobe.PlanLookupKey)})
		planID = *created.PlanResponse.ID
	}
	fc.Customer.Add(ext, "cust_legacy_"+ext, map[string]string{"e2eprobe_cohort": PersistentCohort}, testStart.Add(-90*24*time.Hour))
	sub.PlanID = probetest.Ptr(planID)
	if sub.Metadata == nil {
		sub.Metadata = map[string]string{"e2eprobe_cohort": PersistentCohort}
	}
	if sub.SubscriptionStatus == nil {
		sub.SubscriptionStatus = types.SubscriptionStatusActive.ToPointer()
	}
	fc.Subscription.Add(ext, sub)
	return planID
}

func TestWorld_PersonasReadyAfterProvision(t *testing.T) {
	w, _ := newWorld(t, suite.Default())
	if err := w.Provision(context.Background()); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if !w.Ready() {
		t.Fatal("fixtures must be ready")
	}
	personas := w.Personas()
	if len(personas) != 11 {
		t.Fatalf("personas = %d, want 11", len(personas))
	}
	for _, p := range personas {
		if !p.Ready() {
			t.Errorf("persona %s not ready: gaps=%v", p.Name(), p.Gaps())
		}
	}
	taxed, ok := w.Persona("taxed")
	if !ok || taxed.ExternalID() != "e2eprobe-cust-persistent-0" || taxed.CustomerID() == "" || taxed.SubscriptionID() == "" || taxed.MultiCadenceSubID() == "" {
		t.Errorf("taxed persona = %+v", taxed)
	}
	if _, ok := w.Persona("ghost"); ok {
		t.Error("unknown persona must not resolve")
	}
}

func TestWorld_ProvisionPartialFailureKeepsOthers(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	fc.Wallet.CreateErr = probetest.ServerError(500)
	err := w.Provision(context.Background())
	if err == nil || !strings.Contains(err.Error(), "wallet-a") {
		t.Fatalf("expected an error naming wallet-a, got %v", err)
	}
	if !w.Ready() {
		t.Error("fixtures must still be ready")
	}
	walletA, _ := w.Persona("wallet-a")
	if walletA.Ready() {
		t.Error("wallet-a must not be ready without its wallet")
	}
	taxed, _ := w.Persona("taxed")
	if !taxed.Ready() {
		t.Error("taxed must be ready despite another persona failing")
	}
}

func TestWorld_ReprovisionErrorKeepsReadyPersonas(t *testing.T) {
	w, fc := newWorld(t, suite.Default())
	if err := w.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc.Customer.GetErr = probetest.ServerError(503)
	if err := w.Provision(context.Background()); err == nil {
		t.Fatal("a failing pass must be reported")
	}
	for _, p := range w.Personas() {
		if !p.Ready() {
			t.Errorf("persona %s lost readiness on a transient error", p.Name())
		}
	}
}
