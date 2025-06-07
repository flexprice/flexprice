package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CreditGrantApplication holds the schema definition for the CreditGrantApplication entity.
type CreditGrantApplication struct {
	ent.Schema
}

// Mixin of the CreditGrantApplication.
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
			Immutable(),
		field.String("subscription_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable(),
		field.Time("scheduled_for").
			SchemaType(map[string]string{
				"postgres": "timestamp",
			}),

		field.Time("applied_at").
			SchemaType(map[string]string{
				"postgres": "timestamp",
			}),
		field.Time("billing_period_start").
			SchemaType(map[string]string{
				"postgres": "timestamp",
			}),
		field.Time("billing_period_end").
			SchemaType(map[string]string{
				"postgres": "timestamp",
			}),
		field.Other("amount_applied", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}),
		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(3)",
			}).
			Immutable(),

		field.String("application_reason").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}),

		field.String("subscription_status_at_application").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}),

		field.Bool("is_prorated"),

		field.Other("proration_factor", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}),

		field.Other("full_period_amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}),

		field.Int("retry_count"),

		field.String("failure_reason").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}),

		field.Time("next_retry_at").
			SchemaType(map[string]string{
				"postgres": "timestamp",
			}),

		field.Other("metadata", types.Metadata{}).
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}
