package testutil

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// analyticsV2Visible returns the scope's records: received by DataThrough and, with UseFinal,
// one record per (meter, id) keeping the latest received, as ReplacingMergeTree FINAL does.
func (s *InMemoryMeterUsageStore) analyticsV2Visible(scope events.AnalyticsV2Scope) []*events.MeterUsage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	latest := map[string]*events.MeterUsage{}
	var out []*events.MeterUsage
	for _, r := range s.records {
		if r.TenantID != scope.TenantID || r.EnvironmentID != scope.EnvironmentID ||
			!lo.Contains(scope.ExternalCustomerIDs, r.ExternalCustomerID) || r.IngestedAt.After(scope.DataThrough) {
			continue
		}
		if !scope.UseFinal {
			out = append(out, r)
			continue
		}
		key := r.MeterID + "|" + r.ID
		if prev, ok := latest[key]; !ok || r.IngestedAt.After(prev.IngestedAt) {
			latest[key] = r
		}
	}
	if scope.UseFinal {
		out = lo.Values(latest)
	}
	return out
}

func inLine(r *events.MeterUsage, line events.AnalyticsV2Line) bool {
	return r.MeterID == line.MeterID && !r.Timestamp.Before(line.Start) && r.Timestamp.Before(line.End)
}

func (s *InMemoryMeterUsageStore) GetAnalyticsV2LineUsage(_ context.Context, params *events.AnalyticsV2LineUsageParams) ([]events.AnalyticsV2LineUsage, error) {
	if params == nil || len(params.Lines) == 0 {
		return nil, nil
	}
	out := make([]events.AnalyticsV2LineUsage, len(params.Lines))
	for _, r := range s.analyticsV2Visible(params.Scope) {
		for i, line := range params.Lines {
			if inLine(r, line) {
				out[i].Quantity = out[i].Quantity.Add(r.QtyTotal)
				out[i].EventCount++
				if !r.Timestamp.Before(params.RangeStart) && r.Timestamp.Before(params.RangeEnd) {
					out[i].RangeQuantity = out[i].RangeQuantity.Add(r.QtyTotal)
				}
			}
		}
	}
	return out, nil
}

// analyticsV2Key is the record's value for a row, column or filter key; JSON properties that are not strings read as "".
func analyticsV2Key(r *events.MeterUsage, line events.AnalyticsV2Line, key string, p *events.AnalyticsV2RankParams, loc *time.Location) string {
	switch {
	case key == "source":
		return r.Source
	case key == "meter_id":
		return r.MeterID
	case key == "sub_line_item_id":
		return line.ID
	case key == "feature_id":
		return p.MeterFeatures[r.MeterID]
	case key == events.AnalyticsV2KeyMonth:
		return r.Timestamp.In(loc).Format("2006-01")
	default:
		v, _ := r.Properties[strings.TrimPrefix(key, "properties.")].(string)
		return v
	}
}

// RankAnalyticsV2Rows is the reference implementation of the ranking rules the ClickHouse query follows.
func (s *InMemoryMeterUsageStore) RankAnalyticsV2Rows(_ context.Context, p *events.AnalyticsV2RankParams) (*events.AnalyticsV2RankResult, error) {
	if p == nil || len(p.Lines) == 0 || len(p.GroupBy) == 0 {
		return &events.AnalyticsV2RankResult{}, nil
	}
	loc, err := time.LoadLocation(lo.Ternary(p.Timezone == "", "UTC", p.Timezone))
	if err != nil {
		return nil, err
	}

	// Per (row, column, line): quantity, peak and event count.
	type cell struct {
		group []string
		col   string
		line  int
		q, pk decimal.Decimal
		n     uint64
	}
	cells := map[string]*cell{}
	for _, r := range s.analyticsV2Visible(p.Scope) {
		if r.Timestamp.Before(p.StartTime) || !r.Timestamp.Before(p.EndTime) {
			continue
		}
		for li, line := range p.Lines {
			if !inLine(r, line) {
				continue
			}
			if lo.SomeBy(p.Filters, func(f events.AnalyticsV2Filter) bool {
				return lo.Contains(f.Values, analyticsV2Key(r, line, f.Key, p, loc)) == f.Exclude
			}) {
				continue
			}
			group := lo.Map(p.GroupBy, func(k string, _ int) string { return analyticsV2Key(r, line, k, p, loc) })
			col := ""
			if p.Column != "" {
				col = analyticsV2Key(r, line, p.Column, p, loc)
			}
			k := fmt.Sprintf("%s\x01%s\x01%d", strings.Join(group, "\x00"), col, li)
			c := cells[k]
			if c == nil {
				c = &cell{group: group, col: col, line: li, pk: r.QtyTotal}
				cells[k] = c
			}
			c.q = c.q.Add(r.QtyTotal)
			c.pk = decimal.Max(c.pk, r.QtyTotal)
			c.n++
		}
	}

	// MAX lines: a row is priced by its peak, split across columns by column peak.
	rowLine := func(c *cell) string { return fmt.Sprintf("%s\x01%d", strings.Join(c.group, "\x00"), c.line) }
	rowPeak, colPeakSum := map[string]decimal.Decimal{}, map[string]decimal.Decimal{}
	for _, c := range cells {
		rl := rowLine(c)
		rowPeak[rl] = decimal.Max(rowPeak[rl], c.pk)
		colPeakSum[rl] = colPeakSum[rl].Add(c.pk)
	}

	type row struct {
		group   []string
		m       events.AnalyticsV2Measure
		cols    map[string]events.AnalyticsV2Measure
		lines   map[int]bool
		sortCol decimal.Decimal
	}
	rows := map[string]*row{}
	for _, c := range cells {
		gk := strings.Join(c.group, "\x00")
		a := rows[gk]
		if a == nil {
			a = &row{group: c.group, cols: map[string]events.AnalyticsV2Measure{}, lines: map[int]bool{}}
			rows[gk] = a
		}
		line, rate := p.Lines[c.line], p.Rates[c.line]
		amount := func(v decimal.Decimal) decimal.Decimal {
			if !line.IsMax {
				return c.q.Mul(v)
			}
			rl := rowLine(c)
			if colPeakSum[rl].IsZero() {
				return decimal.Zero
			}
			return v.Mul(rowPeak[rl]).Mul(c.pk).Div(colPeakSum[rl])
		}
		am := events.AnalyticsV2Amounts{
			Usage: amount(rate.Usage), CommitmentUtilized: amount(rate.CommitmentUtilized), Overage: amount(rate.Overage),
			TrueUp: amount(rate.TrueUp), Discount: amount(rate.Discount), Free: amount(rate.Free),
		}
		addAnalyticsV2Amounts(&a.m.Amounts, am)
		col := a.cols[c.col]
		col.Amounts.Usage = col.Amounts.Usage.Add(am.Cost())
		if line.IsMax {
			a.m.Peak = decimal.Max(a.m.Peak, c.pk)
			col.Peak = decimal.Max(col.Peak, c.pk)
		} else {
			a.m.Quantity = a.m.Quantity.Add(c.q)
			col.Quantity = col.Quantity.Add(c.q)
		}
		a.m.EventCount += c.n
		col.EventCount += c.n
		a.cols[c.col] = col
		a.lines[c.line] = true
		if p.Sort.By == events.AnalyticsV2SortByColumn && c.col == p.Sort.Column {
			a.sortCol = a.sortCol.Add(am.Cost())
		}
	}

	var list []*row
	for _, a := range rows {
		if p.Search == "" || strings.Contains(strings.ToLower(strings.Join(a.group, " ")), strings.ToLower(p.Search)) {
			list = append(list, a)
		}
	}
	sortKey := func(a *row) decimal.Decimal {
		switch p.Sort.By {
		case events.AnalyticsV2SortByColumn:
			return a.sortCol
		case events.AnalyticsV2SortByUsage:
			return a.m.Quantity.Add(a.m.Peak)
		case events.AnalyticsV2SortByEventCount:
			return decimal.NewFromInt(int64(a.m.EventCount))
		default:
			return a.m.Amounts.Cost()
		}
	}
	less := func(x, y []string) bool {
		for i := range x {
			if x[i] != y[i] {
				return x[i] < y[i]
			}
		}
		return false
	}
	sort.SliceStable(list, func(i, j int) bool {
		gi, gj := list[i].group, list[j].group
		if p.Sort.By == events.AnalyticsV2SortByName {
			return lo.Ternary(p.Sort.Desc, less(gj, gi), less(gi, gj))
		}
		ki, kj := sortKey(list[i]).Round(9), sortKey(list[j]).Round(9)
		if !ki.Equal(kj) {
			return lo.Ternary(p.Sort.Desc, ki.GreaterThan(kj), ki.LessThan(kj))
		}
		return less(gi, gj)
	})

	out := &events.AnalyticsV2RankResult{TotalCount: int64(len(list))}
	if p.Column != "" {
		out.ColumnTotals = map[string]events.AnalyticsV2Measure{}
	}
	for _, a := range list {
		addAnalyticsV2Amounts(&out.Totals.Amounts, a.m.Amounts)
		out.Totals.Quantity = out.Totals.Quantity.Add(a.m.Quantity)
		out.Totals.Peak = decimal.Max(out.Totals.Peak, a.m.Peak)
		out.Totals.EventCount += a.m.EventCount
		for k, c := range a.cols {
			if out.ColumnTotals == nil {
				continue
			}
			t := out.ColumnTotals[k]
			t.Amounts.Usage = t.Amounts.Usage.Add(c.Amounts.Usage)
			t.Quantity = t.Quantity.Add(c.Quantity)
			t.Peak = decimal.Max(t.Peak, c.Peak)
			out.ColumnTotals[k] = t
		}
	}
	for i := p.Offset; i < len(list) && i < p.Offset+p.Limit; i++ {
		a := list[i]
		r := events.AnalyticsV2Row{Group: a.group, Measure: a.m, LineIndexes: lo.Keys(a.lines)}
		sort.Ints(r.LineIndexes)
		if p.Column != "" {
			r.Columns = a.cols
		}
		out.Rows = append(out.Rows, r)
	}
	return out, nil
}

func addAnalyticsV2Amounts(dst *events.AnalyticsV2Amounts, a events.AnalyticsV2Amounts) {
	dst.Usage = dst.Usage.Add(a.Usage)
	dst.CommitmentUtilized = dst.CommitmentUtilized.Add(a.CommitmentUtilized)
	dst.Overage = dst.Overage.Add(a.Overage)
	dst.TrueUp = dst.TrueUp.Add(a.TrueUp)
	dst.Discount = dst.Discount.Add(a.Discount)
	dst.Free = dst.Free.Add(a.Free)
}

func (s *InMemoryMeterUsageStore) GetAnalyticsV2LinePeaks(_ context.Context, p *events.AnalyticsV2LinePeakParams) ([]decimal.Decimal, error) {
	out := make([]decimal.Decimal, len(p.Lines))
	keyParams := &events.AnalyticsV2RankParams{MeterFeatures: p.MeterFeatures}
	rowPeak := map[string]decimal.Decimal{}
	for _, r := range s.analyticsV2Visible(p.Scope) {
		if r.Timestamp.Before(p.StartTime) || !r.Timestamp.Before(p.EndTime) {
			continue
		}
		for li, line := range p.Lines {
			if !line.IsMax || !inLine(r, line) {
				continue
			}
			group := lo.Map(p.GroupBy, func(k string, _ int) string { return analyticsV2Key(r, line, k, keyParams, time.UTC) })
			k := fmt.Sprintf("%s\x01%d", strings.Join(group, "\x00"), li)
			if prev, ok := rowPeak[k]; !ok || r.QtyTotal.GreaterThan(prev) {
				rowPeak[k] = r.QtyTotal
			}
		}
	}
	for k, pk := range rowPeak {
		var li int
		fmt.Sscan(k[strings.LastIndex(k, "\x01")+1:], &li)
		out[li] = out[li].Add(pk)
	}
	return out, nil
}

// GetAnalyticsV2Detail is the reference for page points and distinct metrics. Windows are bucketed in UTC.
func (s *InMemoryMeterUsageStore) GetAnalyticsV2Detail(_ context.Context, params *events.AnalyticsV2DetailParams) (*events.AnalyticsV2Detail, error) {
	p := &params.Rank
	detail := &events.AnalyticsV2Detail{Distinct: make([]map[string]events.AnalyticsV2Distinct, len(params.Rows))}
	if len(params.Rows) == 0 && params.WindowSize == "" {
		return detail, nil
	}
	page := map[string]int{}
	for i, row := range params.Rows {
		page[strings.Join(row, "\x1f")] = i
	}

	type cell struct {
		rk    string
		w     time.Time
		col   string
		line  int
		q, pk decimal.Decimal
		n     uint64
	}
	cells := map[string]*cell{}
	distinct := make([]map[string]map[string]bool, len(params.Rows))
	for _, r := range s.analyticsV2Visible(p.Scope) {
		if r.Timestamp.Before(p.StartTime) || !r.Timestamp.Before(p.EndTime) {
			continue
		}
		for li, line := range p.Lines {
			if !inLine(r, line) || lo.SomeBy(p.Filters, func(f events.AnalyticsV2Filter) bool {
				return lo.Contains(f.Values, analyticsV2Key(r, line, f.Key, p, time.UTC)) == f.Exclude
			}) {
				continue
			}
			group := lo.Map(p.GroupBy, func(k string, _ int) string { return analyticsV2Key(r, line, k, p, time.UTC) })
			rk := strings.Join(group, "\x1f")
			if row, ok := page[rk]; ok {
				if distinct[row] == nil {
					distinct[row] = map[string]map[string]bool{}
				}
				for _, k := range params.DistinctKeys {
					if distinct[row][k] == nil {
						distinct[row][k] = map[string]bool{}
					}
					distinct[row][k][analyticsV2Key(r, line, k, p, time.UTC)] = true
				}
			}
			if params.WindowSize == "" || (p.Search != "" && !strings.Contains(strings.ToLower(strings.Join(group, " ")), strings.ToLower(p.Search))) {
				continue
			}
			col := ""
			if p.Column != "" {
				col = analyticsV2Key(r, line, p.Column, p, time.UTC)
			}
			w := truncateToWindow(r.Timestamp, params.WindowSize, params.BillingAnchor)
			k := fmt.Sprintf("%s\x01%d\x01%s\x01%d", rk, w.Unix(), col, li)
			c := cells[k]
			if c == nil {
				c = &cell{rk: rk, w: w, col: col, line: li, pk: r.QtyTotal}
				cells[k] = c
			}
			c.q, c.pk, c.n = c.q.Add(r.QtyTotal), decimal.Max(c.pk, r.QtyTotal), c.n+1
		}
	}

	for row, keys := range distinct {
		if keys == nil {
			continue
		}
		detail.Distinct[row] = map[string]events.AnalyticsV2Distinct{}
		for k, values := range keys {
			d := events.AnalyticsV2Distinct{Count: uint64(len(values))}
			if len(values) <= 5 {
				d.Values = lo.Keys(values)
				sort.Strings(d.Values)
			}
			detail.Distinct[row][k] = d
		}
	}

	// Peak weight: row peak x cell peak / sum of the row's cell peaks, per (row, line).
	rowLine := func(c *cell) string { return fmt.Sprintf("%s\x01%d", c.rk, c.line) }
	rowPeak, cellPeakSum := map[string]decimal.Decimal{}, map[string]decimal.Decimal{}
	for _, c := range cells {
		rowPeak[rowLine(c)] = decimal.Max(rowPeak[rowLine(c)], c.pk)
		cellPeakSum[rowLine(c)] = cellPeakSum[rowLine(c)].Add(c.pk)
	}
	// Cells are priced per line, then summed per (row, window, column, additive or MAX).
	priced := map[string]*events.AnalyticsV2PointCell{}
	add := func(row int, c *cell, a events.AnalyticsV2Amounts) {
		isMax := p.Lines[c.line].IsMax
		k := fmt.Sprintf("%d\x01%d\x01%s\x01%v", row, c.w.Unix(), c.col, isMax)
		pc := priced[k]
		if pc == nil {
			pc = &events.AnalyticsV2PointCell{Row: row, WindowStart: c.w, Column: c.col, IsMax: isMax, Peak: c.pk}
			priced[k] = pc
		}
		pc.Amounts = events.AnalyticsV2Amounts{
			Usage: pc.Amounts.Usage.Add(a.Usage), CommitmentUtilized: pc.Amounts.CommitmentUtilized.Add(a.CommitmentUtilized),
			Overage: pc.Amounts.Overage.Add(a.Overage), TrueUp: pc.Amounts.TrueUp.Add(a.TrueUp), Discount: pc.Amounts.Discount.Add(a.Discount),
		}
		pc.Quantity, pc.EventCount = pc.Quantity.Add(c.q), pc.EventCount+c.n
		pc.Peak = decimal.Max(pc.Peak, c.pk)
	}
	for _, c := range cells {
		weight := c.q
		if p.Lines[c.line].IsMax {
			weight = decimal.Zero
			if cellPeakSum[rowLine(c)].IsPositive() {
				weight = rowPeak[rowLine(c)].Mul(c.pk).Div(cellPeakSum[rowLine(c)])
			}
		}
		r := p.Rates[c.line]
		a := events.AnalyticsV2Amounts{
			Usage: weight.Mul(r.Usage), CommitmentUtilized: weight.Mul(r.CommitmentUtilized), Overage: weight.Mul(r.Overage),
			TrueUp: weight.Mul(r.TrueUp), Discount: weight.Mul(r.Discount),
		}
		if row, ok := page[c.rk]; ok {
			add(row, c, a)
		}
		add(-1, c, a)
	}
	for _, pc := range priced {
		detail.Points = append(detail.Points, *pc)
	}
	return detail, nil
}
