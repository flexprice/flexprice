package cron

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

type fakeOptimizeConn struct {
	partitions []meterUsagePartition
	free       uint64
	execErr    map[string]error
	selects    int
	queries    []string
	execs      []string
}

func (f *fakeOptimizeConn) Select(_ context.Context, dest any, query string, _ ...any) error {
	f.selects++
	f.queries = append(f.queries, query)
	v := reflect.ValueOf(dest).Elem()
	if strings.Contains(query, "system.disks") {
		v.Set(reflect.ValueOf([]meterUsageDisk{{FreeSpace: f.free}}))
		return nil
	}
	v.Set(reflect.ValueOf(f.partitions))
	return nil
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

func runOptimize(t *testing.T, conn *fakeOptimizeConn, enabled bool) (resultErr error, optimized, skipped, failed int) {
	t.Helper()
	cfg := &config.Configuration{}
	cfg.ClickHouse.MeterUsageOptimize.Enabled = enabled
	acts := NewMeterUsageOptimizeActivities(conn, cfg)
	env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
	env.RegisterActivity(acts.OptimizeMeterUsageActivity)
	val, err := env.ExecuteActivity(acts.OptimizeMeterUsageActivity)
	if err != nil {
		return err, 0, 0, 0
	}
	var res struct{ Optimized, Skipped, Failed int }
	require.NoError(t, val.Get(&res))
	return nil, res.Optimized, res.Skipped, res.Failed
}

func TestOptimizeMeterUsage_DisabledMakesNoCalls(t *testing.T) {
	conn := &fakeOptimizeConn{}
	err, o, s, f := runOptimize(t, conn, false)
	require.NoError(t, err)
	require.Zero(t, o+s+f)
	require.Zero(t, conn.selects)
	require.Empty(t, conn.execs)
}

func TestOptimizeMeterUsage_InvalidPartitionIDNeverExecuted(t *testing.T) {
	conn := &fakeOptimizeConn{
		free:       1 << 40,
		partitions: []meterUsagePartition{{PartitionID: "2026'; DROP", Parts: 3, Bytes: 10}, {PartitionID: "20261116", Parts: 3, Bytes: 10}},
	}
	err, _, _, _ := runOptimize(t, conn, true)
	require.Error(t, err)
	require.Len(t, conn.execs, 1)
	require.Contains(t, conn.execs[0], "PARTITION ID '20261116' FINAL SETTINGS optimize_throw_if_noop = 1")
}

func TestOptimizeMeterUsage_EveryQueryHasMemoryCap(t *testing.T) {
	conn := &fakeOptimizeConn{
		free:       1 << 40,
		partitions: []meterUsagePartition{{PartitionID: "20261116", Parts: 3, Bytes: 10}},
	}
	err, o, _, _ := runOptimize(t, conn, true)
	require.NoError(t, err)
	require.Equal(t, 1, o)
	for _, q := range append(conn.queries, conn.execs...) {
		require.Contains(t, q, "max_memory_usage = 96636764160")
	}
}

func TestOptimizeMeterUsage_LowDiskSkips(t *testing.T) {
	conn := &fakeOptimizeConn{free: 29, partitions: []meterUsagePartition{{PartitionID: "20261116", Parts: 3, Bytes: 10}}}
	err, _, _, _ := runOptimize(t, conn, true)
	require.Error(t, err)
	require.Empty(t, conn.execs)
}

func TestOptimizeMeterUsage_ContinuesAfterFailure(t *testing.T) {
	conn := &fakeOptimizeConn{
		free:       1 << 40,
		execErr:    map[string]error{"20261115": errors.New("boom")},
		partitions: []meterUsagePartition{{PartitionID: "20261115", Parts: 2, Bytes: 1}, {PartitionID: "20261116", Parts: 2, Bytes: 1}},
	}
	err, _, _, _ := runOptimize(t, conn, true)
	require.Error(t, err)
	require.Len(t, conn.execs, 2)
}

func TestOptimizeMeterUsage_Success(t *testing.T) {
	conn := &fakeOptimizeConn{free: 1 << 40, partitions: []meterUsagePartition{{PartitionID: "20261116", Parts: 2, Bytes: 1}}}
	err, o, s, f := runOptimize(t, conn, true)
	require.NoError(t, err)
	require.Equal(t, []int{1, 0, 0}, []int{o, s, f})
}
