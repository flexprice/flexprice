package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// IntegrationEntityMapping represents a provider integration mapping
// @Description Integration entity mapping for external provider systems
type IntegrationEntityMapping struct {
	// provider is the integration provider name (e.g., "stripe", "razorpay")
	Provider string `json:"provider" validate:"required,oneof=stripe razorpay paypal"`

	// id is the external entity ID from the provider
	ID string `json:"id" validate:"required"`
}

// CreateCustomerRequest represents the request to create a new customer
// @Description Request object for creating a new customer in the system
type CreateCustomerRequest struct {
	// external_id is the unique identifier from your system to reference this customer (required)
	ExternalID string `json:"external_id" validate:"required"`

	// name is the full name or company name of the customer
	Name string `json:"name"`

	// email is the customer's email address and must be a valid email format if provided
	Email string `json:"email" validate:"omitempty,email"`

	// address_line1 is the primary address line with maximum 255 characters
	AddressLine1 string `json:"address_line1" validate:"omitempty,max=255"`

	// address_line2 is the secondary address line with maximum 255 characters
	AddressLine2 string `json:"address_line2" validate:"omitempty,max=255"`

	// address_city is the city name with maximum 100 characters
	AddressCity string `json:"address_city" validate:"omitempty,max=100"`

	// address_state is the state, province, or region name with maximum 100 characters
	AddressState string `json:"address_state" validate:"omitempty,max=100"`

	// address_postal_code is the ZIP code or postal code with maximum 20 characters
	AddressPostalCode string `json:"address_postal_code" validate:"omitempty,max=20"`

	// address_country is the two-letter ISO 3166-1 alpha-2 country code
	AddressCountry string `json:"address_country" validate:"omitempty,len=2,iso3166_1_alpha2"`

	// metadata contains additional key-value pairs for storing extra information
	Metadata map[string]string `json:"metadata,omitempty"`

	// tax_rate_overrides contains tax rate configurations to be linked to this customer
	TaxRateOverrides []*TaxRateOverride `json:"tax_rate_overrides,omitempty"`

	// integration_entity_mapping contains provider integration mappings for this customer
	IntegrationEntityMapping []*IntegrationEntityMapping `json:"integration_entity_mapping,omitempty"`
}

// UpdateCustomerRequest represents the request to update an existing customer
// @Description Request object for updating an existing customer. All fields are optional - only provided fields will be updated
type UpdateCustomerRequest struct {
	// external_id is the updated external identifier for the customer
	ExternalID *string `json:"external_id"`

	// name is the updated name or company name for the customer
	Name *string `json:"name"`

	// email is the updated email address and must be a valid email format if provided
	Email *string `json:"email" validate:"omitempty,email"`

	// address_line1 is the updated primary address line with maximum 255 characters
	AddressLine1 *string `json:"address_line1" validate:"omitempty,max=255"`

	// address_line2 is the updated secondary address line with maximum 255 characters
	AddressLine2 *string `json:"address_line2" validate:"omitempty,max=255"`

	// address_city is the updated city name with maximum 100 characters
	AddressCity *string `json:"address_city" validate:"omitempty,max=100"`

	// address_state is the updated state, province, or region name with maximum 100 characters
	AddressState *string `json:"address_state" validate:"omitempty,max=100"`

	// address_postal_code is the updated postal code with maximum 20 characters
	AddressPostalCode *string `json:"address_postal_code" validate:"omitempty,max=20"`

	// address_country is the updated two-letter ISO 3166-1 alpha-2 country code
	AddressCountry *string `json:"address_country" validate:"omitempty,len=2,iso3166_1_alpha2"`

	// metadata contains updated key-value pairs that will replace existing metadata
	Metadata map[string]string `json:"metadata,omitempty"`

	// integration_entity_mapping contains provider integration mappings for this customer
	IntegrationEntityMapping []*IntegrationEntityMapping `json:"integration_entity_mapping,omitempty"`
}

// CustomerResponse represents the response for customer operations
// @Description Customer response object containing all customer information
type CustomerResponse struct {
	*customer.Customer
}

// ListCustomersResponse represents the response for listing customers
// @Description Response object for listing customers with pagination
type ListCustomersResponse = types.ListResponse[*CustomerResponse]

func (r *CreateCustomerRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	// Validate tax rate overrides if provided
	if len(r.TaxRateOverrides) > 0 {
		for i, taxRate := range r.TaxRateOverrides {
			if err := taxRate.Validate(); err != nil {
				return ierr.WithError(err).
					WithHint("Invalid tax rate configuration at index " + string(rune(i))).
					Mark(ierr.ErrValidation)
			}
		}
	}

	// Validate integration entity mappings if provided
	if len(r.IntegrationEntityMapping) > 0 {
		for i, mapping := range r.IntegrationEntityMapping {
			if err := validator.ValidateRequest(mapping); err != nil {
				return ierr.WithError(err).
					WithHint("Invalid integration entity mapping at index " + string(rune(i))).
					Mark(ierr.ErrValidation)
			}
		}
	}

	return nil
}

func (r *CreateCustomerRequest) ToCustomer(ctx context.Context) *customer.Customer {
	return &customer.Customer{
		ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID:        r.ExternalID,
		Name:              r.Name,
		Email:             r.Email,
		AddressLine1:      r.AddressLine1,
		AddressLine2:      r.AddressLine2,
		AddressCity:       r.AddressCity,
		AddressState:      r.AddressState,
		AddressPostalCode: r.AddressPostalCode,
		AddressCountry:    r.AddressCountry,
		Metadata:          r.Metadata,
		EnvironmentID:     types.GetEnvironmentID(ctx),
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
}

func (r *UpdateCustomerRequest) Validate() error {
	return validator.ValidateRequest(r)
}
