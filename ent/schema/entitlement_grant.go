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

// EntitlementGrant is a concrete, time-boxed usage bucket instantiated from an
// Entitlement Config (an entitlement row with grant_type='TIME_BOXED'). See
// ERD FLE-959 §7.2.
//
// The lifecycle for each row is ACTIVE → (EXHAUSTED) → EXPIRED. The workflow
// only ever sees rows where valid_to > now; the status enum is a durable signal
// so the DB shows at a glance whether a grant is over-quota or just running.
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
			Immutable().
			Comment("References the entitlement row (grant_type=TIME_BOXED) this grant was instantiated from."),

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
			Immutable().
			Comment("Denormalized from the EC's resolution (plan/addon/sub override); enables cycle-window queries without joins."),

		// -------------------------------------------------------------------
		// Grant scope. Together (scope_entity_type, scope_entity_id) identify
		// what this grant meters — same pattern as alert_logs.
		//
		// Phase 1 code only writes scope_entity_type=FEATURE (populated from
		// EC.feature_id). SUBSCRIPTION and GROUP are reserved for future
		// grant-config primitives (see ERD §7.1 open extensions); shipping
		// the columns now avoids a schema migration when we get there.
		// -------------------------------------------------------------------
		field.String("scope_entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Default(string(types.EntitlementGrantScopeFeature)).
			Immutable().
			GoType(types.EntitlementGrantScopeEntityType("")).
			Comment("feature, subscription, or group. Phase 1 only writes feature."),

		field.String("scope_entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable().
			Comment("Feature/subscription/group ID, interpreted by scope_entity_type. Denormalized from the EC's target for hot lookups."),

		field.String("measure").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty().
			Immutable().
			GoType(types.EntitlementGrantMeasure("")).
			Comment("QUANTITY or AMOUNT. Interprets quota and usage on this row."),

		field.Other("quota", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(25,15)",
			}).
			Immutable().
			Comment("Per-grant quota, interpreted by measure. Copied from the resolved EC at open time; immutable for the life of the grant."),

		field.Other("usage", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(25,15)",
			}).
			Default(decimal.Zero).
			Comment("Refreshed by the alert workflow every tick from ClickHouse. Source of truth for billing overage (ERD §8.6)."),

		field.Time("valid_from").
			Immutable().
			Comment("Grant window start (inclusive)."),

		field.Time("valid_to").
			Comment("Grant window end (exclusive). Always <= sub.current_period_end (cycle-boundary cap, ERD §8.4). Updatable to support future extension via a grant-edit workflow."),

		// Distinct from BaseMixin's row-level `status` (published/archived/...).
		// This is the grant's own lifecycle: ACTIVE → EXHAUSTED → EXPIRED.
		field.String("grant_status").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Default(string(types.EntitlementGrantStatusActive)).
			GoType(types.EntitlementGrantStatus("")).
			Comment("Grant lifecycle state — active, exhausted, expired, or superseded. See types.EntitlementGrantStatus."),

		// -------------------------------------------------------------------
		// Alert bookkeeping. Source of truth for fired alerts is alert_logs;
		// last_alert_pct is a fast filter to skip GetLatestAlert calls when
		// the ratio has not moved.
		// -------------------------------------------------------------------
		field.Int("last_alert_pct").
			Optional().
			Nillable().
			Comment("Highest threshold percentage fired for this grant. Fast filter — source of truth is alert_logs."),

		field.Time("last_alert_at").
			Optional().
			Nillable(),

		field.Time("last_computed_at").
			Optional().
			Nillable().
			Comment("Wall-clock time of the last workflow tick that refreshed this row."),
	}
}

func (EntitlementGrant) Indexes() []ent.Index {
	return []ent.Index{
		// Slot-uniqueness. "Live" = ACTIVE or EXHAUSTED — both still occupy the
		// slot on this (config, customer). EXPIRED / SUPERSEDED free it so the
		// next grant can open. See ERD §7.2.
		index.Fields("entitlement_config_id", "customer_id").
			Unique().
			Annotations(entsql.IndexWhere("((grant_status)::text = ANY (ARRAY['active'::text, 'exhausted'::text]))")),

		// Hot lookup used by the alert workflow: "give me every live grant for
		// this customer". Predicate matches the same live-status set so the
		// index stays cheap.
		index.Fields("tenant_id", "environment_id", "customer_id").
			Annotations(entsql.IndexWhere("((grant_status)::text = ANY (ARRAY['active'::text, 'exhausted'::text]))")),

		// Billing-path lookup: any grant on this customer targeting this
		// (scope_entity_type, scope_entity_id) whose window overlaps a cycle,
		// regardless of status. Used by adjustMeterUsageGrants (ERD §8.6).
		// No partial predicate — expired grants must be reachable here.
		index.Fields("tenant_id", "environment_id", "customer_id", "scope_entity_type", "scope_entity_id", "valid_from", "valid_to"),
	}
}
