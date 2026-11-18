package cron

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

var fixedNow = time.Date(2026, 11, 18, 0, 30, 0, 0, time.UTC)

type fakeOptimizeConn struct {
	failCall int
	execs    []string
}

func (f *fakeOptimizeConn) Exec(_ context.Context, query string, _ ...any) error {
	f.execs = append(f.execs, query)
	if len(f.execs) == f.failCall {
		return errors.New("boom")
	}
	return nil
}

func runOptimize(t *testing.T, conn *fakeOptimizeConn, enabled bool, lookback int) (resultErr error, optimized, failed int) {
	t.Helper()
	cfg := &config.Configuration{}
	cfg.ClickHouse.MeterUsageOptimize.Enabled = enabled
	cfg.ClickHouse.MeterUsageOptimize.LookbackDays = lookback
	acts := NewMeterUsageOptimizeActivities(conn, cfg)
	acts.now = func() time.Time { return fixedNow }
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
	for i, id := range []string{"20261117", "20261116", "20261115"} {
		require.Contains(t, conn.execs[i], "PARTITION ID '"+id+"' FINAL")
	}
	for _, q := range conn.execs {
		require.NotContains(t, q, "20261118")
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

func TestOptimizeMeterUsage_LookbackCapped(t *testing.T) {
	conn := &fakeOptimizeConn{}
	err, o, _ := runOptimize(t, conn, true, 9)
	require.NoError(t, err)
	require.Equal(t, maxOptimizeLookback, o)
	require.Len(t, conn.execs, maxOptimizeLookback)
}

func TestOptimizeMeterUsage_ContinuesAfterFailure(t *testing.T) {
	conn := &fakeOptimizeConn{failCall: 2}
	err, _, _ := runOptimize(t, conn, true, 3)
	require.Error(t, err)
	require.Len(t, conn.execs, 3)
	require.Contains(t, conn.execs[2], "PARTITION ID '20261115'")
}
