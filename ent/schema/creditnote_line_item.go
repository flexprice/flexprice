package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/shopspring/decimal"
)

const (
	Idx_tenant_environment_credit_note_id_invoice_line_item_id_unique = "idx_tenant_environment_credit_note_id_invoice_line_item_id_unique"
)

// CreditNoteLineItem holds the schema definition for the CreditNoteLineItem entity.
type CreditNoteLineItem struct {
	ent.Schema
}

func (CreditNoteLineItem) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (CreditNoteLineItem) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		field.String("credit_note_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		field.String("invoice_line_item_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		field.String("display_name").
			NotEmpty(),

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
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			NotEmpty(),

		field.JSON("metadata", map[string]string{}).
			Optional(),
	}
}

// Edges of the CreditNoteLineItem.
func (CreditNoteLineItem) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("credit_note", CreditNote.Type).
			Ref("line_items").
			Field("credit_note_id").
			Unique().
			Required().
			Immutable(),
	}
}
