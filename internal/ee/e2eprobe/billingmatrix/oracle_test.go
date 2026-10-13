package billingmatrix

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func utc(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return loc
}

func assertMoney(t *testing.T, label, want string, got decimal.Decimal) {
	t.Helper()
	if !money(got).Equal(decimal.RequireFromString(want)) {
		t.Errorf("%s: want %s, got %s", label, want, money(got).StringFixed(2))
	}
}

var (
	monthly   = cadence{period: periodMonthly, count: 1}
	quarterly = cadence{period: periodQuarterly, count: 1}
	annual    = cadence{period: periodAnnual, count: 1}
	halfYear  = cadence{period: periodHalfYear, count: 1}
)

func TestGridClampsToMonthEnd(t *testing.T) {
	g := newGrid(utc(2026, time.January, 31), monthly, time.UTC)
	want := []time.Time{utc(2025, time.December, 31), utc(2026, time.January, 31), utc(2026, time.February, 28), utc(2026, time.March, 31), utc(2026, time.April, 30)}
	for i, k := range []int{-1, 0, 1, 2, 3} {
		if got := g.at(k); !got.Equal(want[i]) {
			t.Errorf("k=%d: want %s, got %s", k, want[i], got)
		}
	}
}

// Design doc worked examples, $31/month.
func TestDesignDocCharges(t *testing.T) {
	cost := decimal.NewFromInt(31)

	calJan15 := subSchedule{start: utc(2026, time.January, 15), cad: monthly, loc: time.UTC, calendar: true, prorate: true}
	calJan15.anchor = calendarAnchor(calJan15.start, periodMonthly, time.UTC)
	first := calJan15.firstPeriod()
	if !first.end.Equal(utc(2026, time.February, 1)) {
		t.Fatalf("calendar first period end: %s", first.end)
	}
	got, _ := fixedCharge(cost, calJan15.grid(), first, first.start, true)
	assertMoney(t, "calendar stub Jan 15 (17/31)", "17.00", got)

	got, _ = fixedCharge(cost, calJan15.grid(), first, first.start, false)
	assertMoney(t, "calendar stub Jan 15 under none", "31.00", got)

	addon := span{start: utc(2026, time.January, 20), end: first.end}
	got, _ = fixedCharge(cost, calJan15.grid(), addon, first.start, true)
	assertMoney(t, "addon Jan 20 (12/31)", "12.00", got)
	got, billed := fixedCharge(cost, calJan15.grid(), addon, first.start, false)
	if billed || !got.IsZero() {
		t.Errorf("addon Jan 20 under none: want skipped, got %s billed=%v", got, billed)
	}

	ann := subSchedule{start: utc(2026, time.January, 10), anchor: utc(2026, time.January, 20), cad: monthly, loc: time.UTC, prorate: true}
	got, _ = fixedCharge(cost, ann.grid(), ann.firstPeriod(), ann.start, true)
	assertMoney(t, "anniversary anchor ahead Jan 10 / Jan 20", "10.00", got)

	jan31 := subSchedule{start: utc(2026, time.January, 31), cad: monthly, loc: time.UTC, calendar: true, prorate: true}
	jan31.anchor = calendarAnchor(jan31.start, periodMonthly, time.UTC)
	got, _ = fixedCharge(cost, jan31.grid(), jan31.firstPeriod(), jan31.start, true)
	assertMoney(t, "calendar from Jan 31 (1/31)", "1.00", got)

	end := utc(2026, time.March, 10)
	last := span{start: utc(2026, time.March, 1), end: end}
	got, _ = fixedCharge(cost, calJan15.grid(), last, last.start, true)
	assertMoney(t, "short last period Mar 1 - Mar 10", "9.00", got)
}

// 3.1: quarterly $90 item on an annual calendar sub from Mar 15 → 4 windows: $17 + 3 × $90.
func TestShorterItemWindows(t *testing.T) {
	s := subSchedule{start: utc(2026, time.March, 15), cad: annual, loc: time.UTC, calendar: true, prorate: true}
	s.anchor = calendarAnchor(s.start, periodAnnual, time.UTC)
	ws := s.windows(s.firstPeriod(), quarterly)
	if len(ws) != 4 {
		t.Fatalf("want 4 windows, got %d", len(ws))
	}
	itemGrid := newGrid(s.anchor, quarterly, time.UTC)
	total := decimal.Zero
	for _, w := range ws {
		amt, _ := fixedCharge(decimal.NewFromInt(90), itemGrid, w, w.start, true)
		total = total.Add(money(amt))
	}
	assertMoney(t, "quarterly item on annual calendar stub", "287.00", total)

	// Monthly $31 on a calendar quarterly sub from Feb 15: [Feb 15, Mar 1) = 14/28.
	q := subSchedule{start: utc(2026, time.February, 15), cad: quarterly, loc: time.UTC, calendar: true, prorate: true}
	q.anchor = calendarAnchor(q.start, periodQuarterly, time.UTC)
	ws = q.windows(q.firstPeriod(), monthly)
	amt, _ := fixedCharge(decimal.NewFromInt(31), newGrid(q.anchor, monthly, time.UTC), ws[0], ws[0].start, true)
	assertMoney(t, "monthly on calendar quarterly from Feb 15", "15.50", amt)
}

// D12 examples for longer items.
func TestLongerItemGrid(t *testing.T) {
	cal := subSchedule{start: utc(2026, time.January, 15), cad: monthly, loc: time.UTC, calendar: true, prorate: true}
	cal.anchor = calendarAnchor(cal.start, periodMonthly, time.UTC)
	ps := cal.longerItemPeriods(cal.start, annual, 2)
	if !ps[0].end.Equal(utc(2027, time.January, 1)) || !ps[1].end.Equal(utc(2028, time.January, 1)) {
		t.Fatalf("annual item periods: %v", ps)
	}
	g := newGrid(cal.longerItemAnchor(cal.start, annual), annual, time.UTC)
	amt, _ := fixedCharge(decimal.NewFromInt(365), g, ps[0], ps[0].start, true)
	assertMoney(t, "annual item on calendar monthly from Jan 15", "351.00", amt)

	// 6-month item on a calendar quarterly sub from Apr 30 anchors on Jul 1, not May 1.
	q := subSchedule{start: utc(2026, time.April, 30), cad: quarterly, loc: time.UTC, calendar: true, prorate: true}
	q.anchor = calendarAnchor(q.start, periodQuarterly, time.UTC)
	if a := q.longerItemAnchor(q.start, halfYear); !a.Equal(utc(2026, time.July, 1)) {
		t.Fatalf("half-year item anchor: %s", a)
	}

	// 3.5: annual item added Mar 20 on an anniversary monthly sub anchored Jan 15 → [Mar 20, Jan 15), 301/365.
	ann := subSchedule{start: utc(2026, time.January, 15), anchor: utc(2026, time.January, 15), cad: monthly, loc: time.UTC, prorate: true}
	ps = ann.longerItemPeriods(utc(2026, time.March, 20), annual, 1)
	g = newGrid(ann.longerItemAnchor(ps[0].start, annual), annual, time.UTC)
	amt, _ = fixedCharge(decimal.NewFromInt(365), g, ps[0], ps[0].start, true)
	assertMoney(t, "annual item added Mar 20 on Jan 15 anniversary", "301.00", amt)
}

// D3: seconds, boundaries in local time. New York [Mar 17, Apr 1) on a calendar monthly sub = 0.4845.
func TestFractionAcrossDST(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	s := subSchedule{start: time.Date(2026, time.March, 17, 0, 0, 0, 0, ny), cad: monthly, loc: ny, calendar: true, prorate: true}
	s.anchor = calendarAnchor(s.start, periodMonthly, ny)
	f := s.grid().fraction(s.firstPeriod())
	if got := f.Round(4).String(); got != "0.4845" {
		t.Errorf("DST fraction: want 0.4845, got %s", got)
	}
}

func TestCadenceRule(t *testing.T) {
	cases := []struct {
		item, sub cadence
		allowed   bool
	}{
		{monthly, quarterly, true},
		{annual, monthly, true},
		{cadence{period: periodMonthly, count: 2}, quarterly, false},
		{cadence{period: periodWeekly, count: 1}, monthly, false},
		{cadence{period: periodMonthly, count: 6}, halfYear, true},
	}
	for _, c := range cases {
		if got := c.item.allowedOn(c.sub); got != c.allowed {
			t.Errorf("%v on %v: want %v, got %v", c.item, c.sub, c.allowed, got)
		}
	}
}
