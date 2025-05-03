package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

type EventStatus string

const (
	EventStatusPending   EventStatus = "pending"
	EventStatusProcessed EventStatus = "processed"
	EventStatusFailed    EventStatus = "failed"
)

func (e EventStatus) String() string {
	return string(e)
}

func (e EventStatus) Validate() error {
	if lo.Contains([]EventStatus{
		EventStatusPending,
		EventStatusProcessed,
		EventStatusFailed,
	}, e) {
		return nil
	}

	return ierr.NewError("invalid event status").
		WithHint("Please provide a valid event status").
		WithReportableDetails(map[string]any{
			"event_status": e,
		}).
		Mark(ierr.ErrValidation)
}
