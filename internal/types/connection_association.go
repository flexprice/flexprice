package types

import (
	"slices"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// ConnectionAssociationEntityType is the kind of entity a connection association links.
type ConnectionAssociationEntityType string

const (
	// ConnectionAssociationEntityTypeBillingEntity marks the billing entity that owns the connection.
	ConnectionAssociationEntityTypeBillingEntity ConnectionAssociationEntityType = "billing_entity"
	// ConnectionAssociationEntityTypeCustomer marks a customer's preferred connection.
	ConnectionAssociationEntityTypeCustomer ConnectionAssociationEntityType = "customer"
)

func (t ConnectionAssociationEntityType) String() string {
	return string(t)
}

func (t ConnectionAssociationEntityType) Validate() error {
	allowedValues := []string{
		ConnectionAssociationEntityTypeBillingEntity.String(),
		ConnectionAssociationEntityTypeCustomer.String(),
	}

	if !slices.Contains(allowedValues, string(t)) {
		return ierr.NewError("invalid connection association entity type").
			WithHint("Entity type must be billing_entity or customer").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ConnectionAssociationFilter represents the filter options for listing connection associations
type ConnectionAssociationFilter struct {
	*QueryFilter
	EntityType    ConnectionAssociationEntityType `json:"entity_type,omitempty" form:"entity_type"`
	EntityIDs     []string                        `json:"entity_ids,omitempty" form:"entity_ids"`
	ConnectionIDs []string                        `json:"connection_ids,omitempty" form:"connection_ids"`
	ProviderType  SecretProvider                  `json:"provider_type,omitempty" form:"provider_type"`
}

// NewNoLimitConnectionAssociationFilter creates a connection association filter without pagination
func NewNoLimitConnectionAssociationFilter() *ConnectionAssociationFilter {
	return &ConnectionAssociationFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the connection association filter
func (f *ConnectionAssociationFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return ierr.WithError(err).
				WithHint("invalid query filter").
				Mark(ierr.ErrValidation)
		}
	}
	if f.EntityType != "" {
		if err := f.EntityType.Validate(); err != nil {
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

// GetLimit implements BaseFilter interface
func (f *ConnectionAssociationFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *ConnectionAssociationFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *ConnectionAssociationFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *ConnectionAssociationFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *ConnectionAssociationFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *ConnectionAssociationFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited implements BaseFilter interface
func (f *ConnectionAssociationFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
