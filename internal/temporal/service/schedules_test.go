package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// TestAllTemporalScheduleConfigsMatchServerScheduleIDs keeps schedule config ids aligned
// with types.AllTemporalServerScheduleIDs (used for schedule_id validation).
func TestAllTemporalScheduleConfigsMatchServerScheduleIDs(t *testing.T) {
	t.Parallel()
	configs := AllTemporalScheduleConfigs()
	ids := types.AllTemporalServerScheduleIDs()
	require.Equal(t, len(ids), len(configs), "each managed schedule must have a config entry")

	expected := make(map[types.ScheduleID]struct{}, len(ids))
	for _, id := range ids {
		expected[id] = struct{}{}
	}
	for _, cfg := range configs {
		_, ok := expected[cfg.ID]
		require.True(t, ok, "config id %q not in AllTemporalServerScheduleIDs", cfg.ID)
		delete(expected, cfg.ID)
	}
	require.Empty(t, expected, "missing schedule config ids")
}
