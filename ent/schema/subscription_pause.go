package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// SubscriptionPause holds the schema definition for the SubscriptionPause entity.
type SubscriptionPause struct {
	ent.Schema
}

// Mixin of the SubscriptionPause.
func (SubscriptionPause) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the SubscriptionPause.
func (SubscriptionPause) Fields() []ent.Field {
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
		field.String("pause_status").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("pause_mode").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default("scheduled").
			NotEmpty(),
		field.String("resume_mode").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional(),
		// Decides whether this pause's days are taken off the fixed charges.
		field.String("proration_behavior").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default(string(types.ProrationBehaviorNone)).
			NotEmpty(),
		field.Time("pause_start").
			Default(time.Now),
		field.Time("pause_end").
			Optional().
			Nillable(),
		field.Time("resumed_at").
			Optional().
			Nillable(),
		field.Time("original_period_start").
			Default(time.Now),
		field.Time("original_period_end").
			Default(time.Now),
		field.String("reason").
			SchemaType(map[string]string{
				"postgres": "text",
			}).
			Optional(),
		field.JSON("metadata", map[string]string{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Edges of the SubscriptionPause.
func (SubscriptionPause) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("subscription", Subscription.Type).
			Ref("pauses").
			Unique().
			Field("subscription_id").
			Required(),
	}
}

// Indexes of the SubscriptionPause.
func (SubscriptionPause) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "subscription_id", "status"),
		index.Fields("tenant_id", "environment_id", "pause_start", "status"),
		index.Fields("tenant_id", "environment_id", "pause_end", "status"),
	}
}
