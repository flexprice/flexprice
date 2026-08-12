package temporal

import (
	"reflect"
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
	// Identity and order, not just count: a contributor that prepends, or that
	// overwrites the core entry with its own, would satisfy a length check.
	if !sameFunc(got.Workflows[0], coreWorkflow) {
		t.Error("core workflow is no longer first — ee contribution must append, not prepend or replace")
	}
	if !sameFunc(got.Workflows[1], eeWorkflow) {
		t.Error("ee workflow is not the appended entry")
	}
	if len(got.Activities) != 1 || !sameFunc(got.Activities[0], coreActivity) {
		t.Errorf("core activity was disturbed: got %d entries", len(got.Activities))
	}

	// A contributor scoped to another queue must contribute nothing here.
	got = applyEEContributions(base, service.ServiceParams{}, types.TemporalTaskQueueCron)
	if len(got.Workflows) != 1 {
		t.Fatalf("contributor leaked across queues: got %d workflows, want 1", len(got.Workflows))
	}
}

// sameFunc reports whether two func values are the same function. Funcs are not
// comparable with ==, so identity is checked by code pointer.
func sameFunc(a, b interface{}) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

// TestContributionToForeignQueuePanics covers a trap in the WorkerConfig shape:
// the struct has a TaskQueue field, so a contributor can plausibly set it to a
// queue it wants to own. Workers are started from a fixed list, so such a
// contribution would be registered and never polled — a silent no-op. It must
// fail at startup instead.
func TestContributionToForeignQueuePanics(t *testing.T) {
	original := eeContributors
	t.Cleanup(func() { eeContributors = original })

	eeContributors = []EEContributor{
		func(_ service.ServiceParams, _ types.TemporalTaskQueue) WorkerConfig {
			return WorkerConfig{
				TaskQueue: types.TemporalTaskQueueCron,
				Workflows: []interface{}{func() {}},
			}
		},
	}

	defer func() {
		if recover() == nil {
			t.Error("contributing to a queue other than the one being built should panic")
		}
	}()
	applyEEContributions(
		WorkerConfig{TaskQueue: types.TemporalTaskQueueWorkflows},
		service.ServiceParams{},
		types.TemporalTaskQueueWorkflows,
	)
}
