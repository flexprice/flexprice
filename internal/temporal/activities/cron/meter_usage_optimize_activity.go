package cron

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/config"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/activity"
)

const (
	optimizePartitionTimeout = 30 * time.Minute
	defaultOptimizeLookback  = 7
)

// meterUsageOptimizeConn is the ClickHouse surface the activity needs.
type meterUsageOptimizeConn interface {
	Exec(ctx context.Context, query string, args ...any) error
}

// MeterUsageOptimizeActivities merges closed day-partitions of meter_usage.
type MeterUsageOptimizeActivities struct {
	conn meterUsageOptimizeConn
	cfg  *config.Configuration
}

func NewMeterUsageOptimizeActivities(
	conn meterUsageOptimizeConn,
	cfg *config.Configuration,
) *MeterUsageOptimizeActivities {
	return &MeterUsageOptimizeActivities{conn: conn, cfg: cfg}
}

// OptimizeMeterUsageActivity runs OPTIMIZE FINAL on each closed day-partition.
// ClickHouse skips merged partitions and checks disk space itself.
func (a *MeterUsageOptimizeActivities) OptimizeMeterUsageActivity(ctx context.Context) (*cronModels.MeterUsageOptimizeResult, error) {
	log := activity.GetLogger(ctx)
	result := &cronModels.MeterUsageOptimizeResult{}

	if a.cfg == nil || !a.cfg.ClickHouse.MeterUsageOptimize.Enabled {
		log.Info("Meter usage optimize disabled by config, skipping")
		return result, nil
	}

	lookback := a.cfg.ClickHouse.MeterUsageOptimize.LookbackDays
	if lookback <= 0 {
		lookback = defaultOptimizeLookback
	}

	today := clock.Now().UTC()
	for i := 1; i <= lookback; i++ {
		partitionID := today.AddDate(0, 0, -i).Format("20060102")
		if err := a.optimizePartition(ctx, partitionID); err != nil {
			result.Failed++
			log.Error("Meter usage partition not optimized", "partition", partitionID, "error", err)
			continue
		}
		result.Optimized++
	}

	log.Info("Meter usage optimize finished", "optimized", result.Optimized, "failed", result.Failed)
	if result.Failed > 0 {
		return result, fmt.Errorf("meter_usage optimize incomplete: %d failed", result.Failed)
	}
	return result, nil
}

func (a *MeterUsageOptimizeActivities) optimizePartition(ctx context.Context, partitionID string) error {
	start := clock.Now()
	pctx, cancel := context.WithTimeout(ctx, optimizePartitionTimeout)
	defer cancel()
	query := fmt.Sprintf(`OPTIMIZE TABLE meter_usage PARTITION ID '%s' FINAL SETTINGS optimize_skip_merged_partitions = 1, max_memory_usage = 96636764160`, partitionID)
	if err := a.conn.Exec(pctx, query); err != nil {
		return fmt.Errorf("optimize partition %s: %w", partitionID, err)
	}
	activity.GetLogger(ctx).Info("Meter usage partition optimized",
		"partition", partitionID, "duration", clock.Now().Sub(start).String())
	return nil
}
