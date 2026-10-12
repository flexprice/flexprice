// Package billingmatrix drives live billing scenarios (cadences, anchors, stubs, proration,
// grants, mid-period changes) against the public API and checks every amount against an
// independent oracle of the proration spec in docs/design/2026-10-03-proration-unification-erd.md.
//
// The oracle deliberately does not import internal/types: it re-derives the billing grid
// from the spec so a regression in the product's date math cannot also hide in the check.
package billingmatrix

import (
	"time"

	"github.com/shopspring/decimal"
)

// Period values, as the API spells them.
const (
	periodDaily     = "DAILY"
	periodWeekly    = "WEEKLY"
	periodMonthly   = "MONTHLY"
	periodQuarterly = "QUARTERLY"
	periodHalfYear  = "HALF_YEARLY"
	periodAnnual    = "ANNUAL"
	periodOnetime   = "ONETIME"
)

// cadence is a billing period repeated count times, e.g. MONTHLY×6.
type cadence struct {
	period string
	count  int
}

func (c cadence) n() int { return max(c.count, 1) }

// months is the cadence length in months, or 0 for daily and weekly.
func (c cadence) months() int {
	switch c.period {
	case periodMonthly:
		return c.n()
	case periodQuarterly:
		return 3 * c.n()
	case periodHalfYear:
		return 6 * c.n()
	case periodAnnual:
		return 12 * c.n()
	}
	return 0
}

// days is the cadence length in days for daily and weekly, else 0.
func (c cadence) days() int {
	switch c.period {
	case periodDaily:
		return c.n()
	case periodWeekly:
		return 7 * c.n()
	}
	return 0
}

func (c cadence) equal(o cadence) bool { return c.period == o.period && c.n() == o.n() }

// allowedOn is the D12 rule: equal, dividing, or a whole multiple; month-based only when they differ.
func (c cadence) allowedOn(sub cadence) bool {
	if c.equal(sub) {
		return true
	}
	cm, sm := c.months(), sub.months()
	if cm == 0 || sm == 0 {
		return false
	}
	return sm%cm == 0 || cm%sm == 0
}

// longerThan reports whether c bills less often than sub.
func (c cadence) longerThan(sub cadence) bool {
	cm, sm := c.months(), sub.months()
	if cm > 0 && sm > 0 {
		return cm > sm
	}
	return false
}

// shorterThan reports whether c bills more often than sub (and divides it).
func (c cadence) shorterThan(sub cadence) bool {
	cm, sm := c.months(), sub.months()
	if cm > 0 && sm > 0 {
		return cm < sm
	}
	return false
}

// span is a half-open interval [start, end).
type span struct {
	start time.Time
	end   time.Time
}

func (s span) seconds() int64 {
	return int64(s.end.Sub(s.start).Round(time.Second) / time.Second)
}

func (s span) empty() bool { return !s.end.After(s.start) }

// grid is the D7 schedule anchor + k × cadence in one timezone, clamped to month end.
type grid struct {
	anchor time.Time
	cad    cadence
}

func newGrid(anchor time.Time, cad cadence, loc *time.Location) grid {
	return grid{anchor: anchor.In(loc), cad: cad}
}

func (g grid) at(k int) time.Time {
	if d := g.cad.days(); d > 0 {
		return g.anchor.AddDate(0, 0, k*d)
	}
	return addMonthsClamped(g.anchor, k*g.cad.months())
}

// indexAfter is the smallest k whose date is strictly after t.
func (g grid) indexAfter(t time.Time) int {
	t = t.In(g.anchor.Location())
	k := 0
	if m := g.cad.months(); m > 0 {
		k = ((t.Year()-g.anchor.Year())*12 + int(t.Month()) - int(g.anchor.Month())) / m
	} else {
		k = int(t.Sub(g.anchor).Hours()/24) / g.cad.days()
	}
	for g.at(k).After(t) {
		k--
	}
	for !g.at(k).After(t) {
		k++
	}
	return k
}

func (g grid) next(t time.Time) time.Time { return g.at(g.indexAfter(t)) }

// full is the regular grid period containing t (D2).
func (g grid) full(t time.Time) span {
	k := g.indexAfter(t)
	return span{start: g.at(k - 1), end: g.at(k)}
}

// fraction is D1: seconds used ÷ seconds of the full period containing used.start, capped to [0, 1].
func (g grid) fraction(used span) decimal.Decimal {
	u := used.seconds()
	f := g.full(used.start).seconds()
	if u <= 0 {
		return decimal.Zero
	}
	if u >= f {
		return decimal.NewFromInt(1)
	}
	return decimal.NewFromInt(u).Div(decimal.NewFromInt(f))
}

func addMonthsClamped(t time.Time, n int) time.Time {
	y, m, d := t.Date()
	h, mi, s := t.Clock()
	last := time.Date(y, m+time.Month(n)+1, 0, 0, 0, 0, 0, t.Location()).Day()
	return time.Date(y, m+time.Month(n), min(d, last), h, mi, s, t.Nanosecond(), t.Location())
}

// calendarAnchor is the first calendar boundary of the period strictly after start, local midnight.
func calendarAnchor(start time.Time, period string, loc *time.Location) time.Time {
	t := start.In(loc)
	y, m, d := t.Date()
	switch period {
	case periodDaily:
		return time.Date(y, m, d+1, 0, 0, 0, 0, loc)
	case periodWeekly:
		days := (8 - int(t.Weekday())) % 7
		if days == 0 {
			days = 7
		}
		return time.Date(y, m, d+days, 0, 0, 0, 0, loc)
	case periodMonthly:
		return time.Date(y, m+1, 1, 0, 0, 0, 0, loc)
	case periodQuarterly:
		return time.Date(y, time.Month(((int(m)-1)/3+1)*3+1), 1, 0, 0, 0, 0, loc)
	case periodHalfYear:
		return time.Date(y, time.Month(((int(m)-1)/6+1)*6+1), 1, 0, 0, 0, 0, loc)
	case periodAnnual:
		return time.Date(y+1, time.January, 1, 0, 0, 0, 0, loc)
	}
	return t
}

// subSchedule is the subscription facts every expectation is derived from.
type subSchedule struct {
	start    time.Time
	anchor   time.Time
	cad      cadence
	loc      *time.Location
	calendar bool
	prorate  bool
	end      *time.Time
}

func (s subSchedule) grid() grid { return newGrid(s.anchor, s.cad, s.loc) }

// periodAt is the sub billing period starting at from: [from, next grid date), cut at the sub end.
func (s subSchedule) periodAt(from time.Time) span {
	end := s.grid().next(from)
	if s.end != nil && end.After(*s.end) {
		end = *s.end
	}
	return span{start: from, end: end}
}

// firstPeriod is the opening period [start, anchor-grid next date).
func (s subSchedule) firstPeriod() span { return s.periodAt(s.start) }

// periods lists the sub's billing periods from start, n of them.
func (s subSchedule) periods(n int) []span {
	out := make([]span, 0, n)
	from := s.start
	for i := 0; i < n; i++ {
		p := s.periodAt(from)
		if p.empty() {
			break
		}
		out = append(out, p)
		from = p.end
	}
	return out
}

// windows splits a sub invoice period into a shorter item's windows on the item grid anchored at the sub anchor.
func (s subSchedule) windows(invoice span, item cadence) []span {
	if !item.shorterThan(s.cad) {
		return []span{invoice}
	}
	g := newGrid(s.anchor, item, s.loc)
	var out []span
	for from := invoice.start; from.Before(invoice.end); {
		to := g.next(from)
		if to.After(invoice.end) {
			to = invoice.end
		}
		out = append(out, span{start: from, end: to})
		from = to
	}
	return out
}

// longerItemAnchor is D12: the calendar boundary of the item's cadence when it lands on a sub
// billing date (calendar subs only), otherwise the sub anchor.
func (s subSchedule) longerItemAnchor(itemStart time.Time, item cadence) time.Time {
	if !s.calendar {
		return s.anchor
	}
	a := calendarAnchor(itemStart, item.period, s.loc)
	if s.grid().full(a).start.Equal(a) {
		return a
	}
	return s.anchor
}

// longerItemPeriods lists a longer item's periods from itemStart on its own grid, n of them.
func (s subSchedule) longerItemPeriods(itemStart time.Time, item cadence, n int) []span {
	g := newGrid(s.longerItemAnchor(itemStart, item), item, s.loc)
	out := make([]span, 0, n)
	for from := itemStart; len(out) < n; {
		to := g.next(from)
		if s.end != nil && to.After(*s.end) {
			to = *s.end
		}
		if !to.After(from) {
			break
		}
		out = append(out, span{start: from, end: to})
		from = to
	}
	return out
}

// fixedCharge prices one fixed line for its serviceable span: full cost when the span is a whole
// period of the item grid, cost × fraction under create_prorations, and under none the full cost
// unless the item started after the window start (then nothing until its next period, D5).
func fixedCharge(cost decimal.Decimal, itemGrid grid, serviceable span, windowStart time.Time, prorate bool) (decimal.Decimal, bool) {
	f := itemGrid.fraction(serviceable)
	if !f.LessThan(decimal.NewFromInt(1)) {
		return cost, true
	}
	if !prorate {
		if serviceable.start.Sub(windowStart).Round(time.Second) > 0 {
			return decimal.Zero, false
		}
		return cost, true
	}
	return cost.Mul(f), true
}

// money rounds to the 2-decimal currency precision the probe tenant bills in.
func money(d decimal.Decimal) decimal.Decimal { return d.Round(2) }
