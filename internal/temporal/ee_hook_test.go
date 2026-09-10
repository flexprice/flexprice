package temporal

import (
	"testing"

	eeservice "github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/types"
)

func resetContributors(t *testing.T) {
	t.Helper()
	saved := eeContributors
	eeContributors = nil
	t.Cleanup(func() { eeContributors = saved })
}

func TestApplyEEContributions_CommunityBuildUnchanged(t *testing.T) {
	resetContributors(t)
	in := WorkerConfig{TaskQueue: types.TemporalTaskQueueWorkflows}
	out := applyEEContributions(in, eeservice.ServiceParams{}, types.TemporalTaskQueueWorkflows)
	if len(out.Workflows) != 0 || len(out.Activities) != 0 {
		t.Fatalf("community build must add nothing, got %d workflows %d activities", len(out.Workflows), len(out.Activities))
	}
}

func TestEEContributorCount(t *testing.T) {
	resetContributors(t)
	if EEContributorCount() != 0 {
		t.Fatalf("want 0, got %d", EEContributorCount())
	}
	RegisterEEContributor(func(_ eeservice.ServiceParams, _ types.TemporalTaskQueue) WorkerConfig {
		return WorkerConfig{}
	})
	if EEContributorCount() != 1 {
		t.Fatalf("want 1, got %d", EEContributorCount())
	}
}

func TestApplyEEContributions_SkipsEmptyContribution(t *testing.T) {
	resetContributors(t)
	// A contributor that returns an empty WorkerConfig is skipped, not merged.
	RegisterEEContributor(func(_ eeservice.ServiceParams, _ types.TemporalTaskQueue) WorkerConfig {
		return WorkerConfig{}
	})
	in := WorkerConfig{TaskQueue: types.TemporalTaskQueueWorkflows}
	out := applyEEContributions(in, eeservice.ServiceParams{}, types.TemporalTaskQueueWorkflows)
	if len(out.Workflows) != 0 || len(out.Activities) != 0 {
		t.Fatalf("empty contribution must add nothing, got %d/%d", len(out.Workflows), len(out.Activities))
	}
}

func TestApplyEEContributions_AppendsForMatchingQueue(t *testing.T) {
	resetContributors(t)
	marker := func() {}
	RegisterEEContributor(func(_ eeservice.ServiceParams, q types.TemporalTaskQueue) WorkerConfig {
		if q != types.TemporalTaskQueueWorkflows {
			return WorkerConfig{}
		}
		return WorkerConfig{Workflows: []interface{}{marker}}
	})
	out := applyEEContributions(WorkerConfig{TaskQueue: types.TemporalTaskQueueWorkflows}, eeservice.ServiceParams{}, types.TemporalTaskQueueWorkflows)
	if len(out.Workflows) != 1 {
		t.Fatalf("expected 1 contributed workflow, got %d", len(out.Workflows))
	}
}

func TestApplyEEContributions_WrongQueuePanics(t *testing.T) {
	resetContributors(t)
	RegisterEEContributor(func(_ eeservice.ServiceParams, _ types.TemporalTaskQueue) WorkerConfig {
		return WorkerConfig{TaskQueue: types.TemporalTaskQueueBilling, Workflows: []interface{}{func() {}}}
	})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on task-queue mismatch")
		}
	}()
	applyEEContributions(WorkerConfig{TaskQueue: types.TemporalTaskQueueWorkflows}, eeservice.ServiceParams{}, types.TemporalTaskQueueWorkflows)
}

func TestBuildWorkerConfig_IncludesEEContribution(t *testing.T) {
	resetContributors(t)

	build := func() WorkerConfig {
		return buildWorkerConfig(
			types.TemporalTaskQueueWorkflows,
			eeservice.ServiceParams{},
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		)
	}

	base := len(build().Workflows)

	marker := func() {}
	RegisterEEContributor(func(_ eeservice.ServiceParams, q types.TemporalTaskQueue) WorkerConfig {
		if q != types.TemporalTaskQueueWorkflows {
			return WorkerConfig{}
		}
		return WorkerConfig{Workflows: []interface{}{marker}}
	})
	cfg := build()
	if len(cfg.Workflows) != base+1 {
		t.Fatalf("EE contribution missing: want %d workflows, got %d", base+1, len(cfg.Workflows))
	}
}
