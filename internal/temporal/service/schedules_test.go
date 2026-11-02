package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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

func TestAllTemporalScheduleConfigs_BillingInstalmentsDue(t *testing.T) {
	t.Parallel()
	config, found := lo.Find(AllTemporalScheduleConfigs(), func(cfg types.ScheduleConfig) bool {
		return cfg.ID == types.ScheduleIDBillingInstalmentsDue
	})
	require.True(t, found, "the instalment cron must be scheduled")
	require.Equal(t, 15*time.Minute, config.Interval)
	require.Equal(t, types.TemporalTaskQueueCron, config.TaskQueue)
	require.Equal(t, types.TemporalTaskQueueCron, types.TemporalBillingInstalmentsDueWorkflow.TaskQueue())
	require.NoError(t, types.ScheduleIDBillingInstalmentsDue.Validate())
	require.NoError(t, types.TemporalBillingInstalmentsDueWorkflow.Validate())
}
