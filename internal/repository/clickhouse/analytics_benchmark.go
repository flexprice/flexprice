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

type AnalyticsBenchmarkRepository struct {
	store  *ich.ClickHouseStore
	logger *logger.Logger
}

func NewAnalyticsBenchmarkRepository(store *ich.ClickHouseStore, logger *logger.Logger) events.AnalyticsBenchmarkRepository {
	return &AnalyticsBenchmarkRepository{store: store, logger: logger}
}

func (r *AnalyticsBenchmarkRepository) BulkInsert(ctx context.Context, records []*events.AnalyticsBenchmarkRecord) error {
	if len(records) == 0 {
		return nil
	}

	// Attach settings to context
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"max_memory_usage": 90000000000,
	}))

	stmt, err := r.store.GetConn().PrepareBatch(ctx, `
		INSERT INTO analytics_benchmark (
			tenant_id, environment_id, event_id,
			start_time, end_time,
			external_customer_id, external_customer_ids,
			feature_ids, sources, group_by, window_size, expand,
			include_children, has_property_filters, request_json,
			row_type,
			feature_id, meter_id, sub_line_item_id, group_key, match_status,
			diff_reason,
			feature_price_id, meter_price_id,
			feature_item_count, meter_item_count,
			feature_total_usage, meter_total_usage, usage_diff,
			feature_total_cost, meter_total_cost, cost_diff,
			feature_event_count, meter_event_count,
			currency, created_at
		)
	`)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to prepare analytics_benchmark insert").
			Mark(ierr.ErrDatabase)
	}

	rowsInserted := 0
	var firstInserted *events.AnalyticsBenchmarkRecord

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

		// Default row_type to line_item for backwards-compat when callers don't set it.
		rowType := record.RowType
		if rowType == "" {
			rowType = events.AnalyticsBenchmarkRowLineItem
		}
		// Default diff_reason to none (the column has a default but the binary
		// protocol requires every column be supplied per row).
		diffReason := record.DiffReason
		if diffReason == "" {
			diffReason = events.AnalyticsBenchmarkDiffNone
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
			string(rowType),
			record.FeatureID,
			record.MeterID,
			record.SubLineItemID,
			record.GroupKey,
			string(record.MatchStatus),
			string(diffReason),
			record.FeaturePriceID,
			record.MeterPriceID,
			record.FeatureItemCount,
			record.MeterItemCount,
			record.FeatureTotalUsage,
			record.MeterTotalUsage,
			record.UsageDiff,
			record.FeatureTotalCost,
			record.MeterTotalCost,
			record.CostDiff,
			record.FeatureEventCount,
			record.MeterEventCount,
			record.Currency,
			record.CreatedAt,
		); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to append analytics_benchmark row").
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
			WithHint("Failed to send analytics_benchmark insert").
			Mark(ierr.ErrDatabase)
	}

	r.logger.Debug(ctx, "inserted analytics_benchmark batch",
		"rows", len(records),
		"event_id", firstInserted.EventID,
		"tenant_id", firstInserted.TenantID,
	)

	return nil
}
