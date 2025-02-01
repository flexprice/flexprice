package types

import "fmt"

type WindowSize string

// Note: keep values up to date in the meter package
const (
	WindowSizeMinute WindowSize = "MINUTE"
	WindowSizeHour   WindowSize = "HOUR"
	WindowSizeDay    WindowSize = "DAY"
)

func (w WindowSize) Validate() error {
	if w == "" {
		return nil
	}

	switch w {
	case WindowSizeMinute, WindowSizeHour, WindowSizeDay:
		return nil
	default:
		return fmt.Errorf("invalid window size: %s", w)
	}
}
