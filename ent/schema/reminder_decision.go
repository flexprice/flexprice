package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// ReminderDecision holds what a person decided or edited on the reminder of one customer's overdue invoices in one currency.
// Undecided, unedited reminders have no row; they are rendered when read.
type ReminderDecision struct {
	ent.Schema
}

const ReminderDecisionActiveUniqueConstraint = "idx_reminder_decision_tenant_env_customer_currency"

func (ReminderDecision) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (ReminderDecision) Fields() []ent.Field {
	varchar := func(n string) map[string]string { return map[string]string{"postgres": "varchar(" + n + ")"} }
	text := map[string]string{"postgres": "text"}

	return []ent.Field{
		field.String("id").
			SchemaType(varchar("50")).
			Unique().
			Immutable(),
		field.String("customer_id").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable(),
		field.String("currency").
			SchemaType(varchar("10")).
			NotEmpty().
			Immutable(),
		field.String("draft_status").
			SchemaType(varchar("20")).
			Default("pending"),
		field.String("hold_reason").
			SchemaType(text).
			Optional(),
		field.Time("decided_at").
			Optional().
			Nillable(),
		field.String("tone_override").
			SchemaType(varchar("20")).
			Optional(),
		field.String("subject_override").
			SchemaType(text).
			Optional(),
		field.String("body_override").
			SchemaType(text).
			Optional(),
		field.JSON("invoice_ids", []string{}).
			SchemaType(map[string]string{"postgres": "jsonb"}).
			Default([]string{}),
		field.String("invoice_set_hash").
			SchemaType(varchar("64")),
		field.String("frozen_subject").
			SchemaType(text).
			Optional(),
		field.String("frozen_body").
			SchemaType(text).
			Optional(),
		field.String("frozen_tone").
			SchemaType(varchar("20")).
			Optional(),
		field.Int("version").
			Default(1),
	}
}

func (ReminderDecision) Edges() []ent.Edge {
	return nil
}

func (ReminderDecision) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "customer_id", "currency").
			Unique().
			StorageKey(ReminderDecisionActiveUniqueConstraint).
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text)")),
	}
}
