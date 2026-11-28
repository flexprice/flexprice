package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// AccountingPeriod holds the schema definition for the AccountingPeriod entity.
type AccountingPeriod struct {
	ent.Schema
}

// Mixin of the AccountingPeriod.
func (AccountingPeriod) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the AccountingPeriod.
func (AccountingPeriod) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		// 1st of the month; the period runs to the next month start.
		field.Time("period_start").
			SchemaType(map[string]string{
				"postgres": "date",
			}).
			Immutable(),

		field.String("period_status").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty(),
	}
}

// Indexes of the AccountingPeriod.
func (AccountingPeriod) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "period_start").
			Unique().
			StorageKey("idx_accounting_period_unique").
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text)")),
	}
}
