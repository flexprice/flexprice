package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// JournalEntry holds the schema definition for the JournalEntry entity. Rows are immutable.
type JournalEntry struct {
	ent.Schema
}

const (
	JournalEntryIdempotencyKeyConstraint = "idx_journal_entry_idempotency_key"
)

// Mixin of the JournalEntry.
func (JournalEntry) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the JournalEntry.
func (JournalEntry) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		field.String("event_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			NotEmpty(),

		field.String("source_entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			NotEmpty(),

		field.String("source_entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			NotEmpty(),

		// {event_type}:{source_entity_id}; billing only
		field.String("idempotency_key").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional().
			Nillable().
			Immutable(),

		field.String("accounting_period_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			NotEmpty(),

		// local date of the activity
		field.Time("business_date").
			SchemaType(map[string]string{
				"postgres": "date",
			}).
			Immutable(),

		field.String("customer_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			NotEmpty(),

		field.String("contract_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Immutable(),

		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			Immutable().
			NotEmpty(),

		field.String("reversal_of").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Immutable(),
	}
}

// Indexes of the JournalEntry.
func (JournalEntry) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "idempotency_key").
			Unique().
			StorageKey(JournalEntryIdempotencyKeyConstraint).
			Annotations(entsql.IndexWhere("(idempotency_key IS NOT NULL)")),
		index.Fields("tenant_id", "environment_id", "source_entity_type", "source_entity_id").
			StorageKey("idx_journal_entry_source"),
		index.Fields("tenant_id", "environment_id", "accounting_period_id").
			StorageKey("idx_journal_entry_period"),
		index.Fields("tenant_id", "environment_id", "customer_id").
			StorageKey("idx_journal_entry_customer"),
	}
}
