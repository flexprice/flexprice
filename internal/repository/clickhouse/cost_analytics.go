package clickhouse

import (
	"context"
	"sort"

	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/costanalytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/shopspring/decimal"
)

// costAnalyticsRepository implements costanalytics.Repository on meter_usage.
type costAnalyticsRepository struct {
	store  *clickhouse.ClickHouseStore
	logger *logger.Logger
}

func NewCostAnalyticsRepository(store *clickhouse.ClickHouseStore, logger *logger.Logger) costanalytics.Repository {
	return &costAnalyticsRepository{store: store, logger: logger}
}

func (r *costAnalyticsRepository) GetCostBreakdown(ctx context.Context, params *costanalytics.QueryParams) (*costanalytics.Result, error) {
	if params == nil || params.Query == nil {
		return nil, ierr.NewError("params and query are required").Mark(ierr.ErrValidation)
	}
	if err := params.Query.Validate(); err != nil {
		return nil, err
	}
	if len(params.Lines) == 0 {
		return &costanalytics.Result{Items: []costanalytics.Row{}, Totals: emptyCostRow()}, nil
	}

	span := StartRepositorySpan(ctx, "meter_usage", "get_cost_breakdown", map[string]interface{}{
		"line_windows": len(params.Lines),
		"group_by":     len(params.Query.GroupBy),
		"deduplicate":  params.Query.Deduplicate,
	})
	defer FinishSpan(span)

	result, err := r.run(ctx, params)
	if err != nil {
		SetSpanError(span, err)
		return nil, err
	}

	// Totals ride on the page rows; a page past the last row has none, so read them from the first row.
	if len(result.Items) == 0 && params.Query.Offset > 0 {
		q := *params.Query
		q.Limit, q.Offset = 1, 0
		first, err := r.run(ctx, &costanalytics.QueryParams{
			TenantID: params.TenantID, EnvironmentID: params.EnvironmentID, Query: &q, Lines: params.Lines,
		})
		if err != nil {
			SetSpanError(span, err)
			return nil, err
		}
		result.Totals, result.TotalCount = first.Totals, first.TotalCount
	}

	SetSpanSuccess(span)
	return result, nil
}

func (r *costAnalyticsRepository) run(ctx context.Context, params *costanalytics.QueryParams) (*costanalytics.Result, error) {
	q := params.Query
	built := buildCostBreakdownSQL(params)

	rows, err := r.store.GetConn().Query(ctx, built.query, built.args...)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to query cost breakdown").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	result := &costanalytics.Result{Items: []costanalytics.Row{}, Totals: emptyCostRow()}
	for rows.Next() {
		var (
			group                   = make([]string, len(q.GroupBy))
			cost, qty               string
			events                  uint64
			columns                 map[string]string
			metricCounts            = make([]uint64, len(q.Metrics))
			metricValues            = make([][]string, len(q.Metrics))
			totalCount, totalEvents uint64
			totalCost, totalQty     string
			totalColumns            map[string]string
		)

		targets := make([]any, 0, len(group)+12+2*len(q.Metrics))
		for i := range group {
			targets = append(targets, &group[i])
		}
		targets = append(targets, &cost, &qty, &events)
		if q.Columns != nil {
			targets = append(targets, &columns)
		}
		for i := range q.Metrics {
			targets = append(targets, &metricCounts[i], &metricValues[i])
		}
		targets = append(targets, &totalCount, &totalCost, &totalQty, &totalEvents)
		if q.Columns != nil {
			targets = append(targets, &totalColumns)
		}

		if err := rows.Scan(targets...); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to scan cost breakdown row").
				Mark(ierr.ErrDatabase)
		}

		var parseErr error
		dec := func(v string) decimal.Decimal {
			d, err := decimal.NewFromString(v)
			if err != nil && parseErr == nil {
				parseErr = err
			}
			return d
		}
		decMap := func(m map[string]string) map[string]decimal.Decimal {
			if m == nil {
				return nil
			}
			out := make(map[string]decimal.Decimal, len(m))
			for k, v := range m {
				out[k] = dec(v)
			}
			return out
		}

		row := costanalytics.Row{
			Group:      group,
			Cost:       dec(cost),
			Quantity:   dec(qty),
			EventCount: events,
			Columns:    decMap(columns),
			Metrics:    make(map[string]costanalytics.MetricValue, len(q.Metrics)),
		}
		for i, m := range q.Metrics {
			v := costanalytics.MetricValue{Count: metricCounts[i]}
			if metricCounts[i] <= costanalytics.MaxDistinctValues {
				v.Values = metricValues[i]
				sort.Strings(v.Values)
			}
			row.Metrics[m.Name()] = v
		}
		totals := costanalytics.Row{
			Cost:       dec(totalCost),
			Quantity:   dec(totalQty),
			EventCount: totalEvents,
			Columns:    decMap(totalColumns),
		}
		if parseErr != nil {
			return nil, ierr.WithError(parseErr).
				WithHint("Failed to parse cost breakdown amounts").
				Mark(ierr.ErrDatabase)
		}
		result.Items = append(result.Items, row)
		result.Totals, result.TotalCount = totals, totalCount
	}
	if err := rows.Err(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to read cost breakdown rows").
			Mark(ierr.ErrDatabase)
	}
	return result, nil
}

func emptyCostRow() costanalytics.Row {
	return costanalytics.Row{Cost: decimal.Zero, Quantity: decimal.Zero}
}
