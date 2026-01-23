package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Subscription holds the schema definition for the Subscription entity.
type Subscription struct {
	ent.Schema
}

// Mixin of the Subscription.
func (Subscription) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
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
			Default(string(types.SubscriptionStatusActive)).
			GoType(types.SubscriptionStatus("")),
		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			NotEmpty().
			Immutable(),
		field.Time("billing_anchor").
			Default(time.Now),
		field.Time("start_date").
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
		field.String("billing_cadence").
			NotEmpty().
			Immutable().
			GoType(types.BillingCadence("")),
		field.String("billing_period").
			NotEmpty().
			Immutable().
			GoType(types.BillingPeriod("")),
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
		field.String("pause_status").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default(string(types.PauseStatusNone)).
			GoType(types.PauseStatus("")),
		field.String("active_pause_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),
		field.String("billing_cycle").
			NotEmpty().
			Immutable().
			Default(string(types.BillingCycleAnniversary)).
			GoType(types.BillingCycle("")),
		field.Other("commitment_amount", decimal.Decimal{}).
			Optional().
			Nillable().
			SchemaType(map[string]string{
				"postgres": "decimal(20,6)",
			}),
		field.Other("overage_factor", decimal.Decimal{}).
			Optional().
			Nillable().
			Default(decimal.NewFromInt(1)).
			SchemaType(map[string]string{
				"postgres": "decimal(10,6)",
			}),
		// Payment behavior and collection method fields
		field.String("payment_behavior").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default(string(types.PaymentBehaviorDefaultActive)).
			GoType(types.PaymentBehavior("")).
			Comment("Determines how subscription payments are handled"),
		field.String("collection_method").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default(string(types.CollectionMethodChargeAutomatically)).
			GoType(types.CollectionMethod("")).
			Comment("Determines how invoices are collected"),
		field.String("gateway_payment_method_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional().
			Comment("Gateway payment method ID for this subscription"),
		field.String("customer_timezone").
			Default("UTC"),
		field.String("proration_behavior").
			NotEmpty().
			Immutable().
			Default(string(types.ProrationBehaviorNone)).
			GoType(types.ProrationBehavior("")),
		field.Bool("enable_true_up").
			Default(false).
			Comment("Enable Commitment True Up Fee"),
		field.String("invoicing_customer_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Comment("Customer ID to use for invoicing (can differ from the subscription customer)"),
	}
}

// Edges of the Subscription.
func (Subscription) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("line_items", SubscriptionLineItem.Type),
		edge.To("pauses", SubscriptionPause.Type),
		edge.To("phases", SubscriptionPhase.Type),
		edge.To("schedules", SubscriptionSchedule.Type).
			Comment("Subscription can have multiple schedules for plan changes, addons, etc."),
		edge.To("credit_grants", CreditGrant.Type),
		edge.To("coupon_associations", CouponAssociation.Type).
			Comment("Subscription can have multiple coupon associations"),
		edge.To("coupon_applications", CouponApplication.Type).
			Comment("Subscription can have multiple coupon applications"),
		edge.To("invoicing_customer", Customer.Type).
			Unique().
			Field("invoicing_customer_id").
			Comment("Customer to use for invoicing (can differ from the subscription customer)"),
	}
}

// Indexes of the Subscription.
func (Subscription) Indexes() []ent.Index {
	return []ent.Index{
		// Partial indexes for published status (most common query pattern)
		index.Fields("tenant_id", "environment_id", "customer_id").
			Annotations(entsql.IndexWhere("status = 'published'")),
		// General plan_id index for queries that don't filter by subscription_status
		index.Fields("tenant_id", "environment_id", "plan_id").
			Annotations(entsql.IndexWhere("status = 'published'")),
		index.Fields("tenant_id", "environment_id", "subscription_status").
			Annotations(entsql.IndexWhere("status = 'published'")),
		// For billing period updates (merged with subscription_status)
		index.Fields("tenant_id", "environment_id", "current_period_end", "subscription_status").
			Annotations(entsql.IndexWhere("status = 'published'")),
		// Performance index for plan lookups with billing dimensions (covers active/trialing queries)
		// This index can also serve queries that only need plan_id + subscription_status
		index.Fields("tenant_id", "environment_id", "plan_id", "currency", "billing_period", "billing_period_count").
			Annotations(entsql.IndexWhere("status = 'published' AND subscription_status IN ('active', 'trialing')")),
	}
}
