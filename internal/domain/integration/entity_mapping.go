package integration

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// ProviderType represents the external provider type
type ProviderType string

const (
	ProviderTypeStripe ProviderType = "stripe"
)

// EntityType represents the type of entity being mapped
type EntityType string

const (
	EntityTypeCustomer EntityType = "customer"
	// Future entity types can be added here:
	// EntityTypeSubscription EntityType = "subscription"
	// EntityTypeInvoice     EntityType = "invoice"
)

// EntityIntegrationMapping represents a mapping between a FlexPrice entity and an external provider entity
type EntityIntegrationMapping struct {
	// ID is the unique identifier for the mapping
	ID string `db:"id" json:"id"`

	// EntityID is the FlexPrice entity identifier (customer ID, subscription ID, etc.)
	EntityID string `db:"entity_id" json:"entity_id"`

	// EntityType is the type of entity being mapped (customer, subscription, etc.)
	EntityType EntityType `db:"entity_type" json:"entity_type"`

	// ProviderType is the external provider type (e.g., "stripe")
	ProviderType ProviderType `db:"provider_type" json:"provider_type"`

	// ProviderEntityID is the external provider's entity identifier
	ProviderEntityID string `db:"provider_entity_id" json:"provider_entity_id"`

	// EnvironmentID is the environment identifier for the mapping
	EnvironmentID string `db:"environment_id" json:"environment_id"`

	// Metadata stores additional provider-specific data
	Metadata map[string]interface{} `db:"metadata" json:"metadata"`

	types.BaseModel
}

// TODO: FromEnt and FromEntList will be implemented after Ent code generation

// ValidateProviderType validates the provider type
func ValidateProviderType(providerType ProviderType) bool {
	switch providerType {
	case ProviderTypeStripe:
		return true
	default:
		return false
	}
}

// ValidateEntityType validates the entity type
func ValidateEntityType(entityType EntityType) bool {
	switch entityType {
	case EntityTypeCustomer:
		return true
	default:
		return false
	}
}

// ValidateProviderEntityID validates the provider entity ID format
func ValidateProviderEntityID(providerEntityID string, providerType ProviderType, entityType EntityType) bool {
	if providerEntityID == "" {
		return false
	}

	switch providerType {
	case ProviderTypeStripe:
		switch entityType {
		case EntityTypeCustomer:
			// Stripe customer IDs start with "cus_" and are followed by alphanumeric characters
			if len(providerEntityID) < 8 {
				return false
			}
			return providerEntityID[:4] == "cus_"
		default:
			// Future entity types can have their own validation rules
			return len(providerEntityID) > 0 && len(providerEntityID) <= 255
		}
	default:
		return len(providerEntityID) > 0 && len(providerEntityID) <= 255
	}
}

// Validate validates all fields of the entity integration mapping
func (e *EntityIntegrationMapping) Validate() error {
	if e.EntityID == "" {
		return ierr.NewError("entity_id is required").
			WithHint("Entity ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if !ValidateEntityType(e.EntityType) {
		return ierr.NewError("invalid entity_type").
			WithHint("Entity type must be one of: customer").
			Mark(ierr.ErrValidation)
	}

	if !ValidateProviderType(e.ProviderType) {
		return ierr.NewError("invalid provider_type").
			WithHint("Provider type must be one of: stripe").
			Mark(ierr.ErrValidation)
	}

	if !ValidateProviderEntityID(e.ProviderEntityID, e.ProviderType, e.EntityType) {
		return ierr.NewError("invalid provider_entity_id").
			WithHint("Provider entity ID format is invalid for the specified provider and entity type").
			Mark(ierr.ErrValidation)
	}

	if len(e.EntityID) > 50 {
		return ierr.NewError("entity_id too long").
			WithHint("Entity ID must be less than 50 characters").
			Mark(ierr.ErrValidation)
	}

	if len(string(e.EntityType)) > 50 {
		return ierr.NewError("entity_type too long").
			WithHint("Entity type must be less than 50 characters").
			Mark(ierr.ErrValidation)
	}

	if len(string(e.ProviderType)) > 50 {
		return ierr.NewError("provider_type too long").
			WithHint("Provider type must be less than 50 characters").
			Mark(ierr.ErrValidation)
	}

	if len(e.ProviderEntityID) > 255 {
		return ierr.NewError("provider_entity_id too long").
			WithHint("Provider entity ID must be less than 255 characters").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Helper methods for common use cases

// NewCustomerMapping creates a new entity integration mapping for a customer
func NewCustomerMapping(customerID, providerCustomerID string, providerType ProviderType, environmentID string, metadata map[string]interface{}) *EntityIntegrationMapping {
	return &EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         customerID,
		EntityType:       EntityTypeCustomer,
		ProviderType:     providerType,
		ProviderEntityID: providerCustomerID,
		EnvironmentID:    environmentID,
		Metadata:         metadata,
	}
}

// IsCustomerMapping returns true if this mapping is for a customer entity
func (e *EntityIntegrationMapping) IsCustomerMapping() bool {
	return e.EntityType == EntityTypeCustomer
}

// GetCustomerID returns the customer ID if this is a customer mapping, empty string otherwise
func (e *EntityIntegrationMapping) GetCustomerID() string {
	if e.IsCustomerMapping() {
		return e.EntityID
	}
	return ""
}
