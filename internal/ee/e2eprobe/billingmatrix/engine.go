package billingmatrix

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Family groups scenarios that share a flow; each family runs as its own probe check.
type Family string

const (
	FamilyOpening    Family = "opening"    // creation: opening invoice, periods, anchors, stubs
	FamilyChange     Family = "change"     // mid-period add / remove / modify / cancel
	FamilyGrants     Family = "grants"     // credit and entitlement grant proration
	FamilyRenewal    Family = "renewal"    // backdated subs: grid dates and renewals over many periods
	FamilyValidation Family = "validation" // requests the spec rejects
)

// Families lists every family in the order checks are registered.
var Families = []Family{FamilyOpening, FamilyChange, FamilyGrants, FamilyRenewal, FamilyValidation}

// Interval is how often a family's check runs.
func (f Family) Interval() time.Duration {
	switch f {
	case FamilyOpening, FamilyChange:
		return 10 * time.Minute
	case FamilyValidation:
		return 30 * time.Minute
	}
	return 15 * time.Minute
}

// scenario is one live billing case: provision, act, compare against the oracle.
type scenario struct {
	family Family
	name   string
	// knownIssue names a product bug the scenario hits; it is skipped unless known issues are asserted.
	knownIssue string
	run        func(ctx context.Context, env *env) error
}

// Result is the outcome of one scenario run.
type Result struct {
	Name       string
	Skipped    string // known issue, when skipped
	Err        error
	Mismatches []string
	// CleanupFailures are resources the scenario could not remove; the orphan sweep retries them.
	CleanupFailures []string
}

func (r Result) Failed() bool { return r.Err != nil || len(r.Mismatches) > 0 }

// Engine runs scenarios of every family against one API.
type Engine struct {
	api               API
	runID             string
	assertKnownIssues bool

	mu      sync.Mutex
	cursors map[Family]int
	catalog map[Family][]scenario
	fixture *fixtures
}

// NewEngine builds the scenario catalog for api. Known-issue scenarios run only when assertKnownIssues is set.
func NewEngine(api API, runID string, assertKnownIssues bool) *Engine {
	e := &Engine{
		api:               api,
		runID:             runID,
		assertKnownIssues: assertKnownIssues,
		cursors:           map[Family]int{},
		catalog:           map[Family][]scenario{},
		fixture:           newFixtures(api, runID),
	}
	for _, s := range buildCatalog() {
		e.catalog[s.family] = append(e.catalog[s.family], s)
	}
	for f := range e.catalog {
		sort.SliceStable(e.catalog[f], func(i, j int) bool { return e.catalog[f][i].name < e.catalog[f][j].name })
	}
	return e
}

// Names lists every scenario of a family.
func (e *Engine) Names(f Family) []string {
	out := make([]string, 0, len(e.catalog[f]))
	for _, s := range e.catalog[f] {
		out = append(out, s.name)
	}
	return out
}

// RunNext runs the next n scenarios of a family, rotating so every scenario runs in turn.
func (e *Engine) RunNext(ctx context.Context, f Family, n int) []Result {
	e.mu.Lock()
	all := e.catalog[f]
	start := e.cursors[f]
	if len(all) > 0 {
		e.cursors[f] = (start + n) % len(all)
	}
	e.mu.Unlock()

	picked := make([]scenario, 0, n)
	for i := 0; i < n && i < len(all); i++ {
		picked = append(picked, all[(start+i)%len(all)])
	}
	return e.run(ctx, picked)
}

// RunAll runs every scenario of a family whose name contains filter.
func (e *Engine) RunAll(ctx context.Context, f Family, filter string) []Result {
	picked := make([]scenario, 0, len(e.catalog[f]))
	for _, s := range e.catalog[f] {
		if strings.Contains(s.name, filter) {
			picked = append(picked, s)
		}
	}
	return e.run(ctx, picked)
}

func (e *Engine) run(ctx context.Context, picked []scenario) []Result {
	out := make([]Result, 0, len(picked))
	for _, s := range picked {
		if ctx.Err() != nil {
			break
		}
		if s.knownIssue != "" && !e.assertKnownIssues {
			out = append(out, Result{Name: s.name, Skipped: s.knownIssue})
			continue
		}
		env := &env{api: e.api, fx: e.fixture, runID: e.runID, scenario: s.name, track: &tracker{}}
		err := s.run(ctx, env)
		cleanupFailures := client{api: e.api}.cleanup(context.WithoutCancel(ctx), env.track)
		out = append(out, Result{Name: s.name, Err: err, Mismatches: env.mismatches, CleanupFailures: cleanupFailures})
	}
	return out
}

// Summary renders failures for an alert: one line per failing scenario with its first mismatches.
func Summary(results []Result) (failed []string, detail string) {
	var b strings.Builder
	for _, r := range results {
		if !r.Failed() {
			continue
		}
		failed = append(failed, r.Name)
		b.WriteString(r.Name + ": ")
		if r.Err != nil {
			b.WriteString(fmt.Sprintf("error: %v", r.Err))
		} else {
			shown := r.Mismatches
			if len(shown) > 3 {
				shown = append(shown[:3:3], fmt.Sprintf("… %d more", len(r.Mismatches)-3))
			}
			b.WriteString(strings.Join(shown, "; "))
		}
		b.WriteString("\n")
	}
	return failed, b.String()
}
