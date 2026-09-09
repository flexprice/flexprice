package types

import (
	"time"

	"github.com/samber/lo"
)

func ParseTime(t string) (time.Time, error) {
	return time.Parse(time.RFC3339, t)
}

func FormatTime(t time.Time) string {
	return t.Format(time.RFC3339)
}

// ParseYYYYMMDDToDate converts YYYYMMDD integer to time.Time with beginning of day time
// for ex 20250101 means the credits will expire on 2025-01-01 00:00:00 UTC
// hence they will be available for use until 2024-12-31 23:59:59 UTC
func ParseYYYYMMDDToDate(date *int) *time.Time {
	if date == nil {
		return nil
	}

	parsedTime := time.Date(
		*date/10000,                   // year
		time.Month((*date%10000)/100), // month
		*date%100,                     // day
		0, 0, 0, 0,                    // Set to beginning of day
		time.UTC,
	)
	return &parsedTime
}

// EarliestOf returns the earlier of two instants. time.Time is not an ordered
// type, so the stdlib min/max builtins do not apply.
func EarliestOf(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// LatestOf returns the later of two instants.
func LatestOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// EarliestOfPtr is the nil-safe form of EarliestOf: a nil side yields the other,
// and two nils yield nil.
func EarliestOfPtr(a, b *time.Time) *time.Time {
	if a == nil || b == nil {
		return lo.CoalesceOrEmpty(a, b)
	}
	return lo.ToPtr(EarliestOf(*a, *b))
}

// LatestOfPtr is the nil-safe form of LatestOf.
func LatestOfPtr(a, b *time.Time) *time.Time {
	if a == nil || b == nil {
		return lo.CoalesceOrEmpty(a, b)
	}
	return lo.ToPtr(LatestOf(*a, *b))
}
