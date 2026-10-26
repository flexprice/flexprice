package journal

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// Filter for journal entries. TimeRangeFilter bounds business_date; Accounts and POBIDs match entries with such a line.
type Filter struct {
	*types.QueryFilter
	*types.TimeRangeFilter

	Filters []*types.FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort    []*types.SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`

	JournalEntryIDs     []string                 `json:"journal_entry_ids,omitempty" form:"journal_entry_ids" validate:"omitempty"`
	AccountingPeriodIDs []string                 `json:"accounting_period_ids,omitempty" form:"accounting_period_ids" validate:"omitempty"`
	EventTypes          []types.JournalEventType `json:"event_types,omitempty" form:"event_types" validate:"omitempty"`
	Accounts            []types.LedgerAccountKey `json:"accounts,omitempty" form:"accounts" validate:"omitempty"`
	CustomerIDs         []string                 `json:"customer_ids,omitempty" form:"customer_ids" validate:"omitempty"`
	ContractIDs         []string                 `json:"contract_ids,omitempty" form:"contract_ids" validate:"omitempty"`
	POBIDs              []string                 `json:"pob_ids,omitempty" form:"pob_ids" validate:"omitempty"`
	SourceEntityType    types.RevRecEntityType   `json:"source_entity_type,omitempty" form:"source_entity_type" validate:"omitempty"`
	SourceEntityIDs     []string                 `json:"source_entity_ids,omitempty" form:"source_entity_ids" validate:"omitempty"`
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
	for _, id := range f.JournalEntryIDs {
		if id == "" {
			return ierr.NewError("journal entry id can not be empty").
				WithHint("Journal entry id can not be empty").
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

// SumFilter selects lines for SumLines. Lines in periods before PeriodStart fold into one opening row per group.
type SumFilter struct {
	Currency     string
	PeriodStart  time.Time // first reported month
	PeriodEnd    time.Time // last reported month, inclusive
	Accounts     []types.LedgerAccountKey
	CustomerIDs  []string
	ContractIDs  []string
	POBIDs       []string
	CreatedAtLTE *time.Time // only entries created at or before it
	GroupBy      []types.RevenueReportGroupBy
	// ExcludeOpening keeps only periods from PeriodStart: no opening row, nothing before the range summed.
	ExcludeOpening bool
}
