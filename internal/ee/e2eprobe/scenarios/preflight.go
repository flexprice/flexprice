package scenarios

import (
	"context"
	"fmt"
	"sort"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/world"
)

// preflight compares the tenant with what the suite expects of it: connected
// providers, setting values and provisioned fixtures. Persona gaps ride along
// as attributes so the digest shows them.
type preflight struct{}

func newPreflight(scenario.Deps) *preflight { return &preflight{} }

func (*preflight) Name() string               { return "preflight" }
func (*preflight) Kind() e2eprobe.Kind        { return e2eprobe.KindPreflight }
func (*preflight) Requires() []e2eprobe.Trait { return nil }

func (*preflight) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	conns, err := sc.Client().Tenant().ListConnections(ctx)
	if err != nil {
		return sc.Fail("connections", err)
	}
	have := map[string]bool{}
	for _, c := range conns {
		have[c.ProviderType] = true
	}
	for _, want := range sc.Suite().Tenant.Connections {
		sc.Truth(fmt.Sprintf("connection %s present", want), have[want])
	}

	keys := make([]string, 0, len(sc.Suite().Tenant.Settings))
	for key := range sc.Suite().Tenant.Settings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		got, err := sc.Client().Tenant().GetSetting(ctx, key)
		switch {
		case world.IsNotFound(err):
			sc.Truth(fmt.Sprintf("setting %s exists", key), false)
			continue
		case err != nil:
			return sc.Fail("settings", err)
		}
		want := sc.Suite().Tenant.Settings[key]
		fields := make([]string, 0, len(want))
		for field := range want {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		for _, field := range fields {
			sc.Truth(fmt.Sprintf("setting %s.%s == %v (got %v)", key, field, want[field], got[field]), fmt.Sprint(got[field]) == fmt.Sprint(want[field]))
		}
	}

	sc.Truth("fixtures provisioned", sc.World().Ready())
	for _, p := range sc.World().Personas() {
		for trait, reason := range p.Gaps() {
			sc.Attr("gap."+p.Name()+"."+string(trait), reason)
		}
	}
	return sc.Result("preflight")
}
