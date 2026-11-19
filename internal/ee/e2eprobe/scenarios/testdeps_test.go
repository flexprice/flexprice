package scenarios

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/inbox"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/ledger"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/world"
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
	s := suite.Default()
	w := world.New(d.fc, s, "run-test", nil)
	w.SetClock(d.fc.Clock)
	if err := w.Provision(context.Background()); err != nil {
		t.Fatalf("provision: %v", err)
	}
	d.Deps = scenario.Deps{
		Client:    d.fc,
		World:     w,
		Ledger:    ledger.NewLedger(48 * time.Hour),
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
	for _, name := range []string{"provision", "preflight", "janitor", "webhook-wiring", "ingest", "usage", "wallet-debit", "low-balance-alert", "billing-matrix-opening", "billing-matrix-validation"} {
		if _, ok := cat.Scenario(name); !ok {
			t.Errorf("%s not registered", name)
		}
	}
}
