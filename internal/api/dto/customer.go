package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
)

type CreateCustomerRequest struct {
	ExternalID string `json:"external_id" validate:"required"`
	Name       string `json:"name"`
	Email      string `json:"email"`
}

type UpdateCustomerRequest struct {
	ExternalID *string `json:"external_id"`
	Name       *string `json:"name"`
	Email      *string `json:"email"`
}

type CustomerResponse struct {
	*customer.Customer
}

type ListCustomersResponse struct {
	Customers []CustomerResponse `json:"customers"`
	Total     int                `json:"total"`
	Offset    int                `json:"offset"`
	Limit     int                `json:"limit"`
}

func (r *CreateCustomerRequest) Validate() error {
	return validator.New().Struct(r)
}

func (r *CreateCustomerRequest) ToCustomer(ctx context.Context) *customer.Customer {
	return &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: r.ExternalID,
		Name:       r.Name,
		Email:      r.Email,
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
}

func (r *UpdateCustomerRequest) Validate() error {
	return validator.New().Struct(r)
}
