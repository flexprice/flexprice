package types

// MeterFilter represents the filter options for meter queries
type MeterFilter struct {
	*QueryFilter
	*TimeRangeFilter
	EventName string   `json:"event_name,omitempty"`
	MeterIDs  []string `json:"meter_ids,omitempty"`
}

// NewMeterFilter creates a new MeterFilter with default values
func NewMeterFilter() *MeterFilter {
	return &MeterFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitMeterFilter creates a new MeterFilter with no pagination limits
func NewNoLimitMeterFilter() *MeterFilter {
	return &MeterFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// MeterSortField represents the available fields for sorting meters
type MeterSortField string

const (
	MeterSortFieldCreatedAt MeterSortField = "created_at"
	MeterSortFieldName      MeterSortField = "name"
	MeterSortFieldEventName MeterSortField = "event_name"
)

// Validate validates the meter filter
func (f *MeterFilter) Validate() error {
	if f == nil {
		return nil
	}

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

	return nil
}

// GetLimit implements BaseFilter interface
func (f *MeterFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *MeterFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *MeterFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetStatus implements BaseFilter interface
func (f *MeterFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetOrder implements BaseFilter interface
func (f *MeterFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetExpand implements BaseFilter interface
func (f *MeterFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *MeterFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
