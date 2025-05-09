package clickhouse

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type ProcessedEventRepository struct {
	store  *clickhouse.ClickHouseStore
	logger *logger.Logger
}

func NewProcessedEventRepository(store *clickhouse.ClickHouseStore, logger *logger.Logger) events.ProcessedEventRepository {
	return &ProcessedEventRepository{store: store, logger: logger}
}

// InsertProcessedEvent inserts a single processed event
func (r *ProcessedEventRepository) InsertProcessedEvent(ctx context.Context, event *events.ProcessedEvent) error {
	query := `
		INSERT INTO events_processed (
			id, tenant_id, external_customer_id, customer_id, event_name, source, 
			timestamp, ingested_at, properties, environment_id,
			subscription_id, sub_line_item_id, price_id, meter_id, feature_id, period_id,
			unique_hash, qty_total, qty_billable, qty_free_applied, tier_snapshot, 
			unit_cost, cost, currency, sign
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		)
	`

	propertiesJSON, err := json.Marshal(event.Properties)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to marshal event properties").
			WithReportableDetails(map[string]interface{}{
				"event_id": event.ID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Default sign to 1 if not set
	sign := event.Sign
	if sign == 0 {
		sign = 1
	}

	args := []interface{}{
		event.ID,
		event.TenantID,
		event.ExternalCustomerID,
		event.CustomerID,
		event.EventName,
		event.Source,
		event.Timestamp,
		event.IngestedAt,
		string(propertiesJSON),
		event.EnvironmentID,
		event.SubscriptionID,
		event.SubLineItemID,
		event.PriceID,
		event.MeterID,
		event.FeatureID,
		event.PeriodID,
		event.UniqueHash,
		event.QtyTotal,
		event.QtyBillable,
		event.QtyFreeApplied,
		event.TierSnapshot,
		event.UnitCost,
		event.Cost,
		event.Currency,
		sign,
	}

	err = r.store.GetConn().Exec(ctx, query, args...)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to insert processed event").
			WithReportableDetails(map[string]interface{}{
				"event_id": event.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// BulkInsertProcessedEvents inserts multiple processed events
func (r *ProcessedEventRepository) BulkInsertProcessedEvents(ctx context.Context, events []*events.ProcessedEvent) error {
	if len(events) == 0 {
		return nil
	}

	// Split events in batches of 100
	eventsBatches := lo.Chunk(events, 100)

	for _, eventsBatch := range eventsBatches {
		// Prepare batch statement
		batch, err := r.store.GetConn().PrepareBatch(ctx, `
			INSERT INTO events_processed (
				id, tenant_id, external_customer_id, customer_id, event_name, source, 
				timestamp, ingested_at, properties, environment_id,
				subscription_id, sub_line_item_id, price_id, meter_id, feature_id, period_id,
				unique_hash, qty_total, qty_billable, qty_free_applied, tier_snapshot, 
				unit_cost, cost, currency, sign
			)
		`)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to prepare batch for processed events").
				Mark(ierr.ErrDatabase)
		}

		for _, event := range eventsBatch {
			propertiesJSON, err := json.Marshal(event.Properties)
			if err != nil {
				return ierr.WithError(err).
					WithHint("Failed to marshal event properties").
					WithReportableDetails(map[string]interface{}{
						"event_id": event.ID,
					}).
					Mark(ierr.ErrValidation)
			}

			// Default sign to 1 if not set
			sign := event.Sign
			if sign == 0 {
				sign = 1
			}

			err = batch.Append(
				event.ID,
				event.TenantID,
				event.ExternalCustomerID,
				event.CustomerID,
				event.EventName,
				event.Source,
				event.Timestamp,
				event.IngestedAt,
				string(propertiesJSON),
				event.EnvironmentID,
				event.SubscriptionID,
				event.SubLineItemID,
				event.PriceID,
				event.MeterID,
				event.FeatureID,
				event.PeriodID,
				event.UniqueHash,
				event.QtyTotal,
				event.QtyBillable,
				event.QtyFreeApplied,
				event.TierSnapshot,
				event.UnitCost,
				event.Cost,
				event.Currency,
				sign,
			)

			if err != nil {
				return ierr.WithError(err).
					WithHint("Failed to append processed event to batch").
					WithReportableDetails(map[string]interface{}{
						"event_id": event.ID,
					}).
					Mark(ierr.ErrDatabase)
			}
		}

		// Send batch
		if err := batch.Send(); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to execute batch insert for processed events").
				WithReportableDetails(map[string]interface{}{
					"event_count": len(events),
				}).
				Mark(ierr.ErrDatabase)
		}
	}

	return nil
}

// GetProcessedEvents retrieves processed events based on the provided parameters
func (r *ProcessedEventRepository) GetProcessedEvents(ctx context.Context, params *events.GetProcessedEventsParams) ([]*events.ProcessedEvent, uint64, error) {
	query := `
		SELECT 
			id, tenant_id, external_customer_id, customer_id, event_name, source, 
			timestamp, ingested_at, properties, processed_at, environment_id,
			subscription_id, sub_line_item_id, price_id, meter_id, feature_id, period_id,
			unique_hash, qty_total, qty_billable, qty_free_applied, tier_snapshot, 
			unit_cost, cost, currency, version, sign, final_lag_ms
		FROM events_processed
		WHERE tenant_id = ?
		AND environment_id = ?
		AND timestamp >= ?
		AND timestamp <= ?
	`

	countQuery := `
		SELECT COUNT(*)
		FROM events_processed
		WHERE tenant_id = ?
		AND environment_id = ?
		AND timestamp >= ?
		AND timestamp <= ?
	`

	args := []interface{}{types.GetTenantID(ctx), types.GetEnvironmentID(ctx), params.StartTime, params.EndTime}
	countArgs := []interface{}{types.GetTenantID(ctx), types.GetEnvironmentID(ctx), params.StartTime, params.EndTime}

	// Add filters
	if params.CustomerID != "" {
		query += " AND customer_id = ?"
		countQuery += " AND customer_id = ?"
		args = append(args, params.CustomerID)
		countArgs = append(countArgs, params.CustomerID)
	}

	if params.SubscriptionID != "" {
		query += " AND subscription_id = ?"
		countQuery += " AND subscription_id = ?"
		args = append(args, params.SubscriptionID)
		countArgs = append(countArgs, params.SubscriptionID)
	}

	if params.MeterID != "" {
		query += " AND meter_id = ?"
		countQuery += " AND meter_id = ?"
		args = append(args, params.MeterID)
		countArgs = append(countArgs, params.MeterID)
	}

	if params.FeatureID != "" {
		query += " AND feature_id = ?"
		countQuery += " AND feature_id = ?"
		args = append(args, params.FeatureID)
		countArgs = append(countArgs, params.FeatureID)
	}

	if params.PriceID != "" {
		query += " AND price_id = ?"
		countQuery += " AND price_id = ?"
		args = append(args, params.PriceID)
		countArgs = append(countArgs, params.PriceID)
	}

	// Add FINAL modifier to handle ReplacingMergeTree
	query += " FINAL"
	countQuery += " FINAL"

	// Apply pagination
	if params.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, params.Limit)

		if params.Offset > 0 {
			query += " OFFSET ?"
			args = append(args, params.Offset)
		}
	}

	// Execute count query if requested
	var totalCount uint64 = 0
	if params.CountTotal {
		err := r.store.GetConn().QueryRow(ctx, countQuery, countArgs...).Scan(&totalCount)
		if err != nil {
			return nil, 0, ierr.WithError(err).
				WithHint("Failed to count processed events").
				Mark(ierr.ErrDatabase)
		}
	}

	// Execute main query
	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to query processed events").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var eventsList []*events.ProcessedEvent
	for rows.Next() {
		var event events.ProcessedEvent
		var propertiesJSON string

		err := rows.Scan(
			&event.ID,
			&event.TenantID,
			&event.ExternalCustomerID,
			&event.CustomerID,
			&event.EventName,
			&event.Source,
			&event.Timestamp,
			&event.IngestedAt,
			&propertiesJSON,
			&event.ProcessedAt,
			&event.EnvironmentID,
			&event.SubscriptionID,
			&event.SubLineItemID,
			&event.PriceID,
			&event.MeterID,
			&event.FeatureID,
			&event.PeriodID,
			&event.UniqueHash,
			&event.QtyTotal,
			&event.QtyBillable,
			&event.QtyFreeApplied,
			&event.TierSnapshot,
			&event.UnitCost,
			&event.Cost,
			&event.Currency,
			&event.Version,
			&event.Sign,
			&event.FinalLagMs,
		)
		if err != nil {
			return nil, 0, ierr.WithError(err).
				WithHint("Failed to scan processed event").
				Mark(ierr.ErrDatabase)
		}

		// Parse properties
		if propertiesJSON != "" {
			if err := json.Unmarshal([]byte(propertiesJSON), &event.Properties); err != nil {
				return nil, 0, ierr.WithError(err).
					WithHint("Failed to unmarshal properties").
					Mark(ierr.ErrValidation)
			}
		} else {
			event.Properties = make(map[string]interface{})
		}

		eventsList = append(eventsList, &event)
	}

	return eventsList, totalCount, nil
}

// IsDuplicate checks if an event with the given unique hash already exists
func (r *ProcessedEventRepository) IsDuplicate(ctx context.Context, subscriptionID, meterID string, periodID uint64, uniqueHash string) (bool, error) {
	query := `
		SELECT 1 
		FROM events_processed 
		WHERE subscription_id = ? 
		AND meter_id = ? 
		AND period_id = ? 
		AND unique_hash = ? 
		LIMIT 1
	`

	var exists int
	err := r.store.GetConn().QueryRow(ctx, query, subscriptionID, meterID, periodID, uniqueHash).Scan(&exists)
	if err != nil {
		// If no rows, it means no duplicate
		if err.Error() == "sql: no rows in result set" {
			return false, nil
		}
		return false, ierr.WithError(err).
			WithHint("Failed to check for duplicate event").
			Mark(ierr.ErrDatabase)
	}

	return exists == 1, nil
}

// GetLineItemUsage gets the current usage amounts for a subscription line item in a period
func (r *ProcessedEventRepository) GetLineItemUsage(ctx context.Context, subLineItemID string, periodID uint64) (qty decimal.Decimal, freeUnits decimal.Decimal, err error) {
	query := `
		SELECT 
			sumMerge(qty_state) AS qty_billable,
			sumMerge(free_state) AS qty_free
		FROM agg_usage_period_totals
		WHERE sub_line_item_id = ?
		AND period_id = ?
	`

	err = r.store.GetConn().QueryRow(ctx, query, subLineItemID, periodID).Scan(&qty, &freeUnits)
	if err != nil {
		// If no rows found, return zero values
		if err.Error() == "sql: no rows in result set" {
			return decimal.Zero, decimal.Zero, nil
		}
		return decimal.Zero, decimal.Zero, ierr.WithError(err).
			WithHint("Failed to get line item usage").
			Mark(ierr.ErrDatabase)
	}

	return qty, freeUnits, nil
}

// GetPeriodCost gets the total cost for a subscription in a billing period
func (r *ProcessedEventRepository) GetPeriodCost(ctx context.Context, tenantID, environmentID, customerID, subscriptionID string, periodID uint64) (decimal.Decimal, error) {
	query := `
		SELECT sumMerge(cost_state) AS cost 
		FROM agg_usage_period_totals
		WHERE tenant_id = ?
		AND environment_id = ?
		AND customer_id = ?
		AND subscription_id = ?
		AND period_id = ?
	`

	var cost decimal.Decimal
	err := r.store.GetConn().QueryRow(ctx, query, tenantID, environmentID, customerID, subscriptionID, periodID).Scan(&cost)
	if err != nil {
		// If no rows found, return zero
		if err.Error() == "sql: no rows in result set" {
			return decimal.Zero, nil
		}
		return decimal.Zero, ierr.WithError(err).
			WithHint("Failed to get period cost").
			Mark(ierr.ErrDatabase)
	}

	return cost, nil
}

// GetPeriodFeatureTotals gets usage totals by feature for a subscription in a period
func (r *ProcessedEventRepository) GetPeriodFeatureTotals(ctx context.Context, tenantID, environmentID, customerID, subscriptionID string, periodID uint64) ([]*events.PeriodFeatureTotal, error) {
	query := `
		SELECT 
			feature_id,
			sumMerge(qty_state) AS qty,
			sumMerge(free_state) AS free,
			sumMerge(cost_state) AS cost
		FROM agg_usage_period_totals
		WHERE tenant_id = ?
		AND environment_id = ?
		AND customer_id = ?
		AND subscription_id = ?
		AND period_id = ?
		GROUP BY feature_id
	`

	rows, err := r.store.GetConn().Query(ctx, query, tenantID, environmentID, customerID, subscriptionID, periodID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to query period feature totals").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var results []*events.PeriodFeatureTotal
	for rows.Next() {
		var total events.PeriodFeatureTotal
		err := rows.Scan(&total.FeatureID, &total.Quantity, &total.FreeUnits, &total.Cost)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to scan period feature total").
				Mark(ierr.ErrDatabase)
		}
		results = append(results, &total)
	}

	return results, nil
}

// GetUsageAnalytics gets recent usage analytics for a customer
func (r *ProcessedEventRepository) GetUsageAnalytics(ctx context.Context, tenantID, environmentID, customerID string, lookbackHours int) ([]*events.UsageAnalytic, error) {
	query := `
		SELECT 
			source,
			feature_id,
			sum(cost) AS cost,
			sum(qty_billable) AS usage
		FROM events_processed
		WHERE tenant_id = ?
		AND environment_id = ?
		AND customer_id = ?
		AND timestamp >= now64(3) - INTERVAL ? HOUR
		GROUP BY source, feature_id
		ORDER BY cost DESC
		LIMIT 100
	`

	rows, err := r.store.GetConn().Query(ctx, query, tenantID, environmentID, customerID, lookbackHours)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to query usage analytics").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var results []*events.UsageAnalytic
	for rows.Next() {
		var analytics events.UsageAnalytic
		err := rows.Scan(&analytics.Source, &analytics.FeatureID, &analytics.Cost, &analytics.Usage)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to scan usage analytics").
				Mark(ierr.ErrDatabase)
		}
		results = append(results, &analytics)
	}

	return results, nil
}
