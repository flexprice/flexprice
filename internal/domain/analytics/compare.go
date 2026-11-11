package analytics

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// PreviousWindow: shiftMonths is 0 for rolling windows.
func PreviousWindow(from, to time.Time, compare string) (time.Time, time.Time, int, error) {
	var pf, pt time.Time
	var shift int
	switch compare {
	case "previous_period":
		if from.UTC().Day() == 1 && from.Equal(from.Truncate(24*time.Hour)) {
			n := 1
			for from.AddDate(0, n, 0).Before(to) {
				n++
			}
			pf, pt, shift = from.AddDate(0, -n, 0), shiftBackClamped(to, n), n
		} else {
			pf, pt = from.Add(-to.Sub(from)), from
		}
	case "previous_year":
		pf, pt, shift = from.AddDate(-1, 0, 0), to.AddDate(-1, 0, 0), 12
	default:
		return time.Time{}, time.Time{}, 0, ierr.NewErrorf("unsupported compare %q", compare).
			WithHint("compare must be previous_period or previous_year").
			Mark(ierr.ErrValidation)
	}
	if pt.After(from) {
		pt = from
	}
	return pf, pt, shift, nil
}

// shiftBackClamped overflows to the next month's first day.
func shiftBackClamped(t time.Time, months int) time.Time {
	t = t.UTC()
	first := time.Date(t.Year(), t.Month()-time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	if t.Day() > first.AddDate(0, 1, -1).Day() {
		return first.AddDate(0, 1, 0)
	}
	return time.Date(first.Year(), first.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}
