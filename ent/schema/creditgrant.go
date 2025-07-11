package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CreditGrant holds the schema definition for the CreditGrant entity.
type CreditGrant struct {
	ent.Schema
}

// Mixin of the CreditGrant entity.
func (CreditGrant) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the CreditGrant entity.
func (CreditGrant) Fields() []ent.Field {
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
		field.String("scope").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			GoType(types.CreditGrantScope(types.CreditGrantScopePlan)),
		field.String("plan_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),
		field.String("subscription_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),
		field.Other("credits", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Default(decimal.Zero).
			Immutable(),
		field.String("cadence").
			GoType(types.CreditGrantCadence(types.CreditGrantCadenceOneTime)).
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("period").
			GoType(types.CreditGrantPeriod("")).
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Immutable(),
		field.Int("period_count").
			Optional().
			Nillable().
			Immutable(),
		field.String("expiration_type").
			GoType(types.CreditGrantExpiryType(types.CreditGrantExpiryTypeNever)).
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default(string(types.CreditGrantExpiryTypeNever)).
			NotEmpty().
			Immutable(),
		field.Int("expiration_duration").
			Optional().
			Nillable().
			Immutable(),
		field.String("expiration_duration_unit").
			GoType(types.CreditGrantExpiryDurationUnit(types.CreditGrantExpiryDurationUnitDays)).
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Immutable(),
		field.Int("priority").
			Optional().
			Nillable().
			Immutable(),
		field.JSON("metadata", map[string]string{}).
			Optional().
			Default(map[string]string{}),
	}
}

// Edges of the CreditGrant entity.
func (CreditGrant) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("plan", Plan.Type).
			Ref("credit_grants").
			Field("plan_id").
			Unique(),
		edge.From("subscription", Subscription.Type).
			Ref("credit_grants").
			Field("subscription_id").
			Unique(),
	}
}

// Indexes of the CreditGrant entity.
func (CreditGrant) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "status"),
		index.Fields("tenant_id", "environment_id", "scope", "plan_id").
			Annotations(entsql.IndexWhere("plan_id IS NOT NULL")).
			StorageKey("idx_plan_id_not_null"),

		index.Fields("tenant_id", "environment_id", "scope", "subscription_id").
			Annotations(entsql.IndexWhere("subscription_id IS NOT NULL")).
			StorageKey("idx_subscription_id_not_null"),
	}
}
