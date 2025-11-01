package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// Entitlement holds the schema definition for the Entitlement entity.
type Entitlement struct {
	ent.Schema
}

// Mixin of the Entitlement.
func (Entitlement) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the Entitlement.
func (Entitlement) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Unique().
			Immutable(),
		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default(string(types.ENTITLEMENT_ENTITY_TYPE_PLAN)).
			Optional(),

		field.String("entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional(),
		field.String("feature_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("feature_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.Bool("is_enabled").
			Default(false),
		field.Int64("usage_limit").
			Optional().
			Nillable(),
		field.String("usage_reset_period").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Optional(),
		field.Bool("is_soft_limit").
			Default(false),
		field.String("static_value").
			Optional(),
		field.Int("display_order").
			Default(0),
		field.String("parent_entitlement_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Comment("References the parent entitlement (for subscription-scoped entitlements)"),
	}
}

// Edges of the Entitlement.
func (Entitlement) Edges() []ent.Edge {
	return []ent.Edge{}
}

// Indexes of the Entitlement.
func (Entitlement) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id", "feature_id").
			Unique().
			Annotations(entsql.IndexWhere("status = 'published'")),

		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id"),
		index.Fields("tenant_id", "environment_id", "feature_id"),
		index.Fields("parent_entitlement_id"),
	}
}
