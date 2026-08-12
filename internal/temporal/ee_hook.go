package temporal

import (
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/types"
)

// EEContributor supplies workflows and activities that live in the ee/
// submodule. Community builds have no contributors registered, so every
// contribution point is a no-op and the ee/ tree is never referenced.
//
// Contributors register themselves from an init() guarded by the `ee` build
// tag, which runs before RegisterWorkflowsAndActivities is called. Returning a
// zero WorkerConfig means "nothing to add for this queue".
type EEContributor func(params service.ServiceParams, taskQueue types.TemporalTaskQueue) WorkerConfig

var eeContributors []EEContributor

// RegisterEEContributor is called from ee-tagged init() functions.
func RegisterEEContributor(c EEContributor) {
	eeContributors = append(eeContributors, c)
}

// applyEEContributions merges every EE contribution for a task queue into the
// config the core builder produced. Returns cfg untouched in a community build.
func applyEEContributions(cfg WorkerConfig, params service.ServiceParams, taskQueue types.TemporalTaskQueue) WorkerConfig {
	for _, contribute := range eeContributors {
		extra := contribute(params, taskQueue)
		cfg.Workflows = append(cfg.Workflows, extra.Workflows...)
		cfg.Activities = append(cfg.Activities, extra.Activities...)
	}
	return cfg
}

// EEContributorCount reports how many enterprise contributors are registered. It
// exists so a build-level test can assert that an `-tags ee` binary actually
// reached the ee/ init() functions — a build succeeding does not prove the
// import chain is wired.
func EEContributorCount() int { return len(eeContributors) }
