package customer

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

type Customer struct {
	// ID is the unique identifier for the customer
	ID string `db:"id" json:"id"`

	// ExternalID is the external identifier for the customer
	ExternalID string `db:"external_id" json:"external_id"`

	// Name is the name of the customer
	Name string `db:"name" json:"name"`

	// Email is the email of the customer
	Email string `db:"email" json:"email"`

	types.BaseModel
}

// FromEnt converts an Ent Customer to a domain Customer
func FromEnt(e *ent.Customer) *Customer {
	if e == nil {
		return nil
	}
	return &Customer{
		ID:         e.ID,
		ExternalID: e.ExternalID,
		Name:       e.Name,
		Email:      e.Email,
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

// FromEntList converts a list of Ent Customers to domain Customers
func FromEntList(list []*ent.Customer) []*Customer {
	if list == nil {
		return nil
	}
	customers := make([]*Customer, len(list))
	for i, item := range list {
		customers[i] = FromEnt(item)
	}
	return customers
}
