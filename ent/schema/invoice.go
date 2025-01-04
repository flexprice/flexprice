package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Invoice holds the schema definition for the Invoice entity.
type Invoice struct {
	ent.Schema
}

// Mixin of the Invoice.
func (Invoice) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
	}
}

// Fields of the Invoice.
func (Invoice) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			Immutable(),
		field.String("customer_id").
			NotEmpty().
			Immutable(),
		field.String("subscription_id").
			Optional().
			Nillable(),
		field.String("invoice_type").
			NotEmpty().
			Immutable(),
		field.String("invoice_status").
			Default(string(types.InvoiceStatusDraft)),
		field.String("payment_status").
			Default(string(types.InvoicePaymentStatusPending)),
		field.String("currency").
			NotEmpty().
			Immutable(),
		field.Other("amount_due", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Default(decimal.Zero),
		field.Other("amount_paid", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Default(decimal.Zero),
		field.Other("amount_remaining", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Default(decimal.Zero),
		field.String("description").
			Optional(),
		field.Time("due_date").
			Optional().
			Nillable(),
		field.Time("paid_at").
			Optional().
			Nillable(),
		field.Time("voided_at").
			Optional().
			Nillable(),
		field.Time("finalized_at").
			Optional().
			Nillable(),
		field.Time("period_start").
			Optional().
			Nillable().
			Immutable(),
		field.Time("period_end").
			Optional().
			Nillable().
			Immutable(),
		field.String("invoice_pdf_url").
			Optional().
			Nillable(),
		field.String("billing_reason").
			Optional(),
		field.JSON("metadata", map[string]string{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
		field.Int("version").
			Default(1),
	}
}

// Edges of the Invoice.
func (Invoice) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("line_items", InvoiceLineItem.Type),
	}
}

// Indexes of the Invoice.
func (Invoice) Indexes() []ent.Index {
	return []ent.Index{
		// Common query patterns
		index.Fields("tenant_id", "customer_id", "invoice_status", "payment_status", "status"),
		index.Fields("tenant_id", "subscription_id", "invoice_status", "payment_status", "status"),
		index.Fields("tenant_id", "invoice_type", "invoice_status", "payment_status", "status"),
		index.Fields("tenant_id", "due_date", "invoice_status", "payment_status", "status"),
		// Period based queries
		index.Fields("period_start", "period_end"),
	}
}
