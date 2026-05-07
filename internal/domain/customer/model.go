package customer

import (
	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// Customer represents a customer in the system
type Customer struct {
	// ID is the unique identifier for the customer
	ID string `db:"id" json:"id"`

	// ExternalID is the external identifier for the customer
	ExternalID string `db:"external_id" json:"external_id"`

	// Name is the name of the customer
	Name string `db:"name" json:"name"`

	// Email is the email of the customer
	Email string `db:"email" json:"email"`

	// AddressLine1 is the first line of the customer's address
	AddressLine1 string `db:"address_line1" json:"address_line1"`

	// AddressLine2 is the second line of the customer's address
	AddressLine2 string `db:"address_line2" json:"address_line2"`

	// AddressCity is the city of the customer's address
	AddressCity string `db:"address_city" json:"address_city"`

	// AddressState is the state of the customer's address
	AddressState string `db:"address_state" json:"address_state"`

	// AddressPostalCode is the postal code of the customer's address
	AddressPostalCode string `db:"address_postal_code" json:"address_postal_code"`

	// AddressCountry is the country of the customer's address (ISO 3166-1 alpha-2)
	AddressCountry string `db:"address_country" json:"address_country"`

	// Metadata
	Metadata map[string]string `db:"metadata" json:"metadata"`

	// EnvironmentID is the environment identifier for the customer
	EnvironmentID string `db:"environment_id" json:"environment_id"`

	types.BaseModel
}

// FromEnt converts an ent customer to a domain customer
func FromEnt(c *ent.Customer) *Customer {
	if c == nil {
		return nil
	}
	return &Customer{
		ID:                c.ID,
		ExternalID:        c.ExternalID,
		Name:              c.Name,
		Email:             c.Email,
		AddressLine1:      c.AddressLine1,
		AddressLine2:      c.AddressLine2,
		AddressCity:       c.AddressCity,
		AddressState:      c.AddressState,
		AddressPostalCode: c.AddressPostalCode,
		AddressCountry:    c.AddressCountry,
		Metadata:          c.Metadata,
		EnvironmentID:     c.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  c.TenantID,
			Status:    types.Status(c.Status),
			CreatedAt: c.CreatedAt,
			UpdatedAt: c.UpdatedAt,
			CreatedBy: c.CreatedBy,
			UpdatedBy: c.UpdatedBy,
		},
	}
}

// FromEntList converts a list of ent customers to domain customers
func FromEntList(customers []*ent.Customer) []*Customer {
	result := make([]*Customer, len(customers))
	for i, c := range customers {
		result[i] = FromEnt(c)
	}
	return result
}

// ValidateAddressCountry validates the country code format
func ValidateAddressCountry(country string) bool {
	if country == "" {
		return true
	}
	// Check if country code is exactly 2 characters
	if len(country) != 2 {
		return false
	}
	// TODO: Add validation against ISO 3166-1 alpha-2 codes
	return true
}

// ValidateAddressPostalCode validates the postal code format
func ValidateAddressPostalCode(postalCode string, country string) bool {
	if postalCode == "" {
		return true
	}
	// TODO: Add country-specific postal code validation
	return len(postalCode) <= 20
}

// ValidateAddress validates all address fields
func ValidateAddress(c *Customer) error {
	if !ValidateAddressCountry(c.AddressCountry) {
		return ierr.NewError("invalid country code format").
			WithHint("Country code must be 2 characters").
			Mark(ierr.ErrValidation)
	}
	if !ValidateAddressPostalCode(c.AddressPostalCode, c.AddressCountry) {
		return ierr.NewError("invalid postal code format").
			WithHint("Postal code must be less than 20 characters").
			Mark(ierr.ErrValidation)
	}
	// Validate field lengths
	if len(c.AddressLine1) > 255 {
		return ierr.NewError("address line 1 too long").
			WithHint("Address line 1 must be less than 255 characters").
			Mark(ierr.ErrValidation)
	}
	if len(c.AddressLine2) > 255 {
		return ierr.NewError("address line 2 too long").
			WithHint("Address line 2 must be less than 255 characters").
			Mark(ierr.ErrValidation)
	}
	if len(c.AddressCity) > 100 {
		return ierr.NewError("city name too long").
			WithHint("City name must be less than 100 characters").
			Mark(ierr.ErrValidation)
	}
	if len(c.AddressState) > 100 {
		return ierr.NewError("state name too long").
			WithHint("State name must be less than 100 characters").
			Mark(ierr.ErrValidation)
	}
	return nil
}
