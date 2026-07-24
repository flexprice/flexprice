package schema

import (
	"entgo.io/ent"
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

// Two indexes serve every production read; snapshot writes (usage,
// grant_status, last_computed_at) touch no indexed column, so updates stay
// HOT and the indexes don't churn as the table grows.
func (EntitlementGrant) Indexes() []ent.Index {
	return []ent.Index{
		// Serves the INSERT race (valid_from is deterministic under
		// usage-anchored windows, so two workers opening the same slot collide
		// here) and FindLastBySlot (5-col equality prefix + ORDER BY valid_from
		// DESC LIMIT 1 — no sort, regardless of slot history size).
		index.Fields("tenant_id", "environment_id", "entitlement_config_id", "customer_id", "subscription_id", "valid_from").
			Unique(),

		// Serves the per-tick reads and the billing overlap query: equality on
		// (tenant, env, customer) + range on valid_to bounds every scan to the
		// current cycle. Trailing config/sub let the slot-frontier GROUP BY and
		// the billing subscription filter resolve in-index (both immutable).
		index.Fields("tenant_id", "environment_id", "customer_id", "valid_to", "entitlement_config_id", "subscription_id"),
	}
}
