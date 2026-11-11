package analytics

import (
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportRangeMaxDays(t *testing.T) {
	cases := map[string]int{
		"last_7_days": 7, "last_30_days": 30, "last_90_days": 90,
		"this_month": 31, "last_month": 31, "this_quarter": 92, "this_year": 366,
		"2026-09-01..2026-10-01": 30,
	}
	for rng, want := range cases {
		got, err := ReportRangeMaxDays(rng)
		require.NoError(t, err, rng)
		assert.Equal(t, want, got, rng)
	}
	for _, bad := range []string{"", "last_8_days", "today", "2026-10-01..2026-09-01", "2026-09-01..", "x..y"} {
		_, err := ReportRangeMaxDays(bad)
		require.Error(t, err, bad)
		assert.True(t, ierr.IsValidation(err), bad)
	}
}
