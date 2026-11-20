package scenarios

import (
	"context"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/billingmatrix"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
)

const (
	// billingMatrixEntry is the suite key; catalog names append the family to it.
	billingMatrixEntry     = "billing-matrix"
	billingMatrixPerRun    = 3
	billingMatrixDetailMax = 2000
)

// billingMatrix runs the next few scenarios of one billing-matrix family each
// run, so the family is covered in rotation. Every scenario uses its own
// ephemeral customer; the engine removes leftovers as a janitor sweeper.
type billingMatrix struct {
	family billingmatrix.Family

	once   sync.Once
	engine *billingmatrix.Engine
	perRun int

	mu      sync.Mutex
	skipped map[string]bool
}

func newBillingMatrix(family billingmatrix.Family) *billingMatrix {
	return &billingMatrix{family: family, skipped: map[string]bool{}}
}

func (s *billingMatrix) Name() string             { return billingMatrixEntry + "-" + string(s.family) }
func (*billingMatrix) Kind() e2eprobe.Kind        { return e2eprobe.KindFlow }
func (*billingMatrix) Requires() []e2eprobe.Trait { return nil }

func (s *billingMatrix) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	s.once.Do(func() { s.setup(sc) })
	results := s.engine.RunNext(ctx, s.family, s.perRun)
	for _, r := range results {
		switch {
		case r.Skipped != "":
			s.logSkip(ctx, sc, r)
		case r.Failed():
			sc.Truth(r.Name+": "+firstProblem(r), false)
		}
		if len(r.CleanupFailures) > 0 && sc.Logger() != nil {
			// Leftovers are retried by the janitor's orphan sweep, so they are logged, not paged.
			sc.Logger().Info(ctx, "billing matrix cleanup deferred to janitor", "scenario", r.Name,
				"failures", len(r.CleanupFailures), "first_failure", r.CleanupFailures[0])
		}
	}
	if failed, detail := billingmatrix.Summary(results); len(failed) > 0 {
		sc.Attr("failed_scenarios", strings.Join(failed, ","))
		sc.Attr("detail", clipDetail(detail))
	}
	return sc.Result("billing_matrix_" + string(s.family))
}

// setup builds the engine from the suite entry and hands it to the janitor, once per instance.
func (s *billingMatrix) setup(sc *scenario.Context) {
	cfg, _ := sc.Suite().ScenarioConfig(billingMatrixEntry)
	s.perRun = cfg.PerRun
	if s.perRun <= 0 {
		s.perRun = billingMatrixPerRun
	}
	s.engine = billingmatrix.NewEngine(sc.Client().Raw(), sc.RunID(), cfg.AssertKnownIssues)
	sc.World().AddSweeper(s.engine)
}

// logSkip reports a known-issue skip the first time each scenario hits it.
func (s *billingMatrix) logSkip(ctx context.Context, sc *scenario.Context, r billingmatrix.Result) {
	s.mu.Lock()
	seen := s.skipped[r.Name]
	s.skipped[r.Name] = true
	s.mu.Unlock()
	if seen || sc.Logger() == nil {
		return
	}
	sc.Logger().Info(ctx, "billing matrix scenario skipped: known issue", "scenario", r.Name, "known_issue", r.Skipped)
}

// firstProblem is the one line a failing scenario contributes to the mismatch list.
func firstProblem(r billingmatrix.Result) string {
	if r.Err != nil {
		return "error: " + e2eprobe.Brief(r.Err)
	}
	return r.Mismatches[0]
}

// clipDetail bounds the summary attribute, cutting on a rune boundary.
func clipDetail(detail string) string {
	n := billingMatrixDetailMax
	if len(detail) <= n {
		return detail
	}
	for n > 0 && !utf8.RuneStart(detail[n]) {
		n--
	}
	return detail[:n]
}

func registerBillingMatrix(cat *scenario.Catalog) {
	for _, f := range billingmatrix.Families {
		r := entry(billingMatrixEntry+"-"+string(f), e2eprobe.KindFlow, f.Interval(), func(scenario.Deps) scenario.Scenario { return newBillingMatrix(f) })
		cat.Register(r.info, r.build)
	}
}
