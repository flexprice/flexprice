package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TenantBillingDetails structure for the billing details
type TenantBillingDetails struct {
	Email     string        `json:"email,omitempty"`
	HelpEmail string        `json:"help_email,omitempty"`
	Phone     string        `json:"phone,omitempty"`
	Address   TenantAddress `json:"address,omitempty"`
}

// TenantAddress represents a physical address in the tenant billing details
type TenantAddress struct {
	Line1      string `json:"address_line1,omitempty"`
	Line2      string `json:"address_line2,omitempty"`
	City       string `json:"address_city,omitempty"`
	State      string `json:"address_state,omitempty"`
	PostalCode string `json:"address_postal_code,omitempty"`
	Country    string `json:"address_country,omitempty"`
}

// Tenant holds the schema definition for the Tenant entity.
type Tenant struct {
	ent.Schema
}

func (Tenant) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("name").
			SchemaType(map[string]string{
				"postgres": "varchar(100)",
			}).
			NotEmpty(),
		field.String("status").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Default("published"),
		field.Time("created_at").
			Immutable().
			Default(time.Now),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
		field.JSON("billing_details", TenantBillingDetails{}).
			Optional().
			Default(TenantBillingDetails{}).
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
		field.JSON("metadata", map[string]string{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Indexes of the Tenant.
func (Tenant) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("created_at").
			StorageKey("idx_tenant_created_at"),
	}
}
