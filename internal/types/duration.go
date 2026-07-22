package types

import (
	"fmt"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// DurationUnit is a unit for value+unit duration configs (cooloff, etc.).
type DurationUnit string

const (
	DurationUnitSecond DurationUnit = "SECOND"
	DurationUnitMinute DurationUnit = "MINUTE"
	DurationUnitHour   DurationUnit = "HOUR"
	DurationUnitDay    DurationUnit = "DAY"
)

func (u DurationUnit) Validate() error {
	allowed := []DurationUnit{
		DurationUnitSecond,
		DurationUnitMinute,
		DurationUnitHour,
		DurationUnitDay,
	}
	if !lo.Contains(allowed, u) {
		return ierr.NewError("invalid duration unit").
			WithHint(fmt.Sprintf("Duration unit must be one of: %v", allowed)).
			WithReportableDetails(map[string]any{"allowed": allowed}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// Duration is a generic value+unit time span.
// Optionality is expressed by using *Duration at the call site (nil = unset).
type Duration struct {
	Value int          `json:"value"`
	Unit  DurationUnit `json:"unit"`
}

// IsSet reports whether the duration pointer is non-nil with a positive value and unit.
func (d *Duration) IsSet() bool {
	return d != nil && d.Value > 0 && d.Unit != ""
}

// Validate checks bounds when Duration is present. Nil Duration is valid (unset).
func (d *Duration) Validate() error {
	if d == nil {
		return nil
	}
	if d.Value <= 0 {
		return ierr.NewError("duration value must be greater than zero").
			WithHint("Duration value must be greater than zero").
			WithReportableDetails(map[string]any{"value": d.Value}).
			Mark(ierr.ErrValidation)
	}
	if d.Unit == "" {
		return ierr.NewError("duration unit is required").
			WithHint("Duration unit is required").
			Mark(ierr.ErrValidation)
	}
	return d.Unit.Validate()
}

// ToDuration converts to time.Duration. Returns an error if unset or invalid.
func (d *Duration) ToDuration() (time.Duration, error) {
	if err := d.Validate(); err != nil {
		return 0, err
	}
	if !d.IsSet() {
		return 0, ierr.NewError("duration is not set").
			WithHint("Duration value and unit are required").
			Mark(ierr.ErrValidation)
	}
	switch d.Unit {
	case DurationUnitSecond:
		return time.Duration(d.Value) * time.Second, nil
	case DurationUnitMinute:
		return time.Duration(d.Value) * time.Minute, nil
	case DurationUnitHour:
		return time.Duration(d.Value) * time.Hour, nil
	case DurationUnitDay:
		return time.Duration(d.Value) * 24 * time.Hour, nil
	default:
		return 0, ierr.NewError("invalid duration unit").
			WithHint("Unsupported duration unit").
			Mark(ierr.ErrValidation)
	}
}
