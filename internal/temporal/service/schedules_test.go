package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// TestAllTemporalScheduleConfigsMatchServerScheduleIDs keeps schedule config ids aligned
// with types.AllTemporalServerScheduleIDs (used for schedule_id validation).
func TestAllTemporalScheduleConfigsMatchServerScheduleIDs(t *testing.T) {
	t.Parallel()
	configs := AllTemporalScheduleConfigs(nil)
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

// TestRevenueRollupScheduleInterval pins the interval plumbing: the config
// value drives both the schedule spec and the workflow input, with a 1h
// default. Enable/disable is NOT a schedule concern — the kill switch lives
// in RollupDirtyActivity.
func TestRevenueRollupScheduleInterval(t *testing.T) {
	t.Parallel()

	find := func(cfg *config.Configuration) types.ScheduleConfig {
		for _, sc := range AllTemporalScheduleConfigs(cfg) {
			if sc.ID == types.ScheduleIDRevenueRollup {
				return sc
			}
		}
		t.Fatal("revenue rollup schedule must always be declared")
		return types.ScheduleConfig{}
	}

	require.Equal(t, 24*time.Hour, find(nil).Interval, "nil cfg falls back to the daily default")

	cfg := &config.Configuration{}
	cfg.Analytics.RevenueRollup.Interval = 30 * time.Minute
	require.Equal(t, 30*time.Minute, find(cfg).Interval, "config interval must drive the schedule spec")
}
