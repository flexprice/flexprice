package analytics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreviousWindow(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	d := func(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		name        string
		from, to    time.Time
		compare     string
		pf, pt      time.Time
		shiftMonths int
	}{
		{"full month", from, to, "previous_period", d(2026, 8, 1), d(2026, 9, 1), 1},
		{"month to date", d(2026, 10, 1), d(2026, 10, 13), "previous_period", d(2026, 9, 1), d(2026, 9, 13), 1},
		{"quarter", d(2026, 7, 1), d(2026, 10, 1), "previous_period", d(2026, 4, 1), d(2026, 7, 1), 3},
		{"year to date", d(2026, 1, 1), d(2026, 10, 13), "previous_period", d(2025, 3, 1), d(2025, 12, 13), 10},
		{"rolling", d(2026, 9, 5), d(2026, 9, 12), "previous_period", d(2026, 8, 29), d(2026, 9, 5), 0},
		{"year", from, to, "previous_year", d(2025, 9, 1), d(2025, 10, 1), 12},
		{"leap", d(2028, 2, 29), d(2028, 3, 1), "previous_year", d(2027, 3, 1), d(2027, 3, 1), 12},
		{"mar full", d(2026, 3, 1), d(2026, 3, 31), "previous_period", d(2026, 2, 1), d(2026, 3, 1), 1},
		{"q1 to mar 31", d(2026, 1, 1), d(2026, 3, 31), "previous_period", d(2025, 10, 1), d(2025, 12, 31), 3},
		{"may full", d(2026, 5, 1), d(2026, 5, 31), "previous_period", d(2026, 4, 1), d(2026, 5, 1), 1},
		{"oct full", d(2026, 10, 1), d(2026, 10, 31), "previous_period", d(2026, 9, 1), d(2026, 10, 1), 1},
	}
	for _, c := range cases {
		pf, pt, shift, err := PreviousWindow(c.from, c.to, c.compare)
		require.NoError(t, err, c.name)
		assert.Equal(t, c.pf, pf, c.name)
		assert.Equal(t, c.pt, pt, c.name)
		assert.Equal(t, c.shiftMonths, shift, c.name)
	}

	_, _, _, err := PreviousWindow(from, to, "bogus")
	require.Error(t, err)
}

func TestPreviousWindow_NeverOverlaps(t *testing.T) {
	for m := time.January; m <= time.December; m++ {
		from := time.Date(2026, m, 1, 0, 0, 0, 0, time.UTC)
		for day := 2; day <= 31; day++ {
			to := time.Date(2026, m, day, 0, 0, 0, 0, time.UTC)
			if to.Month() != m {
				continue
			}
			for _, cmp := range []string{"previous_period", "previous_year"} {
				pf, pt, _, err := PreviousWindow(from, to, cmp)
				require.NoError(t, err)
				assert.False(t, pt.After(from), "%s %s..%s -> %s..%s", cmp, from, to, pf, pt)
				assert.True(t, pf.Before(pt), "%s %s..%s", cmp, from, to)
			}
		}
	}
}
