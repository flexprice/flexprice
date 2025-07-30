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
		field.Enum("provider_type").
			Values("flexprice", "stripe", "razorpay").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}),
		field.JSON("metadata", map[string]interface{}{}).
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
		index.Fields("tenant_id", "environment_id"),
		index.Fields("provider_type"),
	}
}
