package cron

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

type fakeOptimizeConn struct {
	execErr map[string]error
	execs   []string
}

func (f *fakeOptimizeConn) Exec(_ context.Context, query string, _ ...any) error {
	f.execs = append(f.execs, query)
	for id, err := range f.execErr {
		if strings.Contains(query, id) {
			return err
		}
	}
	return nil
}

func runOptimize(t *testing.T, conn *fakeOptimizeConn, enabled bool, lookback int) (resultErr error, optimized, failed int) {
	t.Helper()
	cfg := &config.Configuration{}
	cfg.ClickHouse.MeterUsageOptimize.Enabled = enabled
	cfg.ClickHouse.MeterUsageOptimize.LookbackDays = lookback
	acts := NewMeterUsageOptimizeActivities(conn, cfg)
	env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
	env.RegisterActivity(acts.OptimizeMeterUsageActivity)
	val, err := env.ExecuteActivity(acts.OptimizeMeterUsageActivity)
	if err != nil {
		return err, 0, 0
	}
	var res struct{ Optimized, Failed int }
	require.NoError(t, val.Get(&res))
	return nil, res.Optimized, res.Failed
}

func day(t time.Time, ago int) string { return t.AddDate(0, 0, -ago).Format("20060102") }

func TestOptimizeMeterUsage_DisabledMakesNoCalls(t *testing.T) {
	conn := &fakeOptimizeConn{}
	err, o, f := runOptimize(t, conn, false, 7)
	require.NoError(t, err)
	require.Zero(t, o+f)
	require.Empty(t, conn.execs)
}

func TestOptimizeMeterUsage_ClosedPartitionsOnly(t *testing.T) {
	conn := &fakeOptimizeConn{}
	err, o, f := runOptimize(t, conn, true, 3)
	require.NoError(t, err)
	require.Equal(t, []int{3, 0}, []int{o, f})
	require.Len(t, conn.execs, 3)
	today := clock.Now().UTC()
	for i, q := range conn.execs {
		require.Contains(t, q, "PARTITION ID '"+day(today, i+1)+"' FINAL")
		require.NotContains(t, q, day(today, 0))
		require.Contains(t, q, "optimize_skip_merged_partitions = 1")
		require.Contains(t, q, "max_memory_usage = 96636764160")
	}
}

func TestOptimizeMeterUsage_DefaultLookback(t *testing.T) {
	conn := &fakeOptimizeConn{}
	err, o, _ := runOptimize(t, conn, true, 0)
	require.NoError(t, err)
	require.Equal(t, defaultOptimizeLookback, o)
}

func TestOptimizeMeterUsage_ContinuesAfterFailure(t *testing.T) {
	conn := &fakeOptimizeConn{execErr: map[string]error{day(clock.Now().UTC(), 2): errors.New("boom")}}
	err, _, _ := runOptimize(t, conn, true, 3)
	require.Error(t, err)
	require.Len(t, conn.execs, 3)
}
