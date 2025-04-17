package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
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
		field.String("plan_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
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
	}
}

// Edges of the Entitlement.
func (Entitlement) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("plan", Plan.Type).
			Ref("entitlements").
			Field("plan_id").
			Unique().
			Required(),
	}
}

// Indexes of the Entitlement.
func (Entitlement) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "plan_id", "feature_id").
			Unique().
			Annotations(entsql.IndexWhere("status = 'published'")),

		index.Fields("tenant_id", "environment_id", "plan_id"),
		index.Fields("tenant_id", "environment_id", "feature_id"),
	}
}
