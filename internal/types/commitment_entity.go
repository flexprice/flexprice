package types

import (
	"fmt"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

type CommitmentScopeType string

const (
	CommitmentScopeSubscription CommitmentScopeType = "subscription"
	CommitmentScopeGroup        CommitmentScopeType = "group"
	CommitmentScopeMeter        CommitmentScopeType = "meter"
	CommitmentScopePrice        CommitmentScopeType = "price"
)

func (s CommitmentScopeType) Validate() error {
	return validateEnum("scope_entity_type", s, []CommitmentScopeType{
		CommitmentScopeSubscription,
		CommitmentScopeGroup,
		CommitmentScopeMeter,
		CommitmentScopePrice,
	})
}

type CommitmentMeasurement string

const (
	CommitmentMeasurementBillingPeriod CommitmentMeasurement = "billing_period"
	CommitmentMeasurementWindow        CommitmentMeasurement = "window"
)

func (m CommitmentMeasurement) Validate() error {
	return validateEnum("measurement", m, []CommitmentMeasurement{
		CommitmentMeasurementBillingPeriod,
		CommitmentMeasurementWindow,
	})
}

type CommitmentCollectionMethod string

const (
	CommitmentCollectionPerPeriod    CommitmentCollectionMethod = "per_period"
	CommitmentCollectionBillingTerms CommitmentCollectionMethod = "billing_terms"
)

func (c CommitmentCollectionMethod) Validate() error {
	return validateEnum("collection_method", c, []CommitmentCollectionMethod{
		CommitmentCollectionPerPeriod,
		CommitmentCollectionBillingTerms,
	})
}

type CommitmentEarlyTermination string

const (
	CommitmentTerminationForfeit       CommitmentEarlyTermination = "forfeit"
	CommitmentTerminationCreditNote    CommitmentEarlyTermination = "credit_note"
	CommitmentTerminationBillRemaining CommitmentEarlyTermination = "bill_remaining"
)

func (e CommitmentEarlyTermination) Validate() error {
	return validateEnum("on_early_termination", e, []CommitmentEarlyTermination{
		CommitmentTerminationForfeit,
		CommitmentTerminationCreditNote,
		CommitmentTerminationBillRemaining,
	})
}

type CommitmentStatus string

const (
	CommitmentStatusActive     CommitmentStatus = "active"
	CommitmentStatusEnded      CommitmentStatus = "ended"
	CommitmentStatusTerminated CommitmentStatus = "terminated"
)

func (s CommitmentStatus) Validate() error {
	return validateEnum("commitment_status", s, []CommitmentStatus{
		CommitmentStatusActive,
		CommitmentStatusEnded,
		CommitmentStatusTerminated,
	})
}

// CommitmentLineKind tags invoice lines produced for a commitment.
type CommitmentLineKind string

const (
	CommitmentLineUtilized        CommitmentLineKind = "utilized"
	CommitmentLineOverage         CommitmentLineKind = "overage"
	CommitmentLineTrueUp          CommitmentLineKind = "true_up"
	CommitmentLineInstalment      CommitmentLineKind = "instalment"
	CommitmentLineSettlement      CommitmentLineKind = "settlement"
	CommitmentLineCancellationFee CommitmentLineKind = "cancellation_fee"
)

func validateEnum[T ~string](field string, v T, allowed []T) error {
	if v == "" || lo.Contains(allowed, v) {
		return nil
	}

	return ierr.NewErrorf("invalid %s %q", field, v).
		WithHintf("%s must be one of %v", field, allowed).
		Mark(ierr.ErrValidation)
}

const minutesPerDay = 24 * 60

// ActiveWindow is a half-open [Start, End) time-of-day range; End before Start wraps past midnight.
type ActiveWindow struct {
	Start Bucket `json:"start"`
	End   Bucket `json:"end"`
}

type ActiveWindows []ActiveWindow

func (w ActiveWindow) Validate() error {
	if err := w.Start.Validate(); err != nil {
		return err
	}

	if err := w.End.Validate(); err != nil {
		return err
	}

	if w.Start.MinuteOfDay() == w.End.MinuteOfDay() {
		return fmt.Errorf("window start equals end")
	}

	return nil
}

// span returns the window as [start, end) minutes with end > start, extended past 1440 when it wraps.
func (w ActiveWindow) span() (int, int) {
	start := w.Start.MinuteOfDay()
	end := w.End.MinuteOfDay()
	if end <= start {
		end += minutesPerDay
	}

	return start, end
}

func (ws ActiveWindows) Validate() error {
	covered := make([]bool, minutesPerDay)
	for _, w := range ws {
		if err := w.Validate(); err != nil {
			return ierr.WithError(err).
				WithHint("active_windows entries need a valid start and end, with start different from end").
				Mark(ierr.ErrValidation)
		}

		start, end := w.span()
		for m := start; m < end; m++ {
			slot := m % minutesPerDay
			if covered[slot] {
				return ierr.NewError("active_windows overlap").
					WithHint("ranges within one commitment must not overlap").
					Mark(ierr.ErrValidation)
			}

			covered[slot] = true
		}
	}

	return nil
}

// Contains reports whether t, read in loc, falls in any window. Empty windows mean all day.
func (ws ActiveWindows) Contains(t time.Time, loc *time.Location) bool {
	if len(ws) == 0 {
		return true
	}

	local := t.In(loc)
	cur := Bucket{Hour: local.Hour(), Minute: local.Minute()}.MinuteOfDay()
	for _, w := range ws {
		if w.Validate() != nil {
			continue
		}

		start, end := w.span()
		if (cur >= start && cur < end) || (cur+minutesPerDay >= start && cur+minutesPerDay < end) {
			return true
		}
	}

	return false
}
