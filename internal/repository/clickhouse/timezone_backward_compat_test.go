package clickhouse

import (
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

// Backward-compatibility invariant for analytics/usage window bucketing:
//
// Before the timezone feature, ClickHouse window expressions never carried a
// timezone argument (always UTC). For customers without a timezone (empty) — and
// for the explicit "UTC" value — the generated SQL must be byte-identical to the
// legacy expression: no timezone literal, identical to each other.

var allWindowSizes = []types.WindowSize{
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

// TestBackwardCompat_FormatWindowSize_UTCEmitsNoTimezone asserts that empty and
// "UTC" produce identical, timezone-free SQL for every window size.
func TestBackwardCompat_FormatWindowSize_UTCEmitsNoTimezone(t *testing.T) {
	for _, w := range allWindowSizes {
		t.Run(string(w), func(t *testing.T) {
			empty := formatWindowSize(w, "")
			utc := formatWindowSize(w, "UTC")

			assert.Equal(t, empty, utc, "empty and UTC must produce identical SQL")
			assert.NotContains(t, empty, "'", "UTC/empty path must not emit a timezone literal: %s", empty)
		})
	}
}

// TestBackwardCompat_FormatWindowSize_NonUTCDiffersFromUTC is the complement: a
// real timezone must change the SQL (so the UTC invariant above is meaningful).
func TestBackwardCompat_FormatWindowSize_NonUTCDiffersFromUTC(t *testing.T) {
	for _, w := range allWindowSizes {
		t.Run(string(w), func(t *testing.T) {
			utc := formatWindowSize(w, "UTC")
			ist := formatWindowSize(w, "Asia/Kolkata")
			assert.NotEqual(t, utc, ist, "non-UTC tz must alter the SQL for %s", w)
			assert.Contains(t, ist, "Asia/Kolkata", "non-UTC tz must appear in the SQL: %s", ist)
		})
	}
}

// TestBackwardCompat_FormatWindowSizeWithBillingAnchor_UTCEmitsNoTimezone asserts the
// billing-anchor variant is also unchanged for UTC/empty customers.
func TestBackwardCompat_FormatWindowSizeWithBillingAnchor_UTCEmitsNoTimezone(t *testing.T) {
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
			empty := formatWindowSizeWithBillingAnchor(c.window, c.anchor, "")
			utc := formatWindowSizeWithBillingAnchor(c.window, c.anchor, "UTC")
			assert.Equal(t, empty, utc, "empty and UTC must produce identical SQL")
			assert.NotContains(t, strings.TrimSpace(empty), "'UTC'", "must not emit a UTC literal")
		})
	}
}
