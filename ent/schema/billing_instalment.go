package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// BillingInstalment holds the schema definition for one payable instalment of a commitment.
type BillingInstalment struct {
	ent.Schema
}

func (BillingInstalment) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (BillingInstalment) Fields() []ent.Field {
	varchar := func(n string) map[string]string { return map[string]string{"postgres": "varchar(" + n + ")"} }

	return []ent.Field{
		field.String("id").
			SchemaType(varchar("50")).
			Unique().
			Immutable(),

		field.String("owner_type").
			SchemaType(varchar("30")).
			NotEmpty().
			Immutable().
			GoType(types.BillingTermOwnerType("")),

		field.String("owner_id").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable(),

		field.String("billing_term_id").
			SchemaType(varchar("50")).
			Optional().
			Nillable().
			Immutable(),

		field.String("customer_id").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable(),

		field.String("subscription_id").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable(),

		field.String("currency").
			SchemaType(varchar("10")).
			NotEmpty().
			Immutable(),

		field.String("kind").
			SchemaType(varchar("20")).
			NotEmpty().
			Immutable().
			GoType(types.InstalmentKind("")),

		field.String("bill_trigger").
			SchemaType(varchar("20")).
			NotEmpty().
			Immutable().
			GoType(types.InstalmentBillTrigger("")),

		field.Time("bill_on"),

		field.Time("triggered_at").
			Optional().
			Nillable(),

		field.Other("amount", decimal.Decimal{}).
			SchemaType(map[string]string{"postgres": "numeric(20,8)"}),

		field.String("description").
			SchemaType(varchar("255")).
			NotEmpty().
			Immutable(),

		field.Time("service_period_start").
			Immutable(),

		field.Time("service_period_end").
			Immutable(),

		field.String("delivery").
			SchemaType(varchar("30")).
			NotEmpty().
			Immutable().
			GoType(types.InstalmentDelivery("")),

		field.String("instalment_status").
			SchemaType(varchar("20")).
			NotEmpty().
			GoType(types.InstalmentStatus("")),

		field.String("invoice_id").
			SchemaType(varchar("50")).
			Optional().
			Nillable(),

		field.Other("metadata", types.Metadata{}).
			SchemaType(map[string]string{"postgres": "jsonb"}).
			Optional().
			Immutable().
			Default(types.Metadata{}),
	}
}

func (BillingInstalment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "owner_type", "owner_id"),
		index.Fields("tenant_id", "environment_id", "subscription_id", "instalment_status", "bill_on"),
		index.Fields("tenant_id", "environment_id", "instalment_status", "delivery", "bill_on"),
		index.Fields("tenant_id", "environment_id", "invoice_id"),
	}
}
