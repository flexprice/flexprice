package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// EntitlementGrant is a time-boxed usage bucket instantiated from an entitlement
// carrying a grant config. Lifecycle: active → exhausted → expired.
type EntitlementGrant struct {
	ent.Schema
}

func (EntitlementGrant) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (EntitlementGrant) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		field.String("entitlement_config_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		field.String("customer_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		field.String("subscription_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		field.String("scope_entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Default(string(types.EntitlementGrantScopeFeature)).
			Immutable().
			GoType(types.EntitlementGrantScopeEntityType("")),

		field.String("scope_entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		field.String("measure").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty().
			Immutable().
			GoType(types.EntitlementGrantMeasure("")),

		field.Other("quota", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(25,15)",
			}).
			Immutable(),

		field.Other("usage", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(25,15)",
			}).
			Default(decimal.Zero),

		field.Time("valid_from").
			Immutable(),

		field.Time("valid_to"),

		// Distinct from BaseMixin's row-level `status`. Grant lifecycle: active → exhausted → expired.
		field.String("grant_status").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Default(string(types.EntitlementGrantStatusActive)).
			GoType(types.EntitlementGrantStatus("")),

		field.Time("last_computed_at").
			Optional().
			Nillable(),
	}
}

func (EntitlementGrant) Indexes() []ent.Index {
	return []ent.Index{
		// One live grant per (tenant, env, config, customer). Expired/superseded rows free the slot.
		index.Fields("tenant_id", "environment_id", "entitlement_config_id", "customer_id").
			Unique().
			Annotations(entsql.IndexWhere("((grant_status)::text = ANY (ARRAY['active'::text, 'exhausted'::text]))")),

		// Alert-path hot lookup: live grants by customer.
		index.Fields("tenant_id", "environment_id", "customer_id").
			Annotations(entsql.IndexWhere("((grant_status)::text = ANY (ARRAY['active'::text, 'exhausted'::text]))")),

		// Billing-path lookup: grants overlapping a cycle window, any status.
		index.Fields("tenant_id", "environment_id", "customer_id", "scope_entity_type", "scope_entity_id", "valid_from", "valid_to"),
	}
}
