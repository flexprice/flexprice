package contract

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// Filter represents the filter options for contracts
type Filter struct {
	*types.QueryFilter
	*types.TimeRangeFilter

	Filters []*types.FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort    []*types.SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`

	ContractIDs      []string               `json:"contract_ids,omitempty" form:"contract_ids" validate:"omitempty"`
	CustomerIDs      []string               `json:"customer_ids,omitempty" form:"customer_ids" validate:"omitempty"`
	SubscriptionIDs  []string               `json:"subscription_ids,omitempty" form:"subscription_ids" validate:"omitempty"`
	ContractStatuses []types.ContractStatus `json:"contract_statuses,omitempty" form:"contract_statuses" validate:"omitempty"`
	EndedAtGTE       *time.Time             `json:"ended_at_gte,omitempty" form:"ended_at_gte" validate:"omitempty"`
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
	for _, ids := range [][]string{f.ContractIDs, f.CustomerIDs, f.SubscriptionIDs} {
		for _, id := range ids {
			if id == "" {
				return ierr.NewError("id can not be empty").
					WithHint("Contract, customer and subscription ids can not be empty").
					Mark(ierr.ErrValidation)
			}
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
