package utils

import "time"

// LocalDay is t's calendar day in loc, stamped as UTC midnight; a nil loc means UTC.
// e.g. Jan 31 18:30 UTC in Asia/Kolkata → Feb 1 00:00 UTC.
func LocalDay(t time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// LastEndedMonth is the first day of the UTC month before t's.
// e.g. Oct 15 → Sep 1; Jan 10 → Dec 1 of the year before.
func LastEndedMonth(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month()-1, 1, 0, 0, 0, 0, time.UTC)
}

// Months is the first day of each month from start's month to end's month, inclusive; empty when end is earlier.
// e.g. Jan 31 → Mar 2 gives Jan 1, Feb 1, Mar 1.
func Months(start, end time.Time) []time.Time {
	var months []time.Time
	last := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC)
	for month := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC); !month.After(last); month = month.AddDate(0, 1, 0) {
		months = append(months, month)
	}
	return months
}
