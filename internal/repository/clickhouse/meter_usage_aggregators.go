// Package clickhouse provides meter_usage aggregators following the same strategy pattern
// as the events query engine (aggregators.go).
//
// Each aggregation type (SUM, COUNT, COUNT_UNIQUE, MAX, AVG, LATEST) is implemented as
// a separate struct conforming to the MeterUsageAggregator interface. This allows each
// type to own its SQL generation logic, making it easy to add new aggregation types
// without modifying existing code.
//
// Unlike the events aggregators that read from JSON properties, meter_usage aggregators
// operate on the pre-extracted qty_total column, making queries simpler and faster.
package clickhouse

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
)

// MeterUsageAggregator defines the strategy interface for meter_usage aggregations.
// Each aggregation type implements this to generate its specific SQL query.
type MeterUsageAggregator interface {
	// GetQuery returns the full SQL query for this aggregation type
	GetQuery(ctx context.Context, params *events.MeterUsageQueryParams, qb *MeterUsageQueryBuilder) string

	// GetType returns the aggregation type this aggregator handles
	GetType() types.AggregationType
}

// GetMeterUsageAggregator returns the appropriate aggregator for the given type
func GetMeterUsageAggregator(aggType types.AggregationType) MeterUsageAggregator {
	switch aggType {
	case types.AggregationSum, types.AggregationSumWithMultiplier, types.AggregationWeightedSum:
		return &MeterUsageSumAggregator{}
	case types.AggregationCount:
		return &MeterUsageCountAggregator{}
	case types.AggregationCountUnique:
		return &MeterUsageCountUniqueAggregator{}
	case types.AggregationMax:
		return &MeterUsageMaxAggregator{}
	case types.AggregationAvg:
		return &MeterUsageAvgAggregator{}
	case types.AggregationLatest:
		return &MeterUsageLatestAggregator{}
	default:
		return &MeterUsageSumAggregator{}
	}
}

// --- SUM aggregator ---

type MeterUsageSumAggregator struct{}

func (a *MeterUsageSumAggregator) GetType() types.AggregationType {
	return types.AggregationSum
}

func (a *MeterUsageSumAggregator) GetQuery(ctx context.Context, params *events.MeterUsageQueryParams, qb *MeterUsageQueryBuilder) string {
	return qb.BuildQuery("SUM(qty_total)", "COUNT(DISTINCT id)", params)
}

// --- COUNT aggregator ---

type MeterUsageCountAggregator struct{}

func (a *MeterUsageCountAggregator) GetType() types.AggregationType {
	return types.AggregationCount
}

func (a *MeterUsageCountAggregator) GetQuery(ctx context.Context, params *events.MeterUsageQueryParams, qb *MeterUsageQueryBuilder) string {
	return qb.BuildQuery("COUNT(DISTINCT id)", "COUNT(DISTINCT id)", params)
}

// --- COUNT_UNIQUE aggregator ---

type MeterUsageCountUniqueAggregator struct{}

func (a *MeterUsageCountUniqueAggregator) GetType() types.AggregationType {
	return types.AggregationCountUnique
}

func (a *MeterUsageCountUniqueAggregator) GetQuery(ctx context.Context, params *events.MeterUsageQueryParams, qb *MeterUsageQueryBuilder) string {
	// COUNT_UNIQUE needs the unique_hash != '' filter applied via the query builder
	return qb.BuildQuery("COUNT(DISTINCT unique_hash)", "COUNT(DISTINCT id)", params)
}

// --- MAX aggregator ---

type MeterUsageMaxAggregator struct{}

func (a *MeterUsageMaxAggregator) GetType() types.AggregationType {
	return types.AggregationMax
}

func (a *MeterUsageMaxAggregator) GetQuery(ctx context.Context, params *events.MeterUsageQueryParams, qb *MeterUsageQueryBuilder) string {
	return qb.BuildQuery("MAX(qty_total)", "COUNT(DISTINCT id)", params)
}

// --- AVG aggregator ---

type MeterUsageAvgAggregator struct{}

func (a *MeterUsageAvgAggregator) GetType() types.AggregationType {
	return types.AggregationAvg
}

func (a *MeterUsageAvgAggregator) GetQuery(ctx context.Context, params *events.MeterUsageQueryParams, qb *MeterUsageQueryBuilder) string {
	return qb.BuildQuery("AVG(qty_total)", "COUNT(DISTINCT id)", params)
}

// --- LATEST aggregator ---

type MeterUsageLatestAggregator struct{}

func (a *MeterUsageLatestAggregator) GetType() types.AggregationType {
	return types.AggregationLatest
}

func (a *MeterUsageLatestAggregator) GetQuery(ctx context.Context, params *events.MeterUsageQueryParams, qb *MeterUsageQueryBuilder) string {
	return qb.BuildQuery("argMax(qty_total, timestamp)", "COUNT(DISTINCT id)", params)
}

// --- Multi-meter query builder (groups by meter_id) ---

// BuildMultiMeterQuery generates a query that groups results by meter_id.
// Used by GetUsageMultiMeter to query multiple meters in a single round-trip.
func BuildMultiMeterQuery(aggExpr, countExpr string, params *events.MeterUsageQueryParams, qb *MeterUsageQueryBuilder) string {
	windowExpr := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor)
	where, _ := qb.BuildWhereClause(params)
	finalClause, settings := qb.BuildFinalClause(params.UseFinal)

	if windowExpr != "" {
		return fmt.Sprintf(`
			SELECT
				meter_id,
				%s AS window_start,
				%s AS value,
				%s AS event_count
			FROM meter_usage %s
			WHERE %s
			GROUP BY meter_id, window_start
			ORDER BY meter_id, window_start ASC
			%s
		`, windowExpr, aggExpr, countExpr, finalClause, where, settings)
	}

	return fmt.Sprintf(`
		SELECT
			meter_id,
			%s AS value,
			%s AS event_count
		FROM meter_usage %s
		WHERE %s
		GROUP BY meter_id
		%s
	`, aggExpr, countExpr, finalClause, where, settings)
}
