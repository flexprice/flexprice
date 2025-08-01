package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/shopspring/decimal"
)

// InvoiceLineItem holds the schema definition for the InvoiceLineItem entity.
type InvoiceLineItem struct {
	ent.Schema
}

// Mixin of the InvoiceLineItem.
func (InvoiceLineItem) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the InvoiceLineItem.
func (InvoiceLineItem) Fields() []ent.Field {
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
			Nillable().
			Immutable(),
		field.String("plan_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Immutable(),
		field.String("plan_display_name").
			Optional().
			Nillable().
			Immutable(),
		field.String("price_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Immutable(),
		field.String("price_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Immutable(),
		field.String("meter_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Immutable(),
		field.String("meter_display_name").
			Optional().
			Nillable().
			Immutable(),
		field.String("display_name").
			Optional().
			Nillable().
			Immutable(),
		field.Other("amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Default(decimal.Zero).
			Immutable(),
		field.Other("quantity", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Default(decimal.Zero).
			Immutable(),
		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			NotEmpty().
			Immutable(),
		field.Time("period_start").
			Optional().
			Nillable(),
		field.Time("period_end").
			Optional().
			Nillable(),
		field.JSON("metadata", map[string]string{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Edges of the InvoiceLineItem.
func (InvoiceLineItem) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("invoice", Invoice.Type).
			Ref("line_items").
			Field("invoice_id").
			Unique().
			Required().
			Immutable(),
		edge.To("coupon_applications", CouponApplication.Type).
			Comment("Invoice line item can have multiple coupon applications"),
	}
}

// Indexes of the InvoiceLineItem.
func (InvoiceLineItem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "invoice_id", "status"),
		index.Fields("tenant_id", "environment_id", "customer_id", "status"),
		index.Fields("tenant_id", "environment_id", "subscription_id", "status"),
		index.Fields("tenant_id", "environment_id", "price_id", "status"),
		index.Fields("tenant_id", "environment_id", "meter_id", "status"),
		index.Fields("period_start", "period_end"),
	}
}
