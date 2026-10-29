package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// BillingTerm holds an immutable owner collection rule.
type BillingTerm struct {
	ent.Schema
}

// Mixin provides scoped source visibility and audit fields.
func (BillingTerm) Mixin() []ent.Mixin {
	return []ent.Mixin{baseMixin.BaseMixin{}, baseMixin.EnvironmentMixin{}}
}

// Fields defines the persisted collection rule.
func (BillingTerm) Fields() []ent.Field {
	varchar := func(n string) map[string]string { return map[string]string{"postgres": "varchar(" + n + ")"} }
	return []ent.Field{
		field.String("id").SchemaType(varchar("50")).Unique().Immutable(),
		field.String("owner_type").SchemaType(varchar("50")).NotEmpty().Immutable().GoType(types.BillingTermOwnerType("")),
		field.String("owner_id").SchemaType(varchar("50")).NotEmpty().Immutable(),
		field.String("kind").SchemaType(varchar("20")).NotEmpty().Immutable().GoType(types.BillingTermKind("")),
		field.String("value_type").SchemaType(varchar("20")).NotEmpty().Immutable().GoType(types.BillingTermValueType("")),
		field.Other("value", decimal.Decimal{}).SchemaType(map[string]string{"postgres": "numeric(20,8)"}).Optional().Nillable().Immutable(),
		field.Time("bill_on").Optional().Nillable().Immutable(),
		field.Bool("on_trigger").Default(false).Immutable(),
		field.Time("start_date").Optional().Nillable().Immutable(),
		field.Time("end_date").Optional().Nillable().Immutable(),
		field.String("interval").SchemaType(varchar("20")).Optional().Nillable().Immutable().GoType(types.BillingPeriod("")),
		field.Int("interval_count").Optional().Nillable().Positive().Immutable(),
		field.String("bill_at").SchemaType(varchar("20")).Optional().Nillable().Immutable().GoType(types.BillingTermTiming("")),
		field.String("split").SchemaType(varchar("20")).Optional().Nillable().Immutable().GoType(types.BillingTermSplit("")),
		field.String("description").SchemaType(varchar("255")).Optional().Immutable(),
		field.Other("metadata", types.Metadata{}).SchemaType(map[string]string{"postgres": "jsonb"}).Optional().Default(types.Metadata{}).Immutable(),
	}
}

// Edges preserves source references without deletion cascades.
func (BillingTerm) Edges() []ent.Edge {
	return []ent.Edge{edge.To("schedule_items", BillingScheduleItem.Type).Annotations(entsql.OnDelete(entsql.Restrict))}
}

// Indexes bounds current owner and historical source lookups.
func (BillingTerm) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "owner_type", "owner_id", "status"),
		index.Fields("tenant_id", "environment_id", "owner_type", "owner_id", "created_at", "id"),
	}
}

// Annotations adds constraints shared by all persistence paths.
func (BillingTerm) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Checks: map[string]string{
		"billing_terms_kind":       "kind IN ('once', 'recurring')",
		"billing_terms_value_type": "value_type IN ('fixed', 'remainder')",
		"billing_terms_value":      "(value_type = 'fixed' AND value IS NOT NULL AND value > 0) OR (value_type = 'remainder' AND value IS NULL)",
		"billing_terms_shape":      `(kind = 'once' AND ((on_trigger AND bill_on IS NULL) OR (NOT on_trigger AND bill_on IS NOT NULL)) AND start_date IS NULL AND end_date IS NULL AND "interval" IS NULL AND interval_count IS NULL AND bill_at IS NULL AND split IS NULL) OR (kind = 'recurring' AND NOT on_trigger AND bill_on IS NULL AND start_date IS NOT NULL AND end_date IS NOT NULL AND start_date < end_date AND "interval" IS NOT NULL AND "interval" IN ('DAILY', 'WEEKLY', 'MONTHLY', 'QUARTERLY', 'HALF_YEARLY', 'ANNUAL') AND interval_count IS NOT NULL AND interval_count > 0 AND bill_at IS NOT NULL AND bill_at IN ('advance', 'arrear') AND ((value_type = 'fixed' AND split IS NULL) OR (value_type = 'remainder' AND split IS NOT NULL AND split IN ('even', 'proportional'))))`,
	}}}
}
