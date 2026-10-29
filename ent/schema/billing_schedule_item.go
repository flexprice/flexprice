package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// BillingScheduleItem holds one charge and its original source/invoice links.
type BillingScheduleItem struct {
	ent.Schema
}

// Mixin provides scoped source visibility and audit fields.
func (BillingScheduleItem) Mixin() []ent.Mixin {
	return []ent.Mixin{baseMixin.BaseMixin{}, baseMixin.EnvironmentMixin{}}
}

// Fields defines principal, delivery eligibility and execution history.
func (BillingScheduleItem) Fields() []ent.Field {
	varchar := func(n string) map[string]string { return map[string]string{"postgres": "varchar(" + n + ")"} }
	return []ent.Field{
		field.String("id").SchemaType(varchar("50")).Unique().Immutable(),
		field.String("owner_type").SchemaType(varchar("50")).NotEmpty().Immutable().GoType(types.BillingTermOwnerType("")),
		field.String("owner_id").SchemaType(varchar("50")).NotEmpty().Immutable(),
		field.String("billing_term_id").SchemaType(varchar("50")).Optional().Nillable().Immutable(),
		field.String("customer_id").SchemaType(varchar("50")).NotEmpty().Immutable(),
		field.String("subscription_id").SchemaType(varchar("50")).Optional().Nillable().Immutable(),
		field.String("currency").SchemaType(varchar("10")).NotEmpty().Immutable(),
		field.String("kind").SchemaType(varchar("20")).Default(string(types.BillingScheduleCharge)).Immutable().GoType(types.BillingScheduleItemKind("")),
		field.String("bill_trigger").SchemaType(varchar("20")).NotEmpty().Immutable().GoType(types.BillingScheduleTrigger("")),
		field.Time("bill_on").Optional().Nillable(),
		field.Other("amount", decimal.Decimal{}).SchemaType(map[string]string{"postgres": "numeric(20,8)"}).Immutable(),
		field.String("description").SchemaType(varchar("255")).NotEmpty().Immutable(),
		field.Time("service_period_start").Immutable(),
		field.Time("service_period_end").Immutable(),
		field.String("delivery").SchemaType(varchar("30")).NotEmpty().Immutable().GoType(types.BillingTermDelivery("")),
		field.String("schedule_status").SchemaType(varchar("20")).Default(string(types.BillingScheduleScheduled)).GoType(types.BillingScheduleStatus("")),
		field.String("invoice_id").SchemaType(varchar("50")).Optional().Nillable(),
		field.String("invoice_line_item_id").SchemaType(varchar("50")).Optional().Nillable(),
		field.String("cancellation_reason").SchemaType(varchar("30")).Optional().Nillable().GoType(types.BillingScheduleCancellationReason("")),
		field.Other("metadata", types.Metadata{}).SchemaType(map[string]string{"postgres": "jsonb"}).Optional().Default(types.Metadata{}).Immutable(),
	}
}

// Edges keeps the exact rule source available after archival.
func (BillingScheduleItem) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("billing_term", BillingTerm.Type).Ref("schedule_items").Field("billing_term_id").Unique().Immutable(),
		edge.To("source_line", InvoiceLineItem.Type).Unique().Annotations(entsql.OnDelete(entsql.Restrict)),
	}
}

// Indexes bounds due pickup and archived-inclusive source lookups.
func (BillingScheduleItem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "owner_type", "owner_id", "status"),
		index.Fields("tenant_id", "environment_id", "status", "schedule_status", "delivery", "bill_on", "id"),
		index.Fields("tenant_id", "environment_id", "billing_term_id"),
		index.Fields("tenant_id", "environment_id", "invoice_id"),
		index.Fields("tenant_id", "environment_id", "owner_type", "owner_id", "created_at", "id"),
		index.Fields("tenant_id", "environment_id", "invoice_line_item_id").Unique().Annotations(entsql.IndexWhere("(invoice_line_item_id IS NOT NULL)")),
	}
}

// Annotations protects principal and source/claim state combinations.
func (BillingScheduleItem) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Checks: map[string]string{
		"billing_schedule_items_amount":         "amount > 0",
		"billing_schedule_items_service_period": "service_period_start < service_period_end",
		"billing_schedule_items_kind":           "kind IN ('charge', 'settlement') AND (kind = 'settlement' OR billing_term_id IS NOT NULL)",
		"billing_schedule_items_trigger":        "bill_trigger IN ('on_date', 'manual') AND (bill_trigger = 'manual' OR bill_on IS NOT NULL)",
		"billing_schedule_items_delivery":       "delivery IN ('subscription_invoice', 'separate_invoice') AND (delivery != 'subscription_invoice' OR subscription_id IS NOT NULL)",
		"billing_schedule_items_state":          "schedule_status IN ('scheduled', 'invoiced', 'canceled') AND ((invoice_id IS NULL AND invoice_line_item_id IS NULL) OR (invoice_id IS NOT NULL AND invoice_line_item_id IS NOT NULL)) AND (schedule_status != 'scheduled' OR invoice_id IS NULL) AND (schedule_status != 'invoiced' OR invoice_id IS NOT NULL)",
		"billing_schedule_items_cancellation":   "(schedule_status = 'canceled' AND cancellation_reason IS NOT NULL AND cancellation_reason IN ('terms_amended', 'invoice_voided', 'termination')) OR (schedule_status != 'canceled' AND cancellation_reason IS NULL)",
	}}}
}
