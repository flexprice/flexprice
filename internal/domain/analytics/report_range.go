package analytics

import (
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// Worst-case span per preset.
var reportPresetDays = map[string]int{
	"last_7_days":  7,
	"last_30_days": 30,
	"last_90_days": 90,
	"this_month":   31,
	"last_month":   31,
	"this_quarter": 92,
	"this_year":    366,
}

// ReportRangeMaxDays is the preset's worst-case span.
func ReportRangeMaxDays(rng string) (int, error) {
	if d, ok := reportPresetDays[rng]; ok {
		return d, nil
	}
	if from, to, ok := strings.Cut(rng, ".."); ok {
		f, ferr := time.Parse(time.DateOnly, from)
		t, terr := time.Parse(time.DateOnly, to)
		if ferr == nil && terr == nil && t.After(f) {
			return int(t.Sub(f).Hours() / 24), nil
		}
	}
	return 0, ierr.NewErrorf("unsupported range %q", rng).
		WithHint("range must be last_7_days, last_30_days, last_90_days, this_month, last_month, this_quarter, this_year or YYYY-MM-DD..YYYY-MM-DD").
		Mark(ierr.ErrValidation)
}
