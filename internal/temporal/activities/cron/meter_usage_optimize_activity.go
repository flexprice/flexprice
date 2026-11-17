package cron

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/activity"
)

const (
	optimizePartitionTimeout = time.Hour
	optimizeMinFreeFactor    = 3
	defaultOptimizeLookback  = 7
)

var partitionIDPattern = regexp.MustCompile(`^\d{8}$`)

// meterUsageOptimizeConn is the ClickHouse surface the activity needs.
type meterUsageOptimizeConn interface {
	Select(ctx context.Context, dest any, query string, args ...any) error
	Exec(ctx context.Context, query string, args ...any) error
}

type meterUsagePartition struct {
	PartitionID string `ch:"partition_id"`
	Parts       uint64 `ch:"parts"`
	Bytes       uint64 `ch:"bytes"`
}

type meterUsageDisk struct {
	FreeSpace uint64 `ch:"free_space"`
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

// OptimizeMeterUsageActivity runs OPTIMIZE FINAL per closed partition with
// more than one part. Retries only re-list partitions still unmerged.
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

	var partitions []meterUsagePartition
	listQuery := fmt.Sprintf(`SELECT partition_id, count() AS parts, sum(bytes_on_disk) AS bytes
FROM system.parts
WHERE database = currentDatabase() AND table = 'meter_usage' AND active
  AND partition_id >= toString(toYYYYMMDD(today() - %d))
  AND partition_id < toString(toYYYYMMDD(today()))
GROUP BY partition_id
HAVING parts > 1
ORDER BY partition_id`, lookback)
	if err := a.conn.Select(ctx, &partitions, listQuery); err != nil {
		return nil, fmt.Errorf("list meter_usage partitions: %w", err)
	}

	for _, p := range partitions {
		if err := a.optimizePartition(ctx, p); err != nil {
			if errors.Is(err, errOptimizeSkipped) {
				result.Skipped++
			} else {
				result.Failed++
			}
			log.Error("Meter usage partition not optimized", "partition", p.PartitionID, "error", err)
			continue
		}
		result.Optimized++
	}

	log.Info("Meter usage optimize finished",
		"optimized", result.Optimized, "skipped", result.Skipped, "failed", result.Failed)
	if result.Failed+result.Skipped > 0 {
		return result, fmt.Errorf("meter_usage optimize incomplete: %d failed, %d skipped", result.Failed, result.Skipped)
	}
	return result, nil
}

var errOptimizeSkipped = fmt.Errorf("skipped")

func (a *MeterUsageOptimizeActivities) optimizePartition(ctx context.Context, p meterUsagePartition) error {
	if !partitionIDPattern.MatchString(p.PartitionID) {
		return fmt.Errorf("invalid partition id %q", p.PartitionID)
	}

	var disks []meterUsageDisk
	if err := a.conn.Select(ctx, &disks, `SELECT free_space FROM system.disks WHERE name = 'default'`); err != nil {
		return fmt.Errorf("read free disk space: %w", err)
	}
	if len(disks) == 0 {
		return fmt.Errorf("default disk not found")
	}
	if disks[0].FreeSpace < optimizeMinFreeFactor*p.Bytes {
		activity.GetLogger(ctx).Warn("Low disk space, skipping partition",
			"partition", p.PartitionID, "bytes", p.Bytes, "free_space", disks[0].FreeSpace)
		return errOptimizeSkipped
	}

	start := time.Now()
	pctx, cancel := context.WithTimeout(ctx, optimizePartitionTimeout)
	defer cancel()
	query := fmt.Sprintf(`OPTIMIZE TABLE meter_usage PARTITION ID '%s' FINAL SETTINGS optimize_throw_if_noop = 1`, p.PartitionID)
	if err := a.conn.Exec(pctx, query); err != nil {
		return fmt.Errorf("optimize partition %s: %w", p.PartitionID, err)
	}
	activity.GetLogger(ctx).Info("Meter usage partition optimized",
		"partition", p.PartitionID, "parts_before", p.Parts, "bytes", p.Bytes, "duration", time.Since(start).String())
	return nil
}
