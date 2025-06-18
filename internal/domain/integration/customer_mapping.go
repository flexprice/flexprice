package integration

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// CustomerIntegrationMapping is a type alias for EntityIntegrationMapping specifically for customers
// This provides backward compatibility and cleaner APIs for customer-specific operations
type CustomerIntegrationMapping = EntityIntegrationMapping

// NewCustomerIntegrationMapping creates a new customer integration mapping
// This is a convenience wrapper around NewCustomerMapping for backward compatibility
func NewCustomerIntegrationMapping(customerID, providerCustomerID string, providerType ProviderType, environmentID string, metadata map[string]interface{}) *CustomerIntegrationMapping {
	return NewCustomerMapping(customerID, providerCustomerID, providerType, environmentID, metadata)
}

// ValidateCustomerMapping validates a customer integration mapping
// This provides additional customer-specific validation beyond the generic entity validation
func ValidateCustomerMapping(mapping *CustomerIntegrationMapping) error {
	// First validate using the generic entity validation
	if err := mapping.Validate(); err != nil {
		return err
	}

	// Ensure this is actually a customer mapping
	if !mapping.IsCustomerMapping() {
		return ierr.NewError("not a customer mapping").
			WithHint("Entity type must be 'customer'").
			Mark(ierr.ErrValidation)
	}

	// Customer-specific validations
	if mapping.EntityID == "" {
		return ierr.NewError("customer_id is required").
			WithHint("Customer ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	// Validate customer ID format (assuming UUIDs or similar)
	if len(mapping.EntityID) < 8 || len(mapping.EntityID) > 50 {
		return ierr.NewError("invalid customer_id format").
			WithHint("Customer ID must be between 8 and 50 characters").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Helper methods for customer mapping operations

// GetProviderCustomerID returns the provider customer ID for easier access
func (m *CustomerIntegrationMapping) GetProviderCustomerID() string {
	return m.ProviderEntityID
}

// SetProviderCustomerID sets the provider customer ID for easier access
func (m *CustomerIntegrationMapping) SetProviderCustomerID(providerCustomerID string) {
	m.ProviderEntityID = providerCustomerID
}

// IsStripeMapping returns true if this is a Stripe customer mapping
func (m *CustomerIntegrationMapping) IsStripeMapping() bool {
	return m.ProviderType == ProviderTypeStripe
}

// GetExternalID returns the external_id from metadata if present
func (m *CustomerIntegrationMapping) GetExternalID() string {
	if m.Metadata == nil {
		return ""
	}

	if externalID, ok := m.Metadata["external_id"].(string); ok {
		return externalID
	}

	return ""
}

// SetExternalID sets the external_id in metadata
func (m *CustomerIntegrationMapping) SetExternalID(externalID string) {
	if m.Metadata == nil {
		m.Metadata = make(map[string]interface{})
	}
	m.Metadata["external_id"] = externalID
}

// GetStripeAccountID returns the Stripe account ID from metadata if present
func (m *CustomerIntegrationMapping) GetStripeAccountID() string {
	if m.Metadata == nil {
		return ""
	}

	if accountID, ok := m.Metadata["stripe_account_id"].(string); ok {
		return accountID
	}

	return ""
}

// SetStripeAccountID sets the Stripe account ID in metadata
func (m *CustomerIntegrationMapping) SetStripeAccountID(accountID string) {
	if m.Metadata == nil {
		m.Metadata = make(map[string]interface{})
	}
	m.Metadata["stripe_account_id"] = accountID
}

// CustomerMappingFilter is a type alias for EntityIntegrationMappingFilter with customer-specific defaults
type CustomerMappingFilter struct {
	*EntityIntegrationMappingFilter
}

// NewCustomerMappingFilter creates a new filter specifically for customer mappings
func NewCustomerMappingFilter() *CustomerMappingFilter {
	filter := &EntityIntegrationMappingFilter{
		EntityTypes: []EntityType{EntityTypeCustomer},
	}
	return &CustomerMappingFilter{
		EntityIntegrationMappingFilter: filter,
	}
}

// WithProviderType adds a provider type filter
func (f *CustomerMappingFilter) WithProviderType(providerType ProviderType) *CustomerMappingFilter {
	f.ProviderTypes = []ProviderType{providerType}
	return f
}

// WithCustomerIDs adds customer ID filters
func (f *CustomerMappingFilter) WithCustomerIDs(customerIDs ...string) *CustomerMappingFilter {
	f.EntityIDs = customerIDs
	return f
}

// WithProviderCustomerIDs adds provider customer ID filters
func (f *CustomerMappingFilter) WithProviderCustomerIDs(providerCustomerIDs ...string) *CustomerMappingFilter {
	f.ProviderEntityIDs = providerCustomerIDs
	return f
}

// Convenience functions for common operations

// CreateStripeCustomerMapping creates a new Stripe customer mapping with common metadata
func CreateStripeCustomerMapping(customerID, stripeCustomerID, externalID, environmentID string) *CustomerIntegrationMapping {
	metadata := map[string]interface{}{
		"external_id": externalID,
		"created_via": "stripe_webhook",
	}

	mapping := NewCustomerIntegrationMapping(
		customerID,
		stripeCustomerID,
		ProviderTypeStripe,
		environmentID,
		metadata,
	)

	return mapping
}

// ValidateStripeCustomerID validates that a customer ID follows Stripe's format
func ValidateStripeCustomerID(customerID string) bool {
	return ValidateProviderEntityID(customerID, ProviderTypeStripe, EntityTypeCustomer)
}

// ExtractStripeCustomerID extracts the Stripe customer ID from a full Stripe customer object ID
// This handles cases where the ID might have prefixes or need cleaning
func ExtractStripeCustomerID(rawID string) string {
	// For Stripe, customer IDs are already in the correct format
	// But this function provides extensibility for future needs
	if len(rawID) >= 8 && rawID[:4] == "cus_" {
		return rawID
	}
	return ""
}
