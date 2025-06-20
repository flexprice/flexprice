package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	Idx_tenant_environment_credit_note_number_unique = "idx_tenant_environment_credit_note_number_unique"
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

		field.String("credit_note_number").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
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
			NotEmpty(),
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
			Optional().
			Nillable(),

		field.String("memo").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
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
		field.JSON("metadata", map[string]string{}).
			Optional(),
	}
}

func (CreditNote) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "credit_note_number").
			Unique().
			StorageKey(Idx_tenant_environment_credit_note_number_unique).
			Annotations(entsql.IndexWhere("credit_note_number IS NOT NULL AND credit_note_number != '' AND status = 'published'")),
	}
}

// Edges of the Invoice.
func (CreditNote) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("line_items", CreditNoteLineItem.Type),
	}
}
