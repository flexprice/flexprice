package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

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
		// Billing information as JSON field
		field.JSON("billing_info", map[string]interface{}{}).
			Optional().
			Default(map[string]interface{}{}).
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
