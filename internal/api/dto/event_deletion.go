package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/clock"

	"github.com/samber/lo"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/validator"
)

// DeleteEventsRequest hard-deletes ingested events and the meter usage they
// produced, scoped by customer and period and optionally narrowed by feature or
// by exact event id.
type DeleteEventsRequest struct {
	ExternalCustomerIDs []string `json:"external_customer_ids" validate:"required,min=1,max=100"`
	// FeatureIDs narrows the deletion to the meters these features resolve to.
	FeatureIDs []string `json:"feature_ids,omitempty" validate:"omitempty,max=100"`
	// EventIDs narrows the deletion to exact event ids. Requires exactly one
	// customer and no feature_ids.
	EventIDs    []string  `json:"event_ids,omitempty" validate:"omitempty,max=1000"`
	PeriodStart time.Time `json:"period_start" validate:"required" example:"2026-03-01T00:00:00Z"`
	PeriodEnd   time.Time `json:"period_end" validate:"required" example:"2026-03-28T00:00:00Z"`
	// SkipPayload stores the audit row's properties as '' so the deletion leaves
	// no payload copy. Defaults to true: a deletion is usually an erasure, and
	// keeping the payload would defeat it. Send false to retain it.
	SkipPayload *bool `json:"skip_payload,omitempty"`
}

// Validate applies the request-shape rules. Resolution of the ids and the cap on
// the resolved event set are checked by the service.
func (r *DeleteEventsRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	r.ExternalCustomerIDs = lo.Uniq(lo.Compact(r.ExternalCustomerIDs))
	r.FeatureIDs = lo.Uniq(lo.Compact(r.FeatureIDs))
	r.EventIDs = lo.Uniq(lo.Compact(r.EventIDs))

	if len(r.ExternalCustomerIDs) == 0 {
		return ierr.NewError("external_customer_ids is required").
			WithHint("Provide at least one external customer id").
			Mark(ierr.ErrValidation)
	}

	if !r.PeriodStart.Before(r.PeriodEnd) {
		return ierr.NewError("period_start must be before period_end").
			WithHint("period_start must be before period_end").
			Mark(ierr.ErrValidation)
	}

	// A future period would fence the customer's live traffic for as long as the
	// deletion runs, and anything already in Kafka would block its partition.
	if r.PeriodEnd.After(clock.Now()) {
		return ierr.NewError("period_end is in the future").
			WithHint("Delete a period that has already closed").
			WithReportableDetails(map[string]interface{}{
				"period_end": r.PeriodEnd,
			}).
			Mark(ierr.ErrValidation)
	}

	// At most one month, which is what bounds the data one mutation rewrites.
	if r.PeriodEnd.After(r.PeriodStart.AddDate(0, 1, 0)) {
		return ierr.NewError("period is wider than one month").
			WithHint("Split the request into periods of at most one month").
			WithReportableDetails(map[string]interface{}{
				"period_start": r.PeriodStart,
				"period_end":   r.PeriodEnd,
			}).
			Mark(ierr.ErrValidation)
	}

	if len(r.EventIDs) > 0 {
		if len(r.ExternalCustomerIDs) != 1 {
			return ierr.NewError("event_ids requires exactly one external_customer_id").
				WithHint("Send event_ids for a single customer at a time").
				Mark(ierr.ErrValidation)
		}
		if len(r.FeatureIDs) > 0 {
			return ierr.NewError("event_ids cannot be combined with feature_ids").
				WithHint("Narrow by event_ids or by feature_ids, not both").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// DeleteEventsResponse reports the counts. The deleted set is never enumerated:
// it lives in deleted_events_logs, read back by the X-Request-ID of this call.
type DeleteEventsResponse struct {
	DeletedEvents         int    `json:"deleted_events"`
	DeletedMeterUsageRows uint64 `json:"deleted_meter_usage_rows"`
}
