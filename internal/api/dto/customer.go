package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

type BillingConfiguration struct {
	// This is the unique identifier you've assigned to your integration.
	ConnectionCode string `json:"connection_code" validate:"required"`

	// The type of payment provider to use for the customer.
	PaymentProviderType types.SecretProvider `json:"payment_provider_type" validate:"required"`

	// If you already have a customer in your billing provider, you can use this field to link your Flexprice customer to that existing customer.
	// This is useful if you want to use your existing customer's billing information without creating a new one in your billing system.
	ProviderCustomerID string `json:"provider_customer_id" validate:"omitempty"`

	// If you want to create a new customer in your billing provider, you can set this to true.
	// This will create a new customer in your billing provider and link it to your Flexprice customer.
	SyncWithProvider bool `json:"sync_with_provider" validate:"required" default:"false"`
}

func (b *BillingConfiguration) Validate() error {
	err := validator.ValidateRequest(b)
	if err != nil {
		return err
	}

	if err := b.PaymentProviderType.Validate(); err != nil {
		return err
	}

	if !b.SyncWithProvider && b.ProviderCustomerID == "" {
		return ierr.NewError("provider customer id is required when sync_with_provider is false").
			WithHint("Please provide a provider customer id").
			Mark(ierr.ErrValidation)
	}

	return nil
}

type CreateCustomerRequest struct {
	ExternalID        string            `json:"external_id" validate:"required"`
	Name              string            `json:"name"`
	Email             string            `json:"email" validate:"omitempty,email"`
	AddressLine1      string            `json:"address_line1" validate:"omitempty,max=255"`
	AddressLine2      string            `json:"address_line2" validate:"omitempty,max=255"`
	AddressCity       string            `json:"address_city" validate:"omitempty,max=100"`
	AddressState      string            `json:"address_state" validate:"omitempty,max=100"`
	AddressPostalCode string            `json:"address_postal_code" validate:"omitempty,max=20"`
	AddressCountry    string            `json:"address_country" validate:"omitempty,len=2,iso3166_1_alpha2"`
	Metadata          map[string]string `json:"metadata,omitempty"`

	// Billing configuration for the customer
	BillingConfiguration *BillingConfiguration `json:"billing_configuration,omitempty"`
}

type UpdateCustomerRequest struct {
	ExternalID        *string           `json:"external_id"`
	Name              *string           `json:"name"`
	Email             *string           `json:"email" validate:"omitempty,email"`
	AddressLine1      *string           `json:"address_line1" validate:"omitempty,max=255"`
	AddressLine2      *string           `json:"address_line2" validate:"omitempty,max=255"`
	AddressCity       *string           `json:"address_city" validate:"omitempty,max=100"`
	AddressState      *string           `json:"address_state" validate:"omitempty,max=100"`
	AddressPostalCode *string           `json:"address_postal_code" validate:"omitempty,max=20"`
	AddressCountry    *string           `json:"address_country" validate:"omitempty,len=2,iso3166_1_alpha2"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

type CustomerResponse struct {
	*customer.Customer
}

// ListCustomersResponse represents the response for listing customers
type ListCustomersResponse = types.ListResponse[*CustomerResponse]

func (r *CreateCustomerRequest) Validate() error {
	return validator.ValidateRequest(r)
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
