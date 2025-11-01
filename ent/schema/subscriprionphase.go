package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

type SubscriptionPhase struct {
	ent.Schema
}

func (SubscriptionPhase) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
		baseMixin.MetadataMixin{},
	}
}

// Fields of the ScheduledTask.
func (SubscriptionPhase) Fields() []ent.Field {
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
			NotEmpty().
			Comment("Reference to the subscription"),
		field.Time("start_date").
			Immutable().
			Default(time.Now),
		field.Time("end_date").
			Optional().
			Nillable(),
	}
}

// Indexes of the SubscriptionPhase.
func (SubscriptionPhase) Indexes() []ent.Index {
	return []ent.Index{
		// Index for finding phases by tenant and environment
		index.Fields("tenant_id", "environment_id"),
	}
}
