package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// IntegrationEntityType represents the type of entity for integration mapping
type IntegrationEntityType string

const (
	IntegrationEntityTypeCustomer     IntegrationEntityType = "customer"
	IntegrationEntityTypePlan         IntegrationEntityType = "plan"
	IntegrationEntityTypeInvoice      IntegrationEntityType = "invoice"
	IntegrationEntityTypeSubscription IntegrationEntityType = "subscription"
	IntegrationEntityTypePayment      IntegrationEntityType = "payment"
	IntegrationEntityTypeCreditNote   IntegrationEntityType = "credit_note"
	IntegrationEntityTypeAddon        IntegrationEntityType = "addon"
	IntegrationEntityTypeItem         IntegrationEntityType = "item"
	IntegrationEntityTypeItemPrice    IntegrationEntityType = "item_price"
)

func (e IntegrationEntityType) String() string {
	return string(e)
}

func (e IntegrationEntityType) Validate() error {
	allowed := []IntegrationEntityType{
		IntegrationEntityTypeCustomer,
		IntegrationEntityTypePlan,
		IntegrationEntityTypeInvoice,
		IntegrationEntityTypeSubscription,
		IntegrationEntityTypePayment,
		IntegrationEntityTypeCreditNote,
		IntegrationEntityTypeAddon,
		IntegrationEntityTypeItem,
		IntegrationEntityTypeItemPrice,
	}
	if !lo.Contains(allowed, e) {
		return ierr.NewError("invalid entity type").
			WithHint("Entity type must be one of: customer, plan, invoice, subscription, payment, credit_note, addon, item, item_price").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// EntityIntegrationMappingFilter represents filters for entity integration mapping queries
type EntityIntegrationMappingFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// filters allows complex filtering based on multiple fields
	Filters []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort    []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`

	// Entity-specific filters
	EntityIDs  []string              `json:"entity_ids,omitempty" form:"entity_ids" validate:"omitempty"`
	EntityType IntegrationEntityType `json:"entity_type,omitempty" form:"entity_type" validate:"omitempty"`
	EntityID   string                `json:"entity_id,omitempty" form:"entity_id" validate:"omitempty"`

	// Provider-specific filters (only plural variants kept)
	ProviderTypes     []string `json:"provider_types,omitempty" form:"provider_types" validate:"omitempty"`
	ProviderEntityIDs []string `json:"provider_entity_ids,omitempty" form:"provider_entity_ids" validate:"omitempty"`
}

// NewEntityIntegrationMappingFilter creates a new EntityIntegrationMappingFilter with default values
func NewEntityIntegrationMappingFilter() *EntityIntegrationMappingFilter {
	return &EntityIntegrationMappingFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitEntityIntegrationMappingFilter creates a new EntityIntegrationMappingFilter with no pagination limits
func NewNoLimitEntityIntegrationMappingFilter() *EntityIntegrationMappingFilter {
	return &EntityIntegrationMappingFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the entity integration mapping filter
func (f EntityIntegrationMappingFilter) Validate() error {
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

	// Validate entity type if provided
	if f.EntityType != "" {
		if err := f.EntityType.Validate(); err != nil {
			return err
		}
	}

	// Validate provider types if provided
	if len(f.ProviderTypes) > 0 {
		validProviderTypes := map[string]bool{
			"stripe":   true,
			"razorpay": true,
			"paypal":   true,
		}
		for _, pt := range f.ProviderTypes {
			if !validProviderTypes[pt] {
				return ierr.NewError("invalid provider_type").
					WithHint("Provider type must be one of: stripe, razorpay, paypal").
					Mark(ierr.ErrValidation)
			}
		}
	}

	return nil
}

// GetLimit implements BaseFilter interface
func (f *EntityIntegrationMappingFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *EntityIntegrationMappingFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *EntityIntegrationMappingFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *EntityIntegrationMappingFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *EntityIntegrationMappingFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *EntityIntegrationMappingFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited implements BaseFilter interface
func (f *EntityIntegrationMappingFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
