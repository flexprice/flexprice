package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/shopspring/decimal"
)

// JournalLine holds the schema definition for the JournalLine entity. Rows are immutable.
type JournalLine struct {
	ent.Schema
}

// Mixin of the JournalLine.
func (JournalLine) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the JournalLine.
func (JournalLine) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		field.String("journal_entry_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			NotEmpty(),

		field.String("account").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			NotEmpty(),

		field.String("direction").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			Immutable().
			NotEmpty(),

		field.Other("amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Immutable(),

		field.String("pob_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Immutable(),
	}
}

// Indexes of the JournalLine.
func (JournalLine) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "journal_entry_id").
			StorageKey("idx_journal_line_entry"),
		index.Fields("tenant_id", "environment_id", "pob_id", "account").
			StorageKey("idx_journal_line_pob_account"),
	}
}
