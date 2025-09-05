package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

type WindowSize string

// Note: keep values up to date in the meter package
const (
	WindowSizeMinute WindowSize = "MINUTE"
	WindowSize15Min  WindowSize = "15MIN"
	WindowSize30Min  WindowSize = "30MIN"
	WindowSizeHour   WindowSize = "HOUR"
	WindowSize3Hour  WindowSize = "3HOUR"
	WindowSize6Hour  WindowSize = "6HOUR"
	WindowSize12Hour WindowSize = "12HOUR"
	WindowSizeDay    WindowSize = "DAY"
	WindowSizeWeek   WindowSize = "WEEK"
	WindowSizeMonth  WindowSize = "MONTH"
)

func (w WindowSize) Validate() error {
	if w == "" {
		return nil
	}

	if !lo.Contains([]WindowSize{
		WindowSizeMinute,
		WindowSize15Min,
		WindowSize30Min,
		WindowSizeHour,
		WindowSize3Hour,
		WindowSize6Hour,
		WindowSize12Hour,
		WindowSizeDay,
		WindowSizeWeek,
		WindowSizeMonth,
	}, w) {
		return ierr.NewError("invalid window size").
			WithHint("Invalid window size").
			WithReportableDetails(
				map[string]any{
					"window_size": w,
				},
			).
			Mark(ierr.ErrValidation)
	}

	return nil
}
