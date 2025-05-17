package types

type Connection struct {
	ID string `json:"id"`
}

type ConnectionFilter struct {
	*QueryFilter
	*TimeRangeFilter
	ProviderType SecretProvider `json:"provider_type,omitempty" form:"provider_type"`
	Status       []Status       `json:"status,omitempty" form:"status"`
}

func NewConnectionFilter() *ConnectionFilter {
	return &ConnectionFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

func NewNoLimitConnectionFilter() *ConnectionFilter {
	return &ConnectionFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the filter fields
func (f *ConnectionFilter) Validate() error {
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

	if f.ProviderType != "" {
		if err := f.ProviderType.Validate(); err != nil {
			return err
		}
	}

	return nil
}
