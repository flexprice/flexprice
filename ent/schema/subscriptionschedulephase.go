package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// SubscriptionSchedulePhase holds the schema definition for the SubscriptionSchedulePhase entity.
type SubscriptionSchedulePhase struct {
	ent.Schema
}

// Mixin of the SubscriptionSchedulePhase.
func (SubscriptionSchedulePhase) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the SubscriptionSchedulePhase.
func (SubscriptionSchedulePhase) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			NotEmpty().
			Immutable(),
		field.String("schedule_id").
			NotEmpty(),
		field.Int("phase_index").
			NonNegative(),
		field.Time("start_date"),
		field.Time("end_date").
			Optional().
			Nillable(),
		field.Other("commitment_amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "decimal(20,6)",
			}).
			Optional().
			Nillable(),
		field.Other("overage_factor", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "decimal(20,6)",
			}).
			Optional().
			Nillable(),
		field.JSON("line_items", []types.SchedulePhaseLineItem{}).
			Optional(),
		field.JSON("credit_grants", []types.SchedulePhaseCreditGrant{}).
			Optional(),
		field.JSON("metadata", map[string]string{}).
			Optional(),
	}
}

// Edges of the SubscriptionSchedulePhase.
func (SubscriptionSchedulePhase) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("schedule", SubscriptionSchedule.Type).
			Ref("phases").
			Unique().
			Required().
			Field("schedule_id"),
	}
}

// Indexes of the SubscriptionSchedulePhase.
func (SubscriptionSchedulePhase) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "schedule_id", "phase_index").
			Unique(),
		index.Fields("tenant_id", "environment_id", "start_date"),
	}
}
