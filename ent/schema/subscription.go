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

// Subscription holds the schema definition for the Subscription entity.
type Subscription struct {
	ent.Schema
}

// Mixin of the Subscription.
func (Subscription) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
	}
}

// Fields of the Subscription.
func (Subscription) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("lookup_key").
			Optional(),
		field.String("customer_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("plan_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("subscription_status").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default(string(types.SubscriptionStatusActive)),
		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			NotEmpty().
			Immutable(),
		field.Time("billing_anchor").
			Immutable().
			Default(time.Now),
		field.Time("start_date").
			Immutable().
			Default(time.Now),
		field.Time("end_date").
			Optional().
			Nillable(),
		field.Time("current_period_start").
			Default(time.Now),
		field.Time("current_period_end").
			Default(time.Now),
		field.Time("cancelled_at").
			Optional().
			Nillable(),
		field.Time("cancel_at").
			Optional().
			Nillable(),
		field.Bool("cancel_at_period_end").
			Default(false),
		field.Time("trial_start").
			Optional().
			Nillable(),
		field.Time("trial_end").
			Optional().
			Nillable(),
		field.String("invoice_cadence").
			NotEmpty().
			Immutable(),
		field.String("billing_cadence").
			NotEmpty().
			Immutable(),
		field.String("billing_period").
			NotEmpty().
			Immutable(),
		field.Int("billing_period_count").
			Default(1).
			Immutable(),
		field.Int("version").
			Default(1),
		field.JSON("metadata", map[string]string{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Edges of the Subscription.
func (Subscription) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("line_items", SubscriptionLineItem.Type),
	}
}

// Indexes of the Subscription.
func (Subscription) Indexes() []ent.Index {
	return []ent.Index{
		// Common query patterns from repository layer
		index.Fields("tenant_id", "customer_id", "status"),
		index.Fields("tenant_id", "plan_id", "status"),
		index.Fields("tenant_id", "subscription_status", "status"),
		// For billing period updates
		index.Fields("tenant_id", "current_period_end", "subscription_status", "status"),
	}
}
