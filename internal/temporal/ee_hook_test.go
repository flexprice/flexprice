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
	marker := func() {}
	RegisterEEContributor(func(_ eeservice.ServiceParams, q types.TemporalTaskQueue) WorkerConfig {
		if q != types.TemporalTaskQueueWorkflows {
			return WorkerConfig{}
		}
		return WorkerConfig{Workflows: []interface{}{marker}}
	})
	cfg := buildWorkerConfig(
		types.TemporalTaskQueueWorkflows,
		eeservice.ServiceParams{},
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	if len(cfg.Workflows) == 0 {
		t.Fatal("buildWorkerConfig did not include any workflows; EE contribution missing")
	}
}
