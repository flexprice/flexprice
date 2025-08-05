package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// SubscriptionAddon holds the schema definition for the SubscriptionAddon entity.
type SubscriptionAddon struct {
	ent.Schema
}

// Mixin of the SubscriptionAddon.
func (SubscriptionAddon) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the SubscriptionAddon.
func (SubscriptionAddon) Fields() []ent.Field {
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
			Immutable(),

		field.String("addon_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		field.Time("start_date").
			Optional().
			Nillable().
			Default(time.Now),

		field.Time("end_date").
			Optional().
			Nillable(),

		// Lifecycle management
		field.String("addon_status").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty().
			Default(string(types.AddonStatusActive)),

		field.String("cancellation_reason").
			Optional().
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}),

		field.Time("cancelled_at").
			Optional().
			Nillable(),

		field.JSON("metadata", map[string]interface{}{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Edges of the SubscriptionAddon.
func (SubscriptionAddon) Edges() []ent.Edge {
	return []ent.Edge{
		// edge.From("subscription", Subscription.Type).
		// 	Ref("subscription_addons").
		// 	Field("subscription_id").
		// 	Immutable().
		// 	Unique().
		// 	Required(),

		// edge.From("addon", Addon.Type).
		// 	Ref("subscription_addons").
		// 	Field("addon_id").
		// 	Immutable().
		// 	Unique().
		// 	Required(),
	}
}

// Indexes of the SubscriptionAddon.
func (SubscriptionAddon) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "subscription_id", "addon_id"),
	}
}
