package clickhouse

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/usageanalytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/shopspring/decimal"
)

// usageQueryRepository implements usageanalytics.Repository on meter_usage.
type usageQueryRepository struct {
	store  *clickhouse.ClickHouseStore
	logger *logger.Logger
}

func NewUsageQueryRepository(store *clickhouse.ClickHouseStore, logger *logger.Logger) usageanalytics.Repository {
	return &usageQueryRepository{store: store, logger: logger}
}

func (r *usageQueryRepository) GetBreakdown(ctx context.Context, params *usageanalytics.QueryParams) (*usageanalytics.Result, error) {
	if params == nil || params.Query == nil {
		return nil, ierr.NewError("params and query are required").Mark(ierr.ErrValidation)
	}
	if err := params.Query.Validate(); err != nil {
		return nil, err
	}
	if len(params.Lines) == 0 {
		return &usageanalytics.Result{Items: []usageanalytics.Row{}, Totals: emptyUsageRow()}, nil
	}

	span := StartRepositorySpan(ctx, "meter_usage", "get_usage_breakdown", map[string]interface{}{
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
		first, err := r.run(ctx, &usageanalytics.QueryParams{
			TenantID: params.TenantID, EnvironmentID: params.EnvironmentID, Query: &q, Lines: params.Lines,
		})
		if err != nil {
			SetSpanError(span, err)
			return nil, err
		}
		result.Totals, result.TotalCount = first.Totals, first.TotalCount
	}

	if params.Query.WindowSize != "" && len(result.Items) > 0 {
		if err := r.addPoints(ctx, params, result); err != nil {
			SetSpanError(span, err)
			return nil, err
		}
	}

	SetSpanSuccess(span)
	return result, nil
}

func (r *usageQueryRepository) run(ctx context.Context, params *usageanalytics.QueryParams) (*usageanalytics.Result, error) {
	q := params.Query
	built := buildBreakdownSQL(params)

	rows, err := r.store.GetConn().Query(ctx, built.query, built.args...)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to query usage breakdown").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	result := &usageanalytics.Result{Items: []usageanalytics.Row{}, Totals: emptyUsageRow()}
	for rows.Next() {
		var (
			group                                = make([]string, len(q.GroupBy))
			amount, subtotal, qty                string
			events                               uint64
			columns                              map[string]string
			lineIDs                              []string
			metricCounts                         = make([]uint64, len(q.Metrics))
			metricValues                         = make([][]string, len(q.Metrics))
			totalCount, totalEvents              uint64
			totalAmount, totalSubtotal, totalQty string
			totalColumns                         map[string]string
		)

		targets := make([]any, 0, len(group)+12+2*len(q.Metrics))
		for i := range group {
			targets = append(targets, &group[i])
		}
		targets = append(targets, &amount, &subtotal, &qty, &events)
		if q.Columns != nil {
			targets = append(targets, &columns)
		}
		if q.WithLineItemIDs {
			targets = append(targets, &lineIDs)
		}
		for i := range q.Metrics {
			targets = append(targets, &metricCounts[i], &metricValues[i])
		}
		targets = append(targets, &totalCount, &totalAmount, &totalSubtotal, &totalQty, &totalEvents)
		if q.Columns != nil {
			targets = append(targets, &totalColumns)
		}

		if err := rows.Scan(targets...); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to scan usage breakdown row").
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

		row := usageanalytics.Row{
			Group:      group,
			Amount:     dec(amount),
			Subtotal:   dec(subtotal),
			Quantity:   dec(qty),
			EventCount: events,
			Columns:    decMap(columns),
			Metrics:    make(map[string]usageanalytics.MetricValue, len(q.Metrics)),
		}
		if q.WithLineItemIDs {
			sort.Strings(lineIDs)
			row.LineItemIDs = lineIDs
		}
		for i, m := range q.Metrics {
			v := usageanalytics.MetricValue{Count: metricCounts[i]}
			if metricCounts[i] <= usageanalytics.MaxDistinctValues {
				v.Values = metricValues[i]
				sort.Strings(v.Values)
			}
			row.Metrics[m.Name()] = v
		}
		totals := usageanalytics.Row{
			Amount:     dec(totalAmount),
			Subtotal:   dec(totalSubtotal),
			Quantity:   dec(totalQty),
			EventCount: totalEvents,
			Columns:    decMap(totalColumns),
		}
		if parseErr != nil {
			return nil, ierr.WithError(parseErr).
				WithHint("Failed to parse usage breakdown amounts").
				Mark(ierr.ErrDatabase)
		}
		result.Items = append(result.Items, row)
		result.Totals, result.TotalCount = totals, totalCount
	}
	if err := rows.Err(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to read usage breakdown rows").
			Mark(ierr.ErrDatabase)
	}
	return result, nil
}

// addPoints reads amount per window for the page rows.
func (r *usageQueryRepository) addPoints(ctx context.Context, params *usageanalytics.QueryParams, result *usageanalytics.Result) error {
	q := params.Query
	groupID := func(group []string) string { b, _ := json.Marshal(group); return string(b) } // injective, unlike a separator
	rowByGroup := make(map[string]*usageanalytics.Row, len(result.Items))
	pageGroups := make([][]string, 0, len(result.Items))
	for i := range result.Items {
		rowByGroup[groupID(result.Items[i].Group)] = &result.Items[i]
		pageGroups = append(pageGroups, result.Items[i].Group)
	}

	built := buildPointsSQL(params, pageGroups)
	rows, err := r.store.GetConn().Query(ctx, built.query, built.args...)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to query amount over time").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			group                         = make([]string, len(q.GroupBy))
			window, amount, subtotal, qty string
		)
		targets := make([]any, 0, len(group)+4)
		for i := range group {
			targets = append(targets, &group[i])
		}
		targets = append(targets, &window, &amount, &subtotal, &qty)
		if err := rows.Scan(targets...); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to scan amount over time").
				Mark(ierr.ErrDatabase)
		}

		day, err := time.Parse(time.DateOnly, window)
		if err != nil {
			return ierr.WithError(err).WithHint("Failed to parse amount window").Mark(ierr.ErrDatabase)
		}
		start := usageanalytics.DateStart(day.Year(), day.Month(), day.Day(), q.Timezone)
		c, err := decimal.NewFromString(amount)
		if err != nil {
			return ierr.WithError(err).WithHint("Failed to parse amount over time").Mark(ierr.ErrDatabase)
		}
		sub, err := decimal.NewFromString(subtotal)
		if err != nil {
			return ierr.WithError(err).WithHint("Failed to parse subtotal over time").Mark(ierr.ErrDatabase)
		}
		n, err := decimal.NewFromString(qty)
		if err != nil {
			return ierr.WithError(err).WithHint("Failed to parse usage over time").Mark(ierr.ErrDatabase)
		}

		target := rowByGroup[groupID(group)]
		if target == nil {
			continue
		}
		target.Points = append(target.Points, usageanalytics.Point{WindowStart: start, Amount: c, Subtotal: sub, Quantity: n})
	}
	if err := rows.Err(); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to read amount over time").
			Mark(ierr.ErrDatabase)
	}

	for i := range result.Items {
		sortPoints(result.Items[i].Points)
	}
	return nil
}

func sortPoints(points []usageanalytics.Point) {
	sort.Slice(points, func(i, j int) bool { return points[i].WindowStart.Before(points[j].WindowStart) })
}

func emptyUsageRow() usageanalytics.Row {
	return usageanalytics.Row{Amount: decimal.Zero, Subtotal: decimal.Zero, Quantity: decimal.Zero}
}
