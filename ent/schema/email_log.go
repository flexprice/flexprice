package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// Idx_email_log_auto_once is exported so the repository can match pq.Error.Constraint
// when a second automatic email is inserted for the same entity and template.
const Idx_email_log_auto_once = "idx_email_log_auto_once"

// EmailLog holds the schema definition for one email sent to a customer.
type EmailLog struct {
	ent.Schema
}

func (EmailLog) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (EmailLog) Fields() []ent.Field {
	varchar := func(n string) map[string]string { return map[string]string{"postgres": "varchar(" + n + ")"} }

	return []ent.Field{
		field.String("id").
			SchemaType(varchar("50")).
			Unique().
			Immutable(),

		field.String("entity_type").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable().
			GoType(types.SystemEntityType("")),

		field.String("entity_id").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable(),

		field.String("customer_id").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable(),

		field.String("template_key").
			SchemaType(varchar("100")).
			NotEmpty().
			Immutable().
			GoType(types.EmailTemplateKey("")),

		field.String("trigger").
			SchemaType(varchar("20")).
			NotEmpty().
			Immutable().
			GoType(types.EmailTrigger("")),

		field.String("from_address").
			SchemaType(varchar("320")).
			NotEmpty().
			Immutable(),

		field.String("reply_to").
			SchemaType(varchar("320")).
			NotEmpty().
			Immutable(),

		field.Strings("to_addresses").
			SchemaType(map[string]string{"postgres": "jsonb"}).
			Immutable(),

		field.Strings("cc_addresses").
			SchemaType(map[string]string{"postgres": "jsonb"}).
			Optional().
			Immutable().
			Default([]string{}),

		field.Text("subject").
			Immutable(),

		field.String("provider").
			SchemaType(varchar("20")).
			NotEmpty().
			Immutable().
			GoType(types.EmailProvider("")),

		field.String("provider_message_id").
			SchemaType(varchar("100")).
			Optional().
			Nillable(),

		field.String("delivery_status").
			SchemaType(varchar("30")).
			NotEmpty().
			GoType(types.EmailDeliveryStatus("")),

		field.Text("status_reason").
			Optional().
			Nillable(),

		field.Time("sent_at").
			SchemaType(map[string]string{"postgres": "timestamptz"}).
			Optional().
			Nillable(),

		field.Time("delivered_at").
			SchemaType(map[string]string{"postgres": "timestamptz"}).
			Optional().
			Nillable(),

		field.Other("metadata", types.Metadata{}).
			SchemaType(map[string]string{"postgres": "jsonb"}).
			Optional().
			Default(types.Metadata{}),
	}
}

func (EmailLog) Indexes() []ent.Index {
	return []ent.Index{
		// Delivery webhooks look emails up by the provider's id.
		index.Fields("provider", "provider_message_id").
			Unique().
			Annotations(entsql.IndexWhere("provider_message_id IS NOT NULL")),
		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id", "created_at").
			StorageKey("idx_email_log_entity_created_at").
			Annotations(entsql.DescColumns("created_at")),
		// At most one automatic email per entity and template, even if the trigger is redelivered.
		index.Fields("tenant_id", "environment_id", "entity_id", "template_key").
			Unique().
			StorageKey(Idx_email_log_auto_once).
			Annotations(entsql.IndexWhere("\"trigger\" = 'auto'")),
		index.Fields("tenant_id", "environment_id", "delivery_status", "created_at"),
		// The email dispatcher finds environments with queued emails without scanning every email sent.
		index.Fields("tenant_id", "environment_id", "created_at").
			StorageKey("idx_email_log_queued").
			Annotations(entsql.IndexWhere("delivery_status = 'queued'")),
	}
}
