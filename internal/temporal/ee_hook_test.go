package temporal

import (
	"testing"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/types"
)

// TestApplyEEContributionsIsAdditive guards the core invariant of the ee hook:
// contributions may only append. A contributor must never be able to drop or
// reorder what the community build registered.
func TestApplyEEContributionsIsAdditive(t *testing.T) {
	original := eeContributors
	t.Cleanup(func() { eeContributors = original })

	coreWorkflow := func() {}
	coreActivity := func() {}
	eeWorkflow := func() {}

	base := WorkerConfig{
		TaskQueue:  types.TemporalTaskQueueWorkflows,
		Workflows:  []interface{}{coreWorkflow},
		Activities: []interface{}{coreActivity},
	}

	// No contributors: a community build must get its config back unchanged.
	eeContributors = nil
	got := applyEEContributions(base, service.ServiceParams{}, types.TemporalTaskQueueWorkflows)
	if len(got.Workflows) != 1 || len(got.Activities) != 1 {
		t.Fatalf("community build altered: got %d workflows, %d activities; want 1, 1",
			len(got.Workflows), len(got.Activities))
	}

	// One contributor targeting this queue: core entries survive, EE appends.
	eeContributors = []EEContributor{
		func(_ service.ServiceParams, tq types.TemporalTaskQueue) WorkerConfig {
			if tq != types.TemporalTaskQueueWorkflows {
				return WorkerConfig{}
			}
			return WorkerConfig{TaskQueue: tq, Workflows: []interface{}{eeWorkflow}}
		},
	}
	got = applyEEContributions(base, service.ServiceParams{}, types.TemporalTaskQueueWorkflows)
	if len(got.Workflows) != 2 {
		t.Fatalf("ee build: got %d workflows, want 2 (1 core + 1 ee)", len(got.Workflows))
	}
	if len(got.Activities) != 1 {
		t.Fatalf("ee build dropped a core activity: got %d, want 1", len(got.Activities))
	}

	// A contributor scoped to another queue must contribute nothing here.
	got = applyEEContributions(base, service.ServiceParams{}, types.TemporalTaskQueueCron)
	if len(got.Workflows) != 1 {
		t.Fatalf("contributor leaked across queues: got %d workflows, want 1", len(got.Workflows))
	}
}
