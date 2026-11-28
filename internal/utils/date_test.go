package utils

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLocalDay(t *testing.T) {
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	losAngeles, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)
	instant := time.Date(2026, time.January, 31, 18, 30, 0, 0, time.UTC)

	tests := []struct {
		name string
		loc  *time.Location
		want time.Time
	}{
		{name: "ahead of UTC crosses into the next day", loc: kolkata, want: time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)},
		{name: "UTC keeps the day", loc: time.UTC, want: time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC)},
		{name: "behind UTC keeps the earlier day", loc: losAngeles, want: time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC)},
		{name: "nil location is UTC", loc: nil, want: time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, LocalDay(instant, tt.loc))
		})
	}
}

func TestLastEndedMonth(t *testing.T) {
	tests := []struct {
		name string
		t    time.Time
		want time.Time
	}{
		{name: "mid month", t: time.Date(2026, time.October, 15, 10, 0, 0, 0, time.UTC), want: time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)},
		{name: "first instant of a month", t: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC), want: time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)},
		{name: "January wraps to December", t: time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC), want: time.Date(2025, time.December, 1, 0, 0, 0, 0, time.UTC)},
		{name: "a local time is read in UTC", t: time.Date(2026, time.March, 1, 2, 0, 0, 0, time.FixedZone("IST", 5*3600+1800)), want: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, LastEndedMonth(tt.t))
		})
	}
}

func TestMonths(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	tests := []struct {
		name       string
		start, end time.Time
		want       []time.Time
	}{
		{name: "days inside months, from a 31st", start: day(2026, time.January, 31), end: day(2026, time.March, 2),
			want: []time.Time{day(2026, time.January, 1), day(2026, time.February, 1), day(2026, time.March, 1)}},
		{name: "across a year", start: day(2025, time.December, 1), end: day(2026, time.January, 1),
			want: []time.Time{day(2025, time.December, 1), day(2026, time.January, 1)}},
		{name: "one month", start: day(2026, time.May, 1), end: day(2026, time.May, 31), want: []time.Time{day(2026, time.May, 1)}},
		{name: "end before start", start: day(2026, time.May, 1), end: day(2026, time.April, 1), want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Months(tt.start, tt.end))
		})
	}
}
