package clickhouse

import (
	"context"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	ich "github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

type MeterUsageBenchmarkRepository struct {
	store  *ich.ClickHouseStore
	logger *logger.Logger
}

func NewMeterUsageBenchmarkRepository(store *ich.ClickHouseStore, logger *logger.Logger) events.MeterUsageBenchmarkRepository {
	return &MeterUsageBenchmarkRepository{store: store, logger: logger}
}

func (r *MeterUsageBenchmarkRepository) BulkInsert(ctx context.Context, records []*events.MeterUsageBenchmarkRecord) error {
	if len(records) == 0 {
		return nil
	}

	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"max_memory_usage": 90000000000,
	}))

	stmt, err := r.store.GetConn().PrepareBatch(ctx, `
		INSERT INTO meter_usage_benchmark (
			tenant_id, environment_id, event_id,
			start_time, end_time,
			external_customer_id, external_customer_ids,
			feature_ids, sources, group_by, window_size, expand,
			include_children, has_property_filters, request_json,
			nofinal_duration_ms, nofinal_scan_rows, nofinal_scan_bytes,
			nofinal_read_disk_bytes, nofinal_mem_peak_bytes, nofinal_result_rows,
			final_duration_ms, final_scan_rows, final_scan_bytes,
			final_read_disk_bytes, final_mem_peak_bytes, final_result_rows,
			duration_diff_ms, scan_rows_diff, scan_bytes_diff,
			read_disk_bytes_diff, mem_peak_diff_bytes,
			nofinal_total_usage, final_total_usage, usage_diff,
			nofinal_total_cost, final_total_cost, cost_diff,
			nofinal_item_count, final_item_count, results_match,
			nofinal_error, final_error,
			first_side, currency, created_at
		)
	`)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to prepare meter_usage_benchmark insert").
			Mark(ierr.ErrDatabase)
	}

	rowsInserted := 0
	var firstInserted *events.MeterUsageBenchmarkRecord

	now := time.Now().UTC()
	for _, record := range records {
		if record == nil {
			continue
		}
		if record.CreatedAt.IsZero() {
			record.CreatedAt = now
		}
		// ClickHouse Array columns reject nil — coerce to empty slices.
		extCustIDs := record.ExternalCustomerIDs
		if extCustIDs == nil {
			extCustIDs = []string{}
		}
		featureIDs := record.FeatureIDs
		if featureIDs == nil {
			featureIDs = []string{}
		}
		sources := record.Sources
		if sources == nil {
			sources = []string{}
		}
		groupBy := record.GroupBy
		if groupBy == nil {
			groupBy = []string{}
		}
		expand := record.Expand
		if expand == nil {
			expand = []string{}
		}

		if err := stmt.Append(
			record.TenantID,
			record.EnvironmentID,
			record.EventID,
			record.StartTime,
			record.EndTime,
			record.ExternalCustomerID,
			extCustIDs,
			featureIDs,
			sources,
			groupBy,
			record.WindowSize,
			expand,
			record.IncludeChildren,
			record.HasPropertyFilters,
			record.RequestJSON,
			record.NoFinalDurationMs,
			record.NoFinalScanRows,
			record.NoFinalScanBytes,
			record.NoFinalReadDiskBytes,
			record.NoFinalMemPeakBytes,
			record.NoFinalResultRows,
			record.FinalDurationMs,
			record.FinalScanRows,
			record.FinalScanBytes,
			record.FinalReadDiskBytes,
			record.FinalMemPeakBytes,
			record.FinalResultRows,
			record.DurationDiffMs,
			record.ScanRowsDiff,
			record.ScanBytesDiff,
			record.ReadDiskBytesDiff,
			record.MemPeakDiffBytes,
			record.NoFinalTotalUsage,
			record.FinalTotalUsage,
			record.UsageDiff,
			record.NoFinalTotalCost,
			record.FinalTotalCost,
			record.CostDiff,
			record.NoFinalItemCount,
			record.FinalItemCount,
			record.ResultsMatch,
			record.NoFinalError,
			record.FinalError,
			record.FirstSide,
			record.Currency,
			record.CreatedAt,
		); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to append meter_usage_benchmark row").
				Mark(ierr.ErrDatabase)
		}

		rowsInserted++
		if firstInserted == nil {
			firstInserted = record
		}
	}

	if rowsInserted == 0 || firstInserted == nil {
		return nil
	}

	if err := stmt.Send(); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to send meter_usage_benchmark insert").
			Mark(ierr.ErrDatabase)
	}

	r.logger.Debug(ctx, "inserted meter_usage_benchmark batch",
		"rows", len(records),
		"event_id", firstInserted.EventID,
		"tenant_id", firstInserted.TenantID,
	)

	return nil
}
