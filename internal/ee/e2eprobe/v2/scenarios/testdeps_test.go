package scenarios

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/inbox"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/ledger"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/suite"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/world"
	"github.com/flexprice/flexprice/internal/logger"
)

var testNow = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

// testDeps wraps the fake world so a test can move the clock and provision.
type testDeps struct {
	scenario.Deps
	fc  *probetest.Client
	now time.Time
}

func (d *testDeps) advance(by time.Duration) { d.now = d.now.Add(by) }

// newTestDeps builds dependencies around a fake world provisioned from the
// default suite, with a controllable clock shared by the fake and the probe.
func newTestDeps(t *testing.T) *testDeps {
	t.Helper()
	d := &testDeps{fc: probetest.New(), now: testNow}
	d.fc.Clock = func() time.Time { return d.now }
	s := withPayer(suite.Default())
	w := world.New(d.fc, s, "run-test", nil)
	w.SetClock(d.fc.Clock)
	if err := w.Provision(context.Background()); err != nil {
		t.Fatalf("provision: %v", err)
	}
	lg := ledger.NewLedger(48 * time.Hour)
	lg.SetClock(func() time.Time { return d.now })
	d.Deps = scenario.Deps{
		Client:    d.fc,
		World:     w,
		Ledger:    lg,
		Inbox:     inbox.New(inbox.Options{}),
		Suite:     s,
		Logger:    logger.NewNoopLogger(),
		RunID:     "run-test",
		Tick:      10 * time.Minute,
		Now:       func() time.Time { return d.now },
		StartedAt: testNow,
	}
	return d
}

// run executes a scenario once against a persona ("" for tenant-level).
func run(t *testing.T, d *testDeps, s scenario.Scenario, persona string) e2eprobe.Result {
	t.Helper()
	var p *world.Persona
	if persona != "" {
		var ok bool
		if p, ok = d.World.Persona(persona); !ok {
			t.Fatalf("no persona %s", persona)
		}
	}
	return s.Run(context.Background(), scenario.NewContext(d.Deps, p, s.Name()))
}

func TestRegister_CoversDefaultSuite(t *testing.T) {
	cat := scenario.NewCatalog()
	Register(cat)
	for _, name := range []string{"provision", "preflight", "janitor", "webhook-wiring", "ingest", "usage", "wallet-debit", "low-balance-alert", "invoice-freshness", "invoice-tax", "invoice-coupon", "invoice-commitment", "invoice-multi-cadence", "entitlement-usage", "entitlement-enforcement", "grant-inheritance", "lifecycle", "cancel", "line-item-change", "tax-preview", "coupon-preview", "commitment-preview", "checkout-link", "payment-methods", "auto-charge", "billing-matrix-opening", "billing-matrix-validation"} {
		if _, ok := cat.Scenario(name); !ok {
			t.Errorf("%s not registered", name)
		}
	}
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
