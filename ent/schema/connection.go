package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// Connection holds the schema definition for the Connection entity.
type Connection struct {
	ent.Schema
}

// Mixin of the Connection.
func (Connection) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the Connection.
func (Connection) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("name").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty(),
		field.String("provider_type").
			NotEmpty().
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}),
		field.JSON("encrypted_secret_data", map[string]interface{}{}).
			Optional(),
	}
}

// Edges of the Connection.
func (Connection) Edges() []ent.Edge {
	return nil
}

// Indexes of the Connection.
func (Connection) Indexes() []ent.Index {
	return []ent.Index{
		// Composite index with optimal column order for PostgreSQL
		// tenant_id, environment_id, provider_type
		// This order allows efficient lookups by tenant, environment, and provider
		// PostgreSQL can use leftmost prefixes: tenant_id, tenant_id+environment_id, etc.
		index.Fields("tenant_id", "environment_id", "provider_type"),
	}
}
