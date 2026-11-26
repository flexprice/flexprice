package scenario

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/inbox"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/ledger"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/logger"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/world"
)

var testNow = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

// newDeps builds dependencies around a provisioned fake world.
func newDeps(t *testing.T, provision bool) (Deps, *probetest.Client) {
	t.Helper()
	fc := probetest.New()
	fc.Clock = func() time.Time { return testNow }
	s := suite.Default()
	w := world.New(fc, s, "run-test", nil)
	w.SetClock(fc.Clock)
	if provision {
		if err := w.Provision(context.Background()); err != nil {
			t.Fatalf("provision: %v", err)
		}
	}
	return Deps{
		Client:    fc,
		World:     w,
		Ledger:    ledger.NewLedger(48 * time.Hour),
		Inbox:     inbox.New(inbox.Options{}),
		Suite:     s,
		Logger:    logger.Noop(),
		RunID:     "run-test",
		Tick:      10 * time.Minute,
		Now:       func() time.Time { return testNow },
		StartedAt: testNow,
	}, fc
}

// stub is a scenario whose behaviour the test supplies.
type stub struct {
	name   string
	kind   e2eprobe.Kind
	traits []e2eprobe.Trait
	run    func(ctx context.Context, sc *Context) e2eprobe.Result
}

func (s *stub) Name() string               { return s.name }
func (s *stub) Kind() e2eprobe.Kind        { return s.kind }
func (s *stub) Requires() []e2eprobe.Trait { return s.traits }
func (s *stub) Run(ctx context.Context, sc *Context) e2eprobe.Result {
	return s.run(ctx, sc)
}

func registerStub(cat *Catalog, name string, traits []e2eprobe.Trait, every time.Duration, run func(ctx context.Context, sc *Context) e2eprobe.Result) {
	cat.Register(suite.ScenarioInfo{Name: name, Kind: e2eprobe.KindProbe, Traits: traits, Every: every}, func(Deps) Scenario {
		return &stub{name: name, kind: e2eprobe.KindProbe, traits: traits, run: run}
	})
}
