package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
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
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("customer_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("subscription_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),
		field.String("invoice_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("invoice_status").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default(string(types.InvoiceStatusDraft)),
		field.String("payment_status").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default(string(types.PaymentStatusPending)),
		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
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
		field.String("billing_period").
			Optional().
			Nillable().
			Immutable(),
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
		field.String("invoice_number").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Comment("Generated invoice number unique per tenant"),
		field.Int("billing_sequence").
			SchemaType(map[string]string{
				"postgres": "integer",
			}).
			Optional().
			Nillable().
			Comment("Sequence number for subscription billing periods"),
		field.String("idempotency_key").
			SchemaType(map[string]string{
				"postgres": "varchar(100)",
			}).
			Optional().
			Nillable().
			Comment("Key for ensuring idempotent invoice creation"),
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
		index.Fields("tenant_id", "customer_id", "invoice_status", "payment_status", "status").
			StorageKey("idx_tenant_customer_status"),
		index.Fields("tenant_id", "subscription_id", "invoice_status", "payment_status", "status").
			StorageKey("idx_tenant_subscription_status"),
		index.Fields("tenant_id", "invoice_type", "invoice_status", "payment_status", "status").
			StorageKey("idx_tenant_type_status"),
		index.Fields("tenant_id", "due_date", "invoice_status", "payment_status", "status").
			StorageKey("idx_tenant_due_date_status"),
		index.Fields("tenant_id", "invoice_number").
			Unique().
			StorageKey("idx_tenant_invoice_number_unique"),
		index.Fields("idempotency_key").
			Unique().
			StorageKey("idx_idempotency_key_unique").
			Annotations(entsql.IndexWhere("idempotency_key IS NOT NULL")),
		index.Fields("subscription_id", "period_start", "period_end").
			StorageKey("idx_subscription_period_unique").
			Annotations(entsql.IndexWhere("invoice_status != 'VOIDED' AND subscription_id IS NOT NULL")),
	}
}
