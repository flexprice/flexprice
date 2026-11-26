package scenarios

import (
	"context"
	"strconv"
	"strings"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
)

// janitor removes ephemerals past the suite's max age. A delete the tenant
// refuses is a mismatch; an outage during the sweep is an infrastructure failure
// and joins the rollup instead of paging on its own.
type janitor struct{}

func newJanitor(scenario.Deps) *janitor { return &janitor{} }

func (*janitor) Name() string               { return "janitor" }
func (*janitor) Kind() e2eprobe.Kind        { return e2eprobe.KindMaintenance }
func (*janitor) Requires() []e2eprobe.Trait { return nil }

func (*janitor) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	cutoff := sc.Now().Add(-sc.Suite().Defaults.Janitor.MaxAge.Duration)
	report := sc.World().Sweep(ctx, cutoff)
	sc.Attr("archived", strconv.Itoa(report.Archived))
	sc.Attr("orphans", strconv.Itoa(report.Orphans))
	sc.Attr("tax_associations", strconv.Itoa(report.TaxAssociations))
	sc.Attr("swept", strconv.Itoa(report.Swept))
	if len(report.Legacy) > 0 {
		sc.Attr("legacy_fixtures", strings.Join(report.Legacy, ","))
	}
	for _, failure := range report.Failures {
		if e2eprobe.IsInfra(failure) {
			return sc.Fail("sweep", failure)
		}
		sc.Truth(failure.Error(), false)
	}
	return sc.Result("sweep")
}
