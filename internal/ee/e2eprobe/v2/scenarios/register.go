// Package scenarios holds every scenario the probe runs, one file each, and
// registers them into the catalog.
package scenarios

import (
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/suite"
)

type registration struct {
	info  suite.ScenarioInfo
	build scenario.Builder
}

func entry(name string, kind e2eprobe.Kind, every time.Duration, build scenario.Builder, traits ...e2eprobe.Trait) registration {
	return registration{info: suite.ScenarioInfo{Name: name, Kind: kind, Traits: traits, Every: every}, build: build}
}

// Register adds every scenario in this package to cat.
func Register(cat *scenario.Catalog) {
	all := []registration{
		entry("provision", e2eprobe.KindMaintenance, 6*time.Hour, func(d scenario.Deps) scenario.Scenario { return newProvision(d) }),
		entry("preflight", e2eprobe.KindPreflight, 6*time.Hour, func(d scenario.Deps) scenario.Scenario { return newPreflight(d) }),
		entry("janitor", e2eprobe.KindMaintenance, time.Hour, func(d scenario.Deps) scenario.Scenario { return newJanitor(d) }),
		entry("webhook-wiring", e2eprobe.KindProbe, 15*time.Minute, func(d scenario.Deps) scenario.Scenario { return newWebhookWiring(d) }),
		entry("ingest", e2eprobe.KindProbe, 0, func(d scenario.Deps) scenario.Scenario { return newIngest(d) }, e2eprobe.TraitEvents),
	}
	for _, r := range all {
		cat.Register(r.info, r.build)
	}
	registerUsage(cat)
	registerWallet(cat)
	registerInvoices(cat)
	registerEntitlements(cat)
	registerFlows(cat)
	registerPayments(cat)
	registerBillingMatrix(cat)
}
