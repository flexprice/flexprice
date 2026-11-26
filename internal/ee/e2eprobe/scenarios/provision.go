package scenarios

import (
	"context"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
)

// provision re-runs world provisioning so fixtures and personas heal over time.
type provision struct{}

func newProvision(scenario.Deps) *provision { return &provision{} }

func (*provision) Name() string               { return "provision" }
func (*provision) Kind() e2eprobe.Kind        { return e2eprobe.KindMaintenance }
func (*provision) Requires() []e2eprobe.Trait { return nil }

func (*provision) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	if err := sc.World().Provision(ctx); err != nil {
		return sc.Fail("provision", err)
	}
	return sc.Result("provision")
}
