package accountingperiod

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// Filter represents the filter options for accounting periods.
// TimeRangeFilter bounds period_start, not created_at.
type Filter struct {
	*types.QueryFilter
	*types.TimeRangeFilter

	Filters []*types.FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort    []*types.SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`

	AccountingPeriodIDs []string                       `json:"accounting_period_ids,omitempty" form:"accounting_period_ids" validate:"omitempty"`
	PeriodStatuses      []types.AccountingPeriodStatus `json:"period_statuses,omitempty" form:"period_statuses" validate:"omitempty"`
}

func NewFilter() *Filter {
	return &Filter{QueryFilter: types.NewDefaultQueryFilter()}
}

func (f *Filter) Validate() error {
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
	for _, id := range f.AccountingPeriodIDs {
		if id == "" {
			return ierr.NewError("accounting period id can not be empty").
				WithHint("Accounting period id can not be empty").
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

func (f *Filter) queryFilter() *types.QueryFilter {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter()
	}
	return f.QueryFilter
}

func (f *Filter) GetLimit() int           { return f.queryFilter().GetLimit() }
func (f *Filter) GetOffset() int          { return f.queryFilter().GetOffset() }
func (f *Filter) GetSort() string         { return f.queryFilter().GetSort() }
func (f *Filter) GetOrder() string        { return f.queryFilter().GetOrder() }
func (f *Filter) GetStatus() string       { return f.queryFilter().GetStatus() }
func (f *Filter) GetExpand() types.Expand { return f.queryFilter().GetExpand() }
func (f *Filter) IsUnlimited() bool       { return f.queryFilter().IsUnlimited() }
