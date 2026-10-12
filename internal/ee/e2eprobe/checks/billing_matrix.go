package checks

import (
	"context"
	"fmt"
	"strings"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/billingmatrix"
	"github.com/flexprice/flexprice/internal/logger"
)

// billingMatrixProbe runs the next few scenarios of one billing-matrix family each tick, so the
// whole family is covered in rotation. Every scenario uses its own ephemeral customer, which the
// janitor's orphan sweep removes by external-ID prefix.
type billingMatrixProbe struct {
	engine  *billingmatrix.Engine
	family  billingmatrix.Family
	perRun  int
	lg      *logger.Logger
	skipped map[string]bool
}

// NewBillingMatrixProbe builds the check for one family; engine is shared across families.
func NewBillingMatrixProbe(engine *billingmatrix.Engine, family billingmatrix.Family, perRun int, lg *logger.Logger) *billingMatrixProbe {
	return &billingMatrixProbe{engine: engine, family: family, perRun: max(perRun, 1), lg: lg, skipped: map[string]bool{}}
}

func (p *billingMatrixProbe) Name() string        { return "billing-matrix-" + string(p.family) }
func (p *billingMatrixProbe) Kind() e2eprobe.Kind { return e2eprobe.KindScenario }

func (p *billingMatrixProbe) Run(ctx context.Context) error {
	results := p.engine.RunNext(ctx, p.family, p.perRun)
	for _, r := range results {
		if r.Skipped != "" && !p.skipped[r.Name] && p.lg != nil {
			p.skipped[r.Name] = true
			p.lg.Info(ctx, "billing matrix scenario skipped: known issue", "scenario", r.Name, "known_issue", r.Skipped)
		}
		// Leftovers are retried by the janitor's orphan sweep, so they are logged, not paged.
		if len(r.CleanupFailures) > 0 && p.lg != nil {
			p.lg.Info(ctx, "billing matrix cleanup deferred to janitor", "scenario", r.Name,
				"failures", len(r.CleanupFailures), "first_failure", r.CleanupFailures[0])
		}
	}
	failed, detail := billingmatrix.Summary(results)
	if len(failed) == 0 {
		return nil
	}
	return e2eprobe.Errorf(map[string]string{
		"step":             "billing_matrix_" + string(p.family),
		"failed_scenarios": strings.Join(failed, ","),
	}, "%d of %d %s scenarios failed:\n%s", len(failed), len(results), p.family, detail)
}

// String names the family and its size, for startup logs.
func (p *billingMatrixProbe) String() string {
	return fmt.Sprintf("%s (%d scenarios, %d per run)", p.Name(), len(p.engine.Names(p.family)), p.perRun)
}
