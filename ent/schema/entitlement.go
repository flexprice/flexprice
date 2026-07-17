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
			Optional().
			GoType(types.EntitlementEntityType("")),

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
			NotEmpty().
			GoType(types.FeatureType("")),
		field.Bool("is_enabled").
			Default(false),
		field.Int64("usage_limit").
			Optional().
			Nillable(),
		field.String("usage_reset_period").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Optional().
			GoType(types.EntitlementUsageResetPeriod("")),
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
		field.Time("start_date").
			Optional().
			Nillable().
			Comment("Start date for time-bound entitlements (subscription-scoped only)"),
		field.Time("end_date").
			Optional().
			Nillable().
			Comment("End date for time-bound entitlements (subscription-scoped only)"),
		field.JSON("config_value", map[string]interface{}{}).
			Optional().
			SchemaType(map[string]string{"postgres": "jsonb"}),

		// -------------------------------------------------------------------
		// Grant fields — layered on top of the legacy entitlement row so
		// existing entitlements keep working (grant_type defaults to NONE and
		// nothing changes). See ERD FLE-959 §7.1.
		//
		// A row with grant_type='TIME_BOXED' becomes an "entitlement config"
		// that the alert workflow instantiates into entitlement_grants over
		// time (5 h, 1 w, etc. windows), independent of the sub billing cycle.
		// -------------------------------------------------------------------

		field.String("grant_type").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Default(string(types.EntitlementGrantTypeNone)).
			GoType(types.EntitlementGrantType("")).
			Comment("none = legacy behavior, no grants. time_boxed = auto-rotate grants of grant_duration each."),

		field.String("grant_measure").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Optional().
			GoType(types.EntitlementGrantMeasure("")).
			Comment("QUANTITY or AMOUNT. Interprets grant_quota and EG.usage."),

		// Duration as (value, unit) rather than raw nanoseconds — humans read
		// this table too. Only meaningful when grant_type=TIME_BOXED.
		field.Int("grant_duration_value").
			Optional().
			Nillable().
			Comment("Length of each time-boxed grant, expressed with grant_duration_unit. Minimum equivalent of 1 hour."),

		field.String("grant_duration_unit").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			Optional().
			GoType(types.EntitlementGrantDurationUnit("")).
			Comment("HOUR, DAY, or WEEK."),

		field.Other("grant_quota", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(25,15)",
			}).
			Optional().
			Nillable().
			Comment("Per-grant quota, interpreted by grant_measure."),

		field.Bool("parallel").
			Default(false).
			Comment("If true, multiple ECs on the same feature produce independent grants (parallel counting). If false, additive as today."),
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
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text)")),

		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id"),
		index.Fields("tenant_id", "environment_id", "feature_id"),
		index.Fields("tenant_id", "environment_id", "parent_entitlement_id"),

		// Index for time-based queries on subscription-scoped entitlements
		index.Fields("entity_id", "entity_type", "feature_id", "start_date", "end_date").
			Annotations(entsql.IndexWhere("(((entity_type)::text = 'SUBSCRIPTION'::text) AND ((status)::text = 'published'::text))")),
	}
}
