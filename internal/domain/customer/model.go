package customer

import (
	"strings"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/schema"
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
	Name string `db:"name" json:"name" activity:"drop"`

	// Email is the email of the customer
	Email string `db:"email" json:"email" activity:"drop"`

	// Contact is an optional contact number for the customer (e.g. phone)
	Contact *string `db:"contact" json:"contact,omitempty" activity:"drop"`

	// BillingCurrency is the fiat currency invoices are issued in; nil means charge currency is used as-is.
	BillingCurrency *string `db:"billing_currency" json:"billing_currency,omitempty"`

	ContactPersons []*ContactPerson `db:"contact_persons" json:"contact_persons,omitempty" activity:"drop"`

	// AddressLine1 is the first line of the customer's address
	AddressLine1 string `db:"address_line1" json:"address_line1" activity:"drop"`

	// AddressLine2 is the second line of the customer's address
	AddressLine2 string `db:"address_line2" json:"address_line2" activity:"drop"`

	// AddressCity is the city of the customer's address
	AddressCity string `db:"address_city" json:"address_city" activity:"drop"`

	// AddressState is the state of the customer's address
	AddressState string `db:"address_state" json:"address_state" activity:"drop"`

	// AddressPostalCode is the postal code of the customer's address
	AddressPostalCode string `db:"address_postal_code" json:"address_postal_code" activity:"drop"`

	// AddressCountry is the country of the customer's address (ISO 3166-1 alpha-2)
	AddressCountry string `db:"address_country" json:"address_country"`

	// Timezone is the customer's IANA timezone name (e.g. "Asia/Kolkata").
	// Defaults to "UTC". Inherited by subscriptions at creation time.
	Timezone string `db:"timezone" json:"timezone"`

	// TaxTreatment is the customer's tax treatment — taxable (default) or exempt.
	TaxTreatment types.TaxTreatment `db:"tax_treatment" json:"tax_treatment"`

	// Metadata
	Metadata map[string]string `db:"metadata" json:"metadata" activity:"drop"`

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
		Contact:           c.Contact,
		BillingCurrency:   c.BillingCurrency,
		ContactPersons:    contactPersonsFromEnt(c.ContactPersons),
		AddressLine1:      c.AddressLine1,
		AddressLine2:      c.AddressLine2,
		AddressCity:       c.AddressCity,
		AddressState:      c.AddressState,
		AddressPostalCode: c.AddressPostalCode,
		AddressCountry:    c.AddressCountry,
		Timezone:          c.Timezone,
		TaxTreatment:      c.TaxTreatment,
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

func contactPersonsFromEnt(persons []*schema.ContactPerson) []*ContactPerson {
	if persons == nil {
		return nil
	}
	out := make([]*ContactPerson, len(persons))
	for i, p := range persons {
		out[i] = &ContactPerson{
			ID: p.ID, IsPrimary: p.IsPrimary, Salutation: p.Salutation, FirstName: p.FirstName, LastName: p.LastName,
			Email: p.Email, Mobile: p.Mobile, Designation: p.Designation, Department: p.Department,
		}
	}
	return out
}

// ToEntContactPersons converts the customer's contact persons to their stored shape.
func (c *Customer) ToEntContactPersons() []*schema.ContactPerson {
	if c.ContactPersons == nil {
		return nil
	}
	out := make([]*schema.ContactPerson, len(c.ContactPersons))
	for i, p := range c.ContactPersons {
		out[i] = &schema.ContactPerson{
			ID: p.ID, IsPrimary: p.IsPrimary, Salutation: p.Salutation, FirstName: p.FirstName, LastName: p.LastName,
			Email: p.Email, Mobile: p.Mobile, Designation: p.Designation, Department: p.Department,
		}
	}
	return out
}

// ContactPerson is a person at the customer; the one marked primary is the main contact.
type ContactPerson struct {
	ID          string `json:"id,omitempty"`
	IsPrimary   bool   `json:"is_primary,omitempty"`
	Salutation  string `json:"salutation,omitempty" validate:"omitempty,max=25"`
	FirstName   string `json:"first_name" validate:"required,max=100"`
	LastName    string `json:"last_name,omitempty" validate:"omitempty,max=100"`
	Email       string `json:"email,omitempty" validate:"omitempty,email"`
	Mobile      string `json:"mobile,omitempty" validate:"omitempty,max=20"`
	Designation string `json:"designation,omitempty" validate:"omitempty,max=100"`
	Department  string `json:"department,omitempty" validate:"omitempty,max=100"`
}

// ValidatePersons checks the list allows at most one primary and no repeated ids,
// and gives new entries an id.
func ValidatePersons(persons []*ContactPerson) error {
	seen := make(map[string]bool, len(persons))
	people := make(map[string]bool, len(persons))
	primaries := 0
	for _, p := range persons {
		if p == nil {
			return ierr.NewError("contact person is empty").
				WithHint("Contact persons must not contain null entries").
				Mark(ierr.ErrValidation)
		}
		person := strings.ToLower(p.Email + "|" + p.FirstName + "|" + p.LastName)
		if people[person] {
			return ierr.NewError("duplicate contact person").
				WithHint("Contact persons must differ in email or name").
				Mark(ierr.ErrValidation)
		}
		people[person] = true
		if p.IsPrimary {
			primaries++
		}
		if p.ID == "" {
			p.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CONTACT_PERSON)
		}
		if seen[p.ID] {
			return ierr.NewError("duplicate contact person id").
				WithHintf("Contact person id %s appears more than once", p.ID).
				Mark(ierr.ErrValidation)
		}
		seen[p.ID] = true
	}
	if primaries > 1 {
		return ierr.NewError("more than one primary contact person").
			WithHint("Mark at most one contact person as primary").
			Mark(ierr.ErrValidation)
	}
	return nil
}
