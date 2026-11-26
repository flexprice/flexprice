// Package scenarios holds every scenario the probe runs, one file each, and
// registers them into the catalog.
package scenarios

import (
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
)

type registration struct {
	info  suite.ScenarioInfo
	build scenario.Builder
}

func entry(name string, kind e2eprobe.Kind, every time.Duration, build scenario.Builder, traits ...e2eprobe.Trait) registration {
	return registration{info: suite.ScenarioInfo{Name: name, Kind: kind, Traits: traits, Every: every}, build: build}
}

// ingests declares how many events one run sends, so the suite's daily budget
// counts flow traffic as well as profile batches.
func (r registration) ingests(events int) registration {
	r.info.EventsPerRun = events
	return r
}

// checks is the one-line statement of what a run verifies; describe prints it
// next to the personas it runs on, so the suite file and the catalog together
// are the whole picture of what is covered.
func (r registration) checks(summary string) registration {
	r.info.Summary = summary
	return r
}

// covers names the product areas a run exercises; describe groups coverage by
// them and says which areas the suite leaves uncovered.
func (r registration) covers(areas ...e2eprobe.Area) registration {
	r.info.Areas = areas
	return r
}

// Register adds every scenario in this package to cat.
func Register(cat *scenario.Catalog) {
	all := []registration{
		entry("provision", e2eprobe.KindMaintenance, 6*time.Hour, func(d scenario.Deps) scenario.Scenario { return newProvision(d) }).covers(e2eprobe.AreaOperations).checks("the tenant matches the suite: shared fixtures and every persona's traits, with gaps recorded for what cannot be retrofitted"),
		entry("preflight", e2eprobe.KindPreflight, 6*time.Hour, func(d scenario.Deps) scenario.Scenario { return newPreflight(d) }).covers(e2eprobe.AreaOperations).checks("the expected connections and settings exist and the fixtures are complete; persona gaps are reported as attributes"),
		entry("janitor", e2eprobe.KindMaintenance, time.Hour, func(d scenario.Deps) scenario.Scenario { return newJanitor(d) }).covers(e2eprobe.AreaOperations).checks("ephemerals older than janitor.max_age are removed; the previous generation's customers are listed, never deleted"),
		entry("webhook-wiring", e2eprobe.KindProbe, 15*time.Minute, func(d scenario.Deps) scenario.Scenario { return newWebhookWiring(d) }).covers(e2eprobe.AreaWebhooks).checks("webhooks of some kind keep reaching the inbox while clones are created; silence for webhooks.silence_after is one incident"),
		entry("ingest", e2eprobe.KindProbe, 0, func(d scenario.Deps) scenario.Scenario { return newIngest(d) }, e2eprobe.TraitEvents).covers(e2eprobe.AreaUsage).checks("one bulk batch per persona lands every tick, with up to six missed ticks backfilled"),
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
