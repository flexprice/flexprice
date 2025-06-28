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

const (
	Idx_tenant_environment_credit_note_number_unique = "idx_tenant_environment_credit_note_number_unique"
	Idx_tenant_environment_subscription_id_unique    = "idx_tenant_environment_subscription_id_unique"
)

// CreditNote holds the schema definition for the CreditNote entity.
type CreditNote struct {
	ent.Schema
}

// Mixin of the CreditNote entity.
func (CreditNote) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the CreditNote entity.
func (CreditNote) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		field.String("invoice_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
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

		field.String("credit_note_number").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable(),

		field.String("credit_note_status").
			GoType(types.CreditNoteStatus("")).
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default(string(types.CreditNoteStatusDraft)),

		field.String("credit_note_type").
			GoType(types.CreditNoteType("")).
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		field.String("refund_status").
			GoType(types.PaymentStatus("")).
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),

		field.String("reason").
			GoType(types.CreditNoteReason("")).
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		field.String("memo").
			SchemaType(map[string]string{
				"postgres": "text",
			}).
			Immutable(),

		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable(),

		field.String("idempotency_key").
			SchemaType(map[string]string{
				"postgres": "varchar(100)",
			}).
			Immutable().
			Optional().
			Nillable(),

		field.Time("voided_at").
			Optional().
			Nillable(),

		field.Time("finalized_at").
			Optional().
			Nillable(),

		field.JSON("metadata", map[string]string{}).
			Optional(),

		field.Other("total_amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Default(decimal.Zero).
			Immutable(),
	}
}

func (CreditNote) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "credit_note_number").
			Unique().
			StorageKey(Idx_tenant_environment_credit_note_number_unique).
			Annotations(entsql.IndexWhere("credit_note_number IS NOT NULL AND credit_note_number != '' AND status = 'published'")),

		index.Fields("tenant_id", "environment_id", "idempotency_key").
			Annotations(entsql.IndexWhere("idempotency_key IS NOT NULL AND idempotency_key != ''")),

		index.Fields("tenant_id", "environment_id", "invoice_id"),

		index.Fields("tenant_id", "environment_id", "credit_note_status"),

		index.Fields("tenant_id", "environment_id", "credit_note_type"),

		index.Fields("tenant_id", "environment_id", "customer_id"),

		index.Fields("tenant_id", "environment_id", "subscription_id"),
	}
}

// Edges of the CreditNote.
func (CreditNote) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("line_items", CreditNoteLineItem.Type),
	}
}
