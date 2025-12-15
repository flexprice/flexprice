package entityintegrationmapping

import (
	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// EntityIntegrationMapping represents an entity integration mapping in the system
type EntityIntegrationMapping struct {
	// ID is the unique identifier for the mapping
	ID string `db:"id" json:"id"`

	// EntityID is the FlexPrice entity ID (e.g., customer_id, plan_id, etc.)
	EntityID string `db:"entity_id" json:"entity_id"`

	// EntityType is the type of entity (e.g., customer, plan, invoice, subscription, etc.)
	EntityType types.IntegrationEntityType `db:"entity_type" json:"entity_type"`

	// ProviderType is the payment provider type (e.g., stripe, razorpay, etc.)
	ProviderType string `db:"provider_type" json:"provider_type"`

	// ProviderEntityID is the provider's entity ID (e.g., stripe_customer_id, etc.)
	ProviderEntityID string `db:"provider_entity_id" json:"provider_entity_id"`

	// Metadata contains provider-specific data
	Metadata map[string]interface{} `db:"metadata" json:"metadata"`

	// EnvironmentID is the environment identifier
	EnvironmentID string `db:"environment_id" json:"environment_id"`

	types.BaseModel
}

// FromEnt converts an ent EntityIntegrationMapping to a domain EntityIntegrationMapping
func FromEnt(e *ent.EntityIntegrationMapping) *EntityIntegrationMapping {
	if e == nil {
		return nil
	}
	return &EntityIntegrationMapping{
		ID:               e.ID,
		EntityID:         e.EntityID,
		EntityType:       types.IntegrationEntityType(e.EntityType),
		ProviderType:     e.ProviderType,
		ProviderEntityID: e.ProviderEntityID,
		Metadata:         e.Metadata,
		EnvironmentID:    e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
}

// FromEntList converts a list of ent EntityIntegrationMapping to domain EntityIntegrationMapping
func FromEntList(mappings []*ent.EntityIntegrationMapping) []*EntityIntegrationMapping {
	result := make([]*EntityIntegrationMapping, len(mappings))
	for i, e := range mappings {
		result[i] = FromEnt(e)
	}
	return result
}

// ValidateEntityType validates the entity type
func ValidateEntityType(entityType types.IntegrationEntityType) bool {
	return entityType.Validate() == nil
}

// ValidateProviderType validates the provider type
func ValidateProviderType(providerType string) bool {
	validProviders := map[string]bool{
		"stripe":     true,
		"razorpay":   true,
		"paypal":     true,
		"quickbooks": true,
	}
	return validProviders[providerType]
}

// Validate validates the EntityIntegrationMapping
func Validate(m *EntityIntegrationMapping) error {
	if m.EntityID == "" {
		return ierr.NewError("entity_id is required").
			WithHint("Entity ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	if m.EntityType == "" {
		return ierr.NewError("entity_type is required").
			WithHint("Entity type cannot be empty").
			Mark(ierr.ErrValidation)
	}

	if err := m.EntityType.Validate(); err != nil {
		return err
	}

	if m.ProviderType == "" {
		return ierr.NewError("provider_type is required").
			WithHint("Provider type cannot be empty").
			Mark(ierr.ErrValidation)
	}

	if !ValidateProviderType(m.ProviderType) {
		return ierr.NewError("invalid provider_type").
			WithHint("Provider type must be one of: stripe, razorpay, paypal, quickbooks").
			Mark(ierr.ErrValidation)
	}

	if m.ProviderEntityID == "" {
		return ierr.NewError("provider_entity_id is required").
			WithHint("Provider entity ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Validate field lengths
	if len(m.EntityID) > 255 {
		return ierr.NewError("entity_id too long").
			WithHint("Entity ID must be less than 255 characters").
			Mark(ierr.ErrValidation)
	}

	// EntityType length validation is handled by the enum validation

	if len(m.ProviderType) > 50 {
		return ierr.NewError("provider_type too long").
			WithHint("Provider type must be less than 50 characters").
			Mark(ierr.ErrValidation)
	}

	if len(m.ProviderEntityID) > 255 {
		return ierr.NewError("provider_entity_id too long").
			WithHint("Provider entity ID must be less than 255 characters").
			Mark(ierr.ErrValidation)
	}

	return nil
}
