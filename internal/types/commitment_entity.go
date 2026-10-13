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
		CommitmentScopeSubscription, CommitmentScopeGroup, CommitmentScopeMeter, CommitmentScopePrice,
	})
}

type CommitmentMeasurement string

const (
	CommitmentMeasurementBillingPeriod CommitmentMeasurement = "billing_period"
	CommitmentMeasurementWindow        CommitmentMeasurement = "window"
)

func (m CommitmentMeasurement) Validate() error {
	return validateEnum("measurement", m, []CommitmentMeasurement{CommitmentMeasurementBillingPeriod, CommitmentMeasurementWindow})
}

type CommitmentCollectionMethod string

const (
	CommitmentCollectionPerPeriod    CommitmentCollectionMethod = "per_period"
	CommitmentCollectionBillingTerms CommitmentCollectionMethod = "billing_terms"
)

func (c CommitmentCollectionMethod) Validate() error {
	return validateEnum("collection_method", c, []CommitmentCollectionMethod{CommitmentCollectionPerPeriod, CommitmentCollectionBillingTerms})
}

type CommitmentEarlyTermination string

const (
	CommitmentTerminationForfeit       CommitmentEarlyTermination = "forfeit"
	CommitmentTerminationCreditNote    CommitmentEarlyTermination = "credit_note"
	CommitmentTerminationBillRemaining CommitmentEarlyTermination = "bill_remaining"
)

func (e CommitmentEarlyTermination) Validate() error {
	return validateEnum("on_early_termination", e, []CommitmentEarlyTermination{
		CommitmentTerminationForfeit, CommitmentTerminationCreditNote, CommitmentTerminationBillRemaining,
	})
}

type CommitmentStatus string

const (
	CommitmentStatusActive     CommitmentStatus = "active"
	CommitmentStatusEnded      CommitmentStatus = "ended"
	CommitmentStatusTerminated CommitmentStatus = "terminated"
)

func (s CommitmentStatus) Validate() error {
	return validateEnum("commitment_status", s, []CommitmentStatus{CommitmentStatusActive, CommitmentStatusEnded, CommitmentStatusTerminated})
}

// CommitmentLineKind tags invoice lines produced for a commitment.
type CommitmentLineKind string

const (
	CommitmentLineWithin     CommitmentLineKind = "within_commitment"
	CommitmentLineOnDemand   CommitmentLineKind = "on_demand"
	CommitmentLineTrueUp     CommitmentLineKind = "true_up"
	CommitmentLineInstalment CommitmentLineKind = "instalment"
	CommitmentLineSettlement CommitmentLineKind = "settlement"
)

func validateEnum[T ~string](field string, v T, allowed []T) error {
	if v == "" || lo.Contains(allowed, v) {
		return nil
	}
	return ierr.NewErrorf("invalid %s %q", field, v).
		WithHintf("%s must be one of %v", field, allowed).
		Mark(ierr.ErrValidation)
}

// ActiveWindow is a half-open [Start, End) minute-of-day range in HH:MM; End < Start wraps past midnight.
type ActiveWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type ActiveWindows []ActiveWindow

func parseHHMM(s string) (int, error) {
	if len(s) != 5 || s[2] != ':' {
		return 0, fmt.Errorf("invalid time %q", s)
	}
	for _, i := range []int{0, 1, 3, 4} {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("invalid time %q", s)
		}
	}
	h := int(s[0]-'0')*10 + int(s[1]-'0')
	m := int(s[3]-'0')*10 + int(s[4]-'0')
	if h > 23 || m > 59 {
		return 0, fmt.Errorf("invalid time %q", s)
	}
	return h*60 + m, nil
}

// minutes returns the set of minute-of-day slots the window covers.
func (w ActiveWindow) minutes() ([]bool, error) {
	start, err := parseHHMM(w.Start)
	if err != nil {
		return nil, err
	}
	end, err := parseHHMM(w.End)
	if err != nil {
		return nil, err
	}
	if start == end {
		return nil, fmt.Errorf("window start equals end")
	}
	slots := make([]bool, 1440)
	for m := start; m != end; m = (m + 1) % 1440 {
		slots[m] = true
	}
	return slots, nil
}

func (ws ActiveWindows) Validate() error {
	covered := make([]bool, 1440)
	for _, w := range ws {
		slots, err := w.minutes()
		if err != nil {
			return ierr.WithError(err).WithHint("active_windows entries must be HH:MM with start different from end").Mark(ierr.ErrValidation)
		}
		for i, on := range slots {
			if on && covered[i] {
				return ierr.NewError("active_windows overlap").WithHint("ranges within one commitment must not overlap").Mark(ierr.ErrValidation)
			}
			covered[i] = covered[i] || on
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
	cur := local.Hour()*60 + local.Minute()
	for _, w := range ws {
		start, err1 := parseHHMM(w.Start)
		end, err2 := parseHHMM(w.End)
		if err1 != nil || err2 != nil {
			continue
		}
		if start < end && cur >= start && cur < end {
			return true
		}
		if start > end && (cur >= start || cur < end) {
			return true
		}
	}
	return false
}

type CommitmentFilter struct {
	*QueryFilter
	*TimeRangeFilter
	Filters         []*FilterCondition   `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort            []*SortCondition     `json:"sort,omitempty" form:"sort" validate:"omitempty"`
	IDs             []string             `json:"ids,omitempty" form:"ids"`
	SubscriptionIDs []string             `json:"subscription_ids,omitempty" form:"subscription_ids"`
	CustomerIDs     []string             `json:"customer_ids,omitempty" form:"customer_ids"`
	Statuses        []CommitmentStatus   `json:"statuses,omitempty" form:"statuses"`
	ScopeType       *CommitmentScopeType `json:"scope_entity_type,omitempty" form:"scope_entity_type"`
	ScopeIDs        []string             `json:"scope_entity_ids,omitempty" form:"scope_entity_ids"`
	// ActiveFrom/ActiveTo select commitments whose term overlaps [ActiveFrom, ActiveTo).
	ActiveFrom *time.Time `json:"active_from,omitempty" form:"active_from"`
	ActiveTo   *time.Time `json:"active_to,omitempty" form:"active_to"`
}

func NewDefaultCommitmentFilter() *CommitmentFilter {
	return &CommitmentFilter{QueryFilter: NewDefaultQueryFilter()}
}

func NewNoLimitCommitmentFilter() *CommitmentFilter {
	return &CommitmentFilter{QueryFilter: NewNoLimitQueryFilter()}
}

func (f CommitmentFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return err
		}
	}
	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return err
		}
	}
	for _, s := range f.Statuses {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	if f.ScopeType != nil {
		return f.ScopeType.Validate()
	}
	return nil
}

// GetLimit implements BaseFilter interface.
func (f *CommitmentFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface.
func (f *CommitmentFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface.
func (f *CommitmentFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface.
func (f *CommitmentFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter — row-level status, not the commitment status.
func (f *CommitmentFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface.
func (f *CommitmentFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited returns true if this is an unlimited query.
func (f *CommitmentFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
