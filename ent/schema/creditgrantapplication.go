package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type CreditGrantApplication struct {
	ent.Schema
}

func (CreditGrantApplication) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the CreditGrantApplication.
func (CreditGrantApplication) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		field.String("credit_grant_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		field.String("subscription_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		// Timing
		field.Time("scheduled_for"),

		field.Time("applied_at").
			Optional().
			Nillable(),

		// Billing period context
		field.Time("period_start"),

		field.Time("period_end"),

		// Application details
		field.String("application_status").
			GoType(types.ApplicationStatus("")).
			Default(string(types.ApplicationStatusPending)),

		field.Other("credits_applied", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Default(decimal.Zero),

		// Context
		field.String("application_reason").
			GoType(types.CreditGrantApplicationReason("")).
			SchemaType(map[string]string{
				"postgres": "text",
			}).
			NotEmpty().
			Immutable(),

		field.String("subscription_status_at_application").
			GoType(types.SubscriptionStatus("")).
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),

		// Retry handling
		field.Int("retry_count").
			Default(0),

		field.String("failure_reason").
			SchemaType(map[string]string{
				"postgres": "text",
			}).
			Optional().
			Nillable(),

		// Metadata
		field.Other("metadata", types.Metadata{}).
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}).
			Optional(),

		field.String("idempotency_key").
			SchemaType(map[string]string{
				"postgres": "varchar(100)",
			}).
			Unique().
			Immutable(),
	}
}

// Edges of the CreditGrantApplication.
func (CreditGrantApplication) Edges() []ent.Edge {
	return nil // define if you want relationships with CreditGrant, Subscription, etc.
}
