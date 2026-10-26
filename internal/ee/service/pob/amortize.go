package pob

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/shopspring/decimal"
)

// Earned is the part of amount, spread over its service period [start, end), earned before asOf's local day.
// Amounts are additive: a POB earns the sum of Earned over its billed lines, each on its own period.
// e.g. $300 over [Jan 15, Apr 15), prorated: before Mar 10 → 182.95; a $40 proration line over [Apr 1, Apr 15) adds 40 by Apr 15.
func Earned(amount decimal.Decimal, start, end time.Time, recognition types.RecognitionMethod, amortization types.AmortizationMethod, asOf time.Time, loc *time.Location) decimal.Decimal {
	return newAmortizedAmount(amount, start, end, recognition, amortization, loc).earnedBefore(utils.LocalDay(asOf, loc))
}

// amortizedAmount is an amount with its covered local days and their total weight resolved once.
type amortizedAmount struct {
	amount       decimal.Decimal
	term         dayRange
	amortization types.AmortizationMethod
	termWeight   decimal.Decimal
}

// newAmortizedAmount resolves the term; point_in_time and sub-day periods cover their first day only, and period_even
// covers whole months from its start month.
// e.g. a point_in_time charge on Feb 10 covers [Feb 10, Feb 11); period_even [Apr 22, May 22) covers [Apr 1, May 1).
func newAmortizedAmount(amount decimal.Decimal, start, end time.Time, recognition types.RecognitionMethod, amortization types.AmortizationMethod, loc *time.Location) amortizedAmount {
	// e.g. Jan 15 00:00 → Apr 15 00:00 local gives [Jan 15, Apr 15)
	term := dayRange{start: utils.LocalDay(start, loc), end: utils.LocalDay(end, loc)}

	// e.g. a point_in_time charge on Feb 10, or a Feb 10 10:00 → 15:00 period, becomes [Feb 10, Feb 11)
	if recognition == types.RecognitionMethodPointInTime || term.days() == 0 {
		term.end = term.start.AddDate(0, 0, 1)
	} else if amortization == types.AmortizationPeriodEven {
		term = wholeMonths(term)
	}

	a := amortizedAmount{amount: amount, term: term, amortization: amortization}
	// e.g. prorated [Jan 15, Apr 15) → 17/31 + 1 + 1 + 14/30 ≈ 3.0151
	a.termWeight = a.weightBefore(term.end)

	return a
}

// earnedBefore is amount × weightBefore(day) / termWeight.
// e.g. $300 over [Jan 15, Apr 15), prorated, before Feb 1: 300 × (17/31) / 3.0151 = 54.5649.
func (a amortizedAmount) earnedBefore(day time.Time) decimal.Decimal {
	return a.amount.Mul(a.weightBefore(day)).Div(a.termWeight)
}

// weightBefore sums the weights of the term's days before day, a month at a time: daily weighs each day 1,
// period_even weighs each of its whole months 1, period_even_prorated weighs each day 1 / days in its month.
// e.g. term [Jan 15, Apr 15) before Mar 1: daily 17 + 28 = 45; period_even 1 + 1 = 2; prorated 17/31 + 28/28 ≈ 1.548.
func (a amortizedAmount) weightBefore(day time.Time) decimal.Decimal {
	// e.g. before Mar 1 → [Jan 15, Mar 1); before Jan 1 → empty; before May 1 → the whole term
	elapsed := a.term.overlap(dayRange{start: a.term.start, end: day})

	total := decimal.Zero
	// e.g. [Jan 15, Mar 1) visits Jan (17 elapsed days) and Feb (28 elapsed days)
	for month := monthContaining(elapsed.start); month.start.Before(elapsed.end); month = month.next() {
		elapsedInMonth := decimal.NewFromInt(elapsed.overlap(month).days())

		switch a.amortization {
		case types.AmortizationDaily:
			// e.g. Jan adds 17
			total = total.Add(elapsedInMonth)
		case types.AmortizationPeriodEven:
			// e.g. Jan adds 31 / 31 term days in Jan = 1
			total = total.Add(elapsedInMonth.Div(decimal.NewFromInt(a.term.overlap(month).days())))
		default:
			// e.g. Jan adds 17 / 31
			total = total.Add(elapsedInMonth.Div(decimal.NewFromInt(month.days())))
		}
	}

	return total
}

// wholeMonths snaps a term to N whole calendar months from its start month: N is its length in months, a partial
// month counting as one, and at least 1. A start on a month's last day is matched by an end on a month's last day.
// e.g. [Apr 22, May 22) → [Apr 1, May 1); [Sep 7, Oct 1) → [Sep 1, Oct 1); [Sep 30, Dec 31) → [Sep 1, Dec 1)
func wholeMonths(term dayRange) dayRange {
	months := (term.end.Year()-term.start.Year())*12 + int(term.end.Month()-term.start.Month())
	startDay := term.start.Day()
	if term.start.AddDate(0, 0, 1).Day() == 1 {
		startDay = int(monthContaining(term.end).days())
	}
	if term.end.Day() > startDay {
		months++
	}
	start := monthContaining(term.start).start
	return dayRange{start: start, end: start.AddDate(0, max(months, 1), 0)}
}

// dayRange is the local days [start, end), each stamped as UTC midnight by utils.LocalDay.
type dayRange struct {
	start time.Time
	end   time.Time
}

// days is the number of days in r; an inverted range has none.
// e.g. [Jan 15, Feb 1) → 17; [Feb 1, Jan 15) → 0.
func (r dayRange) days() int64 {
	if !r.end.After(r.start) {
		return 0
	}

	return int64(r.end.Sub(r.start).Hours() / 24)
}

// overlap is the days in both ranges; disjoint ranges give an inverted range with no days.
// e.g. [Jan 15, Apr 15) ∩ [Feb 1, Mar 1) → [Feb 1, Mar 1).
func (r dayRange) overlap(other dayRange) dayRange {
	return dayRange{start: laterOf(r.start, other.start), end: earlierOf(r.end, other.end)}
}

func (r dayRange) contains(day time.Time) bool {
	return !day.Before(r.start) && day.Before(r.end)
}

// next is the month after r, for a month range.
// e.g. [Jan 1, Feb 1) → [Feb 1, Mar 1).
func (r dayRange) next() dayRange {
	return dayRange{start: r.end, end: r.end.AddDate(0, 1, 0)}
}

// monthContaining is the calendar month of day.
// e.g. Jan 15 → [Jan 1, Feb 1).
func monthContaining(day time.Time) dayRange {
	start := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC)

	return dayRange{start: start, end: start.AddDate(0, 1, 0)}
}

func laterOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}

	return b
}

func earlierOf(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}

	return b
}
