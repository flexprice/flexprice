package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// Environment holds the schema definition for the Environment entity.
type Environment struct {
	ent.Schema
}

// Mixin of the Environment.
func (Environment) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
	}
}

func (Environment) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("name").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("type").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty(),
	}
}

// Indexes of the Environment.
func (Environment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "type").
			StorageKey("idx_environment_tenant_id_type"),
		index.Fields("tenant_id", "status").
			StorageKey("idx_environment_tenant_status"),
		index.Fields("tenant_id", "created_at").
			StorageKey("idx_environment_tenant_created_at"),
	}
}
