package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// InvoiceReminder holds what a person did with the payment reminder of one overdue invoice: edits, a chosen tone or a decision.
// Reminders nobody acted on have no row; they are rendered when read.
type InvoiceReminder struct {
	ent.Schema
}

const InvoiceReminderActiveUniqueConstraint = "idx_invoice_reminder_tenant_env_invoice"

func (InvoiceReminder) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (InvoiceReminder) Fields() []ent.Field {
	varchar := func(n string) map[string]string { return map[string]string{"postgres": "varchar(" + n + ")"} }
	text := map[string]string{"postgres": "text"}

	return []ent.Field{
		field.String("id").
			SchemaType(varchar("50")).
			Unique().
			Immutable(),
		field.String("invoice_id").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable(),
		field.String("reminder_status").
			GoType(types.ReminderStatus("")).
			SchemaType(varchar("20")).
			Default(string(types.ReminderStatusPending)),
		field.String("status_reason").
			SchemaType(text).
			Optional(),
		field.Time("approved_at").
			Optional().
			Nillable(),
		field.JSON("metadata", map[string]interface{}{}).
			Optional().
			SchemaType(map[string]string{"postgres": "jsonb"}),
		field.String("tone").
			GoType(types.ReminderTone("")).
			SchemaType(varchar("20")),
		field.String("subject").
			SchemaType(text),
		field.String("body").
			SchemaType(text),
		// playbook_id and playbook_step_id name the playbook step that last wrote this reminder.
		field.String("playbook_id").
			SchemaType(varchar("50")).
			Optional(),
		field.String("playbook_step_id").
			SchemaType(varchar("50")).
			Optional(),
	}
}

func (InvoiceReminder) Edges() []ent.Edge {
	return nil
}

func (InvoiceReminder) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "invoice_id").
			Unique().
			StorageKey(InvoiceReminderActiveUniqueConstraint).
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text)")),
	}
}
