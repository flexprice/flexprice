package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

type ConnectionFilter struct {
	*QueryFilter
	*TimeRangeFilter
	ProviderType []SecretProvider        `json:"provider_type,omitempty" form:"provider_type"`
	Status       []Status                `json:"status,omitempty" form:"status"`
	Capabilities []IntegrationCapability `json:"capabilities,omitempty" form:"capabilities"`
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

	// Enforce constraint: can't use both ProviderType and Capabilities filters simultaneously
	if len(f.ProviderType) > 0 && len(f.Capabilities) > 0 {
		return ierr.NewError("cannot specify both provider_type and capabilities at the same time").
			WithHint("Please specify either provider_type or capabilities, not both").
			Mark(ierr.ErrValidation)
	}

	// validate capabilities
	for _, capability := range f.Capabilities {
		if err := capability.Validate(); err != nil {
			return err
		}
	}

	for _, providerType := range f.ProviderType {
		if err := providerType.Validate(); err != nil {
			return err
		}
	}

	return nil
}
