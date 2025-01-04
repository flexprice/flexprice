package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/shopspring/decimal"
)

// InvoiceLineItem holds the schema definition for the InvoiceLineItem entity.
type InvoiceLineItem struct {
	ent.Schema
}

// Mixin of the InvoiceLineItem.
func (InvoiceLineItem) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
	}
}

// Fields of the InvoiceLineItem.
func (InvoiceLineItem) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			Immutable(),
		field.String("invoice_id").
			NotEmpty().
			Immutable(),
		field.String("customer_id").
			NotEmpty().
			Immutable(),
		field.String("subscription_id").
			Optional().
			Nillable(),
		field.String("price_id").
			NotEmpty().
			Immutable(),
		field.String("meter_id").
			Optional().
			Nillable(),
		field.Other("amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Default(decimal.Zero),
		field.Other("quantity", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Default(decimal.Zero),
		field.String("currency").
			NotEmpty().
			Immutable(),
		field.Time("period_start").
			Optional().
			Nillable(),
		field.Time("period_end").
			Optional().
			Nillable(),
		field.JSON("metadata", map[string]string{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Edges of the InvoiceLineItem.
func (InvoiceLineItem) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("invoice", Invoice.Type).
			Ref("line_items").
			Field("invoice_id").
			Unique().
			Required().
			Immutable(),
	}
}

// Indexes of the InvoiceLineItem.
func (InvoiceLineItem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "invoice_id", "status"),
		index.Fields("tenant_id", "customer_id", "status"),
		index.Fields("tenant_id", "subscription_id", "status"),
		index.Fields("tenant_id", "price_id", "status"),
		index.Fields("tenant_id", "meter_id", "status"),
		index.Fields("period_start", "period_end"),
	}
}
