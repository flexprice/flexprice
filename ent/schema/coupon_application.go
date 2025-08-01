package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/shopspring/decimal"
)

// CouponApplication holds the schema definition for the CouponApplication entity.
type CouponApplication struct {
	ent.Schema
}

// Mixin of the CouponApplication.
func (CouponApplication) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the CouponApplication.
func (CouponApplication) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("coupon_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("coupon_association_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Immutable(),
		field.String("invoice_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("invoice_line_item_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),
		field.Time("applied_at").
			Default(time.Now).
			Immutable(),
		field.Other("original_price", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}),
		field.Other("final_price", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}),
		field.Other("discounted_amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}),
		field.String("discount_type").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty().
			Comment("Type of discount applied: fixed or percentage"),
		field.Other("discount_percentage", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(7,4)",
			}).
			Optional().
			Nillable().
			Comment("Percentage value, only for percentage discounts"),
		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			Optional().
			Nillable(),
		field.JSON("coupon_snapshot", map[string]interface{}{}).
			Optional().
			Comment("Frozen coupon configuration at time of application"),
		field.JSON("metadata", map[string]string{}).
			Optional().
			Comment("Additional metadata for coupon application"),
		field.String("subscription_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Comment("Subscription ID this coupon application is associated with"),
	}
}

// Edges of the CouponApplication.
func (CouponApplication) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("coupon", Coupon.Type).
			Ref("coupon_applications").
			Field("coupon_id").
			Unique().
			Immutable().
			Required(),
		edge.From("coupon_association", CouponAssociation.Type).
			Ref("coupon_applications"),
		edge.From("invoice", Invoice.Type).
			Ref("coupon_applications").
			Field("invoice_id").
			Unique().
			Immutable().
			Required(),
		edge.From("invoice_line_item", InvoiceLineItem.Type).
			Ref("coupon_applications").
			Field("invoice_line_item_id").
			Unique(),
		edge.From("subscription", Subscription.Type).
			Ref("coupon_applications").
			Field("subscription_id").
			Unique(),
	}
}

// Indexes of the CouponApplication.
func (CouponApplication) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id"),
		index.Fields("tenant_id", "environment_id", "coupon_id"),
		index.Fields("tenant_id", "environment_id", "invoice_id"),
		index.Fields("tenant_id", "environment_id", "invoice_id", "invoice_line_item_id"),
		index.Fields("tenant_id", "environment_id", "subscription_id"),
		// Optimized index for subscription + coupon queries (used in cadence validation)
		index.Fields("tenant_id", "environment_id", "subscription_id", "coupon_id"),
	}
}
