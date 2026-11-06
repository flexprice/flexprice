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

// visibleUsage returns the scope's records: received by DataThrough and, with UseFinal,
// one record per (meter, id) keeping the latest received, as ReplacingMergeTree FINAL does.
func (s *InMemoryMeterUsageStore) visibleUsage(scope events.AnalyticsV2Scope) []*events.MeterUsage {
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

// bucketOf is an event's billing bucket and group value on a bucketed MAX line, as one key.
func bucketOf(r *events.MeterUsage, li int, line events.AnalyticsV2Line) string {
	anchor := line.BillingAnchor
	grp, _ := r.Properties[line.BucketGroup].(string)
	return fmt.Sprintf("%d\x01%d\x01%s", li, truncateToWindow(r.Timestamp, line.BucketSize, &anchor).Unix(), grp)
}

// bucketMember is one cell's events within one bucket of a bucketed MAX line.
type bucketMember struct {
	key  string // the cell, including whether its events pass the filters
	pass bool
	max  decimal.Decimal
	n    uint64
}

// bucketPeaks collects bucket members and hands each passing member its share: the bucket's peak for the
// member holding it (no ties in test data), and the member's own event count.
type bucketPeaks map[string]map[string]*bucketMember

func (b bucketPeaks) add(bucket, key string, pass bool, qty decimal.Decimal) {
	if b[bucket] == nil {
		b[bucket] = map[string]*bucketMember{}
	}
	mk := fmt.Sprintf("%s\x02%v", key, pass)
	m := b[bucket][mk]
	if m == nil {
		m = &bucketMember{key: key, pass: pass, max: qty}
		b[bucket][mk] = m
	}
	m.max = decimal.Max(m.max, qty)
	if pass {
		m.n++
	}
}

func (b bucketPeaks) each(fn func(key string, q decimal.Decimal, n uint64)) {
	for _, members := range b {
		var owner *bucketMember
		for _, m := range members {
			if owner == nil || m.max.GreaterThan(owner.max) {
				owner = m
			}
		}
		for _, m := range members {
			if !m.pass {
				continue
			}
			q := decimal.Zero
			if m == owner {
				q = m.max
			}
			fn(m.key, q, m.n)
		}
	}
}

func (s *InMemoryMeterUsageStore) GetAnalyticsV2LineUsage(_ context.Context, params *events.AnalyticsV2LineUsageParams) ([]events.AnalyticsV2LineUsage, error) {
	if params == nil || len(params.Lines) == 0 {
		return nil, nil
	}
	out := make([]events.AnalyticsV2LineUsage, len(params.Lines))
	type peak struct {
		line       int
		all, inner decimal.Decimal
	}
	peaks := map[string]*peak{}
	for _, r := range s.visibleUsage(params.Scope) {
		for i, line := range params.Lines {
			if !inLine(r, line) {
				continue
			}
			out[i].EventCount++
			inRange := !r.Timestamp.Before(params.RangeStart) && r.Timestamp.Before(params.RangeEnd)
			if line.IsBucketed() {
				k := bucketOf(r, i, line)
				pk := peaks[k]
				if pk == nil {
					pk = &peak{line: i, all: r.QtyTotal}
					peaks[k] = pk
				}
				pk.all = decimal.Max(pk.all, r.QtyTotal)
				if inRange {
					pk.inner = decimal.Max(pk.inner, r.QtyTotal)
				}
				continue
			}
			out[i].Quantity = out[i].Quantity.Add(r.QtyTotal)
			if inRange {
				out[i].RangeQuantity = out[i].RangeQuantity.Add(r.QtyTotal)
			}
		}
	}
	for _, pk := range peaks {
		out[pk.line].Quantity = out[pk.line].Quantity.Add(pk.all)
		out[pk.line].RangeQuantity = out[pk.line].RangeQuantity.Add(pk.inner)
	}
	return out, nil
}

// keyValue is the record's value for a row, column or filter key; JSON properties that are not strings read as "".
func keyValue(r *events.MeterUsage, line events.AnalyticsV2Line, key string, p *events.AnalyticsV2RankParams, loc *time.Location) string {
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

	// Per (row, column, line): quantity and event count.
	type cell struct {
		group []string
		col   string
		line  int
		q     decimal.Decimal
		n     uint64
	}
	cells := map[string]*cell{}
	cellAt := func(group []string, col string, li int) *cell {
		k := fmt.Sprintf("%s\x01%s\x01%d", strings.Join(group, "\x00"), col, li)
		c := cells[k]
		if c == nil {
			c = &cell{group: group, col: col, line: li}
			cells[k] = c
		}
		return c
	}
	type cellRef struct {
		group []string
		col   string
		line  int
	}
	refs := map[string]cellRef{}
	peaks := bucketPeaks{}
	for _, r := range s.visibleUsage(p.Scope) {
		if r.Timestamp.Before(p.StartTime) || !r.Timestamp.Before(p.EndTime) {
			continue
		}
		for li, line := range p.Lines {
			if !inLine(r, line) {
				continue
			}
			pass := !lo.SomeBy(p.Filters, func(f events.AnalyticsV2Filter) bool {
				return lo.Contains(f.Values, keyValue(r, line, f.Key, p, loc)) == f.Exclude
			})
			group := lo.Map(p.GroupBy, func(k string, _ int) string { return keyValue(r, line, k, p, loc) })
			col := ""
			if p.Column != "" {
				col = keyValue(r, line, p.Column, p, loc)
			}
			if line.IsBucketed() {
				// The bucket's peak is chosen among all events; filtered-out cells are dropped afterwards.
				key := fmt.Sprintf("%s\x01%s\x01%d", strings.Join(group, "\x00"), col, li)
				refs[key] = cellRef{group: group, col: col, line: li}
				peaks.add(bucketOf(r, li, line), key, pass, r.QtyTotal)
				continue
			}
			if !pass {
				continue
			}
			c := cellAt(group, col, li)
			c.q = c.q.Add(r.QtyTotal)
			c.n++
		}
	}
	peaks.each(func(key string, q decimal.Decimal, n uint64) {
		ref := refs[key]
		c := cellAt(ref.group, ref.col, ref.line)
		c.q, c.n = c.q.Add(q), c.n+n
	})

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
		rate := p.Rates[c.line]
		amount := c.q.Mul
		am := events.AnalyticsV2Amounts{
			Usage: amount(rate.Usage), CommitmentUtilized: amount(rate.CommitmentUtilized), Overage: amount(rate.Overage),
			TrueUp: amount(rate.TrueUp), Discount: amount(rate.Discount), Free: amount(rate.Free),
		}
		addAmounts(&a.m.Amounts, am)
		col := a.cols[c.col]
		col.Amounts.Usage = col.Amounts.Usage.Add(am.Cost())
		a.m.Quantity = a.m.Quantity.Add(c.q)
		col.Quantity = col.Quantity.Add(c.q)
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
			return a.m.Quantity
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
		addAmounts(&out.Totals.Amounts, a.m.Amounts)
		out.Totals.Quantity = out.Totals.Quantity.Add(a.m.Quantity)
		out.Totals.EventCount += a.m.EventCount
		for k, c := range a.cols {
			if out.ColumnTotals == nil {
				continue
			}
			t := out.ColumnTotals[k]
			t.Amounts.Usage = t.Amounts.Usage.Add(c.Amounts.Usage)
			t.Quantity = t.Quantity.Add(c.Quantity)
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

func addAmounts(dst *events.AnalyticsV2Amounts, a events.AnalyticsV2Amounts) {
	dst.Usage = dst.Usage.Add(a.Usage)
	dst.CommitmentUtilized = dst.CommitmentUtilized.Add(a.CommitmentUtilized)
	dst.Overage = dst.Overage.Add(a.Overage)
	dst.TrueUp = dst.TrueUp.Add(a.TrueUp)
	dst.Discount = dst.Discount.Add(a.Discount)
	dst.Free = dst.Free.Add(a.Free)
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
		rk   string
		w    time.Time
		col  string
		line int
		q    decimal.Decimal
		n    uint64
	}
	cells := map[string]*cell{}
	cellAt := func(rk string, w time.Time, col string, li int) *cell {
		k := fmt.Sprintf("%s\x01%d\x01%s\x01%d", rk, w.Unix(), col, li)
		c := cells[k]
		if c == nil {
			c = &cell{rk: rk, w: w, col: col, line: li}
			cells[k] = c
		}
		return c
	}
	type cellRef struct {
		rk, text, col string
		w             time.Time
		line          int
	}
	refs := map[string]cellRef{}
	peaks := bucketPeaks{}
	searched := func(text string) bool {
		return p.Search == "" || strings.Contains(strings.ToLower(text), strings.ToLower(p.Search))
	}
	distinct := make([]map[string]map[string]bool, len(params.Rows))
	for _, r := range s.visibleUsage(p.Scope) {
		if r.Timestamp.Before(p.StartTime) || !r.Timestamp.Before(p.EndTime) {
			continue
		}
		for li, line := range p.Lines {
			if !inLine(r, line) {
				continue
			}
			pass := !lo.SomeBy(p.Filters, func(f events.AnalyticsV2Filter) bool {
				return lo.Contains(f.Values, keyValue(r, line, f.Key, p, time.UTC)) == f.Exclude
			})
			group := lo.Map(p.GroupBy, func(k string, _ int) string { return keyValue(r, line, k, p, time.UTC) })
			rk := strings.Join(group, "\x1f")
			if line.IsBucketed() && params.WindowSize != "" {
				col := ""
				if p.Column != "" {
					col = keyValue(r, line, p.Column, p, time.UTC)
				}
				w := truncateToWindow(r.Timestamp, params.WindowSize, params.BillingAnchor)
				key := fmt.Sprintf("%s\x01%d\x01%s\x01%d", rk, w.Unix(), col, li)
				refs[key] = cellRef{rk: rk, text: strings.Join(group, " "), col: col, w: w, line: li}
				peaks.add(bucketOf(r, li, line), key, pass, r.QtyTotal)
			}
			if !pass {
				continue
			}
			if row, ok := page[rk]; ok {
				if distinct[row] == nil {
					distinct[row] = map[string]map[string]bool{}
				}
				for _, k := range params.DistinctKeys {
					if distinct[row][k] == nil {
						distinct[row][k] = map[string]bool{}
					}
					distinct[row][k][keyValue(r, line, k, p, time.UTC)] = true
				}
			}
			if line.IsBucketed() || params.WindowSize == "" || !searched(strings.Join(group, " ")) {
				continue
			}
			col := ""
			if p.Column != "" {
				col = keyValue(r, line, p.Column, p, time.UTC)
			}
			c := cellAt(rk, truncateToWindow(r.Timestamp, params.WindowSize, params.BillingAnchor), col, li)
			c.q, c.n = c.q.Add(r.QtyTotal), c.n+1
		}
	}
	peaks.each(func(key string, q decimal.Decimal, n uint64) {
		ref := refs[key]
		if !searched(ref.text) {
			return
		}
		c := cellAt(ref.rk, ref.w, ref.col, ref.line)
		c.q, c.n = c.q.Add(q), c.n+n
	})

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

	// Cells are priced per line, then summed per (row, window, column).
	priced := map[string]*events.AnalyticsV2PointCell{}
	add := func(row int, c *cell, a events.AnalyticsV2Amounts) {
		k := fmt.Sprintf("%d\x01%d\x01%s", row, c.w.Unix(), c.col)
		pc := priced[k]
		if pc == nil {
			pc = &events.AnalyticsV2PointCell{Row: row, WindowStart: c.w, Column: c.col}
			priced[k] = pc
		}
		pc.Amounts = events.AnalyticsV2Amounts{
			Usage: pc.Amounts.Usage.Add(a.Usage), CommitmentUtilized: pc.Amounts.CommitmentUtilized.Add(a.CommitmentUtilized),
			Overage: pc.Amounts.Overage.Add(a.Overage), TrueUp: pc.Amounts.TrueUp.Add(a.TrueUp), Discount: pc.Amounts.Discount.Add(a.Discount),
		}
		pc.Quantity, pc.EventCount = pc.Quantity.Add(c.q), pc.EventCount+c.n
	}
	for _, c := range cells {
		r := p.Rates[c.line]
		a := events.AnalyticsV2Amounts{
			Usage: c.q.Mul(r.Usage), CommitmentUtilized: c.q.Mul(r.CommitmentUtilized), Overage: c.q.Mul(r.Overage),
			TrueUp: c.q.Mul(r.TrueUp), Discount: c.q.Mul(r.Discount),
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
