package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestGenerateWorkflowID_ReplayDLQIsFixedPerSource(t *testing.T) {
	t.Parallel()
	s := &temporalService{}
	input := func(source string) map[string]interface{} {
		return map[string]interface{}{"source_topic": source}
	}

	first := s.generateWorkflowID(types.TemporalReplayDLQWorkflow, input("events_dlq"))
	second := s.generateWorkflowID(types.TemporalReplayDLQWorkflow, input("events_dlq"))
	other := s.generateWorkflowID(types.TemporalReplayDLQWorkflow, input("meter_usage_dlq"))

	assert.Equal(t, "wf_ReplayDLQWorkflow_events_dlq", first)
	assert.Equal(t, first, second, "same source must map to one ID so Temporal blocks concurrent runs")
	assert.NotEqual(t, first, other, "different sources must replay independently")
}
