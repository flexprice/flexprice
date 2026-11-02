package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// BillingTerm holds the schema definition for a payment term on a commitment.
type BillingTerm struct {
	ent.Schema
}

func (BillingTerm) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (BillingTerm) Fields() []ent.Field {
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

		field.String("kind").
			SchemaType(varchar("20")).
			NotEmpty().
			Immutable().
			GoType(types.BillingTermKind("")),

		field.String("value_type").
			SchemaType(varchar("20")).
			NotEmpty().
			Immutable().
			GoType(types.BillingTermValueType("")),

		field.Other("amount", decimal.Decimal{}).
			SchemaType(map[string]string{"postgres": "numeric(20,8)"}).
			Optional().
			Nillable().
			Immutable(),

		field.Other("percentage", decimal.Decimal{}).
			SchemaType(map[string]string{"postgres": "numeric(10,4)"}).
			Optional().
			Nillable().
			Immutable(),

		field.String("bill_trigger").
			SchemaType(varchar("20")).
			Optional().
			Nillable().
			Immutable().
			GoType(types.InstalmentBillTrigger("")),

		field.Time("bill_on").
			Optional().
			Nillable().
			Immutable(),

		field.Time("covers_from").
			Optional().
			Nillable().
			Immutable(),

		field.Time("covers_to").
			Optional().
			Nillable().
			Immutable(),

		field.Time("starts_on").
			Optional().
			Nillable().
			Immutable(),

		field.Time("ends_on").
			Optional().
			Nillable().
			Immutable(),

		field.Int("period_count").
			Optional().
			Nillable().
			Immutable(),

		field.String("invoice_cadence").
			SchemaType(varchar("20")).
			Optional().
			Nillable().
			Immutable().
			GoType(types.InvoiceCadence("")),

		field.String("split").
			SchemaType(varchar("20")).
			Optional().
			Nillable().
			Immutable().
			GoType(types.BillingTermSplit("")),

		field.String("description").
			SchemaType(varchar("255")).
			Optional().
			Nillable().
			Immutable(),

		field.Other("metadata", types.Metadata{}).
			SchemaType(map[string]string{"postgres": "jsonb"}).
			Optional().
			Immutable().
			Default(types.Metadata{}),
	}
}

func (BillingTerm) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "owner_type", "owner_id", "status"),
	}
}
