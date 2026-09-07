package temporal

import (
	"fmt"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/types"
)

// EEContributor supplies workflows and activities that live in the ee/
// directory. Community builds register none, so every contribution point is a
// no-op and the ee/ tree is never referenced.
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
		if len(extra.Workflows) == 0 && len(extra.Activities) == 0 {
			continue
		}
		if extra.TaskQueue != "" && extra.TaskQueue != taskQueue {
			panic(fmt.Sprintf(
				"ee contribution targets task queue %q while building %q; "+
					"contributors must return an empty WorkerConfig for queues they do not extend",
				extra.TaskQueue, taskQueue))
		}
		cfg.Workflows = append(cfg.Workflows, extra.Workflows...)
		cfg.Activities = append(cfg.Activities, extra.Activities...)
	}
	return cfg
}

// EEContributorCount reports how many enterprise contributors are registered, so
// a build-level test can assert an -tags ee binary reached the ee/ init()s.
func EEContributorCount() int { return len(eeContributors) }
