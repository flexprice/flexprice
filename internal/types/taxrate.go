package types

import (
	"slices"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

type TaxRateType string

const (
	TaxRateTypePercentage TaxRateType = "percentage"
	TaxRateTypeFixed      TaxRateType = "fixed"
)

func (t TaxRateType) String() string {
	return string(t)
}

func (t TaxRateType) Validate() error {
	allowedValues := []string{string(TaxRateTypePercentage), string(TaxRateTypeFixed)}
	if !slices.Contains(allowedValues, string(t)) {
		return ierr.NewError("invalid tax rate type").
			WithHint("Tax rate type must be either percentage or fixed").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// TaxRateScope defines the scope/visibility of a tax rate
type TaxRateScope string

const (
	TaxRateScopeInternal TaxRateScope = "INTERNAL"
	TaxRateScopeExternal TaxRateScope = "EXTERNAL"
	TaxRateScopeOneTime  TaxRateScope = "ONETIME"
)

func (s TaxRateScope) String() string {
	return string(s)
}

func (s TaxRateScope) Validate() error {
	allowedValues := []string{
		TaxRateScopeInternal.String(),
		TaxRateScopeExternal.String(),
		TaxRateScopeOneTime.String(),
	}

	if !slices.Contains(allowedValues, string(s)) {
		return ierr.NewError("invalid tax rate scope").
			WithHint("Tax rate scope must be either INTERNAL, EXTERNAL, or ONETIME").
			Mark(ierr.ErrValidation)
	}
	return nil
}

type TaxrateEntityType string

const (
	TaxrateEntityTypeCustomer     TaxrateEntityType = "CUSTOMER"
	TaxrateEntityTypeSubscription TaxrateEntityType = "SUBSCRIPTION"
	TaxrateEntityTypeInvoice      TaxrateEntityType = "INVOICE"
)

func (t TaxrateEntityType) String() string {
	return string(t)
}

func (t TaxrateEntityType) Validate() error {
	allowedValues := []string{
		TaxrateEntityTypeCustomer.String(),
		TaxrateEntityTypeSubscription.String(),
		TaxrateEntityTypeInvoice.String(),
	}

	if !slices.Contains(allowedValues, string(t)) {
		return ierr.NewError("invalid tax rate entity type").
			WithHint("Invalid tax rate entity type").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// TaxRateStatus defines the status of a tax rate
type TaxRateStatus string

const (
	TaxRateStatusActive   TaxRateStatus = "ACTIVE"
	TaxRateStatusInactive TaxRateStatus = "INACTIVE"
)

func (s TaxRateStatus) String() string {
	return string(s)
}

func (s TaxRateStatus) Validate() error {
	allowedValues := []string{
		TaxRateStatusActive.String(),
		TaxRateStatusInactive.String(),
	}

	if !slices.Contains(allowedValues, string(s)) {
		return ierr.NewError("invalid tax rate status").
			WithHint("Tax rate status must be either ACTIVE or INACTIVE").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// TaxRateAssignmentStatus defines the status of a tax rate assignment
type TaxRateAssignmentStatus string

const (
	TaxRateAssignmentStatusActive    TaxRateAssignmentStatus = "ACTIVE"
	TaxRateAssignmentStatusInactive  TaxRateAssignmentStatus = "INACTIVE"
	TaxRateAssignmentStatusSuspended TaxRateAssignmentStatus = "SUSPENDED"
)

func (s TaxRateAssignmentStatus) String() string {
	return string(s)
}

func (s TaxRateAssignmentStatus) Validate() error {
	allowedValues := []string{
		TaxRateAssignmentStatusActive.String(),
		TaxRateAssignmentStatusInactive.String(),
		TaxRateAssignmentStatusSuspended.String(),
	}

	if !slices.Contains(allowedValues, string(s)) {
		return ierr.NewError("invalid tax rate assignment status").
			WithHint("Tax rate assignment status must be ACTIVE, INACTIVE, or SUSPENDED").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// TaxRateFilter represents filters for taxrate queries
type TaxRateFilter struct {
	*QueryFilter
	*TimeRangeFilter
	Filters    []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort       []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`
	TaxRateIDs []string           `json:"taxrate_ids,omitempty" form:"taxrate_ids" validate:"omitempty"`
	Code       string             `json:"code,omitempty" form:"code" validate:"omitempty"`
	Scope      TaxRateScope       `json:"scope,omitempty" form:"scope" validate:"omitempty"`
}

// NewTaxRateFilter creates a new TaxRateFilter with default values
func NewTaxRateFilter() *TaxRateFilter {
	return &TaxRateFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitTaxRateFilter creates a new TaxRateFilter with no pagination limits
func NewNoLimitTaxRateFilter() *TaxRateFilter {
	return &TaxRateFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the TaxRateFilter
func (f TaxRateFilter) Validate() error {
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

	if f.Filters != nil {
		for _, filter := range f.Filters {
			if err := filter.Validate(); err != nil {
				return err
			}
		}
	}

	if f.Sort != nil {
		for _, sort := range f.Sort {
			if err := sort.Validate(); err != nil {
				return err
			}
		}
	}

	if f.TaxRateIDs != nil {
		for _, id := range f.TaxRateIDs {
			if id == "" {
				return ierr.NewError("taxrate_ids cannot contain empty strings").
					WithHint("Taxrate IDs must be non-empty strings").
					Mark(ierr.ErrValidation)
			}
		}
	}

	if f.Scope != "" {
		if err := f.Scope.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// GetLimit returns the limit for the TaxRateFilter
func (f TaxRateFilter) GetLimit() int {
	return f.QueryFilter.GetLimit()
}
