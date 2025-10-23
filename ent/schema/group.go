package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// Group holds the schema definition for the Group entity.
type Group struct {
	ent.Schema
}

// Mixin of the Group.
func (Group) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the Group.
func (Group) Fields() []ent.Field {
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
		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default("price").
			Immutable(),
		field.String("lookup_key").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional().
			Comment("Idempotency key for group creation"),
	}
}

// Edges of the Group.
func (Group) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("prices", Price.Type).
			Ref("group"),
	}
}

// Indexes of the Group.
func (Group) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "name").
			Unique().
			Annotations(entsql.IndexWhere("status = 'published'")),
		index.Fields("tenant_id", "environment_id", "entity_type"),
		index.Fields("tenant_id", "environment_id", "lookup_key").
			Unique().
			Annotations(entsql.IndexWhere("lookup_key IS NOT NULL AND status = 'published'")),
	}
}
