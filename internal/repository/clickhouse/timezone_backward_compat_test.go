package clickhouse

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

// Backward-compatibility invariant for analytics/usage window bucketing:
// a customer with no timezone (empty) must behave identically to an explicit
// "UTC" customer. Both normalize to the explicit 'UTC' argument, which ClickHouse
// treats identically to omitting it — so unset customers keep UTC bucketing.

var allWindowSizes = []types.WindowSize{
	types.WindowSizeMinute,
	types.WindowSize15Min,
	types.WindowSize30Min,
	types.WindowSizeHour,
	types.WindowSize3Hour,
	types.WindowSize6Hour,
	types.WindowSize12Hour,
	types.WindowSizeDay,
	types.WindowSizeWeek,
	types.WindowSizeMonth,
}

// Empty tz (legacy) and explicit "UTC" must produce identical SQL for every window.
func TestBackwardCompat_FormatWindowSize_EmptyEqualsUTC(t *testing.T) {
	for _, w := range allWindowSizes {
		t.Run(string(w), func(t *testing.T) {
			assert.Equal(t, formatWindowSize(w, "UTC"), formatWindowSize(w, ""),
				"empty and UTC must produce identical SQL")
		})
	}
}

// A malformed timezone must fall back to the UTC form, never reach ClickHouse raw.
func TestBackwardCompat_FormatWindowSize_InvalidFallsBackToUTC(t *testing.T) {
	for _, w := range allWindowSizes {
		t.Run(string(w), func(t *testing.T) {
			assert.Equal(t, formatWindowSize(w, "UTC"), formatWindowSize(w, "Not/AZone"))
		})
	}
}

// Complement: a real non-UTC timezone must change the SQL (so the invariant above
// is meaningful). MINUTE is excluded — minute buckets are timezone-invariant.
func TestBackwardCompat_FormatWindowSize_NonUTCDiffersFromUTC(t *testing.T) {
	for _, w := range allWindowSizes {
		if w == types.WindowSizeMinute {
			continue
		}
		t.Run(string(w), func(t *testing.T) {
			utc := formatWindowSize(w, "UTC")
			ist := formatWindowSize(w, "Asia/Kolkata")
			assert.NotEqual(t, utc, ist, "non-UTC tz must alter the SQL for %s", w)
			assert.Contains(t, ist, "Asia/Kolkata", "non-UTC tz must appear in the SQL: %s", ist)
		})
	}
}

// MINUTE buckets are timezone-invariant: same expression regardless of tz, and no
// tz argument. Guards against the regression where a missing MINUTE case fell
// through to day-level bucketing.
func TestFormatWindowSize_MinuteIsTimezoneInvariant(t *testing.T) {
	want := "toStartOfMinute(timestamp)"
	assert.Equal(t, want, formatWindowSize(types.WindowSizeMinute, ""))
	assert.Equal(t, want, formatWindowSize(types.WindowSizeMinute, "UTC"))
	assert.Equal(t, want, formatWindowSize(types.WindowSizeMinute, "Asia/Kolkata"))
}

// Billing-anchor variant: empty tz and explicit UTC must produce identical SQL.
func TestBackwardCompat_FormatWindowSizeWithBillingAnchor_EmptyEqualsUTC(t *testing.T) {
	anchor := time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		window types.WindowSize
		anchor *time.Time
	}{
		{"month no anchor", types.WindowSizeMonth, nil},
		{"month with anchor", types.WindowSizeMonth, &anchor},
		{"day", types.WindowSizeDay, nil},
		{"week", types.WindowSizeWeek, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t,
				formatWindowSizeWithBillingAnchor(c.window, c.anchor, "UTC"),
				formatWindowSizeWithBillingAnchor(c.window, c.anchor, ""),
				"empty and UTC must produce identical SQL")
		})
	}
}
