package clickhouse

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"golang.org/x/sync/errgroup"
)

func (r *MeterUsageRepository) GetAnalyticsV2LineUsage(ctx context.Context, params *events.AnalyticsV2LineUsageParams) ([]events.AnalyticsV2LineUsage, error) {
	if params == nil || len(params.Lines) == 0 {
		return nil, nil
	}
	span := StartRepositorySpan(ctx, "meter_usage", "get_analytics_v2_line_usage", map[string]interface{}{
		"lines": len(params.Lines),
	})
	defer FinishSpan(span)

	q := buildLineUsageQuery(params)

	rows, err := r.store.GetConn().Query(q.context(ctx), q.sql.String())
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to query analytics line usage").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	out := make([]events.AnalyticsV2LineUsage, len(params.Lines))
	for rows.Next() {
		var idx uint32
		var quantity, rangeQuantity decimal.Decimal
		var n uint64
		if err := rows.Scan(&idx, &quantity, &rangeQuantity, &n); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).WithHint("Failed to scan analytics line usage").Mark(ierr.ErrDatabase)
		}
		out[idx-1] = events.AnalyticsV2LineUsage{Quantity: quantity, RangeQuantity: rangeQuantity, EventCount: n}
	}
	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to read analytics line usage").Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return out, nil
}

func (r *MeterUsageRepository) RankAnalyticsV2Rows(ctx context.Context, params *events.AnalyticsV2RankParams) (*events.AnalyticsV2RankResult, error) {
	if params == nil || len(params.Lines) == 0 || len(params.GroupBy) == 0 {
		return &events.AnalyticsV2RankResult{}, nil
	}
	if len(params.Rates) != len(params.Lines) {
		return nil, ierr.NewError("rates must align with lines").Mark(ierr.ErrValidation)
	}
	span := StartRepositorySpan(ctx, "meter_usage", "rank_analytics_v2_rows", map[string]interface{}{
		"lines": len(params.Lines), "group_by": params.GroupBy, "column": params.Column,
	})
	defer FinishSpan(span)

	q := buildRankQuery(params)
	rows, err := r.store.GetConn().Query(q.context(ctx), q.sql.String())
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to rank analytics rows").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	result := &events.AnalyticsV2RankResult{}
	keys := len(params.GroupBy)
	for rows.Next() {
		group := make([]string, keys)
		var m, t scanMeasure
		var lines []uint32
		var colCost, colQty, totColCost, totColQty map[string]decimal.Decimal
		var colEvents map[string]uint64
		var total uint64

		dest := make([]interface{}, 0, keys+24)
		for i := range group {
			dest = append(dest, &group[i])
		}
		dest = append(dest, m.targets()...)
		dest = append(dest, &lines, &colCost, &colQty, &colEvents, &total)
		dest = append(dest, t.targets()...)
		dest = append(dest, &totColCost, &totColQty)
		if err := rows.Scan(dest...); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).WithHint("Failed to scan analytics rows").Mark(ierr.ErrDatabase)
		}

		row := events.AnalyticsV2Row{
			Group:       group,
			Measure:     m.measure(),
			LineIndexes: lo.Map(lines, func(i uint32, _ int) int { return int(i) - 1 }),
		}
		sort.Ints(row.LineIndexes)
		if params.Column != "" {
			row.Columns = buildColumns(colCost, colQty, colEvents)
		}
		result.Rows = append(result.Rows, row)
		result.TotalCount = int64(total)
		result.Totals = t.measure()
		if params.Column != "" {
			result.ColumnTotals = buildColumns(totColCost, totColQty, nil)
		}
	}
	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to read analytics rows").Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return result, nil
}

// scanMeasure receives one measure's columns in the order the rank query selects them.
type scanMeasure struct {
	usage, utilized, overage, trueUp, discount, free, quantity decimal.Decimal
	events                                                     uint64
}

func (m *scanMeasure) targets() []interface{} {
	return []interface{}{&m.usage, &m.utilized, &m.overage, &m.trueUp, &m.discount, &m.free, &m.quantity, &m.events}
}

func (m *scanMeasure) measure() events.AnalyticsV2Measure {
	return events.AnalyticsV2Measure{
		Amounts: events.AnalyticsV2Amounts{
			Usage: m.usage, CommitmentUtilized: m.utilized, Overage: m.overage,
			TrueUp: m.trueUp, Discount: m.discount, Free: m.free,
		},
		Quantity:   m.quantity,
		EventCount: m.events,
	}
}

// buildColumns builds column measures; a column's amounts hold its cost as Usage.
func buildColumns(cost, quantity map[string]decimal.Decimal, eventCounts map[string]uint64) map[string]events.AnalyticsV2Measure {
	out := make(map[string]events.AnalyticsV2Measure, len(cost))
	for name, c := range cost {
		out[name] = events.AnalyticsV2Measure{
			Amounts:    events.AnalyticsV2Amounts{Usage: c},
			Quantity:   quantity[name],
			EventCount: eventCounts[name],
		}
	}
	return out
}

func (r *MeterUsageRepository) GetAnalyticsV2Detail(ctx context.Context, params *events.AnalyticsV2DetailParams) (*events.AnalyticsV2Detail, error) {
	detail := &events.AnalyticsV2Detail{Distinct: make([]map[string]events.AnalyticsV2Distinct, len(params.Rows))}
	// With no page rows (offset past the end) only the all-rows points are read.
	if len(params.Rank.Lines) == 0 || (len(params.Rows) == 0 && params.WindowSize == "") {
		return detail, nil
	}
	span := StartRepositorySpan(ctx, "meter_usage", "get_analytics_v2_detail", map[string]interface{}{
		"rows": len(params.Rows), "window_size": params.WindowSize, "distinct_keys": params.DistinctKeys,
	})
	defer FinishSpan(span)

	// Points and distinct metrics are independent reads of the same events; run them side by side.
	g, gctx := errgroup.WithContext(ctx)
	if params.WindowSize != "" {
		g.Go(func() error {
			points, err := r.queryPoints(gctx, params)
			detail.Points = points
			return err
		})
	}
	if len(params.DistinctKeys) > 0 && len(params.Rows) > 0 {
		g.Go(func() error { return r.queryDistinct(gctx, params, detail.Distinct) })
	}
	if err := g.Wait(); err != nil {
		SetSpanError(span, err)
		return nil, err
	}
	SetSpanSuccess(span)
	return detail, nil
}

// queryPoints returns priced (page row, window, column) points, plus the same points summed over every
// matching row (Row -1), using grouping sets. Rates are applied in ClickHouse, so the result has no line
// dimension and its size does not grow with the number of meters.
func (r *MeterUsageRepository) queryPoints(ctx context.Context, params *events.AnalyticsV2DetailParams) ([]events.AnalyticsV2PointCell, error) {
	q := buildPointsQuery(params)

	rows, err := r.store.GetConn().Query(q.context(ctx), q.sql.String())
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to query analytics points").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var cells []events.AnalyticsV2PointCell
	for rows.Next() {
		var pageRow uint32
		var w time.Time
		var col string
		var a events.AnalyticsV2Amounts
		var quantity decimal.Decimal
		var n uint64
		if err := rows.Scan(&pageRow, &w, &col, &a.Usage, &a.CommitmentUtilized, &a.Overage, &a.TrueUp, &a.Discount,
			&quantity, &n); err != nil {
			return nil, ierr.WithError(err).WithHint("Failed to scan analytics points").Mark(ierr.ErrDatabase)
		}
		cells = append(cells, events.AnalyticsV2PointCell{
			Row: int(pageRow) - 1, WindowStart: w.UTC(), Column: col,
			Amounts: a, Quantity: quantity, EventCount: n,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to read analytics points").Mark(ierr.ErrDatabase)
	}
	return cells, nil
}

// queryDistinct fills each page row's distinct count per key, with values when there are few.
func (r *MeterUsageRepository) queryDistinct(ctx context.Context, params *events.AnalyticsV2DetailParams, out []map[string]events.AnalyticsV2Distinct) error {
	q := buildDistinctQuery(params)

	rows, err := r.store.GetConn().Query(q.context(ctx), q.sql.String())
	if err != nil {
		return ierr.WithError(err).WithHint("Failed to query analytics metrics").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()
	for rows.Next() {
		var pageRow uint64
		counts := make([]uint64, len(params.DistinctKeys))
		values := make([][]string, len(params.DistinctKeys))
		dest := []interface{}{&pageRow}
		for i := range params.DistinctKeys {
			dest = append(dest, &counts[i], &values[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return ierr.WithError(err).WithHint("Failed to scan analytics metrics").Mark(ierr.ErrDatabase)
		}
		m := make(map[string]events.AnalyticsV2Distinct, len(params.DistinctKeys))
		for i, k := range params.DistinctKeys {
			d := events.AnalyticsV2Distinct{Count: counts[i]}
			if counts[i] <= distinctSampleSize {
				d.Values = values[i]
				sort.Strings(d.Values)
			}
			m[k] = d
		}
		out[pageRow-1] = m
	}
	if err := rows.Err(); err != nil {
		return ierr.WithError(err).WithHint("Failed to read analytics metrics").Mark(ierr.ErrDatabase)
	}
	return nil
}
