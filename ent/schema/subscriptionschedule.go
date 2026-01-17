package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
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
		baseMixin.MetadataMixin{},
	}
}

// Fields of the SubscriptionSchedule.
func (SubscriptionSchedule) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("subscription_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("schedule_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			GoType(types.SubscriptionScheduleChangeType("")).
			Comment("Type of schedule: plan_change, addon_change, etc."),
		field.Time("scheduled_at").
			Comment("When the schedule should execute"),
		field.JSON("configuration", map[string]interface{}{}).
			Comment("Type-specific configuration stored as JSONB"),
		field.Time("executed_at").
			Optional().
			Nillable().
			Comment("When the schedule was executed"),
		field.Time("cancelled_at").
			Optional().
			Nillable().
			Comment("When the schedule was cancelled"),
		field.JSON("execution_result", map[string]interface{}{}).
			Optional().
			Comment("Type-specific execution result stored as JSONB"),
		field.Text("error_message").
			Optional().
			Nillable().
			Comment("Error message if execution failed"),
	}
}

// Edges of the SubscriptionSchedule.
func (SubscriptionSchedule) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("subscription", Subscription.Type).
			Ref("schedules").
			Unique().
			Field("subscription_id").
			Required(),
	}
}

// Indexes of the SubscriptionSchedule.
func (SubscriptionSchedule) Indexes() []ent.Index {
	return []ent.Index{
		// Index for finding schedules by subscription
		index.Fields("subscription_id"),

		// Index for finding pending schedules by type
		index.Fields("status", "schedule_type").
			Annotations(
				entsql.IndexAnnotation{
					Where: "status = 'pending'",
				},
			),

		// Index for finding schedules to execute
		index.Fields("scheduled_at", "status").
			Annotations(
				entsql.IndexAnnotation{
					Where: "status = 'pending'",
				},
			),

		// Index for tenant and environment filtering
		index.Fields("tenant_id", "environment_id"),

		// Unique constraint: one pending schedule per type per subscription
		index.Fields("subscription_id", "schedule_type").
			Unique().
			Annotations(
				entsql.IndexAnnotation{
					Where: "status = 'pending'",
				},
			),
	}
}
