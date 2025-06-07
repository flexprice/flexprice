package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// SubscriptionSchedule holds the schema definition for the SubscriptionSchedule entity.
type SubscriptionSchedule struct {
	ent.Schema
}

// Mixin of the SubscriptionSchedule.
func (SubscriptionSchedule) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the SubscriptionSchedule.
func (SubscriptionSchedule) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			NotEmpty().
			Immutable(),
		field.String("subscription_id").
			NotEmpty(),
		field.String("schedule_status").
			GoType(types.SubscriptionScheduleStatus(types.ScheduleStatusActive)).
			Default(string(types.ScheduleStatusActive)),
		field.Int("current_phase_index").
			Default(0),
		field.String("end_behavior").
			GoType(types.ScheduleEndBehavior(types.EndBehaviorRelease)).
			Default(string(types.EndBehaviorRelease)),
		field.Time("start_date"),
		field.JSON("metadata", map[string]string{}).
			Optional(),
	}
}

// Edges of the SubscriptionSchedule.
func (SubscriptionSchedule) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("phases", SubscriptionSchedulePhase.Type),
		edge.From("subscription", Subscription.Type).
			Ref("schedule").
			Unique().
			Required().
			Field("subscription_id"),
	}
}

// Indexes of the SubscriptionSchedule.
func (SubscriptionSchedule) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "subscription_id"),
		index.Fields("tenant_id", "environment_id", "subscription_id", "schedule_status"),
	}
}
