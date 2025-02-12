package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
)

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
	return validator.New().Struct(r)
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
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
}

func (r *UpdateCustomerRequest) Validate() error {
	return validator.New().Struct(r)
}
