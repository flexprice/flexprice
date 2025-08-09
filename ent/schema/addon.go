package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// Addon holds the schema definition for the Addon entity.
type Addon struct {
	ent.Schema
}

const (
	AddonTenantIDEnvironmentIDLookupKeyConstraint = "addon_tenant_id_environment_id_lookup_key"
)

// Mixin of the Addon.
func (Addon) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the Addon.
func (Addon) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		field.String("lookup_key").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Immutable().
			NotEmpty(),

		field.String("name").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty(),

		field.Text("description").
			Optional(),

		field.String("type").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty().
			Comment("single_instance or multi_instance"),

		field.JSON("metadata", map[string]interface{}{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Edges of the Addon.
func (Addon) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("prices", Price.Type),
		edge.To("entitlements", Entitlement.Type),
	}
}

// Indexes of the Addon.
func (Addon) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "lookup_key").
			Unique().
			StorageKey(AddonTenantIDEnvironmentIDLookupKeyConstraint).
			Annotations(entsql.IndexWhere("status = 'published'" + " AND lookup_key IS NOT NULL AND lookup_key != ''")),
	}
}
